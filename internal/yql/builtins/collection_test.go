package builtins

import (
	"testing"

	"github.com/stretchr/testify/require"

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
		require.NoError(t, err)
		require.True(t, got.Equal(tc.want))
	}
}

func TestResolveCollectionFunctionsKeepOptionalKeys(t *testing.T) {
	stringType := model.Type{Kind: "String"}
	optionalString := model.Optional(stringType)
	list := model.Type{Kind: "List", Elem: &optionalString}
	set, err := Resolve("ToSet", []model.Type{list})
	require.NoError(t, err)
	require.Equal(t, "Dict", set.Kind)
	require.NotNil(t, set.Key)
	require.True(t, set.Key.Equal(optionalString))
	got, err := Resolve("SetIsDisjoint", []model.Type{set, list})
	require.NoError(t, err)
	require.True(t, got.Equal(model.Type{Kind: "Bool"}))
}

func TestResolveCollectionFunctionsRejectNestedOptionalKey(t *testing.T) {
	stringType := model.Type{Kind: "String"}
	nested := model.Optional(model.Optional(stringType))
	list := model.Type{Kind: "List", Elem: &nested}
	{
		_, err := Resolve("ToSet", []model.Type{list})
		require.ErrorContains(t, err, "nested Optional")
	}
}

func TestResolveToSetTupleKey(t *testing.T) {
	inner := model.Type{Kind: "Tuple", Items: []model.Type{{Kind: "String"}, {Kind: "Uint64"}}}
	for _, key := range []model.Type{
		{Kind: "Tuple", Items: []model.Type{inner, model.Optional(model.Type{Kind: "Uint64"})}},
		model.Optional(inner),
	} {
		got, err := Resolve("ToSet", []model.Type{{Kind: "List", Elem: &key}})
		require.NoError(t, err)
		require.NotNil(t, got.Key)
		require.True(t, got.Key.Equal(key))
	}
}

func TestResolveToSetDecimalKey(t *testing.T) {
	key := model.Type{Kind: "Decimal", Precision: 22, Scale: 9}
	got, err := Resolve("ToSet", []model.Type{{Kind: "List", Elem: &key}})
	require.NoError(t, err)
	require.NotNil(t, got.Key)
	require.True(t, got.Key.Equal(key))
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
		{"ListCreate", nil, "expects 1"},
		{"ListCreate", []model.Type{{Kind: "Any"}}, "concrete element type"},
		{"ListCreate", []model.Type{{Kind: "Null"}}, "concrete element type"},
		{"ListCreate", []model.Type{{Kind: "Void"}}, "concrete element type"},
		{"ToSet", []model.Type{stringType}, "List"},
		{"ToSet", nil, "expects 1"},
		{"ToSet", []model.Type{{Kind: "List"}}, "List"},
		{"ToSet", []model.Type{{Kind: "List", Elem: &model.Type{Kind: "Optional"}}}, "Optional has no element"},
		{"ToSet", []model.Type{{Kind: "List", Elem: &model.Type{Kind: "Tuple", Items: []model.Type{stringType}}}}, "at least two"},
		{"ToSet", []model.Type{{Kind: "List", Elem: &model.Type{Kind: "Tuple", Items: []model.Type{stringType, {Kind: "Json"}}}}}, "unsupported dictionary key type Json"},
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
		require.ErrorContains(t, err, tc.want)
	}
}

func TestRegistryRejectsCollectionFunctionOverride(t *testing.T) {
	for _, name := range []string{"ToSet", "SetIsDisjoint", "Yson::ConvertToStringList"} {
		_, err := NewRegistry([]Signature{{Name: name, Returns: model.Type{Kind: "Bool"}}})
		require.ErrorContains(t, err, "known built-in")
	}
}

func TestYsonConvertToStringListPreservesBaseTypeError(t *testing.T) {
	inner := model.Type{Kind: "Optional", Elem: &model.Type{Kind: "String"}}
	_, err := Resolve("Yson::ConvertToStringList", []model.Type{{Kind: "Optional", Elem: &inner}})
	require.ErrorContains(t, err, "nested Optional")
}
