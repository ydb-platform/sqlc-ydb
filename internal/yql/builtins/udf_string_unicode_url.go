package builtins

import "github.com/ydb-platform/sqlc-ydb/internal/model"

func stringUnicodeUrlSignatures(name string) []Signature {
	t := func(kind string) model.Type { return model.Type{Kind: kind} }
	list := func(item model.Type) model.Type { return model.Type{Kind: "List", Elem: &item} }
	param := func(kind string) Parameter { return Parameter{Type: t(kind)} }
	auto := func(kind string) Parameter { return Parameter{Type: t(kind), AutoMap: true} }
	opt := func(kind string) Parameter { return Parameter{Type: model.Optional(t(kind)), Optional: true} }
	sig := func(result model.Type, args ...Parameter) []Signature {
		return []Signature{{Name: name, Arguments: args, Returns: result}}
	}
	switch name {
	case "String::Base32Encode", "String::BinText", "String::HexText":
		return sig(t("String"), auto("String"))
	case "String::Base32Decode", "String::Base32StrictDecode":
		return sig(model.Optional(t("String")), param("String"))
	case "String::IsAscii", "String::IsAsciiSpace", "String::IsAsciiUpper", "String::IsAsciiLower",
		"String::IsAsciiAlpha", "String::IsAsciiAlnum", "String::IsAsciiHex":
		return sig(t("Bool"), auto("String"))
	case "String::Hex", "String::Bin", "String::HumanReadableDuration", "String::HumanReadableQuantity",
		"String::HumanReadableBytes":
		return sig(t("String"), auto("Uint64"))
	case "String::SHex", "String::SBin":
		return sig(t("String"), auto("Int64"))
	case "String::Contains", "String::HasPrefix", "String::HasPrefixIgnoreCase", "String::StartsWith",
		"String::StartsWithIgnoreCase", "String::HasSuffix", "String::HasSuffixIgnoreCase",
		"String::EndsWith", "String::EndsWithIgnoreCase":
		return sig(t("Bool"), Parameter{Type: model.Optional(t("String"))}, param("String"))
	case "String::Reverse":
		return sig(model.Optional(t("String")), Parameter{Type: model.Optional(t("String"))})
	case "String::CollapseText", "String::RightPad", "String::LeftPad":
		args := []Parameter{auto("String"), param("Uint64")}
		if name != "String::CollapseText" {
			args = append(args, opt("String"))
		}
		return sig(t("String"), args...)
	case "String::Prec":
		return sig(t("String"), auto("Double"), param("Uint64"))
	case "String::RemoveAll", "String::RemoveFirst", "String::RemoveLast":
		return sig(t("String"), auto("String"), param("String"))
	case "String::LevensteinDistance":
		return sig(t("Uint64"), auto("String"), auto("String"))
	case "String::JoinFromList":
		return sig(t("String"), Parameter{Type: list(t("String")), AutoMap: true}, param("String"))
	case "String::ToByteList":
		return sig(list(t("Uint8")), param("String"))
	case "String::FromByteList":
		return sig(t("String"), Parameter{Type: list(t("Uint8"))})
	case "String::SplitToList":
		return sig(list(t("String")), Parameter{Type: model.Optional(t("String"))}, param("String"),
			Parameter{Name: "DelimeterString", Type: model.Optional(t("Bool")), Optional: true},
			Parameter{Name: "SkipEmpty", Type: model.Optional(t("Bool")), Optional: true},
			Parameter{Name: "Limit", Type: model.Optional(t("Uint64")), Optional: true})
	case "Unicode::IsAscii", "Unicode::IsSpace", "Unicode::IsUpper", "Unicode::IsLower",
		"Unicode::IsAlpha", "Unicode::IsAlnum", "Unicode::IsHex":
		return sig(t("Bool"), auto("Utf8"))
	case "Unicode::IsUnicodeSet":
		return sig(t("Bool"), auto("Utf8"), param("Utf8"))
	case "Unicode::Strip", "Unicode::Reverse":
		return sig(t("Utf8"), auto("Utf8"))
	case "Unicode::RemoveAll", "Unicode::RemoveFirst", "Unicode::RemoveLast":
		return sig(t("Utf8"), auto("Utf8"), param("Utf8"))
	case "Unicode::ReplaceAll", "Unicode::ReplaceFirst", "Unicode::ReplaceLast":
		return sig(t("Utf8"), auto("Utf8"), param("Utf8"), param("Utf8"))
	case "Unicode::LevensteinDistance":
		return sig(t("Uint64"), auto("Utf8"), auto("Utf8"))
	case "Unicode::ToCodePointList":
		return sig(list(t("Uint32")), auto("Utf8"))
	case "Unicode::FromCodePointList":
		return sig(t("Utf8"), Parameter{Type: list(t("Uint32")), AutoMap: true})
	case "Unicode::JoinFromList":
		return sig(t("Utf8"), Parameter{Type: list(t("Utf8")), AutoMap: true}, param("Utf8"))
	case "Unicode::Translit":
		return sig(t("Utf8"), auto("Utf8"), Parameter{Name: "lang", Type: model.Optional(t("String")), Optional: true})
	case "Unicode::ToUint64":
		return sig(t("Uint64"), auto("Utf8"), Parameter{Name: "prefix", Type: model.Optional(t("Uint16")), Optional: true})
	case "Unicode::TryToUint64":
		return sig(model.Optional(t("Uint64")), auto("Utf8"), Parameter{Name: "prefix", Type: model.Optional(t("Uint16")), Optional: true})
	case "Unicode::Fold":
		return sig(t("Utf8"), auto("Utf8"),
			Parameter{Name: "Language", Type: model.Optional(t("String")), Optional: true},
			Parameter{Name: "DoLowerCase", Type: model.Optional(t("Bool")), Optional: true},
			Parameter{Name: "DoRenyxa", Type: model.Optional(t("Bool")), Optional: true},
			Parameter{Name: "DoSimpleCyr", Type: model.Optional(t("Bool")), Optional: true},
			Parameter{Name: "FillOffset", Type: model.Optional(t("Bool")), Optional: true})
	case "Unicode::SplitToList":
		return sig(list(t("Utf8")), Parameter{Type: model.Optional(t("Utf8"))}, param("Utf8"),
			Parameter{Name: "DelimeterString", Type: model.Optional(t("Bool")), Optional: true},
			Parameter{Name: "SkipEmpty", Type: model.Optional(t("Bool")), Optional: true},
			Parameter{Name: "Limit", Type: model.Optional(t("Uint64")), Optional: true})
	case "Url::GetScheme", "Url::GetTLD", "Url::GetOwner", "Url::CutQueryStringAndFragment",
		"Url::ForceHostNameToPunycode", "Url::ForcePunycodeToHostName":
		return sig(t("String"), auto("String"))
	case "Url::IsKnownTLD", "Url::IsWellKnownTLD", "Url::CanBePunycodeHostName":
		return sig(t("Bool"), auto("String"))
	case "Url::GetDomainLevel":
		return sig(t("Uint64"), auto("String"))
	case "Url::HostNameToPunycode", "Url::PunycodeToHostName":
		return sig(model.Optional(t("String")), auto("String"))
	case "Url::Encode", "Url::Decode", "Url::NormalizeWithDefaultHttpScheme", "Url::GetHost",
		"Url::GetHostPort", "Url::GetSchemeHost", "Url::GetSchemeHostPort", "Url::GetPort",
		"Url::GetTail", "Url::GetPath", "Url::GetFragment", "Url::CutScheme", "Url::CutWWW", "Url::CutWWW2":
		return sig(model.Optional(t("String")), Parameter{Type: model.Optional(t("String"))})
	case "Url::Normalize":
		return sig(model.Optional(t("String")), param("String"))
	case "Url::GetCGIParam":
		return sig(model.Optional(t("String")), Parameter{Type: model.Optional(t("String"))}, param("String"))
	case "Url::GetDomain":
		return sig(model.Optional(t("String")), Parameter{Type: model.Optional(t("String"))}, param("Uint8"))
	case "Url::GetSignificantDomain":
		return sig(t("String"), auto("String"), Parameter{Type: model.Optional(list(t("String"))), Optional: true})
	case "Url::Parse":
		fields := []model.StructField{}
		for _, field := range []string{"Frag", "Host", "ParseError", "Pass", "Path", "Port", "Query", "Scheme", "User"} {
			fields = append(fields, model.StructField{Name: field, Type: model.Optional(t("String"))})
		}
		return sig(model.Type{Kind: "Struct", Fields: fields}, auto("String"))
	case "Url::QueryStringToList", "Url::QueryStringToDict":
		result := model.Type{Kind: "Tuple", Items: []model.Type{t("String"), t("String")}}
		result = list(result)
		if name == "Url::QueryStringToDict" {
			result = model.Type{Kind: "Dict", Key: ptrType(t("String")), Elem: ptrType(list(t("String")))}
		}
		return sig(result, auto("String"),
			Parameter{Name: "KeepBlankValues", Type: model.Optional(t("Bool")), Optional: true},
			Parameter{Name: "Strict", Type: model.Optional(t("Bool")), Optional: true},
			Parameter{Name: "MaxFields", Type: model.Optional(t("Uint32")), Optional: true},
			Parameter{Name: "Separator", Type: model.Optional(t("String")), Optional: true})
	case "Url::BuildQueryString":
		separator := Parameter{Name: "Separator", Type: model.Optional(t("String")), Optional: true}
		listStrings := list(model.Optional(t("String")))
		return []Signature{
			{Name: name, Arguments: []Parameter{{Type: model.Type{Kind: "Dict", Key: ptrType(t("String")), Elem: ptrType(list(t("String")))}, AutoMap: true}, separator}, Returns: t("String")},
			{Name: name, Arguments: []Parameter{{Type: model.Type{Kind: "Dict", Key: ptrType(t("String")), Elem: ptrType(listStrings)}, AutoMap: true}, separator}, Returns: t("String")},
			{Name: name, Arguments: []Parameter{{Type: model.Type{Kind: "Dict", Key: ptrType(t("String")), Elem: ptrType(t("String"))}, AutoMap: true}, separator}, Returns: t("String")},
			{Name: name, Arguments: []Parameter{{Type: model.Type{Kind: "Dict", Key: ptrType(t("String")), Elem: ptrType(model.Optional(t("String")))}, AutoMap: true}, separator}, Returns: t("String")},
			{Name: name, Arguments: []Parameter{{Type: list(model.Type{Kind: "Tuple", Items: []model.Type{t("String"), t("String")}}), AutoMap: true}, separator}, Returns: t("String")},
			{Name: name, Arguments: []Parameter{{Type: list(model.Type{Kind: "Tuple", Items: []model.Type{t("String"), model.Optional(t("String"))}}), AutoMap: true}, separator}, Returns: t("String")},
		}
	default:
		return nil
	}
}

func ptrType(value model.Type) *model.Type { return &value }
