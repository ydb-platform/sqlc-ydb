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
	switch ctx := coveringExpressionContext(root).(type) {
	case *parser.Bit_subexprContext, *parser.Add_subexprContext:
		operation = ctx
	}
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
		// Operator rules contain alternating rule operands and terminal operators.
		operand := child.(antlr.ParserRuleContext)
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
	paren, _ := coveringExpressionContext(root).(*parser.Smart_parenthesisContext)
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

// Follow only grammar wrappers spanning the whole expression. Empty optional
// rules may be siblings, but operands and parenthesized bodies have smaller spans.
func coveringExpressionContext(root antlr.ParserRuleContext) antlr.ParserRuleContext {
	for {
		var covering antlr.ParserRuleContext
		for _, child := range root.GetChildren() {
			if rule, ok := child.(antlr.ParserRuleContext); ok && sameSpan(root, rule) {
				covering = rule
				break
			}
		}
		if covering == nil {
			return root
		}
		root = covering
	}
}
