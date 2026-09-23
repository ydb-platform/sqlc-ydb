package analyzer

import (
	"fmt"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	parser "github.com/ydb-platform/yql-parsers/go"
)

func resolveBoolean(root antlr.ParserRuleContext, scope expressionScope) (model.Type, bool, error) {
	var operands []antlr.ParserRuleContext
	operator := ""
	switch operation := coveringExpressionContext(root).(type) {
	case *parser.ExprContext:
		if len(operation.AllOR()) != 0 {
			operator = "OR"
			for _, operand := range operation.AllOr_subexpr() {
				operands = append(operands, operand)
			}
		}
	case *parser.Or_subexprContext:
		if len(operation.AllAND()) != 0 {
			operator = "AND"
			for _, operand := range operation.AllAnd_subexpr() {
				operands = append(operands, operand)
			}
		}
	case *parser.And_subexprContext:
		if len(operation.AllXOR()) != 0 {
			operator = "XOR"
			for _, operand := range operation.AllXor_subexpr() {
				operands = append(operands, operand)
			}
		}
	case *parser.Con_subexprContext:
		if operation.Unary_op() != nil && operation.Unary_op().NOT() != nil {
			operator = "NOT"
			operands = append(operands, operation.Unary_subexpr())
		}
	}
	if len(operands) == 0 {
		return model.Type{}, false, nil
	}
	result := model.Type{Kind: "Bool"}
	if operator == "NOT" {
		result = model.Type{Kind: "Null"}
	}
	optional := false
	for _, operand := range operands {
		typ, err := resolveScalarNode(operand, scope)
		if err != nil {
			return model.Type{}, true, fmt.Errorf("cannot resolve %s operand: %w", operator, err)
		}
		if typ.Kind != "Null" && typ.UnwrapOptional().Kind != "Bool" {
			return model.Type{}, true, fmt.Errorf("%s operand has type %s, want Bool or Optional<Bool>", operator, typ.String())
		}
		optional = optional || typ.Kind == "Null" || typ.IsOptional()
		if typ.Kind != "Null" {
			result = model.Type{Kind: "Bool"}
		}
	}
	if optional && result.Kind != "Null" {
		result = model.Optional(result)
	}
	return result, true, nil
}
