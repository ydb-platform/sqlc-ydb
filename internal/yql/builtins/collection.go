package builtins

import (
	"fmt"
	"strings"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func resolveAsTuple(args []model.Type) (model.Type, error) {
	if len(args) == 0 {
		return model.Type{}, fmt.Errorf("AsTuple produces an empty Tuple that cannot be represented by the offline resolver")
	}
	for i, arg := range args {
		if err := validateConcreteOrNull(arg); err != nil {
			return model.Type{}, fmt.Errorf("AsTuple argument %d: %w", i+1, err)
		}
	}
	return model.Type{Kind: "Tuple", Items: args}, nil
}

func resolveAsStruct(args []CallArgument) (model.Type, error) {
	if len(args) == 0 {
		return model.Type{}, fmt.Errorf("AsStruct produces an empty Struct that cannot be represented by the offline resolver")
	}
	fields := make([]model.StructField, 0, len(args))
	seen := make(map[string]bool, len(args))
	for i, arg := range args {
		if arg.Name == "" {
			return model.Type{}, fmt.Errorf("AsStruct argument %d requires a field name with AS", i+1)
		}
		if seen[arg.Name] {
			return model.Type{}, fmt.Errorf("AsStruct has duplicate field name %q", arg.Name)
		}
		if err := validateConcreteOrNull(arg.Type); err != nil {
			return model.Type{}, fmt.Errorf("AsStruct field %q: %w", arg.Name, err)
		}
		seen[arg.Name] = true
		fields = append(fields, model.StructField{Name: arg.Name, Type: arg.Type})
	}
	return model.Type{Kind: "Struct", Fields: fields}, nil
}

func resolveAsList(args []model.Type) (model.Type, error) {
	if len(args) == 0 {
		return model.Type{}, fmt.Errorf("AsList produces an empty List with no element type; use ListCreate")
	}
	elem, err := CommonType(args...)
	if err != nil {
		return model.Type{}, fmt.Errorf("AsList element common type: %w", err)
	}
	return model.Type{Kind: "List", Elem: &elem}, nil
}

func listArgument(name string, arg model.Type) (model.Type, bool, error) {
	list, nullable, err := collectionBase(arg)
	if err != nil || list.Kind != "List" || list.Elem == nil {
		return model.Type{}, false, fmt.Errorf("%s argument 1 must be List<T> or Optional<List<T>>", name)
	}
	if err := validateConcreteOrNull(*list.Elem); err != nil {
		return model.Type{}, false, fmt.Errorf("%s argument 1: %w", name, err)
	}
	return *list.Elem, nullable, nil
}

func resolveListLength(args []model.Type) (model.Type, error) {
	if err := arity("ListLength", args, 1); err != nil {
		return model.Type{}, err
	}
	_, nullable, err := listArgument("ListLength", args[0])
	if err != nil {
		return model.Type{}, err
	}
	return withOptional(model.Type{Kind: "Uint64"}, nullable), nil
}

func resolveListHas(args []model.Type) (model.Type, error) {
	if err := arity("ListHas", args, 2); err != nil {
		return model.Type{}, err
	}
	elem, _, err := listArgument("ListHas", args[0])
	if err != nil {
		return model.Type{}, err
	}
	switch elem.UnwrapOptional().Kind {
	case "Json", "JsonDocument", "Yson":
		return model.Type{}, fmt.Errorf("ListHas requires an equatable element type, got %s", elem.String())
	}
	if err := exactCollectionValue(elem, args[1]); err != nil {
		return model.Type{}, fmt.Errorf("ListHas argument 2 must match element type %s: %w", elem.String(), err)
	}
	return model.Type{Kind: "Bool"}, nil
}

func resolveListTransform(name string, args []model.Type) (model.Type, error) {
	if err := arity(name, args, 2); err != nil {
		return model.Type{}, err
	}
	elem, nullable, err := listArgument(name, args[0])
	if err != nil {
		return model.Type{}, err
	}
	callback := args[1]
	if callback.Kind != "Callable" || len(callback.Items) != 1 || callback.Elem == nil {
		return model.Type{}, fmt.Errorf("%s argument 2 must be a one-argument Callable", name)
	}
	if !callback.Items[0].Equal(elem) {
		return model.Type{}, fmt.Errorf("%s callback argument must be %s, got %s", name, elem.String(), callback.Items[0].String())
	}
	if err := validateConcreteOrNull(*callback.Elem); err != nil {
		return model.Type{}, fmt.Errorf("%s callback result: %w", name, err)
	}
	if strings.EqualFold(name, "ListFilter") {
		predicate, _, err := baseType(*callback.Elem)
		if err != nil || predicate.Kind != "Bool" {
			return model.Type{}, fmt.Errorf("ListFilter callback must return Bool or Optional<Bool>")
		}
		return withOptional(args[0].UnwrapOptional(), nullable), nil
	}
	if callback.Elem.Kind == "Null" || callback.Elem.Kind == "Void" {
		return model.Type{}, fmt.Errorf("ListMap callback must return a concrete value type")
	}
	result := *callback.Elem
	return withOptional(model.Type{Kind: "List", Elem: &result}, nullable), nil
}

func resolveToDict(args []model.Type) (model.Type, error) {
	if err := arity("ToDict", args, 1); err != nil {
		return model.Type{}, err
	}
	pair, nullable, err := listArgument("ToDict", args[0])
	if err != nil {
		return model.Type{}, err
	}
	if pair.Kind != "Tuple" || len(pair.Items) != 2 {
		return model.Type{}, fmt.Errorf("ToDict argument 1 must be List<Tuple<K,V>>")
	}
	if err := validateDictionaryKey(pair.Items[0]); err != nil {
		return model.Type{}, fmt.Errorf("ToDict argument 1: %w", err)
	}
	key, value := pair.Items[0], pair.Items[1]
	return withOptional(model.Type{Kind: "Dict", Key: &key, Elem: &value}, nullable), nil
}

func resolveDictAccess(name string, args []model.Type) (model.Type, error) {
	if err := arity(name, args, 2); err != nil {
		return model.Type{}, err
	}
	dict, _, err := collectionBase(args[0])
	if err != nil || dict.Kind != "Dict" || dict.Key == nil || dict.Elem == nil {
		return model.Type{}, fmt.Errorf("%s argument 1 must be Dict<K,V> or Optional<Dict<K,V>>", name)
	}
	if err := validateDictionaryKey(*dict.Key); err != nil {
		return model.Type{}, fmt.Errorf("%s argument 1: %w", name, err)
	}
	if err := validateConcreteOrNull(*dict.Elem); err != nil {
		return model.Type{}, fmt.Errorf("%s argument 1: %w", name, err)
	}
	if err := exactCollectionValue(*dict.Key, args[1]); err != nil {
		return model.Type{}, fmt.Errorf("%s argument 2 must match key type %s: %w", name, dict.Key.String(), err)
	}
	if strings.EqualFold(name, "DictContains") {
		return model.Type{Kind: "Bool"}, nil
	}
	return model.Optional(*dict.Elem), nil
}

func exactCollectionValue(expected, actual model.Type) error {
	if err := validateConcreteOrNull(actual); err != nil {
		return err
	}
	if expected.Kind == "Optional" && actual.Kind == "Null" {
		return nil
	}
	expectedBase, actualBase := expected.UnwrapOptional(), actual.UnwrapOptional()
	if expectedBase.Equal(actualBase) || ((expectedBase.Kind == "String" || expectedBase.Kind == "Utf8") && (actualBase.Kind == "String" || actualBase.Kind == "Utf8")) {
		return nil
	}
	return fmt.Errorf("got %s", actual.String())
}

func resolveListCreate(args []model.Type) (model.Type, error) {
	if err := arity("ListCreate", args, 1); err != nil {
		return model.Type{}, err
	}
	if err := validateConcreteOrNull(args[0]); err != nil || args[0].Kind == "Null" || args[0].Kind == "Void" {
		return model.Type{}, fmt.Errorf("ListCreate requires a concrete element type")
	}
	item := args[0]
	return model.Type{Kind: "List", Elem: &item}, nil
}

func resolveToSet(args []model.Type) (model.Type, error) {
	if err := arity("ToSet", args, 1); err != nil {
		return model.Type{}, err
	}
	list, nullable, err := collectionBase(args[0])
	if err != nil || list.Kind != "List" || list.Elem == nil {
		return model.Type{}, fmt.Errorf("ToSet argument 1 must be List<K> or Optional<List<K>>")
	}
	if err := validateDictionaryKey(*list.Elem); err != nil {
		return model.Type{}, fmt.Errorf("ToSet argument 1: %w", err)
	}
	key := *list.Elem
	value := model.Type{Kind: "Void"}
	return withOptional(model.Type{Kind: "Dict", Key: &key, Elem: &value}, nullable), nil
}

func resolveSetIsDisjoint(args []model.Type) (model.Type, error) {
	if err := arity("SetIsDisjoint", args, 2); err != nil {
		return model.Type{}, err
	}
	left, leftNullable, err := collectionBase(args[0])
	if err != nil || left.Kind != "Dict" || left.Key == nil || left.Elem == nil {
		return model.Type{}, fmt.Errorf("SetIsDisjoint argument 1 must be Dict<K,V> or Optional<Dict<K,V>>")
	}
	if err := validateDictionaryKey(*left.Key); err != nil {
		return model.Type{}, fmt.Errorf("SetIsDisjoint argument 1: %w", err)
	}
	right, rightNullable, err := collectionBase(args[1])
	if err != nil {
		return model.Type{}, fmt.Errorf("SetIsDisjoint argument 2: %w", err)
	}
	var rightKey model.Type
	switch right.Kind {
	case "List":
		if right.Elem == nil {
			return model.Type{}, fmt.Errorf("SetIsDisjoint argument 2 List has no element type")
		}
		rightKey = *right.Elem
	case "Dict":
		if right.Key == nil || right.Elem == nil {
			return model.Type{}, fmt.Errorf("SetIsDisjoint argument 2 Dict requires key and value types")
		}
		rightKey = *right.Key
	default:
		return model.Type{}, fmt.Errorf("SetIsDisjoint argument 2 must be List<K>, Dict<K,V>, or an Optional form")
	}
	if err := validateDictionaryKey(rightKey); err != nil {
		return model.Type{}, fmt.Errorf("SetIsDisjoint argument 2: %w", err)
	}
	if !left.Key.Equal(rightKey) {
		return model.Type{}, fmt.Errorf("SetIsDisjoint arguments must have the same key type, got %s and %s", left.Key.String(), rightKey.String())
	}
	return withOptional(model.Type{Kind: "Bool"}, leftNullable || rightNullable), nil
}

func collectionBase(value model.Type) (model.Type, bool, error) {
	if value.Kind != "Optional" {
		return value, false, nil
	}
	if value.Elem == nil || value.Elem.Kind == "Optional" {
		return model.Type{}, false, fmt.Errorf("malformed or nested Optional collection")
	}
	return *value.Elem, true, nil
}

func validateDictionaryKey(key model.Type) error {
	if key.Kind == "Optional" {
		if key.Elem == nil {
			return fmt.Errorf("dictionary key Optional has no element type")
		}
		if key.Elem.Kind == "Optional" {
			return fmt.Errorf("nested Optional dictionary key is not supported")
		}
		return validateDictionaryKey(*key.Elem)
	}
	if key.Kind == "Tuple" {
		if len(key.Items) < 2 {
			return fmt.Errorf("dictionary key Tuple requires at least two items")
		}
		for _, item := range key.Items {
			if err := validateDictionaryKey(item); err != nil {
				return err
			}
		}
		return nil
	}
	if (!supportedScalarKinds[key.Kind] && key.Kind != "Decimal") || key.Kind == "Json" || key.Kind == "JsonDocument" || key.Kind == "Yson" || key.Kind == "Null" || key.Kind == "Void" {
		return fmt.Errorf("unsupported dictionary key type %s", key.String())
	}
	if err := validateConcreteOrNull(key); err != nil {
		return fmt.Errorf("invalid dictionary key: %w", err)
	}
	return nil
}
