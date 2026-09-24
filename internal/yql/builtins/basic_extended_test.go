package builtins

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestCurrentAndRandomBuiltinTypes(t *testing.T) {
	for name, kind := range map[string]string{
		"CurrentUtcDate": "Date", "CurrentUtcDatetime": "Datetime", "CurrentUtcTimestamp": "Timestamp",
		"Random": "Double", "RandomNumber": "Uint64", "RandomUuid": "Uuid",
	} {
		t.Run(name, func(t *testing.T) {
			for _, args := range [][]model.Type{nil, {scalar("Uint32")}, {model.Optional(scalar("String")), scalar("Null")}} {
				if len(args) == 0 && strings.HasPrefix(name, "Random") {
					{
						_, err := Resolve(name, args)
						require.Error(t, err)
					}
					continue
				}
				assertResolved(t, strings.ToLower(name), args, scalar(kind))
			}
			{
				_, err := Resolve(name, []model.Type{{Kind: "Any"}})
				require.Error(t, err)
			}
			{
				_, err := defaultRegistry.ResolveCall(name, []CallArgument{{Name: "unexpected", Type: scalar("Uint32")}})
				require.Error(t, err)
			}
		})
	}
	assertResolved(t, "Version", nil, scalar("String"))
	{
		_, err := Resolve("Version", []model.Type{scalar("String")})
		require.Error(t, err)
	}
}

func TestCountIfType(t *testing.T) {
	for _, typ := range []model.Type{scalar("Bool"), model.Optional(scalar("Bool")), scalar("Null")} {
		assertResolved(t, "COUNT_IF", []model.Type{typ}, scalar("Uint64"))
	}
	for _, args := range [][]model.Type{nil, {scalar("Uint64")}, {scalar("String")}, {scalar("Bool"), scalar("Bool")}} {
		{
			_, err := Resolve("COUNT_IF", args)
			require.Error(t, err)
		}
	}
}

func TestCurrentTimezoneBuiltinTypes(t *testing.T) {
	for name, kind := range map[string]string{"CurrentTzDate": "TzDate", "CurrentTzDatetime": "TzDatetime", "CurrentTzTimestamp": "TzTimestamp"} {
		for _, zone := range []model.Type{scalar("String"), model.Optional(scalar("String")), scalar("Null")} {
			assertResolved(t, name, []model.Type{zone}, model.Optional(scalar(kind)))
			assertResolved(t, name, []model.Type{zone, scalar("Uint32"), scalar("Null")}, model.Optional(scalar(kind)))
		}
		for _, args := range [][]model.Type{nil, {scalar("Utf8")}, {scalar("Uint32")}, {scalar("String"), scalar("Any")}} {
			{
				got, err := Resolve(name, args)
				require.Error(t, err)
				require.Equal(t, "", got.Kind)
			}
		}
	}
}

func TestNanvlTypes(t *testing.T) {
	assertResolved(t, "NANVL", []model.Type{scalar("Float"), scalar("Float")}, scalar("Float"))
	assertResolved(t, "NANVL", []model.Type{model.Optional(scalar("Float")), scalar("Double")}, model.Optional(scalar("Double")))
	for _, args := range [][]model.Type{nil, {scalar("Float")}, {scalar("Double"), scalar("Int32")}} {
		{
			_, err := Resolve("NANVL", args)
			require.Error(t, err)
		}
	}
}
