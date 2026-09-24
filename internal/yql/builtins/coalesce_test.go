package builtins

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestCoalesceIntegerLiterals(t *testing.T) {
	literal := func(kind, value string) CallArgument {
		integer, ok := new(big.Int).SetString(value, 10)
		require.True(t, ok)
		return CallArgument{Type: scalar(kind), IntegerLiteral: integer}
	}
	u32 := CallArgument{Type: model.Optional(scalar("Uint32"))}
	u64 := CallArgument{Type: model.Optional(scalar("Uint64"))}
	for _, tc := range []struct {
		name string
		args []CallArgument
		want model.Type
	}{
		{"fitting fallback", []CallArgument{u32, literal("Int32", "0")}, scalar("Uint32")},
		{"explicit wide fitting fallback", []CallArgument{u32, literal("Uint64", "4294967295")}, scalar("Uint32")},
		{"negative fallback uses common type", []CallArgument{u32, literal("Int32", "-1")}, scalar("Int32")},
		{"large fallback uses common type", []CallArgument{u32, literal("Int64", "4294967296")}, scalar("Int64")},
		{"literal first retains its type", []CallArgument{literal("Int32", "0"), u32}, scalar("Int32")},
		{"negative with Uint64 uses server common type", []CallArgument{u64, literal("Int32", "-1")}, scalar("Uint64")},
		{"typed parameter is not a fitting literal", []CallArgument{u32, {Type: scalar("Int64")}}, scalar("Int64")},
		{"optional fallbacks", []CallArgument{u32, {Type: model.Optional(scalar("Int64"))}}, model.Optional(scalar("Int64"))},
		{"null before typed fallback", []CallArgument{{Type: scalar("Null")}, u32, literal("Int32", "0")}, scalar("Uint32")},
		{"signed minimum fits", []CallArgument{{Type: model.Optional(scalar("Int8"))}, literal("Int32", "-128")}, scalar("Int8")},
		{"below signed minimum widens", []CallArgument{{Type: model.Optional(scalar("Int8"))}, literal("Int32", "-129")}, scalar("Int32")},
		{"above signed maximum widens", []CallArgument{{Type: model.Optional(scalar("Int8"))}, literal("Int32", "128")}, scalar("Int32")},
		{"null fallback keeps optional", []CallArgument{u32, {Type: scalar("Null")}}, u32.Type},
		{"String then Utf8", []CallArgument{{Type: model.Optional(scalar("String"))}, {Type: scalar("Utf8")}}, scalar("String")},
		{"Utf8 then String", []CallArgument{{Type: model.Optional(scalar("Utf8"))}, {Type: scalar("String")}}, scalar("String")},
		{"optional string family", []CallArgument{{Type: model.Optional(scalar("Utf8"))}, {Type: model.Optional(scalar("String"))}}, model.Optional(scalar("String"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, name := range []string{"COALESCE", "NVL"} {
				got, err := defaultRegistry.ResolveCall(name, tc.args)
				require.NoError(t, err)
				require.True(t, got.Equal(tc.want))
			}
		})
	}
	arg := literal("Int32", "0")
	_, _ = defaultRegistry.ResolveCall("COALESCE", []CallArgument{u32, arg})
	require.Equal(t, "Int32", arg.Type.Kind)
	require.Equal(t, 0, arg.IntegerLiteral.Sign())
}

func TestCoalesceRejectsUnresolvedAndIncompatibleTypes(t *testing.T) {
	for _, args := range [][]CallArgument{
		{{Type: model.Type{Kind: "Any"}}},
		{{Type: scalar("String")}, {Type: scalar("Bool")}},
		{{Type: model.Optional(model.Optional(scalar("Int32")))}},
	} {
		{
			got, err := defaultRegistry.ResolveCall("COALESCE", args)
			require.Error(t, err)
			require.Equal(t, "", got.Kind)
		}
	}
}
