package builtins

import (
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-engine-ydb/internal/model"
)

func scalar(kind string) model.Type { return model.Type{Kind: kind} }

func TestCommonType(t *testing.T) {
	tests := []struct {
		name  string
		args  []model.Type
		want  model.Type
		error string
	}{
		{name: "same concrete type", args: []model.Type{scalar("Utf8"), scalar("Utf8")}, want: scalar("Utf8")},
		{name: "optional is preserved", args: []model.Type{scalar("Int32"), model.Optional(scalar("Int32"))}, want: model.Optional(scalar("Int32"))},
		{name: "contextual null makes concrete type optional", args: []model.Type{scalar("Null"), scalar("String")}, want: model.Optional(scalar("String"))},
		{name: "integer numeric promotion", args: []model.Type{scalar("Int16"), scalar("Uint32")}, want: scalar("Uint32")},
		{name: "integer and floating promotion", args: []model.Type{scalar("Uint64"), scalar("Float")}, want: scalar("Float")},
		{name: "float and double promotion", args: []model.Type{scalar("Float"), scalar("Double")}, want: scalar("Double")},
		{name: "same decimal", args: []model.Type{{Kind: "Decimal", Precision: 22, Scale: 9}, {Kind: "Decimal", Precision: 22, Scale: 9}}, want: model.Type{Kind: "Decimal", Precision: 22, Scale: 9}},
		{name: "no arguments", error: "at least one"},
		{name: "only null", args: []model.Type{scalar("Null"), scalar("Null")}, error: "only Null"},
		{name: "different strings", args: []model.Type{scalar("String"), scalar("Utf8")}, error: "no common type"},
		{name: "different decimals", args: []model.Type{{Kind: "Decimal", Precision: 10, Scale: 2}, {Kind: "Decimal", Precision: 12, Scale: 2}}, error: "no common type"},
		{name: "nested optional", args: []model.Type{model.Optional(model.Optional(scalar("Int32")))}, error: "nested Optional"},
		{name: "unresolved any", args: []model.Type{scalar("Any"), scalar("Int32")}, error: "unsupported type"},
		{name: "unknown kind", args: []model.Type{scalar("Mystery"), scalar("Mystery")}, error: "unsupported type"},
		{name: "malformed list", args: []model.Type{{Kind: "List"}, {Kind: "List"}}, error: "element type"},
		{name: "malformed scalar", args: []model.Type{{Kind: "Utf8", Elem: typePointer(scalar("String"))}}, error: "unexpected type parameters"},
		{name: "invalid decimal precision", args: []model.Type{{Kind: "Decimal", Precision: 36, Scale: 2}}, error: "invalid Decimal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CommonType(tt.args...)
			if tt.error != "" {
				if err == nil || !strings.Contains(err.Error(), tt.error) {
					t.Fatalf("CommonType() error = %v, want substring %q", err, tt.error)
				}
				return
			}
			if err != nil {
				t.Fatalf("CommonType() unexpected error: %v", err)
			}
			if !sameType(got, tt.want) {
				t.Fatalf("CommonType() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestResolveDoesNotMutateArguments(t *testing.T) {
	backing := make([]model.Type, 3)
	backing[0] = scalar("Bool")
	backing[1] = scalar("Int32")
	backing[2] = scalar("Sentinel")
	args := backing[:2]
	if _, err := Resolve("IF", args); err != nil {
		t.Fatalf("Resolve(IF) unexpected error: %v", err)
	}
	if backing[2].Kind != "Sentinel" {
		t.Fatalf("Resolve(IF) mutated caller backing array: %#v", backing)
	}
}

func TestResolveCoreFunctions(t *testing.T) {
	tests := []struct {
		name string
		args []model.Type
		want model.Type
	}{
		{name: "coalesce", args: []model.Type{model.Optional(scalar("Utf8")), scalar("Utf8")}, want: scalar("Utf8")},
		{name: "NVL", args: []model.Type{scalar("Null"), model.Optional(scalar("Uint64"))}, want: model.Optional(scalar("Uint64"))},
		{name: "if", args: []model.Type{scalar("Bool"), scalar("Int16"), scalar("Uint32")}, want: scalar("Uint32")},
		{name: "IF", args: []model.Type{model.Optional(scalar("Bool")), scalar("String")}, want: model.Optional(scalar("String"))},
		{name: "length", args: []model.Type{model.Optional(scalar("Utf8"))}, want: model.Optional(scalar("Uint32"))},
		{name: "LEN", args: []model.Type{scalar("String")}, want: scalar("Uint32")},
		{name: "substring", args: []model.Type{model.Optional(scalar("String")), scalar("Uint32"), scalar("Null")}, want: model.Optional(scalar("String"))},
		{name: "find", args: []model.Type{scalar("Utf8"), scalar("Utf8")}, want: model.Optional(scalar("Uint32"))},
		{name: "FIND", args: []model.Type{scalar("Null"), scalar("String")}, want: model.Optional(scalar("Uint32"))},
		{name: "RFIND", args: []model.Type{scalar("String"), scalar("String"), scalar("Uint32")}, want: model.Optional(scalar("Uint32"))},
		{name: "startswith", args: []model.Type{scalar("Utf8"), model.Optional(scalar("Utf8"))}, want: model.Optional(scalar("Bool"))},
		{name: "EndsWith", args: []model.Type{scalar("String"), scalar("String")}, want: scalar("Bool")},
		{name: "StartsWith", args: []model.Type{scalar("String"), scalar("Null")}, want: model.Optional(scalar("Bool"))},
		{name: "abs", args: []model.Type{model.Optional(scalar("Int32"))}, want: model.Optional(scalar("Int32"))},
		{name: "ABS", args: []model.Type{{Kind: "Decimal", Precision: 22, Scale: 9}}, want: model.Type{Kind: "Decimal", Precision: 22, Scale: 9}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(tt.name, tt.args)
			if err != nil {
				t.Fatalf("Resolve(%q) unexpected error: %v", tt.name, err)
			}
			if !sameType(got, tt.want) {
				t.Fatalf("Resolve(%q) = %#v, want %#v", tt.name, got, tt.want)
			}
		})
	}
}

func TestResolveCoreStringFunctionsAcceptNullableUint32Positions(t *testing.T) {
	assertResolved(t, "SUBSTRING", []model.Type{
		scalar("String"),
		model.Optional(scalar("Uint16")),
		model.Optional(scalar("Uint32")),
	}, scalar("String"))
	assertResolved(t, "FIND", []model.Type{
		scalar("String"),
		scalar("String"),
		model.Optional(scalar("Uint8")),
	}, model.Optional(scalar("Uint32")))
}

func TestResolveAggregates(t *testing.T) {
	tests := []struct {
		name string
		args []model.Type
		want model.Type
	}{
		{name: "COUNT", args: nil, want: scalar("Uint64")},
		{name: "count", args: []model.Type{model.Optional(scalar("Utf8"))}, want: scalar("Uint64")},
		{name: "MIN", args: []model.Type{scalar("Int32")}, want: model.Optional(scalar("Int32"))},
		{name: "MAX", args: []model.Type{model.Optional(scalar("Double"))}, want: model.Optional(scalar("Double"))},
		{name: "MIN", args: []model.Type{scalar("Utf8")}, want: model.Optional(scalar("Utf8"))},
		{name: "SUM", args: []model.Type{scalar("Int8")}, want: model.Optional(scalar("Int64"))},
		{name: "SUM", args: []model.Type{model.Optional(scalar("Uint32"))}, want: model.Optional(scalar("Uint64"))},
		{name: "SUM", args: []model.Type{scalar("Float")}, want: model.Optional(scalar("Float"))},
		{name: "SUM", args: []model.Type{{Kind: "Decimal", Precision: 22, Scale: 9}}, want: model.Optional(model.Type{Kind: "Decimal", Precision: 35, Scale: 9})},
		{name: "AVG", args: []model.Type{scalar("Int64")}, want: model.Optional(scalar("Double"))},
		{name: "AVG", args: []model.Type{scalar("Float")}, want: model.Optional(scalar("Double"))},
		{name: "AVG", args: []model.Type{scalar("Interval")}, want: model.Optional(scalar("Double"))},
		{name: "AVG", args: []model.Type{scalar("Double")}, want: model.Optional(scalar("Double"))},
	}

	for _, tt := range tests {
		t.Run(tt.name+typeList(tt.args), func(t *testing.T) {
			got, err := Resolve(tt.name, tt.args)
			if err != nil {
				t.Fatalf("Resolve(%q) unexpected error: %v", tt.name, err)
			}
			if !sameType(got, tt.want) {
				t.Fatalf("Resolve(%q) = %#v, want %#v", tt.name, got, tt.want)
			}
		})
	}
}

func TestResolveLibraryFunctions(t *testing.T) {
	tests := []struct {
		name string
		args []model.Type
		want model.Type
	}{
		{name: "String::Base64Encode", args: []model.Type{model.Optional(scalar("String"))}, want: model.Optional(scalar("String"))},
		{name: "String::Base64Decode", args: []model.Type{scalar("String")}, want: model.Optional(scalar("String"))},
		{name: "String::Find", args: []model.Type{scalar("String"), scalar("String"), model.Optional(scalar("Uint64"))}, want: scalar("Int64")},
		{name: "String::Substring", args: []model.Type{model.Optional(scalar("String")), scalar("Null"), scalar("Uint64")}, want: model.Optional(scalar("String"))},
		{name: "String::Substring", args: []model.Type{scalar("Null"), scalar("Uint64")}, want: model.Optional(scalar("String"))},
		{name: "String::AsciiToLower", args: []model.Type{scalar("String")}, want: scalar("String")},
		{name: "String::ReplaceAll", args: []model.Type{model.Optional(scalar("String")), scalar("String"), scalar("String")}, want: model.Optional(scalar("String"))},
		{name: "Unicode::IsUtf", args: []model.Type{scalar("String")}, want: scalar("Bool")},
		{name: "Unicode::GetLength", args: []model.Type{model.Optional(scalar("Utf8"))}, want: model.Optional(scalar("Uint64"))},
		{name: "Unicode::GetLength", args: []model.Type{scalar("Null")}, want: model.Optional(scalar("Uint64"))},
		{name: "Unicode::Find", args: []model.Type{scalar("Utf8"), scalar("Utf8")}, want: model.Optional(scalar("Uint64"))},
		{name: "Unicode::Substring", args: []model.Type{model.Optional(scalar("Utf8")), scalar("Uint64"), scalar("Null")}, want: model.Optional(scalar("Utf8"))},
		{name: "Unicode::ToUpper", args: []model.Type{scalar("Utf8")}, want: scalar("Utf8")},
		{name: "DateTime::GetYear", args: []model.Type{model.Optional(scalar("Timestamp"))}, want: model.Optional(scalar("Uint16"))},
		{name: "DateTime::GetYear", args: []model.Type{scalar("Date32")}, want: scalar("Int32")},
		{name: "DateTime::GetMonth", args: []model.Type{scalar("TzDatetime64")}, want: scalar("Uint8")},
		{name: "DateTime::GetDayOfYear", args: []model.Type{scalar("Date")}, want: scalar("Uint16")},
		{name: "DateTime::GetTimezoneName", args: []model.Type{model.Optional(scalar("TzTimestamp"))}, want: model.Optional(scalar("String"))},
	}

	for _, tt := range tests {
		t.Run(tt.name+typeList(tt.args), func(t *testing.T) {
			got, err := Resolve(tt.name, tt.args)
			if err != nil {
				t.Fatalf("Resolve(%q) unexpected error: %v", tt.name, err)
			}
			if !sameType(got, tt.want) {
				t.Fatalf("Resolve(%q) = %#v, want %#v", tt.name, got, tt.want)
			}
		})
	}
}

func TestResolveEveryDocumentedLibraryEntry(t *testing.T) {
	autoString := []string{
		"String::Base64Encode", "String::EscapeC", "String::UnescapeC", "String::HexEncode",
		"String::EncodeHtml", "String::DecodeHtml", "String::CgiEscape", "String::CgiUnescape",
		"String::Strip", "String::Collapse", "String::AsciiToLower", "String::AsciiToUpper", "String::AsciiToTitle",
	}
	for _, name := range autoString {
		assertResolved(t, name, []model.Type{scalar("String")}, scalar("String"))
	}
	for _, name := range []string{"String::Base64Decode", "String::Base64StrictDecode", "String::HexDecode"} {
		assertResolved(t, name, []model.Type{scalar("String")}, model.Optional(scalar("String")))
	}
	for _, name := range []string{"String::Find", "String::ReverseFind"} {
		assertResolved(t, name, []model.Type{scalar("String"), scalar("String")}, scalar("Int64"))
	}
	for _, name := range []string{"String::ReplaceAll", "String::ReplaceFirst", "String::ReplaceLast"} {
		assertResolved(t, name, []model.Type{scalar("String"), scalar("String"), scalar("String")}, scalar("String"))
	}
	for _, name := range []string{
		"Unicode::ToLower", "Unicode::ToUpper", "Unicode::ToTitle", "Unicode::Normalize",
		"Unicode::NormalizeNFC", "Unicode::NormalizeNFD", "Unicode::NormalizeNFKC", "Unicode::NormalizeNFKD",
	} {
		assertResolved(t, name, []model.Type{scalar("Utf8")}, scalar("Utf8"))
	}
	dateTimeResults := map[string]string{
		"DateTime::GetYear":                "Uint16",
		"DateTime::GetDayOfYear":           "Uint16",
		"DateTime::GetMonth":               "Uint8",
		"DateTime::GetMonthName":           "String",
		"DateTime::GetWeekOfYear":          "Uint8",
		"DateTime::GetWeekOfYearIso8601":   "Uint8",
		"DateTime::GetDayOfMonth":          "Uint8",
		"DateTime::GetDayOfWeek":           "Uint8",
		"DateTime::GetDayOfWeekName":       "String",
		"DateTime::GetHour":                "Uint8",
		"DateTime::GetMinute":              "Uint8",
		"DateTime::GetSecond":              "Uint8",
		"DateTime::GetMillisecondOfSecond": "Uint32",
		"DateTime::GetMicrosecondOfSecond": "Uint32",
		"DateTime::GetTimezoneId":          "Uint16",
		"DateTime::GetTimezoneName":        "String",
	}
	for name, result := range dateTimeResults {
		assertResolved(t, name, []model.Type{scalar("Timestamp")}, scalar(result))
	}
}

func assertResolved(t *testing.T, name string, args []model.Type, want model.Type) {
	t.Helper()
	got, err := Resolve(name, args)
	if err != nil {
		t.Errorf("Resolve(%q) unexpected error: %v", name, err)
		return
	}
	if !sameType(got, want) {
		t.Errorf("Resolve(%q) = %#v, want %#v", name, got, want)
	}
}

func TestResolveRejectsUnknownOrInvalidCalls(t *testing.T) {
	tests := []struct {
		name  string
		args  []model.Type
		error string
	}{
		{name: "Mystery", args: []model.Type{scalar("Int32")}, error: "unsupported YQL function"},
		{name: "string::Base64Encode", args: []model.Type{scalar("String")}, error: "unsupported YQL function"},
		{name: "LENGTH", args: nil, error: "expects 1 argument"},
		{name: "LENGTH", args: []model.Type{scalar("Int32")}, error: "argument 1"},
		{name: "SUBSTRING", args: []model.Type{scalar("String")}, error: "expects 2 or 3 arguments"},
		{name: "SUBSTRING", args: []model.Type{scalar("String"), scalar("Uint32"), scalar("Uint32"), scalar("Uint32")}, error: "expects 2 or 3 arguments"},
		{name: "SUBSTRING", args: []model.Type{scalar("String"), scalar("Utf8")}, error: "argument 2"},
		{name: "FIND", args: []model.Type{scalar("String")}, error: "expects 2 or 3 arguments"},
		{name: "FIND", args: []model.Type{scalar("String"), scalar("Utf8")}, error: "same string type"},
		{name: "RFIND", args: []model.Type{scalar("String"), scalar("String"), scalar("Uint32"), scalar("Uint32")}, error: "expects 2 or 3 arguments"},
		{name: "IF", args: []model.Type{scalar("Int32"), scalar("Int32"), scalar("Int32")}, error: "condition"},
		{name: "IF", args: []model.Type{scalar("Bool"), scalar("String"), scalar("Utf8")}, error: "no common type"},
		{name: "COALESCE", args: nil, error: "at least 1 argument"},
		{name: "COALESCE", args: []model.Type{scalar("Null")}, error: "only Null"},
		{name: "COUNT", args: []model.Type{scalar("Int32"), scalar("Int32")}, error: "expects 0 or 1 argument"},
		{name: "MIN", args: []model.Type{scalar("Json")}, error: "supported comparable"},
		{name: "SUM", args: []model.Type{scalar("String")}, error: "numeric"},
		{name: "AVG", args: []model.Type{scalar("Utf8")}, error: "numeric or Interval"},
		{name: "String::Base64Decode", args: []model.Type{model.Optional(scalar("String"))}, error: "non-optional String"},
		{name: "String::Substring", args: []model.Type{scalar("String")}, error: "expects 2 or 3 arguments"},
		{name: "Unicode::Find", args: []model.Type{scalar("String"), scalar("String")}, error: "argument 1"},
		{name: "Unicode::Substring", args: []model.Type{scalar("Utf8")}, error: "expects 2 or 3 arguments"},
		{name: "DateTime::GetYear", args: []model.Type{scalar("Int64")}, error: "date/time"},
		{name: "DateTime::getYear", args: []model.Type{scalar("Date")}, error: "unsupported YQL function"},
	}

	for _, tt := range tests {
		t.Run(tt.name+typeList(tt.args), func(t *testing.T) {
			got, err := Resolve(tt.name, tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.error) {
				t.Fatalf("Resolve(%q) = %#v, error %v; want error containing %q", tt.name, got, err, tt.error)
			}
			if got.Kind != "" {
				t.Fatalf("Resolve(%q) returned a type on error: %#v", tt.name, got)
			}
		})
	}
}

func TestResolveCoalesceRejectsMixedBaseTypes(t *testing.T) {
	tests := []struct {
		name     string
		function string
		args     []model.Type
	}{
		{name: "coalesce non-optional numeric", function: "COALESCE", args: []model.Type{scalar("Int32"), scalar("Int64")}},
		{name: "coalesce optional numeric", function: "coalesce", args: []model.Type{model.Optional(scalar("Int32")), scalar("Int64")}},
		{name: "NVL numeric", function: "NVL", args: []model.Type{model.Optional(scalar("Uint16")), scalar("Uint32")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(tt.function, tt.args)
			if err == nil || !strings.Contains(err.Error(), "CAST") || !strings.Contains(err.Error(), "same YQL type") {
				t.Fatalf("Resolve(%q) = %#v, error %v; want mixed-type CAST error", tt.function, got, err)
			}
		})
	}
}

func TestResolveCoreStringPositionsRejectUnsupportedTypes(t *testing.T) {
	tests := []struct {
		name     string
		function string
		args     []model.Type
	}{
		{name: "SUBSTRING Utf8 source", function: "SUBSTRING", args: []model.Type{scalar("Utf8"), scalar("Uint32")}},
		{name: "SUBSTRING optional Utf8 source", function: "SUBSTRING", args: []model.Type{model.Optional(scalar("Utf8")), scalar("Uint32")}},
		{name: "SUBSTRING signed offset", function: "SUBSTRING", args: []model.Type{scalar("String"), scalar("Int8")}},
		{name: "SUBSTRING signed length", function: "SUBSTRING", args: []model.Type{scalar("String"), scalar("Uint32"), scalar("Int16")}},
		{name: "SUBSTRING optional signed offset", function: "SUBSTRING", args: []model.Type{scalar("String"), model.Optional(scalar("Int32"))}},
		{name: "SUBSTRING Int64 offset", function: "SUBSTRING", args: []model.Type{scalar("String"), scalar("Int64")}},
		{name: "SUBSTRING Uint64 offset", function: "SUBSTRING", args: []model.Type{scalar("String"), scalar("Uint64")}},
		{name: "FIND signed offset", function: "FIND", args: []model.Type{scalar("String"), scalar("String"), scalar("Int32")}},
		{name: "RFIND optional Uint64 offset", function: "RFIND", args: []model.Type{scalar("Utf8"), scalar("Utf8"), model.Optional(scalar("Uint64"))}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(tt.function, tt.args)
			if err == nil {
				t.Fatalf("Resolve(%q) = %#v, want error", tt.function, got)
			}
			if strings.Contains(tt.name, "Utf8 source") {
				if !strings.Contains(err.Error(), "Unicode::Substring") {
					t.Fatalf("Resolve(%q) error = %v; want Unicode::Substring advice", tt.function, err)
				}
				return
			}
			if !strings.Contains(err.Error(), "CAST") || !strings.Contains(err.Error(), "Uint32") {
				t.Fatalf("Resolve(%q) error = %v; want CAST AS Uint32 advice", tt.name, err)
			}
		})
	}
}

func TestCast(t *testing.T) {
	tests := []struct {
		name   string
		source model.Type
		target model.Type
		want   model.Type
		error  string
	}{
		{name: "identity", source: scalar("Uint64"), target: scalar("Uint64"), want: scalar("Uint64")},
		{name: "explicit optional target", source: scalar("Uint64"), target: model.Optional(scalar("Uint64")), want: model.Optional(scalar("Uint64"))},
		{name: "optional source propagates", source: model.Optional(scalar("Int32")), target: scalar("Int64"), want: model.Optional(scalar("Int64"))},
		{name: "contextual null", source: scalar("Null"), target: scalar("Utf8"), want: model.Optional(scalar("Utf8"))},
		{name: "signed widening is total", source: scalar("Int16"), target: scalar("Int64"), want: scalar("Int64")},
		{name: "unsigned widening is total", source: scalar("Uint8"), target: scalar("Uint64"), want: scalar("Uint64")},
		{name: "unsigned to wider signed is total", source: scalar("Uint32"), target: scalar("Int64"), want: scalar("Int64")},
		{name: "signed to unsigned may fail", source: scalar("Int32"), target: scalar("Uint64"), want: model.Optional(scalar("Uint64"))},
		{name: "unsigned to same width signed may fail", source: scalar("Uint32"), target: scalar("Int32"), want: model.Optional(scalar("Int32"))},
		{name: "narrowing may fail", source: scalar("Int64"), target: scalar("Int32"), want: model.Optional(scalar("Int32"))},
		{name: "integer to double is total", source: scalar("Uint64"), target: scalar("Double"), want: scalar("Double")},
		{name: "float to integer may fail", source: scalar("Float"), target: scalar("Int32"), want: model.Optional(scalar("Int32"))},
		{name: "string parse may fail", source: scalar("String"), target: scalar("Uint64"), want: model.Optional(scalar("Uint64"))},
		{name: "optional string parse remains one optional", source: model.Optional(scalar("String")), target: scalar("Uint64"), want: model.Optional(scalar("Uint64"))},
		{name: "utf8 to string is total", source: scalar("Utf8"), target: scalar("String"), want: scalar("String")},
		{name: "string to utf8 may fail validation", source: scalar("String"), target: scalar("Utf8"), want: model.Optional(scalar("Utf8"))},
		{name: "unsupported composite", source: model.Type{Kind: "List", Elem: typePointer(scalar("Int32"))}, target: model.Type{Kind: "List", Elem: typePointer(scalar("Int64"))}, error: "unsupported CAST"},
		{name: "invalid target null", source: scalar("Int32"), target: scalar("Null"), error: "target"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Cast(tt.source, tt.target)
			if tt.error != "" {
				if err == nil || !strings.Contains(err.Error(), tt.error) {
					t.Fatalf("Cast() error = %v, want substring %q", err, tt.error)
				}
				return
			}
			if err != nil {
				t.Fatalf("Cast() unexpected error: %v", err)
			}
			if !sameType(got, tt.want) {
				t.Fatalf("Cast() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func sameType(left, right model.Type) bool {
	if left.Kind != right.Kind || left.Precision != right.Precision || left.Scale != right.Scale {
		return false
	}
	if (left.Elem == nil) != (right.Elem == nil) || (left.Key == nil) != (right.Key == nil) || len(left.Items) != len(right.Items) {
		return false
	}
	if left.Elem != nil && !sameType(*left.Elem, *right.Elem) {
		return false
	}
	if left.Key != nil && !sameType(*left.Key, *right.Key) {
		return false
	}
	for i := range left.Items {
		if !sameType(left.Items[i], right.Items[i]) {
			return false
		}
	}
	return true
}

func typeList(types []model.Type) string {
	var out strings.Builder
	for _, value := range types {
		out.WriteByte('_')
		out.WriteString(value.Kind)
	}
	return out.String()
}

func typePointer(value model.Type) *model.Type { return &value }
