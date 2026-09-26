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
	relations    []relation
	bindings     map[string]model.Type
	arguments    map[string]sqlcArgument
	lambdas      map[string]lambdaBinding
	grouped      bool
	functions    *builtins.Registry
	inSubqueries map[int]model.Type
}

func resolveExpression(expr parser.IExprContext, scope expressionScope) (model.Type, error) {
	if expr == nil {
		return model.Type{}, fmt.Errorf("missing expression")
	}
	if literal, ok, err := literalType(expr); ok || err != nil {
		return literal, err
	}
	if typ, ok, err := resolveArithmetic(expr, scope); ok {
		return typ, err
	}
	if typ, ok, err := resolveBoolean(expr, scope); ok {
		return typ, err
	}
	if typ, ok, err := resolveStructLiteral(expr, scope); ok {
		return typ, err
	}
	if typ, ok, err := resolveMemberAccess(expr, scope); ok {
		return typ, err
	}
	if bind := directBind(expr); bind != nil {
		name := bindName(bind)
		typeValue, ok := scope.bindings[name]
		if !ok {
			return model.Type{}, fmt.Errorf("cannot resolve type of parameter $%s; add DECLARE", name)
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
	if name, invokes, ok := directCallChain(expr); ok {
		return resolveCallChain(name, invokes, scope)
	}
	if typeValue, ok, err := concatenationType(expr, scope); ok {
		return typeValue, err
	}
	if typeValue, ok, err := resolveComparison(expr, scope); ok {
		return typeValue, err
	}
	if len(columnRefs(expr)) != 0 {
		return model.Type{}, fmt.Errorf("computed result expression %q is not supported", expr.GetText())
	}
	return model.Type{}, fmt.Errorf("unsupported result expression %q", expr.GetText())
}

func resolveStructLiteral(expr parser.IExprContext, scope expressionScope) (model.Type, bool, error) {
	var literal parser.IStruct_literalContext
	descendants(expr, func(node antlr.Tree) {
		if ctx, ok := node.(parser.IStruct_literalContext); ok && sameSpan(expr, ctx) {
			literal = ctx
		}
	})
	if literal == nil {
		return model.Type{}, false, nil
	}
	if literal.Expr_struct_list() == nil {
		return model.Type{}, true, fmt.Errorf("empty struct literal has no fields")
	}
	fields := literal.Expr_struct_list().AllExpr()
	if len(fields)%2 != 0 {
		return model.Type{}, true, fmt.Errorf("struct literal requires field names and values")
	}
	args := make([]builtins.CallArgument, 0, len(fields)/2)
	for i := 0; i < len(fields); i += 2 {
		var name parser.IId_exprContext
		descendants(fields[i], func(node antlr.Tree) {
			if ctx, ok := node.(parser.IId_exprContext); ok && sameSpan(fields[i], ctx) {
				name = ctx
			}
		})
		if name == nil {
			return model.Type{}, true, fmt.Errorf("struct literal field name %q must be an identifier", fields[i].GetText())
		}
		value, err := resolveExpression(fields[i+1], scope)
		if err != nil {
			return model.Type{}, true, fmt.Errorf("struct literal field %q: %w", identifier(name.GetText()), err)
		}
		args = append(args, builtins.CallArgument{Name: identifier(name.GetText()), Type: value})
	}
	typ, err := scope.functions.ResolveCall("AsStruct", args)
	return typ, true, err
}

func resolveComparison(expr antlr.ParserRuleContext, scope expressionScope) (model.Type, bool, error) {
	var operands []antlr.ParserRuleContext
	nullSafe := false
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
			if inSubquery(condition.In_expr()) != nil {
				return model.Type{}, true, fmt.Errorf("IN subqueries are supported only in WHERE predicates; they are not yet supported in projections, CASE, IF, or HAVING")
			}
			typ, err := resolveINCondition(xor, scope)
			return typ, true, err
		}
		operands = append(operands, xor.Eq_subexpr())
		for _, operand := range condition.AllEq_subexpr() {
			operands = append(operands, operand)
		}
		nullSafe = len(condition.AllDistinct_from_op()) == len(operands)-1
		if len(operands) < 2 {
			return model.Type{}, false, nil
		}
	}
	typ, matched, err := comparisonBoolType(operands, scope)
	if nullSafe && err == nil {
		typ = model.Type{Kind: "Bool"}
	}
	return typ, matched, err
}

