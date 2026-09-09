package builtins

import (
	"fmt"

	"github.com/ydb-platform/sqlc-engine-ydb/internal/model"
)

func resolveLibrary(name string, args []model.Type) (model.Type, error) {
	switch name {
	case "String::Base64Encode", "String::EscapeC", "String::UnescapeC", "String::HexEncode",
		"String::EncodeHtml", "String::DecodeHtml", "String::CgiEscape", "String::CgiUnescape",
		"String::Strip", "String::Collapse", "String::AsciiToLower", "String::AsciiToUpper", "String::AsciiToTitle":
		return autoMapUnary(name, args, "String", "String")
	case "String::Base64Decode", "String::Base64StrictDecode", "String::HexDecode":
		return nullableStringDecoder(name, args)
	case "String::Find", "String::ReverseFind":
		return resolveStringFind(name, args)
	case "String::Substring":
		return resolveLibrarySubstring(name, args, "String")
	case "String::ReplaceAll", "String::ReplaceFirst", "String::ReplaceLast":
		return resolveStringReplace(name, args)
	case "Unicode::IsUtf":
		return exactUnary(name, args, "String", "Bool")
	case "Unicode::GetLength":
		return autoMapUnary(name, args, "Utf8", "Uint64")
	case "Unicode::Find", "Unicode::RFind":
		return resolveUnicodeFind(name, args)
	case "Unicode::Substring":
		if len(args) != 2 && len(args) != 3 {
			return model.Type{}, fmt.Errorf("%s expects 2 or 3 arguments, got %d", name, len(args))
		}
		return resolveLibrarySubstring(name, args, "Utf8")
	case "Unicode::ToLower", "Unicode::ToUpper", "Unicode::ToTitle", "Unicode::Normalize",
		"Unicode::NormalizeNFC", "Unicode::NormalizeNFD", "Unicode::NormalizeNFKC", "Unicode::NormalizeNFKD":
		return autoMapUnary(name, args, "Utf8", "Utf8")
	case "DateTime::GetYear":
		return resolveDateTimeGetYear(name, args)
	case "DateTime::GetDayOfYear":
		return dateTimeComponent(name, args, "Uint16")
	case "DateTime::GetMonth", "DateTime::GetWeekOfYear", "DateTime::GetWeekOfYearIso8601",
		"DateTime::GetDayOfMonth", "DateTime::GetDayOfWeek", "DateTime::GetHour",
		"DateTime::GetMinute", "DateTime::GetSecond":
		return dateTimeComponent(name, args, "Uint8")
	case "DateTime::GetMillisecondOfSecond", "DateTime::GetMicrosecondOfSecond":
		return dateTimeComponent(name, args, "Uint32")
	case "DateTime::GetTimezoneId":
		return dateTimeComponent(name, args, "Uint16")
	case "DateTime::GetMonthName", "DateTime::GetDayOfWeekName", "DateTime::GetTimezoneName":
		return dateTimeComponent(name, args, "String")
	default:
		return model.Type{}, fmt.Errorf("unsupported YQL function %q", name)
	}
}

func autoMapUnary(name string, args []model.Type, input, output string) (model.Type, error) {
	if err := arity(name, args, 1); err != nil {
		return model.Type{}, err
	}
	base, nullable, err := baseType(args[0])
	if err != nil || (base.Kind != input && base.Kind != "Null") {
		return model.Type{}, fmt.Errorf("%s argument 1 must be %s or Optional<%s>", name, input, input)
	}
	return withOptional(model.Type{Kind: output}, nullable || base.Kind == "Null"), nil
}

func exactUnary(name string, args []model.Type, input, output string) (model.Type, error) {
	if err := arity(name, args, 1); err != nil {
		return model.Type{}, err
	}
	base, nullable, err := baseType(args[0])
	if err != nil || nullable || base.Kind != input {
		return model.Type{}, fmt.Errorf("%s argument 1 must be non-optional %s", name, input)
	}
	return model.Type{Kind: output}, nil
}

func nullableStringDecoder(name string, args []model.Type) (model.Type, error) {
	if err := arity(name, args, 1); err != nil {
		return model.Type{}, err
	}
	base, nullable, err := baseType(args[0])
	if err != nil || nullable || base.Kind != "String" {
		return model.Type{}, fmt.Errorf("%s argument 1 must be non-optional String", name)
	}
	return optional(model.Type{Kind: "String"}), nil
}

