package builtins

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestAggregateListResultAndArguments(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []model.Type
		want string
	}{
		{"AGGREGATE_LIST", []model.Type{{Kind: "String"}}, "List<String>"},
		{"aggregate_list", []model.Type{model.Optional(model.Type{Kind: "String"})}, "List<String>"},
		{"AGG_LIST", []model.Type{{Kind: "Uint64"}, {Kind: "Int32"}}, "List<Uint64>"},
		{"AGGREGATE_LIST", []model.Type{{Kind: "String"}, {Kind: "Int8"}}, "List<String>"},
		{"AGGREGATE_LIST", []model.Type{{Kind: "String"}, {Kind: "Int16"}}, "List<String>"},
		{"AGGREGATE_LIST", []model.Type{{Kind: "String"}, {Kind: "Uint8"}}, "List<String>"},
		{"AGGREGATE_LIST", []model.Type{{Kind: "String"}, {Kind: "Uint16"}}, "List<String>"},
		{"AGGREGATE_LIST", []model.Type{{Kind: "String"}, {Kind: "Uint32"}}, "List<String>"},
		{"AGGREGATE_LIST", []model.Type{{Kind: "String"}, {Kind: "Uint64"}}, "List<String>"},
		{"AGGREGATE_LIST_DISTINCT", []model.Type{{Kind: "Utf8"}}, "List<Utf8>"},
	} {
		t.Run(tc.name+typeList(tc.args), func(t *testing.T) {
			got, err := Resolve(tc.name, tc.args)
			require.NoError(t, err)
			require.Equal(t, tc.want, got.String())
		})
	}
	for _, tc := range []struct {
		args []model.Type
		want string
	}{
		{nil, "expects 1 or 2"},
		{[]model.Type{{Kind: "Null"}}, "concrete"},
		{[]model.Type{{Kind: "String"}, {Kind: "Bool"}}, "integer"},
		{[]model.Type{{Kind: "String"}, {Kind: "Int64"}}, "Uint64"},
		{[]model.Type{{Kind: "String"}, model.Optional(model.Type{Kind: "Int32"})}, "non-optional"},
		{[]model.Type{{Kind: "String"}, {Kind: "Uint64"}, {Kind: "Uint64"}}, "expects 1 or 2"},
	} {
		_, err := Resolve("AGGREGATE_LIST", tc.args)
		require.ErrorContains(t, err, tc.want)
	}
}

func TestJsonFromSerializeJson(t *testing.T) {
	item := model.Type{Kind: "String"}
	list := model.Type{Kind: "List", Elem: &item}
	uintType := model.Type{Kind: "Uint64"}
	dict := model.Type{Kind: "Dict", Key: &item, Elem: &uintType}
	structure := model.Type{Kind: "Struct", Fields: []model.StructField{{Name: "name", Type: item}, {Name: "count", Type: uintType}}}
	for _, name := range []string{"Json::From", "Yson::From"} {
		for _, arg := range []model.Type{item, uintType, model.Optional(item), list, model.Optional(list), {Kind: "List", Elem: typePointer(model.Type{Kind: "Utf8"})}, dict, structure, {Kind: "Tuple", Items: []model.Type{item, uintType}}} {
			resource, err := Resolve(name, []model.Type{arg})
			require.NoError(t, err)
			require.Equal(t, "Resource<'Yson2.Node'>", resource.String())
			got, err := Resolve("Yson::SerializeJson", []model.Type{resource})
			require.NoError(t, err)
			require.Equal(t, "Optional<Json>", got.String())
		}
	}
	for _, tc := range []struct {
		name string
		args []model.Type
		want string
	}{
		{"Json::From", nil, "expects 1"},
		{"Json::From", []model.Type{{Kind: "Date"}}, "not supported"},
		{"Json::From", []model.Type{{Kind: "List", Elem: typePointer(model.Type{Kind: "Date"})}}, "not supported"},
		{"Json::From", []model.Type{{Kind: "Dict", Key: typePointer(model.Optional(item)), Elem: &uintType}}, "dictionary key"},
		{"Yson::SerializeJson", []model.Type{{Kind: "String"}}, "Resource"},
		{"Yson::SerializeJson", []model.Type{{Kind: "Resource<'Other.Node'>"}}, "Resource"},
	} {
		_, err := Resolve(tc.name, tc.args)
		require.ErrorContains(t, err, tc.want)
	}
}

func TestListCreateTypeAndContextualEmptyFallback(t *testing.T) {
	stringType := model.Type{Kind: "String"}
	innerList := model.Type{Kind: "List", Elem: &stringType}
	emptyList, err := Resolve("ListCreate", []model.Type{model.Optional(innerList)})
	require.NoError(t, err)
	require.Equal(t, "List<Optional<List<String>>>", emptyList.String())
	for _, name := range []string{"NVL", "COALESCE"} {
		got, err := defaultRegistry.ResolveCall(name, []CallArgument{
			{Type: model.Optional(innerList)},
			{Type: emptyList, EmptyList: true},
		})
		require.NoError(t, err)
		require.Equal(t, "List<String>", got.String())
		{
			_, err := defaultRegistry.ResolveCall(name, []CallArgument{
				{Type: model.Optional(innerList)},
				{Type: emptyList},
			})
			require.Error(t, err)
		}
	}
}
