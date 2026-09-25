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

func TestResolveCollectionOperations(t *testing.T) {
	key := model.Type{Kind: "String"}
	value := model.Type{Kind: "Uint64"}
	boolean := model.Type{Kind: "Bool"}
	pair := model.Type{Kind: "Tuple", Items: []model.Type{key, value}}
	list := model.Type{Kind: "List", Elem: &key}
	dict := model.Type{Kind: "Dict", Key: &key, Elem: &value}
	for _, tc := range []struct {
		name string
		args []model.Type
		want model.Type
	}{
		{"AsTuple", []model.Type{key, value}, pair},
		{"AsList", []model.Type{key, key}, list},
		{"ListLength", []model.Type{list}, model.Type{Kind: "Uint64"}},
		{"ListLength", []model.Type{model.Optional(list)}, model.Optional(model.Type{Kind: "Uint64"})},
		{"ListHas", []model.Type{list, key}, boolean},
		{"ListHas", []model.Type{model.Optional(list), key}, boolean},
		{"ListMap", []model.Type{list, {Kind: "Callable", Items: []model.Type{key}, Elem: &value}}, model.Type{Kind: "List", Elem: &value}},
		{"ListMap", []model.Type{model.Optional(list), {Kind: "Callable", Items: []model.Type{key}, Elem: &value}}, model.Optional(model.Type{Kind: "List", Elem: &value})},
		{"ListFilter", []model.Type{list, {Kind: "Callable", Items: []model.Type{key}, Elem: &boolean}}, list},
		{"ListFilter", []model.Type{list, {Kind: "Callable", Items: []model.Type{key}, Elem: &model.Type{Kind: "Optional", Elem: &boolean}}}, list},
		{"ListFilter", []model.Type{model.Optional(list), {Kind: "Callable", Items: []model.Type{key}, Elem: &boolean}}, model.Optional(list)},
		{"ToDict", []model.Type{{Kind: "List", Elem: &pair}}, dict},
		{"ToDict", []model.Type{model.Optional(model.Type{Kind: "List", Elem: &pair})}, model.Optional(dict)},
		{"DictContains", []model.Type{dict, key}, boolean},
		{"DictContains", []model.Type{model.Optional(dict), key}, boolean},
		{"DictLookup", []model.Type{dict, key}, model.Optional(value)},
		{"DictLookup", []model.Type{model.Optional(dict), key}, model.Optional(value)},
		{"DictLookup", []model.Type{{Kind: "Dict", Key: &key, Elem: &model.Type{Kind: "Optional", Elem: &value}}, key}, model.Optional(model.Optional(value))},
		{"DictLookup", []model.Type{{Kind: "Dict", Key: &key, Elem: &model.Type{Kind: "Void"}}, key}, model.Optional(model.Type{Kind: "Void"})},
	} {
		t.Run(tc.name+"_"+tc.want.String(), func(t *testing.T) {
			got, err := Resolve(tc.name, tc.args)
			require.NoError(t, err)
			require.True(t, got.Equal(tc.want), "%s: got %s, want %s", tc.name, got.String(), tc.want.String())
		})
	}
}

func TestResolveCollectionLookupCompatibleStrings(t *testing.T) {
	value := model.Type{Kind: "Uint64"}
	for _, keyKind := range []string{"String", "Utf8"} {
		for _, searchKind := range []string{"String", "Utf8"} {
			for _, optionalKey := range []bool{false, true} {
				for _, optionalSearch := range []bool{false, true} {
					key := model.Type{Kind: keyKind}
					search := model.Type{Kind: searchKind}
					if optionalKey {
						key = model.Optional(key)
					}
					if optionalSearch {
						search = model.Optional(search)
					}
					for _, function := range []string{"ListHas", "DictContains", "DictLookup"} {
						t.Run(function+"_"+key.String()+"_"+search.String(), func(t *testing.T) {
							collection := model.Type{Kind: "Dict", Key: &key, Elem: &value}
							want := model.Type{Kind: "Bool"}
							if function == "ListHas" {
								collection = model.Type{Kind: "List", Elem: &key}
							}
							if function == "DictLookup" {
								want = model.Optional(value)
							}
							got, err := Resolve(function, []model.Type{collection, search})
							require.NoError(t, err)
							require.True(t, got.Equal(want), "got %s, want %s", got.String(), want.String())
						})
					}
				}
			}
		}
	}
}

