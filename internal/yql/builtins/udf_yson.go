package builtins

import (
	"fmt"
	"strings"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

const ysonOptionsResource = "Resource<'Yson2.Options'>"

func resolveYsonCall(name string, args []CallArgument) (model.Type, bool, error) {
	if !isDocumentedYson(name) {
		return model.Type{}, false, nil
	}
	if name == "Yson::Options" {
		result, err := resolveSignatures(name, args, ysonNamedSignatures(name))
		return result, true, err
	}
	if name == "Yson::SerializeJson" {
		if len(args) == 0 {
			return model.Type{}, true, fmt.Errorf("%s expects 1 to 5 arguments, got 0", name)
		}
		_, nullable, err := ysonNodeArgument(name, args[0].Type, 1)
		if err != nil {
			return model.Type{}, true, err
		}
		bound := append([]CallArgument(nil), args...)
		bound[0].Type = withOptional(model.Type{Kind: ysonNodeResource}, nullable)
		result, err := resolveSignatures(name, bound, ysonNamedSignatures(name))
		return result, true, err
	}
	for i, arg := range args {
		if arg.Name != "" {
			return model.Type{}, true, fmt.Errorf("%s does not support named argument %q at position %d", name, arg.Name, i+1)
		}
	}
	plain := make([]model.Type, len(args))
	for i := range args {
		plain[i] = args[i].Type
	}
	switch name {
	case "Yson::From":
		if err := arity(name, plain, 1); err != nil {
			return model.Type{}, true, err
		}
		if err := validateYsonCompatible(plain[0], true); err != nil {
			return model.Type{}, true, fmt.Errorf("%s argument 1: %w", name, err)
		}
		return model.Type{Kind: ysonNodeResource}, true, nil
	case "Yson::ConvertTo":
		if len(args) < 2 || len(args) > 3 {
			return model.Type{}, true, fmt.Errorf("%s expects 2 or 3 arguments, got %d", name, len(args))
		}
		_, nullable, err := ysonNodeArgument(name, plain[0], 1)
		if err != nil {
			return model.Type{}, true, err
		}
		if args[1].TypeArgument == nil {
			return model.Type{}, true, fmt.Errorf("%s argument 2 must be a YQL type expression", name)
		}
		if err := validateYsonCompatible(*args[1].TypeArgument, false); err != nil {
			return model.Type{}, true, fmt.Errorf("%s target type: %w", name, err)
		}
		if len(args) == 3 {
			if err := ysonOptionArgument(name, plain[2], 3); err != nil {
				return model.Type{}, true, err
			}
		}
		return withOptional(*args[1].TypeArgument, nullable), true, nil
	case "Yson::Parse", "Yson::ParseJson", "Yson::ParseJsonDecodeUtf8":
		if len(args) < 1 || len(args) > 2 {
			return model.Type{}, true, fmt.Errorf("%s expects 1 or 2 arguments, got %d", name, len(args))
		}
		base, nullable, err := baseType(plain[0])
		if err != nil {
			return model.Type{}, true, fmt.Errorf("%s argument 1: %w", name, err)
		}
		typed := "Yson"
		if name != "Yson::Parse" {
			typed = "Json"
		}
		if base.Kind != typed && base.Kind != "String" && base.Kind != ysonNodeResource {
			return model.Type{}, true, fmt.Errorf("%s argument 1 must be %s or String, including Optional forms", name, typed)
		}
		if len(args) == 2 {
			if err := ysonOptionArgument(name, plain[1], 2); err != nil {
				return model.Type{}, true, err
			}
		}
		result := model.Type{Kind: ysonNodeResource}
		return withOptional(result, nullable || base.Kind == "String"), true, nil
	}
	result, _ := ysonDocumentedResult(name)
	var err error
	switch {
	case name == "Yson::WithAttributes" || name == "Yson::Equals":
		if err = arity(name, plain, 2); err != nil {
			break
		}
		var nullable bool
		for i := range plain {
			var optional bool
			_, optional, err = ysonNodeArgument(name, plain[i], i+1)
			if err != nil {
				break
			}
			nullable = nullable || optional
		}
		result = withOptional(result, nullable)
	case name == "Yson::Contains" || strings.HasPrefix(name, "Yson::Lookup") || strings.HasPrefix(name, "Yson::YPath"):
		if len(args) < 2 || len(args) > 3 {
			err = fmt.Errorf("%s expects 2 or 3 arguments, got %d", name, len(args))
			break
		}
		_, _, err = ysonNodeArgument(name, plain[0], 1)
		if err == nil {
			err = requireExact(name, plain, 1, "String")
		}
		if err == nil && len(args) == 3 {
			err = ysonOptionArgument(name, plain[2], 3)
		}
	case name == "Yson::GetLength" || strings.HasPrefix(name, "Yson::ConvertTo") && name != "Yson::ConvertTo":
		if len(args) < 1 || len(args) > 2 {
			err = fmt.Errorf("%s expects 1 or 2 arguments, got %d", name, len(args))
			break
		}
		_, _, err = ysonNodeArgument(name, plain[0], 1)
		if err == nil && len(args) == 2 {
			err = ysonOptionArgument(name, plain[1], 2)
		}
	case name == "Yson::Attributes" || name == "Yson::GetHash" || strings.HasPrefix(name, "Yson::Is") || strings.HasPrefix(name, "Yson::Serialize"):
		if err = arity(name, plain, 1); err != nil {
			break
		}
		var nullable bool
		_, nullable, err = ysonNodeArgument(name, plain[0], 1)
		result = withOptional(result, nullable)
	default:
		err = fmt.Errorf("unsupported YQL function %q", name)
	}
	if err != nil {
		return model.Type{}, true, err
	}
	return result, true, nil
}

func isDocumentedYson(name string) bool {
	switch name {
	case "Yson::Options", "Yson::From", "Yson::ConvertTo", "Yson::Parse", "Yson::ParseJson", "Yson::ParseJsonDecodeUtf8", "Yson::SerializeJson":
		return true
	}
	_, ok := ysonDocumentedResult(name)
	return ok
}

func ysonDocumentedResult(name string) (model.Type, bool) {
	t := func(kind string) model.Type { return model.Type{Kind: kind} }
	list := func(item model.Type) model.Type { return model.Type{Kind: "List", Elem: &item} }
	dict := func(item model.Type) model.Type {
		key := t("String")
		return model.Type{Kind: "Dict", Key: &key, Elem: &item}
	}
	node := t(ysonNodeResource)
	if strings.HasPrefix(name, "Yson::ConvertTo") {
		suffix := strings.TrimPrefix(name, "Yson::ConvertTo")
		if suffix == "List" {
			return list(node), true
		}
		if suffix == "Dict" {
			return dict(node), true
		}
		for _, kind := range []string{"Bool", "Int64", "Uint64", "Double", "String"} {
			if suffix == kind {
				return model.Optional(t(kind)), true
			}
			if suffix == kind+"List" {
				return list(t(kind)), true
			}
			if suffix == kind+"Dict" {
				return dict(t(kind)), true
			}
		}
	}
	if strings.HasPrefix(name, "Yson::Lookup") || strings.HasPrefix(name, "Yson::YPath") {
		suffix := strings.TrimPrefix(name, "Yson::Lookup")
		if strings.HasPrefix(name, "Yson::YPath") {
			suffix = strings.TrimPrefix(name, "Yson::YPath")
		}
		switch suffix {
		case "":
			return model.Optional(node), true
		case "List":
			return model.Optional(list(node)), true
		case "Dict":
			return model.Optional(dict(node)), true
		case "Bool", "Int64", "Uint64", "Double", "String":
			return model.Optional(t(suffix)), true
		}
	}
	switch name {
	case "Yson::WithAttributes":
		return model.Optional(node), true
	case "Yson::Equals", "Yson::IsEntity", "Yson::IsString", "Yson::IsDouble", "Yson::IsUint64", "Yson::IsInt64", "Yson::IsBool", "Yson::IsList", "Yson::IsDict":
		return t("Bool"), true
	case "Yson::GetHash":
		return t("Uint64"), true
	case "Yson::GetLength":
		return model.Optional(t("Uint64")), true
	case "Yson::Contains":
		return model.Optional(t("Bool")), true
	case "Yson::Attributes":
		return dict(node), true
	case "Yson::Serialize", "Yson::SerializeText", "Yson::SerializePretty":
		return t("Yson"), true
	default:
		return model.Type{}, false
	}
}

func ysonNamedSignatures(name string) []Signature {
	boolean := model.Optional(model.Type{Kind: "Bool"})
	if name == "Yson::Options" {
		return []Signature{{Name: name, Arguments: []Parameter{
			{Name: "AutoConvert", Type: boolean, Optional: true},
			{Name: "Strict", Type: boolean, Optional: true},
		}, Returns: model.Type{Kind: ysonOptionsResource}}}
	}
	if name == "Yson::SerializeJson" {
		return []Signature{{Name: name, Arguments: []Parameter{
			{Type: model.Type{Kind: ysonNodeResource}, AutoMap: true},
			{Type: model.Optional(model.Type{Kind: ysonOptionsResource}), Optional: true},
			{Name: "SkipMapEntity", Type: boolean, Optional: true},
			{Name: "EncodeUtf8", Type: boolean, Optional: true},
			{Name: "WriteNanAsString", Type: boolean, Optional: true},
		}, Returns: model.Optional(model.Type{Kind: "Json"})}}
	}
	return nil
}

func ysonNodeArgument(name string, value model.Type, index int) (model.Type, bool, error) {
	base, nullable, err := baseType(value)
	if err != nil {
		return model.Type{}, false, fmt.Errorf("%s argument %d: %w", name, index, err)
	}
	if base.Kind == "Null" {
		return base, true, nil
	}
	if base.Kind != ysonNodeResource && base.Kind != "Yson" && base.Kind != "Json" {
		return model.Type{}, false, fmt.Errorf("%s argument %d must be a Yson node Resource, Yson, or Json, including Optional forms", name, index)
	}
	return base, nullable, nil
}

func ysonOptionArgument(name string, value model.Type, index int) error {
	base, _, err := baseType(value)
	if err != nil || base.Kind != ysonOptionsResource && base.Kind != "Null" {
		return fmt.Errorf("%s argument %d must be Yson::Options resource or Null", name, index)
	}
	return nil
}

func validateYsonCompatible(value model.Type, source bool) error {
	if err := validateConcreteOrNull(value); err != nil {
		return err
	}
	switch value.Kind {
	case "Optional", "List":
		return validateYsonCompatible(*value.Elem, source)
	case "Dict":
		if value.Key == nil || value.Key.IsOptional() || value.Key.Kind != "String" && value.Key.Kind != "Utf8" {
			return fmt.Errorf("YSON dictionary key must be non-optional String or Utf8")
		}
		return validateYsonCompatible(*value.Elem, source)
	case "Tuple":
		for _, item := range value.Items {
			if err := validateYsonCompatible(item, source); err != nil {
				return err
			}
		}
		return nil
	case "Struct":
		for _, field := range value.Fields {
			if err := validateYsonCompatible(field.Type, source); err != nil {
				return fmt.Errorf("field %q: %w", field.Name, err)
			}
		}
		return nil
	case "Null", "Void":
		if source {
			return nil
		}
	case "String", "Utf8", "Bool", "Int8", "Int16", "Int32", "Int64", "Uint8", "Uint16", "Uint32", "Uint64", "Float", "Double", "Yson", "Json", ysonNodeResource:
		return nil
	}
	return fmt.Errorf("type %s is not supported by Yson::From/ConvertTo", value.String())
}
