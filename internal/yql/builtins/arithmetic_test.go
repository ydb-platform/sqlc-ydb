package builtins

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestArithmetic(t *testing.T) {
	for _, tc := range []struct{ left, right, want string }{
		{"Int8", "Int8", "Int8"}, {"Int32", "Int64", "Int64"},
		{"Uint32", "Int32", "Int32"}, {"Uint64", "Int32", "Uint64"},
		{"Int64", "Float", "Float"}, {"Float", "Double", "Double"},
	} {
		for _, op := range []string{"+", "-", "*"} {
			for _, nullable := range []bool{false, true} {
				left, right, want := model.Type{Kind: tc.left}, model.Type{Kind: tc.right}, model.Type{Kind: tc.want}
				if nullable {
					left = model.Optional(left)
					want = model.Optional(want)
				}
				got, err := Arithmetic(op, left, right)
				require.NoError(t, err)
				require.True(t, got.Equal(want))
			}
		}
	}
	for _, kind := range []string{"Null", "Bool", "Utf8", "Date", "Decimal"} {
		{
			_, err := Arithmetic("+", model.Type{Kind: kind}, model.Type{Kind: "Int64"})
			require.Error(t, err)
		}
	}
	for _, op := range []string{"/", "%", "||"} {
		{
			_, err := Arithmetic(op, model.Type{Kind: "Int64"}, model.Type{Kind: "Int64"})
			require.Error(t, err)
		}
	}
}

func TestCanWidenInteger(t *testing.T) {
	for _, tc := range []struct {
		source, target string
		want           bool
	}{
		{"Int8", "Int64", true}, {"Uint32", "Uint64", true}, {"Uint32", "Int64", true},
		{"Int64", "Int32", false}, {"Uint64", "Int64", false}, {"Int32", "Uint64", false},
		{"Double", "Int64", false}, {"Int64", "Double", false}, {"Bool", "Int64", false},
	} {
		source, target := model.Type{Kind: tc.source}, model.Type{Kind: tc.target}
		for _, optionalSource := range []bool{false, true} {
			for _, optionalTarget := range []bool{false, true} {
				s, d := source, target
				if optionalSource {
					s = model.Optional(s)
				}
				if optionalTarget {
					d = model.Optional(d)
				}
				{
					got := CanWidenInteger(s, d)
					require.Equal(t, (tc.want && (!optionalSource || optionalTarget)), got)
				}
			}
		}
	}
}