func TestResolveCollectionLookupNullAndIncompatibleTypes(t *testing.T) {
	for _, function := range []string{"ListHas", "DictContains", "DictLookup"} {
		t.Run(function, func(t *testing.T) {
			key := model.Type{Kind: "String"}
			value := model.Type{Kind: "Uint64"}
			collection := model.Type{Kind: "Dict", Key: &key, Elem: &value}
			if function == "ListHas" {
				collection = model.Type{Kind: "List", Elem: &key}
			}
			_, err := Resolve(function, []model.Type{collection, {Kind: "Null"}})
			require.ErrorContains(t, err, "got Null")
			key = model.Optional(key)
			got, err := Resolve(function, []model.Type{collection, {Kind: "Null"}})
			require.NoError(t, err)
			if function == "DictLookup" {
				require.True(t, got.Equal(model.Optional(value)))
			} else {
				require.Equal(t, "Bool", got.Kind)
			}
			key = model.Type{Kind: "Bool"}
			_, err = Resolve(function, []model.Type{collection, value})
			require.ErrorContains(t, err, "got Uint64")
			key = model.Type{Kind: "String"}
			_, err = Resolve(function, []model.Type{collection, {Kind: "Tuple", Items: []model.Type{value, value}}})
			require.ErrorContains(t, err, "got Tuple")
		})
	}
}

func TestResolveListHasRejectsNonEquatableDocumentElements(t *testing.T) {
	for _, kind := range []string{"Json", "JsonDocument", "Yson"} {
		for _, optional := range []bool{false, true} {
			element := model.Type{Kind: kind}
			if optional {
				element = model.Optional(element)
			}
			t.Run(element.String(), func(t *testing.T) {
				_, err := Resolve("ListHas", []model.Type{{Kind: "List", Elem: &element}, element})
				require.ErrorContains(t, err, "equatable element type")
			})
		}
	}
}

func TestResolveCollectionOperationArity(t *testing.T) {
	for _, tc := range []struct {
		name  string
		arity int
	}{
		{"ListLength", 1},
		{"ListHas", 2},
		{"ListMap", 2},
		{"ListFilter", 2},
		{"ToDict", 1},
		{"DictContains", 2},
		{"DictLookup", 2},
		{"SetIsDisjoint", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, count := range []int{0, tc.arity + 1} {
				args := make([]model.Type, count)
				for i := range args {
					args[i] = model.Type{Kind: "Uint64"}
				}
				got, err := Resolve(tc.name, args)
				require.ErrorContains(t, err, tc.name+" expects")
				require.Empty(t, got.Kind)
			}
		})
	}
}

func TestResolveCollectionPreservesOptionalElementsAndPayloads(t *testing.T) {
	key := model.Optional(model.Type{Kind: "String"})
	value := model.Optional(model.Type{Kind: "Json"})
	list := model.Type{Kind: "List", Elem: &key}
	callback := model.Type{Kind: "Callable", Items: []model.Type{key}, Elem: &value}
	mapped := model.Type{Kind: "List", Elem: &value}
	got, err := Resolve("ListMap", []model.Type{model.Optional(list), callback})
	require.NoError(t, err)
	require.True(t, got.Equal(model.Optional(mapped)), "got %s", got.String())

	pair := model.Type{Kind: "Tuple", Items: []model.Type{key, value}}
	got, err = Resolve("ToDict", []model.Type{{Kind: "List", Elem: &pair}})
	require.NoError(t, err)
	require.True(t, got.Equal(model.Type{Kind: "Dict", Key: &key, Elem: &value}))
	payload, err := Resolve("DictLookup", []model.Type{got, {Kind: "Null"}})
	require.NoError(t, err)
	require.True(t, payload.Equal(model.Optional(value)), "got %s", payload.String())
}

func TestResolveCollectionRejectsUnresolvedNestedTypes(t *testing.T) {
	key := model.Type{Kind: "String"}
	unresolved := model.Type{Kind: "Any"}
	record := model.Type{Kind: "Struct", Fields: []model.StructField{{Name: "value", Type: unresolved}}}
	list := model.Type{Kind: "List", Elem: &key}
	for _, tc := range []struct {
		name string
		args []model.Type
		want string
	}{
		{"ListLength", []model.Type{{Kind: "List", Elem: &record}}, "ListLength argument 1"},
		{"ListMap", []model.Type{list, {Kind: "Callable", Items: []model.Type{key}, Elem: &record}}, "ListMap callback result"},
		{"DictLookup", []model.Type{{Kind: "Dict", Key: &key, Elem: &record}, key}, "DictLookup argument 1"},
		{"ListHas", []model.Type{list, record}, "ListHas argument 2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Resolve(tc.name, tc.args)
			require.ErrorContains(t, err, tc.want)
			require.ErrorContains(t, err, "unsupported type")
			require.Empty(t, got.Kind)
		})
	}
	got, err := defaultRegistry.ResolveCall("AsStruct", []CallArgument{{Name: "details", Type: record}})
	require.ErrorContains(t, err, `AsStruct field "details"`)
	require.ErrorContains(t, err, "unsupported type")
	require.Empty(t, got.Kind)
}

