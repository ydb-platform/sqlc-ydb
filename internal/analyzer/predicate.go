package analyzer

import (
	"fmt"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	"github.com/ydb-platform/sqlc-ydb/internal/yql/builtins"
	parser "github.com/ydb-platform/yql-parsers/go"
)

func validatePredicateContexts(block queryBlock, root antlr.Tree, relations []relation, bindings map[string]model.Type, subqueries map[int]model.Type) []model.Diagnostic {
	var predicates []parser.IExprContext
	scopeDescendants(root, func(node antlr.Tree) {
		switch ctx := node.(type) {
		case *parser.Select_coreContext:
			if ctx.WHERE() != nil {
				predicates = append(predicates, ctx.Expr(0))
			}
		case *parser.Join_constraintContext:
			if ctx.ON() != nil && ctx.Expr() != nil {
				predicates = append(predicates, ctx.Expr())
			}
		case *parser.Update_stmtContext:
			if ctx.WHERE() != nil && ctx.Expr() != nil {
				predicates = append(predicates, ctx.Expr())
			}
		case *parser.Delete_stmtContext:
			if ctx.WHERE() != nil && ctx.Expr() != nil {
				predicates = append(predicates, ctx.Expr())
			}
		}
	})
	seen := map[int]bool{}
	var diagnostics []model.Diagnostic
	for _, predicate := range predicates {
		if predicate == nil || seen[predicate.GetStart().GetStart()] {
			continue
		}
		seen[predicate.GetStart().GetStart()] = true
		if containsAggregate(predicate) {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, predicate, "aggregate functions are not allowed in WHERE or JOIN predicates; use HAVING after aggregation"))
			continue
		}
		if err := validatePredicate(predicate, expressionScope{relations: relations, bindings: bindings, functions: block.functions, inSubqueries: subqueries}); err != nil {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, predicate, fmt.Sprintf("invalid predicate: %v", err)))
		}
	}
	return diagnostics
}

