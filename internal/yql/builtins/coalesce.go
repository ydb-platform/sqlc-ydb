package builtins

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func isCoalesce(name string) bool {
	return strings.EqualFold(name, "COALESCE") || strings.EqualFold(name, "NVL")
}

func resolveCoalesceArguments(name string, args []CallArgument) (model.Type, error) {
	if len(args) == 0 {
		return model.Type{}, fmt.Errorf("%s expects at least 1 argument", name)
	}
	result := model.Type{Kind: "Null"}
	for _, argument := range args {
		left, leftOptional := result.UnwrapOptional(), result.IsOptional()
		right, rightOptional, err := baseType(argument.Type)
		if err != nil {
			return model.Type{}, fmt.Errorf("%s: %w", name, err)
		}
		if left.Kind == "Null" {
			result = argument.Type
			continue
		}
		if right.Kind == "Null" {
			continue
		}
		if argument.EmptyList && argument.Type.Kind == "List" && left.Kind == "List" {
			right = left
			rightOptional = false
		}
		// YQL reconciles COALESCE from left to right. Only the original
		// right-hand literal can narrow to the accumulated left-hand type.
		if !rightOptional && isInteger(left.Kind) && isInteger(right.Kind) && integerLiteralFits(argument.IntegerLiteral, left.Kind) {
			right = left
		}
		if (left.Kind == "String" && right.Kind == "Utf8") || (left.Kind == "Utf8" && right.Kind == "String") {
			left, right = model.Type{Kind: "String"}, model.Type{Kind: "String"}
		}
		common, err := commonConcreteType(left, right)
		if err != nil {
			return model.Type{}, fmt.Errorf("%s arguments have incompatible types: %w; use CAST to convert them to the same YQL type", name, err)
		}
		result = withOptional(common, leftOptional && rightOptional)
	}
	if result.Kind == "Null" {
		return model.Type{}, fmt.Errorf("%s cannot infer a concrete type from only Null arguments", name)
	}
	return result, nil
}

func integerLiteralFits(value *big.Int, kind string) bool {
	if value == nil {
		return false
	}
	signed, bits := integerInfo(kind)
	if !signed {
		return value.Sign() >= 0 && value.BitLen() <= bits
	}
	limit := new(big.Int).Lsh(big.NewInt(1), uint(bits-1))
	return value.Cmp(new(big.Int).Neg(limit)) >= 0 && value.Cmp(limit) < 0
}
