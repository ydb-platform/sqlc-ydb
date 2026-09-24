package builtins

import (
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestDocumentedYsonUDFTypes(t *testing.T) {
	typ := func(kind string) model.Type { return model.Type{Kind: kind} }
	list := func(item model.Type) model.Type { return model.Type{Kind: "List", Elem: &item} }
	dict := func(item model.Type) model.Type {
		key := typ("String")
		return model.Type{Kind: "Dict", Key: &key, Elem: &item}
	}
	opt := model.Optional
	node, options, str, js, ys := typ(ysonNodeResource), typ(ysonOptionsResource), typ("String"), typ("Json"), typ("Yson")
	boolType, intType, uintType, doubleType := typ("Bool"), typ("Int64"), typ("Uint64"), typ("Double")
	call := func(types ...model.Type) []CallArgument {
		args := make([]CallArgument, len(types))
		for i := range types {
			args[i].Type = types[i]
		}
		return args
	}
	tests := []struct {
		names string
		args  []CallArgument
		want  model.Type
	}{
		{"Yson::Options", nil, options},
		{"Yson::From", call(list(str)), node},
		{"Yson::ConvertTo", []CallArgument{{Type: node}, {TypeArgument: &str}}, str},
		{"Yson::Parse", call(ys), node},
		{"Yson::ParseJson Yson::ParseJsonDecodeUtf8", call(js), node},
		{"Yson::WithAttributes", call(node, node), opt(node)},
		{"Yson::Equals", call(node, node), boolType},
		{"Yson::GetHash", call(node), uintType},
		{"Yson::IsEntity Yson::IsString Yson::IsDouble Yson::IsUint64 Yson::IsInt64 Yson::IsBool Yson::IsList Yson::IsDict", call(node), boolType},
		{"Yson::GetLength", call(node), opt(uintType)},
		{"Yson::ConvertToBool", call(node), opt(boolType)},
		{"Yson::ConvertToInt64", call(node), opt(intType)},
		{"Yson::ConvertToUint64", call(node), opt(uintType)},
		{"Yson::ConvertToDouble", call(node), opt(doubleType)},
		{"Yson::ConvertToString", call(node), opt(str)},
		{"Yson::ConvertToList", call(node), list(node)},
		{"Yson::ConvertToBoolList", call(node), list(boolType)},
		{"Yson::ConvertToInt64List", call(node), list(intType)},
		{"Yson::ConvertToUint64List", call(node), list(uintType)},
		{"Yson::ConvertToDoubleList", call(node), list(doubleType)},
		{"Yson::ConvertToStringList", call(node), list(str)},
		{"Yson::ConvertToDict", call(node), dict(node)},
		{"Yson::ConvertToBoolDict", call(node), dict(boolType)},
		{"Yson::ConvertToInt64Dict", call(node), dict(intType)},
		{"Yson::ConvertToUint64Dict", call(node), dict(uintType)},
		{"Yson::ConvertToDoubleDict", call(node), dict(doubleType)},
		{"Yson::ConvertToStringDict", call(node), dict(str)},
		{"Yson::Contains", call(node, str), opt(boolType)},
		{"Yson::Lookup Yson::YPath", call(node, str), opt(node)},
		{"Yson::LookupBool Yson::YPathBool", call(node, str), opt(boolType)},
		{"Yson::LookupInt64 Yson::YPathInt64", call(node, str), opt(intType)},
		{"Yson::LookupUint64 Yson::YPathUint64", call(node, str), opt(uintType)},
		{"Yson::LookupDouble Yson::YPathDouble", call(node, str), opt(doubleType)},
		{"Yson::LookupString Yson::YPathString", call(node, str), opt(str)},
		{"Yson::LookupDict Yson::YPathDict", call(node, str), opt(dict(node))},
		{"Yson::LookupList Yson::YPathList", call(node, str), opt(list(node))},
		{"Yson::Attributes", call(node), dict(node)},
		{"Yson::Serialize Yson::SerializeText Yson::SerializePretty", call(node), ys},
		{"Yson::SerializeJson", call(node), opt(js)},
	}
	seen := map[string]bool{}
	for _, group := range tests {
		for _, name := range strings.Fields(group.names) {
			seen[name] = true
			t.Run(name, func(t *testing.T) {
				got, handled, err := resolveYsonCall(name, group.args)
				if !handled || err != nil || !got.Equal(group.want) {
					t.Fatalf("resolve(%s) = %s, handled=%t, %v; want %s", name, got, handled, err, group.want)
				}
				if name != "Yson::Options" {
					if _, _, err := resolveYsonCall(name, nil); err == nil {
						t.Fatal("missing required argument was accepted")
					}
				}
			})
		}
	}
	if len(seen) != 57 {
		t.Fatalf("covered %d documented Yson names, want 57", len(seen))
	}
	if _, handled, _ := resolveYsonCall("Yson::Unlisted", nil); handled {
		t.Fatal("undocumented Yson function was accepted")
	}
}

func TestYsonUDFOverloadsAndDiagnostics(t *testing.T) {
	str := model.Type{Kind: "String"}
	js := model.Type{Kind: "Json"}
	ys := model.Type{Kind: "Yson"}
	node := model.Type{Kind: ysonNodeResource}
	options := model.Type{Kind: ysonOptionsResource}
	for _, tt := range []struct {
		name string
		args []CallArgument
		want string
	}{
		{"Yson::Parse", []CallArgument{{Type: str}}, "Optional<" + ysonNodeResource + ">"},
		{"Yson::ParseJson", []CallArgument{{Type: str}, {Type: model.Type{Kind: "Null"}}}, "Optional<" + ysonNodeResource + ">"},
		{"Yson::ParseJson", []CallArgument{{Type: js}, {Type: options}}, ysonNodeResource},
		{"Yson::ParseJsonDecodeUtf8", []CallArgument{{Type: str}}, "Optional<" + ysonNodeResource + ">"},
		{"Yson::Serialize", []CallArgument{{Type: model.Optional(node)}}, "Optional<Yson>"},
		{"Yson::Serialize", []CallArgument{{Type: model.Type{Kind: "Null"}}}, "Optional<Yson>"},
		{"Yson::GetHash", []CallArgument{{Type: model.Optional(node)}}, "Optional<Uint64>"},
		{"Yson::Equals", []CallArgument{{Type: node}, {Type: model.Optional(node)}}, "Optional<Bool>"},
		{"Yson::ConvertToStringList", []CallArgument{{Type: model.Optional(js)}, {Type: options}}, "List<String>"},
		{"Yson::ConvertToStringList", []CallArgument{{Type: model.Type{Kind: "Null"}}}, "List<String>"},
		{"Yson::LookupString", []CallArgument{{Type: ys}, {Type: str}, {Type: options}}, "Optional<String>"},
		{"Yson::Options", []CallArgument{{Name: "Strict", Type: model.Type{Kind: "Bool"}}}, ysonOptionsResource},
		{"Yson::SerializeJson", []CallArgument{{Type: js}, {Name: "EncodeUtf8", Type: model.Type{Kind: "Bool"}}}, "Optional<Json>"},
		{"Yson::SerializeJson", []CallArgument{{Type: model.Optional(node)}, {Type: options}, {Name: "WriteNanAsString", Type: model.Type{Kind: "Bool"}}}, "Optional<Json>"},
		{"Yson::From", []CallArgument{{Type: model.Type{Kind: "Struct", Fields: []model.StructField{{Name: "x", Type: str}}}}}, ysonNodeResource},
		{"Yson::From", []CallArgument{{Type: model.Type{Kind: "Void"}}}, ysonNodeResource},
		{"Yson::From", []CallArgument{{Type: model.Type{Kind: "Null"}}}, ysonNodeResource},
	} {
		got, _, err := resolveYsonCall(tt.name, tt.args)
		if err != nil || got.String() != tt.want {
			t.Errorf("%s = %s, %v; want %s", tt.name, got, err, tt.want)
		}
	}
	target := model.Type{Kind: "Dict", Key: &str, Elem: &js}
	got, _, err := resolveYsonCall("Yson::ConvertTo", []CallArgument{{Type: node}, {TypeArgument: &target}, {Type: options}})
	if err != nil || !got.Equal(target) {
		t.Fatalf("Yson::ConvertTo target = %s, %v; want %s", got, err, target)
	}
	for _, tt := range []struct {
		name string
		args []CallArgument
		want string
	}{
		{"Yson::From", []CallArgument{{Type: model.Type{Kind: "Uuid"}}}, "not supported"},
		{"Yson::From", []CallArgument{{Type: model.Type{Kind: "Dict", Key: ptrType(model.Optional(str)), Elem: &str}}}, "key"},
		{"Yson::From", []CallArgument{{Type: model.Type{Kind: "Struct", Fields: []model.StructField{{Name: "bad", Type: model.Type{Kind: "Uuid"}}}}}}, `field "bad"`},
		{"Yson::From", []CallArgument{{Type: model.Type{Kind: "Tuple", Items: []model.Type{str, {Kind: "Uuid"}}}}}, "not supported"},
		{"Yson::From", []CallArgument{{Name: "value", Type: str}}, "does not support named argument"},
		{"Yson::ConvertTo", []CallArgument{{Type: node}, {Type: str}}, "type expression"},
		{"Yson::ConvertTo", []CallArgument{{Type: str}, {TypeArgument: &str}}, "Yson node"},
		{"Yson::ConvertTo", []CallArgument{{Type: node}, {TypeArgument: ptrType(model.Type{Kind: "Uuid"})}}, "not supported"},
		{"Yson::ConvertTo", []CallArgument{{Type: node}, {TypeArgument: &str}, {Type: str}}, "Options resource"},
		{"Yson::Parse", []CallArgument{{Type: js}}, "Yson or String"},
		{"Yson::Parse", []CallArgument{{Type: model.Optional(model.Optional(ys))}}, "nested Optional"},
		{"Yson::Parse", []CallArgument{{Type: ys}, {Type: str}}, "Options resource"},
		{"Yson::Equals", []CallArgument{{Type: node}, {Type: str}}, "Yson node"},
		{"Yson::LookupString", []CallArgument{{Type: node}, {Type: model.Type{Kind: "Utf8"}}}, "String"},
		{"Yson::Contains", []CallArgument{{Type: node}, {Type: str}, {Type: str}}, "Options resource"},
		{"Yson::GetLength", []CallArgument{{Type: node}, {Type: str}}, "Options resource"},
		{"Yson::SerializeJson", []CallArgument{{Type: node}, {Name: "Bogus", Type: str}}, "unknown named"},
		{"Yson::SerializeJson", []CallArgument{{Type: str}}, "Yson node"},
		{"Yson::Options", []CallArgument{{Name: "Strict", Type: str}}, "Bool"},
		{"Yson::GetHash", []CallArgument{{Type: options}}, "Yson node"},
	} {
		_, handled, err := resolveYsonCall(tt.name, tt.args)
		if !handled || err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s error = %v, handled=%t; want %q", tt.name, err, handled, tt.want)
		}
	}
}
