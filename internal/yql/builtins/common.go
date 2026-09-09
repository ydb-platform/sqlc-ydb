// Package builtins resolves the deliberately supported subset of YQL built-in
// and C++ library functions to concrete model types.
package builtins

import (
	"fmt"
	"strings"

	"github.com/ydb-platform/sqlc-engine-ydb/internal/model"
)

// CommonType returns the YQL common type used for branch and value
// reconciliation. Null is accepted only as a contextual input. It never escapes
// as a successful result.
func CommonType(types ...model.Type) (model.Type, error) {
	if len(types) == 0 {
		return model.Type{}, fmt.Errorf("common type requires at least one type")
	}

	var common model.Type
	nullable := false
	for _, value := range types {
		base, optional, err := baseType(value)
		if err != nil {
			return model.Type{}, err
		}
		if base.Kind == "Null" {
			nullable = true
			continue
		}
		nullable = nullable || optional
		if common.Kind == "" {
			common = base
			continue
		}
		common, err = commonConcreteType(common, base)
		if err != nil {
			return model.Type{}, err
		}
	}

	if common.Kind == "" {
		return model.Type{}, fmt.Errorf("cannot infer a concrete common type from only Null values")
	}
	if nullable {
		return optional(common), nil
	}
	return common, nil
}

func commonConcreteType(left, right model.Type) (model.Type, error) {
	if equalType(left, right) {
		return left, nil
	}
	if isInteger(left.Kind) && isInteger(right.Kind) {
		return commonInteger(left.Kind, right.Kind), nil
	}
	if isPrimitiveNumber(left.Kind) && isPrimitiveNumber(right.Kind) && left.Kind != "Decimal" && right.Kind != "Decimal" {
		if left.Kind == "Double" || right.Kind == "Double" {
			return model.Type{Kind: "Double"}, nil
		}
		if left.Kind == "Float" || right.Kind == "Float" {
			return model.Type{Kind: "Float"}, nil
		}
	}
	return model.Type{}, fmt.Errorf("types %s and %s have no common type in the supported YQL subset", typeName(left), typeName(right))
}

func commonInteger(left, right string) model.Type {
	leftSigned, leftBits := integerInfo(left)
	rightSigned, rightBits := integerInfo(right)
	if leftSigned == rightSigned {
		if rightBits > leftBits {
			return model.Type{Kind: right}
		}
		return model.Type{Kind: left}
	}
	if !leftSigned {
		leftSigned, rightSigned = rightSigned, leftSigned
		leftBits, rightBits = rightBits, leftBits
	}
	if leftBits >= rightBits {
		return model.Type{Kind: fmt.Sprintf("Int%d", leftBits)}
	}
	return model.Type{Kind: fmt.Sprintf("Uint%d", rightBits)}
}

func baseType(value model.Type) (model.Type, bool, error) {
	if value.Kind == "Optional" {
		if value.Elem == nil {
			return model.Type{}, false, fmt.Errorf("Optional type has no element type")
		}
		if value.Elem.Kind == "Optional" {
			return model.Type{}, false, fmt.Errorf("nested Optional types are not supported by the resolver")
		}
		if err := validateConcreteOrNull(*value.Elem); err != nil {
			return model.Type{}, false, err
		}
		return *value.Elem, true, nil
	}
	if err := validateConcreteOrNull(value); err != nil {
		return model.Type{}, false, err
	}
	return value, false, nil
}