func resolveStringFind(name string, args []model.Type) (model.Type, error) {
	if len(args) != 2 && len(args) != 3 {
		return model.Type{}, fmt.Errorf("%s expects 2 or 3 arguments, got %d", name, len(args))
	}
	base, nullable, err := baseType(args[0])
	if err != nil || base.Kind != "String" {
		return model.Type{}, fmt.Errorf("%s argument 1 must be String or Optional<String>", name)
	}
	if err := requireExact(name, args, 1, "String"); err != nil {
		return model.Type{}, err
	}
	if len(args) == 3 {
		if err := requireOptionalScalar(name, args, 2, "Uint64"); err != nil {
			return model.Type{}, err
		}
	}
	return withOptional(model.Type{Kind: "Int64"}, nullable), nil
}

func resolveUnicodeFind(name string, args []model.Type) (model.Type, error) {
	if len(args) != 2 && len(args) != 3 {
		return model.Type{}, fmt.Errorf("%s expects 2 or 3 arguments, got %d", name, len(args))
	}
	base, _, err := baseType(args[0])
	if err != nil || base.Kind != "Utf8" {
		return model.Type{}, fmt.Errorf("%s argument 1 must be Utf8 or Optional<Utf8>", name)
	}
	if err := requireExact(name, args, 1, "Utf8"); err != nil {
		return model.Type{}, err
	}
	if len(args) == 3 {
		if err := requireOptionalScalar(name, args, 2, "Uint64"); err != nil {
			return model.Type{}, err
		}
	}
	return optional(model.Type{Kind: "Uint64"}), nil
}

func resolveLibrarySubstring(name string, args []model.Type, stringKind string) (model.Type, error) {
	if len(args) != 2 && len(args) != 3 {
		return model.Type{}, fmt.Errorf("%s expects 2 or 3 arguments, got %d", name, len(args))
	}
	base, nullable, err := baseType(args[0])
	if err != nil || (base.Kind != stringKind && base.Kind != "Null") {
		return model.Type{}, fmt.Errorf("%s argument 1 must be %s or Optional<%s>", name, stringKind, stringKind)
	}
	for i := 1; i < len(args); i++ {
		if err := requireOptionalScalar(name, args, i, "Uint64"); err != nil {
			return model.Type{}, err
		}
	}
	return withOptional(model.Type{Kind: stringKind}, nullable || base.Kind == "Null"), nil
}

func resolveStringReplace(name string, args []model.Type) (model.Type, error) {
	if err := arity(name, args, 3); err != nil {
		return model.Type{}, err
	}
	base, nullable, err := baseType(args[0])
	if err != nil || base.Kind != "String" {
		return model.Type{}, fmt.Errorf("%s argument 1 must be String or Optional<String>", name)
	}
	for i := 1; i < 3; i++ {
		if err := requireExact(name, args, i, "String"); err != nil {
			return model.Type{}, err
		}
	}
	return withOptional(model.Type{Kind: "String"}, nullable), nil
}

func requireExact(name string, args []model.Type, index int, kind string) error {
	base, nullable, err := baseType(args[index])
	if err != nil || nullable || base.Kind != kind {
		return fmt.Errorf("%s argument %d must be non-optional %s", name, index+1, kind)
	}
	return nil
}

func requireOptionalScalar(name string, args []model.Type, index int, kind string) error {
	base, _, err := baseType(args[index])
	if err != nil || (base.Kind != kind && base.Kind != "Null") {
		return fmt.Errorf("%s argument %d must be %s, Optional<%s>, or Null", name, index+1, kind, kind)
	}
	return nil
}

func resolveDateTimeGetYear(name string, args []model.Type) (model.Type, error) {
	base, nullable, err := dateTimeArgument(name, args)
	if err != nil {
		return model.Type{}, err
	}
	result := "Uint16"
	if isExtendedDateTime(base.Kind) {
		result = "Int32"
	}
	return withOptional(model.Type{Kind: result}, nullable), nil
}

func dateTimeComponent(name string, args []model.Type, result string) (model.Type, error) {
	_, nullable, err := dateTimeArgument(name, args)
	if err != nil {
		return model.Type{}, err
	}
	return withOptional(model.Type{Kind: result}, nullable), nil
}

func dateTimeArgument(name string, args []model.Type) (model.Type, bool, error) {
	if err := arity(name, args, 1); err != nil {
		return model.Type{}, false, err
	}
	base, nullable, err := baseType(args[0])
	if err != nil || (!isBasicDateTime(base.Kind) && !isExtendedDateTime(base.Kind)) {
		return model.Type{}, false, fmt.Errorf("%s argument 1 must be a date/time value or its Optional form", name)
	}
	return base, nullable, nil
}

func isBasicDateTime(kind string) bool {
	switch kind {
	case "Date", "Datetime", "Timestamp", "TzDate", "TzDatetime", "TzTimestamp":
		return true
	default:
		return false
	}
}

func isExtendedDateTime(kind string) bool {
	switch kind {
	case "Date32", "Datetime64", "Timestamp64", "TzDate32", "TzDatetime64", "TzTimestamp64":
		return true
	default:
		return false
	}
}
