package builtins

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestResolveUnwrap(t *testing.T) {
	json := model.Type{Kind: "Json"}
	text := model.Type{Kind: "String"}
	for _, tc := range []struct {
		args []model.Type
		want model.Type
	}{
		{[]model.Type{model.Optional(json)}, json},
		{[]model.Type{model.Optional(json), text}, json},
		{[]model.Type{model.Optional(json), {Kind: "Utf8"}}, json},
		{[]model.Type{json}, json},
		{[]model.Type{model.Optional(model.Optional(json))}, model.Optional(json)},
	} {
		got, err := Resolve("UNWRAP", tc.args)
		require.NoError(t, err)
		require.True(t, got.Equal(tc.want), "got %s, want %s", got.String(), tc.want.String())
	}
}

func TestResolveUnwrapRejectsInvalidArguments(t *testing.T) {
	for _, tc := range []struct {
		args []model.Type
		want string
	}{
		{nil, "expects 1 or 2"},
		{[]model.Type{{Kind: "String"}, {Kind: "String"}, {Kind: "String"}}, "expects 1 or 2"},
		{[]model.Type{{Kind: "Null"}}, "concrete"},
		{[]model.Type{{Kind: "Optional"}}, "no element"},
		{[]model.Type{{Kind: "Any"}}, "unsupported type"},
		{[]model.Type{model.Optional(model.Type{Kind: "Json"}), {Kind: "Uint64"}}, "String"},
		{[]model.Type{model.Optional(model.Type{Kind: "Json"}), model.Optional(model.Type{Kind: "String"})}, "String"},
		{[]model.Type{model.Optional(model.Type{Kind: "Json"}), {Kind: "Null"}}, "String or Utf8"},
	} {
		_, err := Resolve("UNWRAP", tc.args)
		require.ErrorContains(t, err, tc.want)
	}
}
