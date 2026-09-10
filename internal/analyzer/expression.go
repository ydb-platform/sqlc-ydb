package analyzer

import (
	"fmt"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	"github.com/ydb-platform/sqlc-ydb/internal/yql/builtins"
	parser "github.com/ydb-platform/yql-parsers/go"
)

type expressionScope struct {
	relations []relation
	bindings  map[string]model.Type
	grouped   bool
	predicate bool
}

func resolveExpression(expr parser.IExprContext, scope expressionScope) (model.Type, error) {
	if expr == nil {
		return model.Type{}, fmt.Errorf("missing expression")
	}
	if literal, ok, err := literalType(expr); ok || err != nil {
		return literal, err
	}
	if bind := directBind(expr); bind != nil {
		name := bindName(bind)
		typeValue, ok := scope.bindings[name]
		if !ok {
			return model.Type{}, fmt.Errorf("cannot resolve type of parameter $%s in expression", name)
		}
		return typeValue, nil
	}
	if refs := columnRefs(expr); len(refs) == 1 && isPureColumnExpression(expr) {
		column, err := resolveColumn(scope.relations, refs[0])
		if err != nil {
			return model.Type{}, err
		}
		return column.Type, nil
	}
	if cast := coveringCast(expr); cast != nil {
		return resolveCast(cast, scope)
	}
	if caseExpr := coveringCase(expr); caseExpr != nil {
		return resolveCase(caseExpr, scope)
	}
	if name, invoke, ok := directFunctionCall(expr); ok {
		return resolveFunction(name, invoke, scope)
	}
	if typeValue, ok, err := concatenationType(expr, scope.bindings); ok {
		return typeValue, err
	}
	if scope.predicate {
		if typeValue, ok, err := resolveComparison(expr, scope); ok {
			return typeValue, err
		}
	}
	if len(columnRefs(expr)) != 0 {
		return model.Type{}, fmt.Errorf("computed result expression %q is not supported", expr.GetText())
	}
	return model.Type{}, fmt.Errorf("unsupported result expression %q", expr.GetText())
}

func resolveComparison(expr parser.IExprContext, scope expressionScope) (model.Type, bool, error) {
	var operands []antlr.ParserRuleContext
	descendants(expr, func(node antlr.Tree) {
		ctx, ok := node.(*parser.Eq_subexprContext)
		if !ok || !sameSpan(expr, ctx) || len(ctx.AllNeq_subexpr()) < 2 {
			return
		}
		for _, operand := range ctx.AllNeq_subexpr() {
			operands = append(operands, operand)
		}
	})
	if len(operands) == 0 {
		var xor *parser.Xor_subexprContext
		descendants(expr, func(node antlr.Tree) {
			ctx, ok := node.(*parser.Xor_subexprContext)
			if ok && sameSpan(expr, ctx) && ctx.Cond_expr() != nil {
				xor = ctx
			}
		})
		if xor == nil {
			return model.Type{}, false, nil
		}
		condition := xor.Cond_expr()
		if condition.ISNULL() != nil || condition.NOTNULL() != nil || condition.NULL() != nil {
			if _, err := resolveScalarNode(xor.Eq_subexpr(), scope); err != nil {
				return model.Type{}, true, fmt.Errorf("cannot resolve null-check operand: %w", err)
			}
			return model.Type{Kind: "Bool"}, true, nil
		}
		if condition.IN() != nil {
			return model.Type{}, true, fmt.Errorf("typed IN predicates are not yet supported in CASE, IF, or HAVING")
		}
		operands = append(operands, xor.Eq_subexpr())
		for _, operand := range condition.AllEq_subexpr() {
			operands = append(operands, operand)
		}
		if len(operands) < 2 {
			return model.Type{}, false, nil
		}
	}
	return comparisonBoolType(operands, scope)
}

func comparisonBoolType(operands []antlr.ParserRuleContext, scope expressionScope) (model.Type, bool, error) {
	types := make([]model.Type, 0, len(operands))
	for _, operand := range operands {
		typeValue, err := resolveScalarNode(operand, scope)
		if err != nil {
			return model.Type{}, true, fmt.Errorf("cannot resolve comparison operand %q: %w", operand.GetText(), err)
		}
		types = append(types, typeValue)
	}
	common, err := builtins.CommonType(types...)
	if err != nil {
		return model.Type{}, true, fmt.Errorf("comparison operands have incompatible types: %w", err)
	}
	result := model.Type{Kind: "Bool"}
	if common.IsOptional() {
		result = model.Optional(result)
	}
	return result, true, nil
}

