package builtins

import (
	"fmt"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

// Arithmetic resolves the supported primitive numeric operators. Decimal and
// temporal arithmetic need their own rules and are deliberately excluded.
func Arithmetic(operator string, left, right model.Type) (model.Type, error) {
	if operator != "+" && operator != "-" && operator != "*" {
		return model.Type{}, fmt.Errorf("unsupported arithmetic operator %q; supported operators are +, -, and *", operator)
	}
	for _, operand := range []model.Type{left, right} {
		base := operand.UnwrapOptional()
		if !isInteger(base.Kind) && base.Kind != "Float" && base.Kind != "Double" {
			return model.Type{}, fmt.Errorf("arithmetic %s requires primitive numeric operands, got %s", operator, operand.String())
		}
	}
	return CommonType(left, right)
}

// CanWidenInteger permits lossless integer assignment without changing the
// expression's resolved type. Other conversions require an explicit CAST.
func CanWidenInteger(source, target model.Type) bool {
	if source.IsOptional() && !target.IsOptional() {
		return false
	}
	sourceSigned, sourceBits := integerInfo(source.UnwrapOptional().Kind)
	targetSigned, targetBits := integerInfo(target.UnwrapOptional().Kind)
	if sourceBits == 0 || targetBits == 0 {
		return false
	}
	return sourceSigned == targetSigned && sourceBits <= targetBits || !sourceSigned && targetSigned && sourceBits < targetBits
}
