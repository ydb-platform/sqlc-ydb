package builtins

import (
	"fmt"
	"strings"

	"github.com/ydb-platform/sqlc-engine-ydb/internal/model"
)

// Resolve validates a supported YQL function call and returns its concrete
// result type. SQL built-ins are case-insensitive; C++ library module and
// function names use their documented case-sensitive Module::Function spelling.
func Resolve(name string, args []model.Type) (model.Type, error) {
	if strings.Contains(name, "::") {
		return resolveLibrary(name, args)
	}

	switch strings.ToUpper(name) {
	case "COALESCE", "NVL":
		return resolveCoalesce(name, args)
	case "IF":
		return resolveIf(args)
	case "LENGTH", "LEN":
		return resolveLength(name, args)
	case "SUBSTRING":
		return resolveSubstring(name, args)
	case "FIND", "RFIND":
		return resolveFind(name, args)
	case "STARTSWITH", "ENDSWITH":
		return resolveAffix(name, args)
	case "ABS":
		return resolveAbs(args)
	case "COUNT":
		return resolveCount(args)
	case "MIN", "MAX":
		return resolveMinMax(name, args)
	case "SUM":
		return resolveSum(args)
	case "AVG":
		return resolveAvg(args)
	default:
		return model.Type{}, fmt.Errorf("unsupported YQL function %q", name)
	}
}

func resolveCoalesce(name string, args []model.Type) (model.Type, error) {
	if len(args) == 0 {
		return model.Type{}, fmt.Errorf("%s expects at least 1 argument", name)
	}
	result, err := CommonType(args...)
	if err != nil {
		return model.Type{}, fmt.Errorf("%s: %w", name, err)
	}
	for _, arg := range args {
		base, isOptional, baseErr := baseType(arg)
		if baseErr != nil {
			return model.Type{}, fmt.Errorf("%s: %w", name, baseErr)
		}
		if base.Kind != "Null" && !isOptional {
			return result.UnwrapOptional(), nil
		}
	}
	return result, nil
}

func resolveIf(args []model.Type) (model.Type, error) {
	if len(args) != 2 && len(args) != 3 {
		return model.Type{}, fmt.Errorf("IF expects 2 or 3 arguments, got %d", len(args))
	}
	condition, _, err := baseType(args[0])
	if err != nil || condition.Kind != "Bool" {
		return model.Type{}, fmt.Errorf("IF condition must be Bool or Optional<Bool>")
	}
	branches := args[1:]
	if len(args) == 2 {
		branches = []model.Type{args[1], {Kind: "Null"}}
	}
	result, err := CommonType(branches...)
	if err != nil {
		return model.Type{}, fmt.Errorf("IF branches: %w", err)
	}
	return result, nil
}

func resolveLength(name string, args []model.Type) (model.Type, error) {
	if err := arity(name, args, 1); err != nil {
		return model.Type{}, err
	}
	_, nullable, err := stringArgument(name, args, 0, true)
	if err != nil {
		return model.Type{}, err
	}
	return withOptional(model.Type{Kind: "Uint32"}, nullable), nil
}

func resolveSubstring(name string, args []model.Type) (model.Type, error) {
	if len(args) != 2 && len(args) != 3 {
		return model.Type{}, fmt.Errorf("%s expects 2 or 3 arguments, got %d", name, len(args))
	}
	source, nullable, err := stringArgument(name, args, 0, true)
	if err != nil {
		return model.Type{}, err
	}
	for i := 1; i < len(args); i++ {
		if err := integerOrNullArgument(name, args, i); err != nil {
			return model.Type{}, err
		}
	}
	return withOptional(source, nullable), nil
}

func resolveFind(name string, args []model.Type) (model.Type, error) {
	if len(args) != 2 && len(args) != 3 {
		return model.Type{}, fmt.Errorf("%s expects 2 or 3 arguments, got %d", name, len(args))
	}
	left, _, err := stringOrNullArgument(name, args, 0)
	if err != nil {
		return model.Type{}, err
	}
	right, _, err := stringOrNullArgument(name, args, 1)
	if err != nil {
		return model.Type{}, err
	}
	if left.Kind == "Null" && right.Kind == "Null" {
		return model.Type{}, fmt.Errorf("%s cannot infer String or Utf8 from two Null arguments", name)
	}
	if left.Kind != "Null" && right.Kind != "Null" && left.Kind != right.Kind {
		return model.Type{}, fmt.Errorf("%s arguments 1 and 2 must have the same string type", name)
	}
	if len(args) == 3 {
		if err := integerOrNullArgument(name, args, 2); err != nil {
			return model.Type{}, err
		}
	}
	return optional(model.Type{Kind: "Uint32"}), nil
}

