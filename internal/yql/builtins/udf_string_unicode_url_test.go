package builtins

import (
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestStringUnicodeUrlDocumentedFunctions(t *testing.T) {
	typ := func(kind string) model.Type { return model.Type{Kind: kind} }
	list := func(item model.Type) model.Type { return model.Type{Kind: "List", Elem: &item} }
	dict := func(key, item model.Type) model.Type { return model.Type{Kind: "Dict", Key: &key, Elem: &item} }
	str, utf, u8, u32, u64, i64, dbl := typ("String"), typ("Utf8"), typ("Uint8"), typ("Uint32"), typ("Uint64"), typ("Int64"), typ("Double")
	boolean := typ("Bool")
	optStr := model.Optional(str)
	optU64 := model.Optional(u64)
	urlFields := []model.StructField{}
	for _, name := range []string{"Frag", "Host", "ParseError", "Pass", "Path", "Port", "Query", "Scheme", "User"} {
		urlFields = append(urlFields, model.StructField{Name: name, Type: optStr})
	}
	tests := []struct {
		names string
		args  []model.Type
		want  model.Type
	}{
		{"String::Base32Encode String::BinText String::HexText", []model.Type{str}, str},
		{"String::Base32Decode String::Base32StrictDecode", []model.Type{str}, optStr},
		{"String::IsAscii String::IsAsciiSpace String::IsAsciiUpper String::IsAsciiLower String::IsAsciiAlpha String::IsAsciiAlnum String::IsAsciiHex", []model.Type{str}, boolean},
		{"String::Hex String::Bin String::HumanReadableDuration String::HumanReadableQuantity String::HumanReadableBytes", []model.Type{u64}, str},
		{"String::SHex String::SBin", []model.Type{i64}, str},
		{"String::Contains String::HasPrefix String::HasPrefixIgnoreCase String::StartsWith String::StartsWithIgnoreCase String::HasSuffix String::HasSuffixIgnoreCase String::EndsWith String::EndsWithIgnoreCase", []model.Type{optStr, str}, boolean},
		{"String::Reverse", []model.Type{str}, optStr},
		{"String::CollapseText String::LeftPad String::RightPad", []model.Type{str, u64}, str},
		{"String::Prec", []model.Type{dbl, u64}, str},
		{"String::RemoveAll String::RemoveFirst String::RemoveLast", []model.Type{str, str}, str},
		{"String::LevensteinDistance", []model.Type{str, str}, u64},
		{"String::JoinFromList", []model.Type{list(str), str}, str},
		{"String::ToByteList", []model.Type{str}, list(u8)},
		{"String::FromByteList", []model.Type{list(u8)}, str},
		{"String::SplitToList", []model.Type{str, str}, list(str)},
		{"Unicode::IsAscii Unicode::IsSpace Unicode::IsUpper Unicode::IsLower Unicode::IsAlpha Unicode::IsAlnum Unicode::IsHex", []model.Type{utf}, boolean},
		{"Unicode::IsUnicodeSet", []model.Type{utf, utf}, boolean},
		{"Unicode::Strip Unicode::Reverse", []model.Type{utf}, utf},
		{"Unicode::RemoveAll Unicode::RemoveFirst Unicode::RemoveLast", []model.Type{utf, utf}, utf},
		{"Unicode::ReplaceAll Unicode::ReplaceFirst Unicode::ReplaceLast", []model.Type{utf, utf, utf}, utf},
		{"Unicode::LevensteinDistance", []model.Type{utf, utf}, u64},
		{"Unicode::ToCodePointList", []model.Type{utf}, list(u32)},
		{"Unicode::FromCodePointList", []model.Type{list(u32)}, utf},
		{"Unicode::JoinFromList", []model.Type{list(utf), utf}, utf},
		{"Unicode::Translit Unicode::Fold", []model.Type{utf}, utf},
		{"Unicode::ToUint64", []model.Type{utf}, u64},
		{"Unicode::TryToUint64", []model.Type{utf}, optU64},
		{"Unicode::SplitToList", []model.Type{utf, utf}, list(utf)},
		{"Url::GetScheme Url::GetTLD Url::GetOwner Url::CutQueryStringAndFragment Url::ForceHostNameToPunycode Url::ForcePunycodeToHostName", []model.Type{str}, str},
		{"Url::IsKnownTLD Url::IsWellKnownTLD Url::CanBePunycodeHostName", []model.Type{str}, boolean},
		{"Url::GetDomainLevel", []model.Type{str}, u64},
		{"Url::HostNameToPunycode Url::PunycodeToHostName", []model.Type{str}, optStr},
		{"Url::Encode Url::Decode Url::NormalizeWithDefaultHttpScheme Url::GetHost Url::GetHostPort Url::GetSchemeHost Url::GetSchemeHostPort Url::GetPort Url::GetTail Url::GetPath Url::GetFragment Url::CutScheme Url::CutWWW Url::CutWWW2", []model.Type{str}, optStr},
		{"Url::Normalize", []model.Type{str}, optStr},
		{"Url::GetCGIParam", []model.Type{str, str}, optStr},
		{"Url::GetDomain", []model.Type{str, u8}, optStr},
		{"Url::GetSignificantDomain", []model.Type{str}, str},
		{"Url::Parse", []model.Type{str}, model.Type{Kind: "Struct", Fields: urlFields}},
		{"Url::QueryStringToList", []model.Type{str}, list(model.Type{Kind: "Tuple", Items: []model.Type{str, str}})},
		{"Url::QueryStringToDict", []model.Type{str}, dict(str, list(str))},
		{"Url::BuildQueryString", []model.Type{dict(str, list(optStr))}, str},
		{"Url::BuildQueryString", []model.Type{dict(str, list(str))}, str},
		{"Url::BuildQueryString", []model.Type{dict(str, optStr)}, str},
		{"Url::BuildQueryString", []model.Type{dict(str, str)}, str},
		{"Url::BuildQueryString", []model.Type{list(model.Type{Kind: "Tuple", Items: []model.Type{str, optStr}})}, str},
		{"Url::BuildQueryString", []model.Type{list(model.Type{Kind: "Tuple", Items: []model.Type{str, str}})}, str},
	}
	seen := map[string]bool{}
	for _, group := range tests {
		for _, name := range strings.Fields(group.names) {
			seen[name] = true
			t.Run(name, func(t *testing.T) {
				if len(stringUnicodeUrlSignatures(name)) == 0 {
					t.Fatal("documented function has no resolver")
				}
				got, err := Resolve(name, group.args)
				if err != nil || !got.Equal(group.want) {
					t.Fatalf("resolve(%v) = %s, %v; want %s", group.args, got, err, group.want)
				}
				if _, err := Resolve(name, nil); err == nil {
					t.Fatal("missing required argument was accepted")
				}
				tooMany := append(append([]model.Type(nil), group.args...), boolean, boolean, boolean, boolean, boolean, boolean)
				if _, err := Resolve(name, tooMany); err == nil {
					t.Fatal("excess arguments were accepted")
				}
				bad := append([]model.Type(nil), group.args...)
				bad[0] = boolean
				if _, err := Resolve(name, bad); err == nil {
					t.Fatal("wrong first argument type was accepted")
				}
			})
		}
	}
	if len(seen) != 100 {
		t.Fatalf("covered %d new documented names, want 100", len(seen))
	}
}

func TestStringUnicodeUrlNullableAndNamedOptions(t *testing.T) {
	str := model.Type{Kind: "String"}
	utf := model.Type{Kind: "Utf8"}
	boolType := model.Type{Kind: "Bool"}
	tests := []struct {
		name string
		args []CallArgument
		want model.Type
	}{
		{"String::SplitToList", []CallArgument{{Type: str}, {Type: str}, {Name: "Limit", Type: model.Type{Kind: "Uint64"}}}, model.Type{Kind: "List", Elem: &str}},
		{"Unicode::SplitToList", []CallArgument{{Type: utf}, {Type: utf}, {Name: "SkipEmpty", Type: boolType}}, model.Type{Kind: "List", Elem: &utf}},
		{"Unicode::Fold", []CallArgument{{Type: utf}, {Name: "DoRenyxa", Type: boolType}}, utf},
		{"Url::QueryStringToList", []CallArgument{{Type: str}, {Name: "Strict", Type: boolType}}, model.Type{Kind: "List", Elem: &model.Type{Kind: "Tuple", Items: []model.Type{str, str}}}},
		{"Url::BuildQueryString", []CallArgument{{Type: model.Type{Kind: "Dict", Key: &str, Elem: ptrType(model.Optional(str))}}, {Name: "Separator", Type: str}}, str},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := defaultRegistry.ResolveCall(tt.name, tt.args)
			if err != nil || !got.Equal(tt.want) {
				t.Fatalf("resolve named = %s, %v; want %s", got, err, tt.want)
			}
			bad := append([]CallArgument(nil), tt.args...)
			bad[len(bad)-1].Name = "Unknown"
			if _, err := defaultRegistry.ResolveCall(tt.name, bad); err == nil {
				t.Fatal("unknown named option was accepted")
			}
			bad = append([]CallArgument(nil), tt.args...)
			bad[len(bad)-1].Type = model.Type{Kind: "Json"}
			if _, err := defaultRegistry.ResolveCall(tt.name, bad); err == nil {
				t.Fatal("wrong named option type was accepted")
			}
		})
	}
	for _, tt := range []struct {
		name string
		args []model.Type
		want model.Type
	}{
		{"String::Base32Encode", []model.Type{model.Optional(str)}, model.Optional(str)},
		{"String::Contains", []model.Type{{Kind: "Null"}, str}, boolType},
		{"String::SplitToList", []model.Type{{Kind: "Null"}, str}, model.Type{Kind: "List", Elem: &str}},
		{"Unicode::IsAscii", []model.Type{model.Optional(utf)}, model.Optional(boolType)},
		{"Unicode::TryToUint64", []model.Type{utf}, model.Optional(model.Type{Kind: "Uint64"})},
		{"Url::Parse", []model.Type{model.Optional(str)}, model.Optional(stringUnicodeUrlSignatures("Url::Parse")[0].Returns)},
	} {
		got, err := Resolve(tt.name, tt.args)
		if err != nil || !got.Equal(tt.want) {
			t.Errorf("%s = %s, %v; want %s", tt.name, got, err, tt.want)
		}
	}
}

func TestStringUnicodeUrlNamesCannotBeOverridden(t *testing.T) {
	for _, name := range []string{"String::Base32Encode", "Unicode::Fold", "Url::GetDomain"} {
		_, err := NewRegistry([]Signature{{Name: name, Returns: model.Type{Kind: "Bool"}}})
		if err == nil || !strings.Contains(err.Error(), "known built-in") {
			t.Errorf("NewRegistry(%s): %v", name, err)
		}
	}
}
