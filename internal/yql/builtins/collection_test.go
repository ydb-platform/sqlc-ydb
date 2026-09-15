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
		{"ToSet", []model.Type{{Kind: "List", Elem: &model.Type{Kind: "Json"}}}, "dictionary key"},
		{"SetIsDisjoint", []model.Type{set, {Kind: "List", Elem: &uint64Type}}, "same key type"},
		{"SetIsDisjoint", []model.Type{{Kind: "List", Elem: &stringType}, {Kind: "List", Elem: &stringType}}, "argument 1"},
		{"Yson::ConvertToStringList", []model.Type{stringType}, "Json or Yson"},
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