func resolveScalarNode(root antlr.ParserRuleContext, scope expressionScope) (model.Type, error) {
	var literal parser.ILiteral_valueContext
	descendants(root, func(node antlr.Tree) {
		ctx, ok := node.(parser.ILiteral_valueContext)
		if ok && sameSpan(root, ctx) {
			literal = ctx
		}
	})
	if literal != nil {
		return literalValueType(literal)
	}
	if bind := directBind(root); bind != nil {
		name := bindName(bind)
		typeValue, ok := scope.bindings[name]
		if !ok {
			return model.Type{}, fmt.Errorf("cannot resolve type of parameter $%s", name)
		}
		return typeValue, nil
	}
	refs := columnRefs(root)
	if len(refs) == 1 && sameSpan(root, refs[0].ctx) {
		column, err := resolveColumn(scope.relations, refs[0])
		if err != nil {
			return model.Type{}, err
		}
		return column.Type, nil
	}
	var cast *parser.Cast_exprContext
	var caseExpr *parser.Case_exprContext
	var functionUnary *parser.Unary_subexprContext
	descendants(root, func(node antlr.Tree) {
		switch ctx := node.(type) {
		case *parser.Cast_exprContext:
			if sameSpan(root, ctx) {
				cast = ctx
			}
		case *parser.Case_exprContext:
			if sameSpan(root, ctx) {
				caseExpr = ctx
			}
		case *parser.Unary_subexprContext:
			if sameSpan(root, ctx) {
				functionUnary = ctx
			}
		}
	})
	if cast != nil {
		return resolveCast(cast, scope)
	}
	if caseExpr != nil {
		return resolveCase(caseExpr, scope)
	}
	if functionUnary != nil {
		if name, invoke, ok := functionCallFromUnary(functionUnary); ok {
			return resolveFunction(name, invoke, scope)
		}
	}
	return model.Type{}, fmt.Errorf("unsupported scalar expression %q", root.GetText())
}

func directBind(root antlr.ParserRuleContext) parser.IBind_parameterContext {
	var binds []parser.IBind_parameterContext
	descendants(root, func(node antlr.Tree) {
		if bind, ok := node.(parser.IBind_parameterContext); ok {
			binds = append(binds, bind)
		}
	})
	if len(binds) == 1 && root.GetText() == binds[0].GetText() {
		return binds[0]
	}
	return nil
}

func coveringCast(expr parser.IExprContext) *parser.Cast_exprContext {
	var result *parser.Cast_exprContext
	descendants(expr, func(node antlr.Tree) {
		ctx, ok := node.(*parser.Cast_exprContext)
		if ok && sameSpan(expr, ctx) {
			result = ctx
		}
	})
	return result
}

func coveringCase(expr parser.IExprContext) *parser.Case_exprContext {
	var result *parser.Case_exprContext
	descendants(expr, func(node antlr.Tree) {
		ctx, ok := node.(*parser.Case_exprContext)
		if ok && sameSpan(expr, ctx) {
			result = ctx
		}
	})
	return result
}

func sameSpan(left, right antlr.ParserRuleContext) bool {
	return left.GetStart() != nil && left.GetStop() != nil && right.GetStart() != nil && right.GetStop() != nil &&
		left.GetStart().GetStart() == right.GetStart().GetStart() && left.GetStop().GetStop() == right.GetStop().GetStop()
}

func resolveCast(cast *parser.Cast_exprContext, scope expressionScope) (model.Type, error) {
	if cast.Expr() == nil || cast.Type_name_or_bind() == nil {
		return model.Type{}, fmt.Errorf("invalid CAST expression")
	}
	if cast.Type_name_or_bind().Bind_parameter() != nil {
		return model.Type{}, fmt.Errorf("parameterized CAST target types are unsupported")
	}
	source, err := resolveExpression(cast.Expr(), scope)
	if err != nil {
		return model.Type{}, fmt.Errorf("cannot resolve CAST input: %w", err)
	}
	target, err := parseType(cast.Type_name_or_bind().GetText())
	if err != nil {
		return model.Type{}, err
	}
	return builtins.Cast(source, target)
}

