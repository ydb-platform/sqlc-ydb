package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParameterTypeConfiguration(t *testing.T) {
	c, err := Parse([]byte(`version: "2"
sql:
- engine: ydb
  schema: schema.sql
  queries: queries.sql
  analyzer:
    parameters:
      CreateBooks:
        books: List<Struct<id:Uint64,title:Utf8>>
        имя: Utf8
`))
	require.NoError(t, err)
	require.Equal(t, map[string]map[string]string{
		"CreateBooks": {"books": "List<Struct<id:Uint64,title:Utf8>>", "имя": "Utf8"},
	}, c.SQL[0].Analyzer.Parameters)
}

func TestRejectInvalidParameterTypeConfiguration(t *testing.T) {
	const prefix = "version: '2'\nsql:\n- engine: ydb\n  schema: schema.sql\n  queries: queries.sql\n  analyzer:\n    parameters:\n"
	for _, tc := range []struct{ name, input, want string }{
		{"empty query name", "      '': {id: Uint64}\n", "query name is required"},
		{"empty query parameters", "      Read: {}\n", "at least one parameter is required"},
		{"empty parameter name", "      Read: {'': Uint64}\n", "parameter name"},
		{"dollar prefix", "      Read: {'$id': Uint64}\n", "omit the leading $"},
		{"empty parameter type", "      Read: {id: ''}\n", "type is required"},
		{"duplicate query", "      Read: {id: Uint64}\n      Read: {id: Utf8}\n", "already defined"},
		{"duplicate parameter", "      Read: {id: Uint64, id: Utf8}\n", "already defined"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(prefix + tc.input))
			require.ErrorContains(t, err, tc.want)
		})
	}
}
