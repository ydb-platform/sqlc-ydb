package builtins

import (
	"fmt"
	"strings"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

// Resolve validates a supported YQL function call and returns its concrete
// result type. SQL built-ins are case-insensitive; C++ library module and
// function names use their documented case-sensitive Module::Function spelling.
func Resolve(name string, args []model.Type) (model.Type, error) {
	callArgs := make([]CallArgument, len(args))
	for i := range args {
		callArgs[i].Type = args[i]
	}
	return defaultRegistry.ResolveCall(name, callArgs)
}

func resolveLegacy(name string, args []model.Type) (model.Type, error) {
	if result, handled, err := resolveHistogramAggregate(name, args); handled {
		return result, err
	}
	if strings.Contains(name, "::") {
		return resolveLibrary(name, args)
	}

	if resolve := lookupCore(name); resolve != nil {
		return resolve(args)
	}
	return model.Type{}, fmt.Errorf("unsupported YQL function %q", name)
}

type functionResolver func([]model.Type) (model.Type, error)

func lookupCore(name string) functionResolver {
	switch strings.ToUpper(name) {
	case "IF":
		return resolveIf
	case "NANVL":
		return resolveNanvl
	case "CURRENTUTCDATE":
		return func(args []model.Type) (model.Type, error) { return resolveDependencyValue(name, "Date", 0, args) }
	case "CURRENTUTCDATETIME":
		return func(args []model.Type) (model.Type, error) { return resolveDependencyValue(name, "Datetime", 0, args) }
	case "CURRENTUTCTIMESTAMP":
		return func(args []model.Type) (model.Type, error) { return resolveDependencyValue(name, "Timestamp", 0, args) }
	case "CURRENTTZDATE":
		return func(args []model.Type) (model.Type, error) { return resolveCurrentTimezone(name, "TzDate", args) }
	case "CURRENTTZDATETIME":
		return func(args []model.Type) (model.Type, error) { return resolveCurrentTimezone(name, "TzDatetime", args) }
	case "CURRENTTZTIMESTAMP":
		return func(args []model.Type) (model.Type, error) { return resolveCurrentTimezone(name, "TzTimestamp", args) }
	case "RANDOM":
		return func(args []model.Type) (model.Type, error) { return resolveDependencyValue(name, "Double", 1, args) }
	case "RANDOMNUMBER":
		return func(args []model.Type) (model.Type, error) { return resolveDependencyValue(name, "Uint64", 1, args) }
	case "RANDOMUUID":
		return func(args []model.Type) (model.Type, error) { return resolveDependencyValue(name, "Uuid", 1, args) }
	case "VERSION":
		return func(args []model.Type) (model.Type, error) {
			if err := arity(name, args, 0); err != nil {
				return model.Type{}, err
			}
			return model.Type{Kind: "String"}, nil
		}
	case "LENGTH", "LEN":
		return func(args []model.Type) (model.Type, error) { return resolveLength(name, args) }
	case "SUBSTRING":
		return func(args []model.Type) (model.Type, error) { return resolveSubstring(name, args) }
	case "FIND", "RFIND":
		return func(args []model.Type) (model.Type, error) { return resolveFind(name, args) }
	case "STARTSWITH", "ENDSWITH":
		return func(args []model.Type) (model.Type, error) { return resolveAffix(name, args) }
	case "ABS":
		return resolveAbs
	case "TOSET":
		return resolveToSet
	case "LISTCREATE":
		return resolveListCreate
	case "SETISDISJOINT":
		return resolveSetIsDisjoint
	case "COUNT":
		return resolveCount
	case "COUNT_IF":
		return resolveCountIf
	case "MIN", "MAX":
		return func(args []model.Type) (model.Type, error) { return resolveMinMax(name, args) }
	case "SUM":
		return resolveSum
	case "AVG":
		return resolveAvg
	case "AGGREGATE_LIST", "AGG_LIST", "AGGREGATE_LIST_DISTINCT", "AGG_LIST_DISTINCT":
		return resolveAggregateList
	default:
		return nil
	}
}

// Clock and random arguments control evaluation dependencies, not result
// nullability. Their types must still be resolved before generation.
func resolveDependencyValue(name, kind string, minimum int, args []model.Type) (model.Type, error) {
	if len(args) < minimum {
		return model.Type{}, fmt.Errorf("%s requires at least %d dependency argument", name, minimum)
	}
	for _, arg := range args {
		if err := validateConcreteOrNull(arg); err != nil {
			return model.Type{}, err
		}
	}
	return model.Type{Kind: kind}, nil
}

func resolveCurrentTimezone(name, kind string, args []model.Type) (model.Type, error) {
	if len(args) == 0 {
		return model.Type{}, fmt.Errorf("%s requires a String timezone argument", name)
	}
	zone, _, err := baseType(args[0])
	if err != nil || (zone.Kind != "String" && zone.Kind != "Null") {
		return model.Type{}, fmt.Errorf("%s timezone must be String or Optional<String>", name)
	}
	result, err := resolveDependencyValue(name, kind, 0, args[1:])
	if err != nil {
		return model.Type{}, err
	}
	// Zone names are evaluated by YDB; an unknown zone produces NULL.
	return model.Optional(result), nil
}

func resolveCountIf(args []model.Type) (model.Type, error) {
	if err := arity("COUNT_IF", args, 1); err != nil {
		return model.Type{}, err
	}
	base, _, err := baseType(args[0])
	if err != nil || (base.Kind != "Bool" && base.Kind != "Null") {
		return model.Type{}, fmt.Errorf("COUNT_IF argument must be Bool or Optional<Bool>")
	}
	return model.Type{Kind: "Uint64"}, nil
}

func resolveAggregateList(args []model.Type) (model.Type, error) {
	if len(args) < 1 || len(args) > 2 {
		return model.Type{}, fmt.Errorf("AGGREGATE_LIST expects 1 or 2 arguments, got %d", len(args))
	}
	item, _, err := baseType(args[0])
	if err != nil || item.Kind == "Null" || item.Kind == "Void" {
		return model.Type{}, fmt.Errorf("AGGREGATE_LIST argument 1 needs a concrete value type")
	}
	if len(args) == 2 {
		limit, nullable, err := baseType(args[1])
		if err != nil || nullable {
			return model.Type{}, fmt.Errorf("AGGREGATE_LIST limit must be a non-optional integer convertible to Uint64")
		}
		switch limit.Kind {
		case "Int8", "Int16", "Int32", "Uint8", "Uint16", "Uint32", "Uint64":
		default:
			return model.Type{}, fmt.Errorf("AGGREGATE_LIST limit must be a non-optional integer convertible to Uint64")
		}
	}
	return model.Type{Kind: "List", Elem: &item}, nil
}

func resolveNanvl(args []model.Type) (model.Type, error) {
	if err := arity("NANVL", args, 2); err != nil {
		return model.Type{}, err
	}
	for _, arg := range args {
		base, _, err := baseType(arg)
		if err != nil || (base.Kind != "Float" && base.Kind != "Double") {
			return model.Type{}, fmt.Errorf("NANVL arguments must be Float or Double, including Optional forms")
		}
	}
	return CommonType(args...)
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
	base, nullable, err := baseType(args[0])
	if err != nil || (base.Kind != "String" && base.Kind != "Utf8") {
		return model.Type{}, fmt.Errorf("%s argument 1 must be String or Utf8", name)
	}
	return withOptional(model.Type{Kind: "Uint32"}, nullable), nil
}

func resolveSubstring(name string, args []model.Type) (model.Type, error) {
	if len(args) != 2 && len(args) != 3 {
		return model.Type{}, fmt.Errorf("%s expects 2 or 3 arguments, got %d", name, len(args))
	}
	source, nullable, err := baseType(args[0])
	if err != nil || source.Kind != "String" {
		return model.Type{}, fmt.Errorf("%s argument 1 must be String or Optional<String>; use Unicode::Substring for Utf8", name)
	}
	for i := 1; i < len(args); i++ {
		if err := coreStringPositionArgument(name, args, i); err != nil {
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
		if err := coreStringPositionArgument(name, args, 2); err != nil {
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

func stringOrNullArgument(name string, args []model.Type, index int) (model.Type, bool, error) {
	base, nullable, err := baseType(args[index])
	if err != nil || (base.Kind != "String" && base.Kind != "Utf8" && base.Kind != "Null") {
		return model.Type{}, false, fmt.Errorf("%s argument %d must be String, Utf8, an Optional string, or Null", name, index+1)
	}
	return base, nullable || base.Kind == "Null", nil
}

func coreStringPositionArgument(name string, args []model.Type, index int) error {
	base, _, err := baseType(args[index])
	if err != nil || (base.Kind != "Null" && base.Kind != "Uint8" && base.Kind != "Uint16" && base.Kind != "Uint32") {
		return fmt.Errorf("%s argument %d must be Null, Uint8, Uint16, Uint32, or an Optional of one of those types; use CAST(... AS Uint32) for other integer types", name, index+1)
	}
	return nil
}

func withOptional(value model.Type, nullable bool) model.Type {
	if nullable {
		return optional(value)
	}
	return value
}
