package builtins

import (
	"fmt"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

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

func resolveYsonConvertToStringList(args []model.Type) (model.Type, error) {
	if err := arity("Yson::ConvertToStringList", args, 1); err != nil {
		return model.Type{}, err
	}
	base, _, err := baseType(args[0])
	if err != nil {
		return model.Type{}, fmt.Errorf("Yson::ConvertToStringList: %w", err)
	}
	if base.Kind != "Json" && base.Kind != "Yson" && base.Kind != "Null" {
		return model.Type{}, fmt.Errorf("Yson::ConvertToStringList argument 1 must be Json or Yson, including an Optional form")
	}
	item := model.Type{Kind: "String"}
	return model.Type{Kind: "List", Elem: &item}, nil
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
