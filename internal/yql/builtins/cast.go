package builtins

import (
	"fmt"

	"github.com/ydb-platform/sqlc-engine-ydb/internal/model"
)

// Cast returns the static result type of a supported YQL CAST. Conversions that
// may fail add one Optional level, as YQL returns NULL for an unrepresentable
// value. The target may explicitly request Optional as well.
func Cast(source, target model.Type) (model.Type, error) {
	sourceBase, sourceOptional, err := baseType(source)
	if err != nil {
		return model.Type{}, fmt.Errorf("CAST source: %w", err)
	}
	targetBase, targetOptional, err := baseType(target)
	if err != nil {
		return model.Type{}, fmt.Errorf("CAST target: %w", err)
	}
	if targetBase.Kind == "Null" {
		return model.Type{}, fmt.Errorf("CAST target cannot be Null")
	}
	if sourceBase.Kind == "Null" {
		return optional(targetBase), nil
	}

	mayFail, supported := castRule(sourceBase, targetBase)
	if !supported {
		return model.Type{}, fmt.Errorf("unsupported CAST from %s to %s", typeName(source), typeName(target))
	}
	return withOptional(targetBase, sourceOptional || targetOptional || mayFail), nil
}

func castRule(source, target model.Type) (mayFail, supported bool) {
	if equalType(source, target) {
		return false, true
	}
	if isInteger(source.Kind) && isInteger(target.Kind) {
		return !totalIntegerCast(source.Kind, target.Kind), true
	}
	if isPrimitiveNumber(source.Kind) && isPrimitiveNumber(target.Kind) && source.Kind != "Decimal" && target.Kind != "Decimal" {
		if (source.Kind == "Float" || source.Kind == "Double") && isInteger(target.Kind) {
			return true, true
		}
		return false, true
	}
	if (source.Kind == "String" || source.Kind == "Utf8") && isPrimitiveNumber(target.Kind) {
		return true, true
	}
	if isPrimitiveNumber(source.Kind) && target.Kind == "String" {
		return false, true
	}
	if source.Kind == "Utf8" && target.Kind == "String" {
		return false, true
	}
	if source.Kind == "String" && target.Kind == "Utf8" {
		return true, true
	}
	return false, false
}

func totalIntegerCast(source, target string) bool {
	sourceSigned, sourceBits := integerInfo(source)
	targetSigned, targetBits := integerInfo(target)
	if sourceSigned == targetSigned {
		return targetBits >= sourceBits
	}
	if sourceSigned {
		return false
	}
	return targetSigned && targetBits > sourceBits
}