func resolveCase(caseExpr *parser.Case_exprContext, scope expressionScope) (model.Type, error) {
	whenExprs := caseExpr.AllWhen_expr()
	if len(whenExprs) == 0 {
		return model.Type{}, fmt.Errorf("CASE requires at least one WHEN branch")
	}
	directExprs := caseExpr.AllExpr()
	var selector parser.IExprContext
	if len(directExprs) != 0 && directExprs[0].GetStart().GetStart() < whenExprs[0].GetStart().GetStart() {
		selector = directExprs[0]
	}
	if caseExpr.ELSE() == nil {
		return model.Type{}, fmt.Errorf("CASE requires an ELSE branch")
	}
	var branchTypes []model.Type
	for _, when := range whenExprs {
		parts := when.AllExpr()
		if len(parts) != 2 {
			return model.Type{}, fmt.Errorf("invalid CASE WHEN branch")
		}
		conditionScope := scope
		conditionScope.predicate = true
		conditionType, err := resolveExpression(parts[0], conditionScope)
		if err != nil {
			return model.Type{}, fmt.Errorf("cannot resolve CASE condition: %w", err)
		}
		if selector == nil {
			base := conditionType.UnwrapOptional()
			if base.Kind != "Bool" && conditionType.Kind != "Null" {
				return model.Type{}, fmt.Errorf("CASE WHEN condition has type %s, want Bool", conditionType.String())
			}
		} else {
			selectorType, err := resolveExpression(selector, scope)
			if err != nil {
				return model.Type{}, fmt.Errorf("cannot resolve CASE selector: %w", err)
			}
			if _, err := builtins.CommonType(selectorType, conditionType); err != nil {
				return model.Type{}, fmt.Errorf("CASE selector and WHEN value have incompatible types %s and %s: %w", selectorType.String(), conditionType.String(), err)
			}
		}
		branchType, err := resolveExpression(parts[1], scope)
		if err != nil {
			return model.Type{}, fmt.Errorf("cannot resolve CASE result: %w", err)
		}
		branchTypes = append(branchTypes, branchType)
	}
	var elseExpr parser.IExprContext
	elseStart := caseExpr.ELSE().GetSymbol().GetStart()
	for _, expr := range directExprs {
		if expr.GetStart().GetStart() > elseStart {
			elseExpr = expr
			break
		}
	}
	if elseExpr == nil {
		return model.Type{}, fmt.Errorf("CASE requires an ELSE expression")
	}
	elseType, err := resolveExpression(elseExpr, scope)
	if err != nil {
		return model.Type{}, fmt.Errorf("cannot resolve CASE ELSE result: %w", err)
	}
	branchTypes = append(branchTypes, elseType)
	result, err := builtins.CommonType(branchTypes...)
	if err != nil {
		return model.Type{}, fmt.Errorf("CASE branches have incompatible types: %w", err)
	}
	return result, nil
}

func directFunctionCall(expr parser.IExprContext) (string, *parser.Invoke_exprContext, bool) {
	var unary *parser.Unary_subexprContext
	descendants(expr, func(node antlr.Tree) {
		ctx, ok := node.(*parser.Unary_subexprContext)
		if ok && sameSpan(expr, ctx) {
			unary = ctx
		}
	})
	if unary == nil || unary.Unary_casual_subexpr() == nil {
		return "", nil, false
	}
	return functionCallFromUnary(unary)
}

func functionCallFromUnary(unary *parser.Unary_subexprContext) (string, *parser.Invoke_exprContext, bool) {
	casual := unary.Unary_casual_subexpr()
	if casual == nil {
		return "", nil, false
	}
	suffix := casual.Unary_subexpr_suffix()
	if suffix == nil || len(suffix.AllInvoke_expr()) != 1 {
		return "", nil, false
	}
	invoke, ok := suffix.Invoke_expr(0).(*parser.Invoke_exprContext)
	if !ok || suffix.GetText() != invoke.GetText() {
		return "", nil, false
	}
	if casual.Id_expr() != nil {
		return identifier(casual.Id_expr().GetText()), invoke, true
	}
	atom := casual.Atom_expr()
	if atom != nil && atom.NAMESPACE() != nil && atom.An_id_or_type() != nil && atom.Id_or_type() != nil {
		return identifier(atom.An_id_or_type().GetText()) + "::" + identifier(atom.Id_or_type().GetText()), invoke, true
	}
	return "", nil, false
}

func resolveFunction(name string, invoke *parser.Invoke_exprContext, scope expressionScope) (model.Type, error) {
	if strings.EqualFold(name, "count") && invoke.ASTERISK() != nil {
		return model.Type{Kind: "Uint64"}, nil
	}
	if invoke.Opt_set_quantifier() != nil && invoke.Opt_set_quantifier().GetText() != "" {
		return model.Type{}, fmt.Errorf("set quantifiers in function %q are unsupported", name)
	}
	var args []model.Type
	if list := invoke.Named_expr_list(); list != nil {
		for _, named := range list.AllNamed_expr() {
			if named.AS() != nil {
				return model.Type{}, fmt.Errorf("named arguments in function %q are unsupported", name)
			}
			if isAggregateFunction(name) && containsAggregate(named.Expr()) {
				return model.Type{}, fmt.Errorf("aggregate function %q cannot contain another aggregate", name)
			}
			argumentScope := scope
			argumentScope.predicate = strings.EqualFold(name, "if") && len(args) == 0
			typeValue, err := resolveExpression(named.Expr(), argumentScope)
			if err != nil {
				return model.Type{}, fmt.Errorf("cannot resolve argument of %s: %w", name, err)
			}
			args = append(args, typeValue)
		}
	}
	if strings.EqualFold(name, "count") {
		if len(args) != 1 {
			return model.Type{}, fmt.Errorf("function %q expects one argument or *", name)
		}
		return model.Type{Kind: "Uint64"}, nil
	}
	result, err := builtins.Resolve(name, args)
	if err != nil {
		return model.Type{}, err
	}
	if scope.grouped && groupMakesAggregateNonOptional(name) && len(args) == 1 && args[0].Kind != "Null" && !args[0].IsOptional() {
		result = result.UnwrapOptional()
	}
	return result, nil
}

func groupMakesAggregateNonOptional(name string) bool {
	switch strings.ToLower(name) {
	case "min", "max", "sum", "avg":
		return true
	default:
		return false
	}
}
