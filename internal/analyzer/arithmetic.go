package analyzer

import (
	"fmt"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	"github.com/ydb-platform/sqlc-ydb/internal/yql/builtins"
	parser "github.com/ydb-platform/yql-parsers/go"
)

func resolveArithmetic(root antlr.ParserRuleContext, scope expressionScope) (model.Type, bool, error) {
	if inner := parenthesizedExpression(root); inner != nil {
		typ, err := resolveExpression(inner, scope)
		return typ, true, err
	}
	var operation antlr.ParserRuleContext
	descendants(root, func(node antlr.Tree) {
		switch ctx := node.(type) {
		case *parser.Bit_subexprContext:
			if sameSpan(root, ctx) && len(ctx.AllAdd_subexpr()) > 1 {
				operation = ctx
			}
		case *parser.Add_subexprContext:
			if sameSpan(root, ctx) && len(ctx.AllMul_subexpr()) > 1 {
				operation = ctx
			}
		}
	})
	if operation == nil {
		return model.Type{}, false, nil
	}
	var result model.Type
	operator := ""
	for _, child := range operation.GetChildren() {
		if token, ok := child.(antlr.TerminalNode); ok {
			operator = token.GetText()
			continue
		}
		operand, ok := child.(antlr.ParserRuleContext)
		if !ok {
			continue
		}
		typ, err := resolveScalarNode(operand, scope)
		if err != nil {
			return model.Type{}, true, fmt.Errorf("cannot resolve arithmetic operand: %w", err)
		}
		if result.Kind == "" {
			result = typ
			continue
		}
		result, err = builtins.Arithmetic(operator, result, typ)
		if err != nil {
			return model.Type{}, true, err
		}
	}
	return result, true, nil
}

// Unwrap only a single scalar expression. Tuples, lambdas and subqueries must
// retain their own semantics rather than being treated as parentheses.
func parenthesizedExpression(root antlr.ParserRuleContext) parser.IExprContext {
	var paren *parser.Smart_parenthesisContext
	descendants(root, func(node antlr.Tree) {
		if ctx, ok := node.(*parser.Smart_parenthesisContext); ok && sameSpan(root, ctx) {
			paren = ctx
		}
	})
	if paren == nil || paren.COMMA() != nil || paren.Select_subexpr() == nil {
		return nil
	}
	selectExpr := paren.Select_subexpr()
	if selectExpr.Cte_with_clause() != nil || selectExpr.Select_subexpr_core() == nil {
		return nil
	}
	core := selectExpr.Select_subexpr_core()
	if len(core.AllUnion_op()) != 0 || len(core.AllSelect_subexpr_intersect()) != 1 {
		return nil
	}
	intersect := core.Select_subexpr_intersect(0)
	if len(intersect.AllIntersect_op()) != 0 || len(intersect.AllSelect_or_expr()) != 1 {
		return nil
	}
	tuple := intersect.Select_or_expr(0).Tuple_or_expr()
	if tuple == nil || tuple.AS() != nil || len(tuple.AllCOMMA()) != 0 {
		return nil
	}
	return tuple.Expr()
}