func validatePredicate(expr parser.IExprContext, scope expressionScope) error {
	if len(expr.AllOr_subexpr()) == 0 {
		return fmt.Errorf("unsupported predicate %q", expr.GetText())
	}
	for _, or := range expr.AllOr_subexpr() {
		for _, and := range or.AllAnd_subexpr() {
			for _, xor := range and.AllXor_subexpr() {
				atom, ok := xor.(*parser.Xor_subexprContext)
				if !ok {
					return fmt.Errorf("unsupported predicate %q", xor.GetText())
				}
				if err := validatePredicateAtom(atom, scope); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func validatePredicateAtom(atom *parser.Xor_subexprContext, scope expressionScope) error {
	if condition := atom.Cond_expr(); condition != nil {
		var left model.Type
		var err error
		if condition.IN() != nil {
			left, err = resolveINOperand(atom.Eq_subexpr(), scope)
		} else {
			left, err = resolveScalarNode(atom.Eq_subexpr(), scope)
		}
		if err != nil {
			return fmt.Errorf("cannot resolve predicate operand %q: %w", atom.Eq_subexpr().GetText(), err)
		}
		if condition.ISNULL() != nil || condition.NOTNULL() != nil || condition.NULL() != nil {
			return nil
		}
		if condition.IN() != nil && condition.In_expr() != nil {
			types := []model.Type{left}
			inExpr := condition.In_expr()
			if subquery, ok := scope.inSubqueries[inExpr.GetStart().GetTokenIndex()]; ok {
				if err := validateINSubqueryTypes(left, subquery); err != nil {
					return fmt.Errorf("IN subquery key types are incompatible: %s and %s: %w", left.String(), subquery.String(), err)
				}
				return nil
			}
			if bind := directBind(inExpr); bind != nil && inExpr.GetText() == bind.GetText() {
				typeValue, ok := scope.bindings[bindName(bind)]
				if !ok || typeValue.Kind != "List" || typeValue.Elem == nil {
					return fmt.Errorf("direct IN operand %q requires a List parameter", bind.GetText())
				}
				types = append(types, *typeValue.Elem)
			} else {
				var expressions []parser.IExprContext
				descendants(inExpr, func(node antlr.Tree) {
					candidate, ok := node.(parser.IExprContext)
					if !ok {
						return
					}
					for parent := candidate.GetParent(); parent != nil && parent != inExpr; parent = parent.GetParent() {
						if _, nested := parent.(parser.IExprContext); nested {
							return
						}
					}
					expressions = append(expressions, candidate)
				})
				if len(expressions) == 0 {
					return fmt.Errorf("unsupported IN operand %q", inExpr.GetText())
				}
				for _, expression := range expressions {
					if bind := directBind(expression); bind != nil {
						if typeValue, ok := scope.bindings[bindName(bind)]; ok && typeValue.Kind == "List" {
							return fmt.Errorf("parenthesized List parameter %q is not a valid IN operand; use IN %s", bind.GetText(), bind.GetText())
						}
					}
					typeValue, err := resolveExpression(expression, scope)
					if err != nil {
						return fmt.Errorf("cannot resolve IN operand %q: %w", expression.GetText(), err)
					}
					types = append(types, typeValue)
				}
			}
			if _, err := builtins.CommonType(types...); err != nil {
				return fmt.Errorf("predicate operands have incompatible types: %w", err)
			}
			return nil
		}
		_, _, err = resolveComparison(atom, scope)
		return err
	}

	eq := atom.Eq_subexpr()
	if len(eq.AllNeq_subexpr()) > 1 {
		operands := make([]antlr.ParserRuleContext, 0, len(eq.AllNeq_subexpr()))
		for _, operand := range eq.AllNeq_subexpr() {
			operands = append(operands, operand)
		}
		_, _, err := comparisonBoolType(operands, scope)
		return err
	}
	var unaryNot *parser.Con_subexprContext
	descendants(eq, func(node antlr.Tree) {
		ctx, ok := node.(*parser.Con_subexprContext)
		if ok && sameSpan(eq, ctx) && ctx.Unary_op() != nil && ctx.Unary_op().NOT() != nil {
			unaryNot = ctx
		}
	})
	if unaryNot != nil {
		if nested := nestedBooleanExpression(unaryNot.Unary_subexpr()); nested != nil {
			return validatePredicate(nested, scope)
		}
		typeValue, err := resolveScalarNode(unaryNot.Unary_subexpr(), scope)
		if err != nil {
			return fmt.Errorf("cannot resolve NOT operand: %w", err)
		}
		if typeValue.Kind != "Null" && typeValue.UnwrapOptional().Kind != "Bool" {
			return fmt.Errorf("NOT operand has type %s, want Bool", typeValue.String())
		}
		return nil
	}
	typeValue, err := resolveScalarNode(eq, scope)
	if err != nil {
		if nested := nestedBooleanExpression(eq); nested != nil {
			return validatePredicate(nested, scope)
		}
		return fmt.Errorf("cannot resolve predicate operand %q: %w", eq.GetText(), err)
	}
	if typeValue.Kind != "Null" && typeValue.UnwrapOptional().Kind != "Bool" {
		return fmt.Errorf("predicate expression has type %s, want Bool", typeValue.String())
	}
	return nil
}

func nestedBooleanExpression(root antlr.ParserRuleContext) parser.IExprContext {
	var result parser.IExprContext
	descendants(root, func(node antlr.Tree) {
		candidate, ok := node.(parser.IExprContext)
		if !ok || result != nil {
			return
		}
		for current := antlr.Tree(candidate); current != root; {
			parent := current.GetParent()
			if parent == nil {
				return
			}
			ruleChildren := 0
			for i := 0; i < parent.GetChildCount(); i++ {
				child := parent.GetChild(i)
				if rule, ok := child.(antlr.ParserRuleContext); ok {
					if rule.GetText() != "" {
						ruleChildren++
					}
					continue
				}
				terminal, ok := child.(antlr.TerminalNode)
				if !ok {
					return
				}
				switch terminal.GetSymbol().GetTokenType() {
				case parser.YQLLexerLPAREN, parser.YQLLexerRPAREN, parser.YQLLexerNOT:
				default:
					return
				}
			}
			if ruleChildren != 1 {
				return
			}
			current = parent
		}
		result = candidate
	})
	return result
}
