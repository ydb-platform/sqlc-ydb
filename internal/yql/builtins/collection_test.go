package builtins

import (
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestResolveCollectionFunctions(t *testing.T) {
	stringType := model.Type{Kind: "String"}
	uint64Type := model.Type{Kind: "Uint64"}
	voidType := model.Type{Kind: "Void"}
	set := model.Type{Kind: "Dict", Key: &stringType, Elem: &voidType}
	for _, tc := range []struct {
		name string
		args []model.Type
		want model.Type
	}{
		{"ToSet", []model.Type{{Kind: "List", Elem: &stringType}}, set},
		{"SetIsDisjoint", []model.Type{set, {Kind: "List", Elem: &stringType}}, model.Type{Kind: "Bool"}},
		{"SetIsDisjoint", []model.Type{set, {Kind: "Dict", Key: &stringType, Elem: &uint64Type}}, model.Type{Kind: "Bool"}},
		{"Yson::ConvertToStringList", []model.Type{{Kind: "Json"}}, model.Type{Kind: "List", Elem: &stringType}},
		{"Yson::ConvertToStringList", []model.Type{model.Optional(model.Type{Kind: "Yson"})}, model.Type{Kind: "List", Elem: &stringType}},
		{"ToSet", []model.Type{model.Optional(model.Type{Kind: "List", Elem: &stringType})}, model.Optional(set)},
		{"SetIsDisjoint", []model.Type{model.Optional(set), model.Optional(model.Type{Kind: "List", Elem: &stringType})}, model.Optional(model.Type{Kind: "Bool"})},
	} {
		got, err := Resolve(tc.name, tc.args)
		if err != nil {
			t.Fatalf("Resolve(%s): %v", tc.name, err)
		}
		if !got.Equal(tc.want) {
			t.Fatalf("Resolve(%s) = %s, want %s", tc.name, got.String(), tc.want.String())
		}
	}
}

func TestResolveCollectionFunctionsKeepOptionalKeys(t *testing.T) {
	stringType := model.Type{Kind: "String"}
	optionalString := model.Optional(stringType)
	list := model.Type{Kind: "List", Elem: &optionalString}
	set, err := Resolve("ToSet", []model.Type{list})
	if err != nil {
		t.Fatal(err)
	}
	if set.Kind != "Dict" || set.Key == nil || !set.Key.Equal(optionalString) {
		t.Fatalf("ToSet(List<String?>) = %s", set.String())
	}
	got, err := Resolve("SetIsDisjoint", []model.Type{set, list})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(model.Type{Kind: "Bool"}) {
		t.Fatalf("SetIsDisjoint() = %s", got.String())
	}
}

func TestResolveCollectionFunctionsRejectNestedOptionalKey(t *testing.T) {
	stringType := model.Type{Kind: "String"}
	nested := model.Optional(model.Optional(stringType))
	list := model.Type{Kind: "List", Elem: &nested}
	if _, err := Resolve("ToSet", []model.Type{list}); err == nil || !strings.Contains(err.Error(), "nested Optional") {
		t.Fatalf("ToSet nested Optional key error = %v", err)
	}
}

func TestResolveToSetTupleKey(t *testing.T) {
	inner := model.Type{Kind: "Tuple", Items: []model.Type{{Kind: "String"}, {Kind: "Uint64"}}}
	for _, key := range []model.Type{
		{Kind: "Tuple", Items: []model.Type{inner, model.Optional(model.Type{Kind: "Uint64"})}},
		model.Optional(inner),
	} {
		got, err := Resolve("ToSet", []model.Type{{Kind: "List", Elem: &key}})
		if err != nil {
			t.Fatal(err)
		}
		if got.Key == nil || !got.Key.Equal(key) {
			t.Fatalf("ToSet() = %s", got.String())
		}
	}
}

func TestResolveToSetDecimalKey(t *testing.T) {
	key := model.Type{Kind: "Decimal", Precision: 22, Scale: 9}
	got, err := Resolve("ToSet", []model.Type{{Kind: "List", Elem: &key}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Key == nil || !got.Key.Equal(key) {
		t.Fatalf("ToSet() = %s", got.String())
	}
}

func TestResolveCollectionFunctionsRejectInvalidCalls(t *testing.T) {
	stringType := model.Type{Kind: "String"}
	uint64Type := model.Type{Kind: "Uint64"}
	voidType := model.Type{Kind: "Void"}
	set := model.Type{Kind: "Dict", Key: &stringType, Elem: &voidType}
	for _, tc := range []struct {
		name string
		args []model.Type
		want string
	}{
		{"ToSet", []model.Type{stringType}, "List"},
		{"ToSet", nil, "expects 1"},
		{"ToSet", []model.Type{{Kind: "List"}}, "List"},
		{"ToSet", []model.Type{{Kind: "List", Elem: &model.Type{Kind: "Optional"}}}, "Optional has no element"},
		{"ToSet", []model.Type{{Kind: "List", Elem: &model.Type{Kind: "Tuple", Items: []model.Type{stringType}}}}, "at least two"},
		{"ToSet", []model.Type{{Kind: "List", Elem: &model.Type{Kind: "String", Elem: &stringType}}}, "invalid dictionary key"},
		{"ToSet", []model.Type{{Kind: "List", Elem: &model.Type{Kind: "Json"}}}, "dictionary key"},
		{"ToSet", []model.Type{{Kind: "List", Elem: &model.Type{Kind: "JsonDocument"}}}, "dictionary key"},
		{"SetIsDisjoint", []model.Type{model.Optional(model.Type{Kind: "Optional", Elem: &set}), set}, "argument 1"},
		{"SetIsDisjoint", []model.Type{{Kind: "Dict"}, set}, "argument 1"},
		{"SetIsDisjoint", []model.Type{set, {Kind: "List", Elem: &uint64Type}}, "same key type"},
		{"SetIsDisjoint", []model.Type{set, {Kind: "Optional"}}, "argument 2"},
		{"SetIsDisjoint", []model.Type{set, {Kind: "List"}}, "no element type"},
		{"SetIsDisjoint", []model.Type{set, {Kind: "Dict"}}, "requires key and value"},
		{"SetIsDisjoint", []model.Type{set, stringType}, "argument 2"},
		{"SetIsDisjoint", []model.Type{{Kind: "Dict", Key: &model.Type{Kind: "Json"}, Elem: &voidType}, {Kind: "List", Elem: &model.Type{Kind: "Json"}}}, "argument 1"},
		{"SetIsDisjoint", []model.Type{set, {Kind: "List", Elem: &model.Type{Kind: "Yson"}}}, "argument 2"},
		{"SetIsDisjoint", []model.Type{set, {Kind: "Dict", Key: &model.Type{Kind: "Json"}, Elem: &voidType}}, "argument 2"},
		{"SetIsDisjoint", []model.Type{{Kind: "List", Elem: &stringType}, {Kind: "List", Elem: &stringType}}, "argument 1"},
		{"Yson::ConvertToStringList", []model.Type{stringType}, "Yson node Resource, Yson, or Json"},
		{"Yson::ConvertToStringList", nil, "expects 1"},
	} {
		_, err := Resolve(tc.name, tc.args)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("Resolve(%s) error = %v, want %q", tc.name, err, tc.want)
		}
	}
}

func TestRegistryRejectsCollectionFunctionOverride(t *testing.T) {
	for _, name := range []string{"ToSet", "SetIsDisjoint", "Yson::ConvertToStringList"} {
		_, err := NewRegistry([]Signature{{Name: name, Returns: model.Type{Kind: "Bool"}}})
		if err == nil || !strings.Contains(err.Error(), "known built-in") {
			t.Fatalf("NewRegistry(%s) error = %v", name, err)
		}
	}
}

func TestYsonConvertToStringListPreservesBaseTypeError(t *testing.T) {
	inner := model.Type{Kind: "Optional", Elem: &model.Type{Kind: "String"}}
	_, err := Resolve("Yson::ConvertToStringList", []model.Type{{Kind: "Optional", Elem: &inner}})
	if err == nil || !strings.Contains(err.Error(), "nested Optional") {
		t.Fatalf("error = %v, want nested Optional cause", err)
	}
}
