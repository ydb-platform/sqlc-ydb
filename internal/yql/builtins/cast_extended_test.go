package builtins

import (
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestBooleanAndTimestampCasts(t *testing.T) {
	for _, integer := range []string{"Int8", "Int16", "Int32", "Int64", "Uint8", "Uint16", "Uint32", "Uint64"} {
		for _, pair := range [][2]string{{integer, "Bool"}, {"Bool", integer}} {
			for _, optionalInput := range []bool{false, true} {
				source, want := scalar(pair[0]), scalar(pair[1])
				if optionalInput {
					source, want = model.Optional(source), model.Optional(want)
				}
				got, err := Cast(source, scalar(pair[1]))
				if err != nil || !got.Equal(want) {
					t.Fatalf("CAST %s AS %s = %s, %v; want %s", source.String(), pair[1], got.String(), err, want.String())
				}
			}
		}
	}
	for _, tc := range []struct {
		source, target string
		optional       bool
	}{
		{"Timestamp", "String", false}, {"Timestamp", "Uint64", false}, {"Uint64", "Timestamp", true},
		{"Date", "String", false}, {"Datetime", "String", false},
		{"String", "Json", true}, {"Utf8", "Json", true},
	} {
		for _, optionalInput := range []bool{false, true} {
			source, want := scalar(tc.source), scalar(tc.target)
			if optionalInput {
				source = model.Optional(source)
			}
			if tc.optional || optionalInput {
				want = model.Optional(want)
			}
			got, err := Cast(source, scalar(tc.target))
			if err != nil || !got.Equal(want) {
				t.Fatalf("CAST %s AS %s = %s, %v; want %s", source.String(), tc.target, got.String(), err, want.String())
			}
		}
	}
}