func comparisonBoolType(operands []antlr.ParserRuleContext, scope expressionScope) (model.Type, bool, error) {
	types := make([]model.Type, 0, len(operands))
	allNull := true
	for _, operand := range operands {
		typeValue, err := resolveScalarNode(operand, scope)
		if err != nil {
			return model.Type{}, true, fmt.Errorf("cannot resolve comparison operand %q: %w", operand.GetText(), err)
		}
		types = append(types, typeValue)
		allNull = allNull && typeValue.Kind == "Null"
	}
	if allNull {
		return model.Optional(model.Type{Kind: "Bool"}), true, nil
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
	if typ, ok, err := resolveArithmetic(root, scope); ok {
		return typ, err
	}
	if typ, ok, err := resolveBoolean(root, scope); ok {
		return typ, err
	}
	if typ, ok, err := concatenationType(root, scope); ok {
		return typ, err
	}
	if typ, ok, err := resolveComparison(root, scope); ok {
		return typ, err
	}
	if typ, ok, err := resolveMemberAccess(root, scope); ok {
		return typ, err
	}
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
			return model.Type{}, fmt.Errorf("cannot resolve type of parameter $%s; add DECLARE", name)
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
		if name, invokes, ok := callChainFromUnary(functionUnary); ok {
			return resolveCallChain(name, invokes, scope)
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

func sameOrWrappedExpression(outer, inner antlr.ParserRuleContext) bool {
	text := outer.GetText()
	if text == inner.GetText() {
		return true
	}
	for len(text) >= 2 && text[0] == '(' && text[len(text)-1] == ')' {
		text = text[1 : len(text)-1]
		if text == inner.GetText() {
			return true
		}
	}
	return false
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
		conditionType, err := resolveExpression(parts[0], scope)
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
	name, invokes, ok := callChainFromUnary(unary)
	if !ok || len(invokes) != 1 || strings.HasPrefix(name, "$") {
		return "", nil, false
	}
	return name, invokes[0], true
}

func directCallChain(expr parser.IExprContext) (string, []*parser.Invoke_exprContext, bool) {
	var unary *parser.Unary_subexprContext
	descendants(expr, func(node antlr.Tree) {
		ctx, ok := node.(*parser.Unary_subexprContext)
		if ok && sameSpan(expr, ctx) {
			unary = ctx
		}
	})
	if unary == nil {
		return "", nil, false
	}
	return callChainFromUnary(unary)
}

func callChainFromUnary(unary *parser.Unary_subexprContext) (string, []*parser.Invoke_exprContext, bool) {
	casual := unary.Unary_casual_subexpr()
	if casual == nil {
		return "", nil, false
	}
	suffix := casual.Unary_subexpr_suffix()
	if suffix == nil || len(suffix.AllInvoke_expr()) == 0 {
		return "", nil, false
	}
	invokes := make([]*parser.Invoke_exprContext, 0, len(suffix.AllInvoke_expr()))
	var suffixText strings.Builder
	for _, call := range suffix.AllInvoke_expr() {
		invoke, ok := call.(*parser.Invoke_exprContext)
		if !ok {
			return "", nil, false
		}
		invokes = append(invokes, invoke)
		suffixText.WriteString(invoke.GetText())
	}
	if suffix.GetText() != suffixText.String() {
		return "", nil, false
	}
	if casual.Id_expr() != nil {
		return identifier(casual.Id_expr().GetText()), invokes, true
	}
	atom := casual.Atom_expr()
	if atom != nil && atom.Bind_parameter() != nil {
		return "$" + bindName(atom.Bind_parameter()), invokes, true
	}
	if atom != nil && atom.NAMESPACE() != nil && atom.An_id_or_type() != nil && atom.Id_or_type() != nil {
		return identifier(atom.An_id_or_type().GetText()) + "::" + identifier(atom.Id_or_type().GetText()), invokes, true
	}
	return "", nil, false
}

func resolveCallChain(name string, invokes []*parser.Invoke_exprContext, scope expressionScope) (model.Type, error) {
	var result model.Type
	var err error
	if strings.HasPrefix(name, "$") {
		var ok bool
		bindingName := strings.TrimPrefix(name, "$")
		result, ok = scope.bindings[bindingName]
		if !ok {
			return model.Type{}, fmt.Errorf("unknown callable binding %s", name)
		}
		if result.Kind == "Lambda" && len(invokes) != 0 {
			binding := scope.lambdas[bindingName]
			if binding.lambda == nil {
				return model.Type{}, fmt.Errorf("unknown lambda binding %s", name)
			}
			provided, err := invocationTypes(invokes[0], scope)
			if err != nil {
				return model.Type{}, err
			}
			captured := scope
			captured.bindings = binding.bindings
			captured.lambdas = binding.lambdas
			result, err = resolveLambda(binding.lambda, provided, captured)
			if err != nil {
				return model.Type{}, err
			}
			result = *result.Elem
			invokes = invokes[1:]
		}
	} else {
		result, err = resolveFunction(name, invokes[0], scope)
		if err != nil {
			return model.Type{}, err
		}
		invokes = invokes[1:]
	}
	for _, invoke := range invokes {
		if result.Kind != "Callable" || result.Elem == nil {
			return model.Type{}, fmt.Errorf("%s is not callable", result.String())
		}
		provided, err := invocationTypes(invoke, scope)
		if err != nil {
			return model.Type{}, err
		}
		if len(provided) != len(result.Items) {
			return model.Type{}, fmt.Errorf("callable expects %d arguments, got %d", len(result.Items), len(provided))
		}
		nullableReturn := false
		for i, actual := range provided {
			expected := result.Items[i]
			if expected.Equal(actual) || expected.IsOptional() && (expected.UnwrapOptional().Equal(actual) || actual.Kind == "Null") {
				continue
			}
			if expected.Kind == "Resource<'DateTime2.TM64'>" && dateTimeFormatInput(actual.UnwrapOptional()) {
				nullableReturn = nullableReturn || actual.IsOptional()
				continue
			}
			if expected.Kind == "String" && actual.Equal(model.Optional(expected)) && result.Elem.IsOptional() && strings.HasPrefix(result.Elem.UnwrapOptional().Kind, "Resource<'DateTime2.TM") {
				continue
			}
			return model.Type{}, fmt.Errorf("callable argument %d has type %s, want %s", i+1, actual.String(), expected.String())
		}
		result = *result.Elem
		if nullableReturn && !result.IsOptional() {
			result = model.Optional(result)
		}
	}
	return result, nil
}

func invocationTypes(invoke *parser.Invoke_exprContext, scope expressionScope) ([]model.Type, error) {
	if invoke.ASTERISK() != nil || invoke.Opt_set_quantifier() != nil && invoke.Opt_set_quantifier().GetText() != "" {
		return nil, fmt.Errorf("callable invocation requires positional arguments")
	}
	var provided []model.Type
	if list := invoke.Named_expr_list(); list != nil {
		for _, arg := range list.AllNamed_expr() {
			if arg.AS() != nil {
				return nil, fmt.Errorf("callable invocation does not support named arguments")
			}
			typ, err := resolveExpression(arg.Expr(), scope)
			if err != nil {
				return nil, err
			}
			provided = append(provided, typ)
		}
	}
	return provided, nil
}

func dateTimeFormatInput(value model.Type) bool {
	switch value.Kind {
	case "Resource<'DateTime2.TM'>", "Resource<'DateTime2.TM64'>", "Date", "Datetime", "Timestamp", "TzDate", "TzDatetime", "TzTimestamp", "Date32", "Datetime64", "Timestamp64", "TzDate32", "TzDatetime64", "TzTimestamp64":
		return true
	default:
		return false
	}
}

func resolveFunction(name string, invoke *parser.Invoke_exprContext, scope expressionScope) (model.Type, error) {
	if strings.EqualFold(name, "count") && invoke.ASTERISK() != nil {
		return model.Type{Kind: "Uint64"}, nil
	}
	if invoke.Opt_set_quantifier() != nil && invoke.Opt_set_quantifier().GetText() != "" {
		return model.Type{}, fmt.Errorf("set quantifiers in function %q are unsupported", name)
	}
	if strings.EqualFold(name, "ListCreate") {
		return resolveListCreateType(invoke)
	}
	var callArgs []builtins.CallArgument
	if list := invoke.Named_expr_list(); list != nil {
		for _, named := range list.AllNamed_expr() {
			if isAggregateFunction(name) && containsAggregate(named.Expr()) {
				return model.Type{}, fmt.Errorf("aggregate function %q cannot contain another aggregate", name)
			}
			if name == "Yson::ConvertTo" && len(callArgs) == 1 {
				if named.AS() != nil {
					return model.Type{}, fmt.Errorf("Yson::ConvertTo target type must be positional")
				}
				target, err := parseType(named.Expr().GetText())
				if err != nil {
					return model.Type{}, fmt.Errorf("Yson::ConvertTo requires a literal YQL target type: %w", err)
				}
				callArgs = append(callArgs, builtins.CallArgument{TypeArgument: &target})
				continue
			}
			var typeValue model.Type
			var err error
			if (name == "Json::From" || name == "Yson::From") && len(callArgs) == 0 && directEmptyAsList(named.Expr()) {
				typeValue = model.Type{Kind: "EmptyList"}
			} else if (strings.EqualFold(name, "ListMap") || strings.EqualFold(name, "ListFilter")) && len(callArgs) == 1 {
				if lambda := directLambda(named.Expr()); lambda != nil {
					listType := callArgs[0].Type.UnwrapOptional()
					if listType.Kind != "List" || listType.Elem == nil {
						return model.Type{}, fmt.Errorf("%s first argument must be a typed List", name)
					}
					typeValue, err = resolveLambda(lambda, []model.Type{*listType.Elem}, scope)
				} else if bind := directBind(named.Expr()); bind != nil && scope.bindings[bindName(bind)].Kind == "Lambda" && scope.lambdas[bindName(bind)].lambda != nil {
					listType := callArgs[0].Type.UnwrapOptional()
					if listType.Kind != "List" || listType.Elem == nil {
						return model.Type{}, fmt.Errorf("%s first argument must be a typed List", name)
					}
					binding := scope.lambdas[bindName(bind)]
					captured := scope
					captured.bindings = binding.bindings
					captured.lambdas = binding.lambdas
					typeValue, err = resolveLambda(binding.lambda, []model.Type{*listType.Elem}, captured)
				} else {
					typeValue, err = resolveExpression(named.Expr(), scope)
				}
			} else {
				typeValue, err = resolveExpression(named.Expr(), scope)
			}
			if err != nil {
				return model.Type{}, fmt.Errorf("cannot resolve argument of %s: %w", name, err)
			}
			argument := builtins.CallArgument{Type: typeValue, IntegerLiteral: integerLiteralValue(named.Expr()), StringLiteral: stringLiteralValue(named.Expr())}
			if callName, _, ok := directFunctionCall(named.Expr()); ok && strings.EqualFold(callName, "ListCreate") {
				argument.EmptyList = true
			}
			if named.AS() != nil {
				argument.Name = identifier(named.An_id_or_type().GetText())
			}
			callArgs = append(callArgs, argument)
		}
	}
	if strings.EqualFold(name, "count") {
		for _, arg := range callArgs {
			if arg.Name != "" {
				return model.Type{}, fmt.Errorf("named arguments in aggregate %q are unsupported", name)
			}
		}
		if len(callArgs) != 1 {
			return model.Type{}, fmt.Errorf("function %q expects one argument or *", name)
		}
		return model.Type{Kind: "Uint64"}, nil
	}
	result, err := scope.functions.ResolveCall(name, callArgs)
	if err != nil {
		return model.Type{}, err
	}
	if scope.grouped && groupMakesAggregateNonOptional(name) && len(callArgs) == 1 && callArgs[0].Type.Kind != "Null" && !callArgs[0].Type.IsOptional() {
		result = result.UnwrapOptional()
	}
	return result, nil
}

func directEmptyAsList(expr parser.IExprContext) bool {
	name, invoke, ok := directFunctionCall(expr)
	return ok && strings.EqualFold(name, "AsList") && invoke.ASTERISK() == nil && invoke.Named_expr_list() == nil &&
		(invoke.Opt_set_quantifier() == nil || invoke.Opt_set_quantifier().GetText() == "")
}

func resolveListCreateType(invoke *parser.Invoke_exprContext) (model.Type, error) {
	list := invoke.Named_expr_list()
	if list == nil || len(list.AllNamed_expr()) != 1 {
		return model.Type{}, fmt.Errorf("ListCreate expects one literal YQL element type")
	}
	arg := list.Named_expr(0)
	if arg.AS() != nil || arg.Expr() == nil {
		return model.Type{}, fmt.Errorf("ListCreate expects one literal YQL element type")
	}
	item, err := parseType(arg.Expr().GetText())
	if err != nil {
		return model.Type{}, fmt.Errorf("ListCreate requires a literal YQL element type: %w", err)
	}
	return builtins.Resolve("ListCreate", []model.Type{item})
}

func groupMakesAggregateNonOptional(name string) bool {
	switch strings.ToLower(name) {
	case "min", "max", "sum", "avg":
		return true
	default:
		return false
	}
}