func resolveAffix(name string, args []model.Type) (model.Type, error) {
	if err := arity(name, args, 2); err != nil {
		return model.Type{}, err
	}
	left, leftNullable, err := stringOrNullArgument(name, args, 0)
	if err != nil {
		return model.Type{}, err
	}
	right, rightNullable, err := stringOrNullArgument(name, args, 1)
	if err != nil {
		return model.Type{}, err
	}
	if left.Kind == "Null" && right.Kind == "Null" {
		return model.Type{}, fmt.Errorf("%s cannot infer String or Utf8 from two Null arguments", name)
	}
	if left.Kind != "Null" && right.Kind != "Null" && left.Kind != right.Kind {
		return model.Type{}, fmt.Errorf("%s arguments 1 and 2 must have the same string type", name)
	}
	return withOptional(model.Type{Kind: "Bool"}, leftNullable || rightNullable), nil
}

func resolveAbs(args []model.Type) (model.Type, error) {
	if err := arity("ABS", args, 1); err != nil {
		return model.Type{}, err
	}
	base, nullable, err := baseType(args[0])
	if err != nil || !isPrimitiveNumber(base.Kind) {
		return model.Type{}, fmt.Errorf("ABS argument 1 must be numeric")
	}
	return withOptional(base, nullable), nil
}

func resolveCount(args []model.Type) (model.Type, error) {
	if len(args) > 1 {
		return model.Type{}, fmt.Errorf("COUNT expects 0 or 1 argument, got %d", len(args))
	}
	if len(args) == 1 {
		if _, _, err := baseType(args[0]); err != nil {
			return model.Type{}, fmt.Errorf("COUNT: %w", err)
		}
	}
	return model.Type{Kind: "Uint64"}, nil
}

func resolveMinMax(name string, args []model.Type) (model.Type, error) {
	if err := arity(name, args, 1); err != nil {
		return model.Type{}, err
	}
	base, _, err := baseType(args[0])
	if err != nil || (!isPrimitiveNumber(base.Kind) && base.Kind != "String" && base.Kind != "Utf8") {
		return model.Type{}, fmt.Errorf("%s argument 1 must be a supported comparable scalar", name)
	}
	return optional(base), nil
}

func resolveSum(args []model.Type) (model.Type, error) {
	if err := arity("SUM", args, 1); err != nil {
		return model.Type{}, err
	}
	base, _, err := baseType(args[0])
	if err != nil || !isPrimitiveNumber(base.Kind) {
		return model.Type{}, fmt.Errorf("SUM argument 1 must be numeric")
	}
	if signed, bits := integerInfo(base.Kind); bits != 0 {
		if signed {
			base = model.Type{Kind: "Int64"}
		} else {
			base = model.Type{Kind: "Uint64"}
		}
	} else if base.Kind == "Decimal" {
		base.Precision = 35
	}
	return optional(base), nil
}

func resolveAvg(args []model.Type) (model.Type, error) {
	if err := arity("AVG", args, 1); err != nil {
		return model.Type{}, err
	}
	base, _, err := baseType(args[0])
	if err != nil || (!isPrimitiveNumber(base.Kind) && base.Kind != "Interval" && base.Kind != "Interval64") {
		return model.Type{}, fmt.Errorf("AVG argument 1 must be numeric or Interval")
	}
	if isInteger(base.Kind) || base.Kind == "Float" || base.Kind == "Interval" || base.Kind == "Interval64" {
		base = model.Type{Kind: "Double"}
	}
	return optional(base), nil
}

func arity(name string, args []model.Type, want int) error {
	if len(args) != want {
		return fmt.Errorf("%s expects %d argument%s, got %d", name, want, plural(want), len(args))
	}
	return nil
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func stringArgument(name string, args []model.Type, index int, optionalAllowed bool) (model.Type, bool, error) {
	base, nullable, err := baseType(args[index])
	if err != nil || (base.Kind != "String" && base.Kind != "Utf8") || (!optionalAllowed && nullable) {
		qualifier := "String or Utf8"
		if !optionalAllowed {
			qualifier = "non-optional String or Utf8"
		}
		return model.Type{}, false, fmt.Errorf("%s argument %d must be %s", name, index+1, qualifier)
	}
	return base, nullable, nil
}

func stringOrNullArgument(name string, args []model.Type, index int) (model.Type, bool, error) {
	base, nullable, err := baseType(args[index])
	if err != nil || (base.Kind != "String" && base.Kind != "Utf8" && base.Kind != "Null") {
		return model.Type{}, false, fmt.Errorf("%s argument %d must be String, Utf8, an Optional string, or Null", name, index+1)
	}
	return base, nullable || base.Kind == "Null", nil
}

func integerOrNullArgument(name string, args []model.Type, index int) error {
	base, _, err := baseType(args[index])
	if err != nil || (base.Kind != "Null" && !isInteger(base.Kind)) {
		return fmt.Errorf("%s argument %d must be an integer, optional integer, or Null", name, index+1)
	}
	return nil
}

func withOptional(value model.Type, nullable bool) model.Type {
	if nullable {
		return optional(value)
	}
	return value
}