func validateConcreteOrNull(value model.Type) error {
	if value.Kind == "" || value.Kind == "Any" {
		return fmt.Errorf("unsupported type %q cannot participate in resolution", value.Kind)
	}
	switch value.Kind {
	case "Optional", "List", "Stream", "Flow", "Set":
		if value.Elem == nil {
			return fmt.Errorf("%s type has no element type", value.Kind)
		}
		if value.Key != nil || len(value.Items) != 0 || value.Precision != 0 || value.Scale != 0 {
			return fmt.Errorf("%s has unexpected type parameters", value.Kind)
		}
		return validateConcreteOrNull(*value.Elem)
	case "Dict":
		if value.Key == nil || value.Elem == nil {
			return fmt.Errorf("Dict type requires key and element types")
		}
		if len(value.Items) != 0 || value.Precision != 0 || value.Scale != 0 {
			return fmt.Errorf("Dict has unexpected type parameters")
		}
		if err := validateConcreteOrNull(*value.Key); err != nil {
			return err
		}
		return validateConcreteOrNull(*value.Elem)
	case "Tuple":
		if len(value.Items) == 0 {
			return fmt.Errorf("Tuple type requires at least one item type")
		}
		if value.Key != nil || value.Elem != nil || value.Precision != 0 || value.Scale != 0 {
			return fmt.Errorf("Tuple has unexpected type parameters")
		}
		for _, item := range value.Items {
			if err := validateConcreteOrNull(item); err != nil {
				return err
			}
		}
		return nil
	case "Decimal":
		if value.Precision < 1 || value.Precision > 35 || value.Scale < 0 || value.Scale > value.Precision {
			return fmt.Errorf("invalid Decimal(%d,%d): precision must be 1..35 and scale 0..precision", value.Precision, value.Scale)
		}
		if value.Key != nil || value.Elem != nil || len(value.Items) != 0 {
			return fmt.Errorf("Decimal has unexpected type parameters")
		}
		return nil
	default:
		if !supportedScalarKinds[value.Kind] {
			return fmt.Errorf("unsupported type %q cannot participate in resolution", value.Kind)
		}
		if value.Key != nil || value.Elem != nil || len(value.Items) != 0 || value.Precision != 0 || value.Scale != 0 {
			return fmt.Errorf("%s has unexpected type parameters", value.Kind)
		}
		return nil
	}
}

var supportedScalarKinds = map[string]bool{
	"Bool": true, "Int8": true, "Int16": true, "Int32": true, "Int64": true,
	"Uint8": true, "Uint16": true, "Uint32": true, "Uint64": true,
	"Float": true, "Double": true, "String": true, "Utf8": true,
	"Yson": true, "Json": true, "JsonDocument": true, "Uuid": true, "DyNumber": true,
	"Date": true, "Datetime": true, "Timestamp": true, "Interval": true,
	"Date32": true, "Datetime64": true, "Timestamp64": true, "Interval64": true,
	"TzDate": true, "TzDatetime": true, "TzTimestamp": true,
	"TzDate32": true, "TzDatetime64": true, "TzTimestamp64": true,
	"Null": true,
}

func optional(value model.Type) model.Type {
	if value.Kind == "Optional" {
		return value
	}
	return model.Optional(value)
}

func isPrimitiveNumber(kind string) bool {
	return isInteger(kind) || kind == "Float" || kind == "Double" || kind == "Decimal"
}

func isInteger(kind string) bool {
	_, bits := integerInfo(kind)
	return bits != 0
}

func integerInfo(kind string) (signed bool, bits int) {
	switch kind {
	case "Int8":
		return true, 8
	case "Int16":
		return true, 16
	case "Int32":
		return true, 32
	case "Int64":
		return true, 64
	case "Uint8":
		return false, 8
	case "Uint16":
		return false, 16
	case "Uint32":
		return false, 32
	case "Uint64":
		return false, 64
	default:
		return false, 0
	}
}

func equalType(left, right model.Type) bool {
	if left.Kind != right.Kind || left.Precision != right.Precision || left.Scale != right.Scale {
		return false
	}
	if (left.Elem == nil) != (right.Elem == nil) || (left.Key == nil) != (right.Key == nil) || len(left.Items) != len(right.Items) {
		return false
	}
	if left.Elem != nil && !equalType(*left.Elem, *right.Elem) {
		return false
	}
	if left.Key != nil && !equalType(*left.Key, *right.Key) {
		return false
	}
	for i := range left.Items {
		if !equalType(left.Items[i], right.Items[i]) {
			return false
		}
	}
	return true
}

func typeName(value model.Type) string {
	switch value.Kind {
	case "Optional", "List", "Stream", "Flow", "Set":
		if value.Elem != nil {
			return value.Kind + "<" + typeName(*value.Elem) + ">"
		}
	case "Dict":
		if value.Key != nil && value.Elem != nil {
			return "Dict<" + typeName(*value.Key) + "," + typeName(*value.Elem) + ">"
		}
	case "Tuple":
		items := make([]string, len(value.Items))
		for i := range value.Items {
			items[i] = typeName(value.Items[i])
		}
		return "Tuple<" + strings.Join(items, ",") + ">"
	case "Decimal":
		return fmt.Sprintf("Decimal(%d,%d)", value.Precision, value.Scale)
	}
	return value.Kind
}