func TestResolveCollectionOperationsRejectInvalidCalls(t *testing.T) {
	key := model.Type{Kind: "String"}
	value := model.Type{Kind: "Uint64"}
	boolean := model.Type{Kind: "Bool"}
	list := model.Type{Kind: "List", Elem: &key}
	dict := model.Type{Kind: "Dict", Key: &key, Elem: &value}
	for _, tc := range []struct {
		name string
		args []model.Type
		want string
	}{
		{"AsTuple", nil, "empty Tuple"},
		{"AsTuple", []model.Type{{Kind: "Any"}}, "argument 1"},
		{"AsList", nil, "empty List"},
		{"AsList", []model.Type{key, value}, "common type"},
		{"ListLength", []model.Type{key}, "List"},
		{"ListHas", []model.Type{list, value}, "element type"},
		{"ListHas", []model.Type{key, key}, "ListHas argument 1 must be List"},
		{"ListMap", []model.Type{key, boolean}, "ListMap argument 1 must be List"},
		{"lIsTmAp", []model.Type{key, boolean}, "ListMap argument 1 must be List"},
		{"ListMap", []model.Type{list, boolean}, "Callable"},
		{"ListMap", []model.Type{list, {Kind: "Callable", Items: []model.Type{value}, Elem: &value}}, "callback argument"},
		{"ListFilter", []model.Type{list, {Kind: "Callable", Items: []model.Type{key}, Elem: &value}}, "Bool"},
		{"ListMap", []model.Type{list, {Kind: "Callable", Items: []model.Type{key, key}, Elem: &value}}, "one-argument Callable"},
		{"ListMap", []model.Type{list, {Kind: "Callable", Items: []model.Type{key}, Elem: &model.Type{Kind: "Null"}}}, "concrete value type"},
		{"ListMap", []model.Type{list, {Kind: "Callable", Items: []model.Type{key}, Elem: &model.Type{Kind: "Void"}}}, "concrete value type"},
		{"ToDict", []model.Type{list}, "Tuple"},
		{"ToDict", []model.Type{key}, "ToDict argument 1 must be List"},
		{"ToDict", []model.Type{{Kind: "List", Elem: &model.Type{Kind: "Tuple", Items: []model.Type{key, value, value}}}}, "List<Tuple<K,V>>"},
		{"ToDict", []model.Type{{Kind: "List", Elem: &model.Type{Kind: "Tuple", Items: []model.Type{{Kind: "Json"}, value}}}}, "unsupported dictionary key type Json"},
		{"DictContains", []model.Type{list, key}, "Dict<K,V>"},
		{"dIcTcOnTaInS", []model.Type{list, key}, "DictContains argument 1 must be Dict<K,V>"},
		{"DictLookup", []model.Type{{Kind: "Dict", Key: &model.Type{Kind: "Json"}, Elem: &value}, key}, "unsupported dictionary key type Json"},
		{"DictContains", []model.Type{dict, value}, "key type"},
		{"DictLookup", []model.Type{dict, value}, "key type"},
	} {
		t.Run(tc.name+"_"+tc.want, func(t *testing.T) {
			_, err := Resolve(tc.name, tc.args)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestResolveAsStructFields(t *testing.T) {
	args := []CallArgument{
		{Name: "name", Type: model.Type{Kind: "Utf8"}},
		{Name: "count", Type: model.Type{Kind: "Uint64"}},
	}
	want := model.Type{Kind: "Struct", Fields: []model.StructField{{Name: "name", Type: args[0].Type}, {Name: "count", Type: args[1].Type}}}
	_, err := defaultRegistry.ResolveCall("AsStruct", nil)
	require.ErrorContains(t, err, "empty Struct")
	got, err := defaultRegistry.ResolveCall("AsStruct", args)
	require.NoError(t, err)
	require.True(t, got.Equal(want))
	_, err = defaultRegistry.ResolveCall("AsStruct", []CallArgument{{Type: args[0].Type}})
	require.ErrorContains(t, err, "field name")
	_, err = defaultRegistry.ResolveCall("AsStruct", []CallArgument{args[0], args[0]})
	require.ErrorContains(t, err, "duplicate")
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
