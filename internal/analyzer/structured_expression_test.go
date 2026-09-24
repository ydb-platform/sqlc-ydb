package analyzer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
	"github.com/ydb-platform/sqlc-ydb/internal/yql/builtins"
)

func TestStructuredMemberExpressions(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY(id));"}}
	for _, tc := range []struct{ name, declaration, sql, result, failure string }{
		{"projection", "Struct<id:Uint64,label:Utf8>", "SELECT $key.id AS value;", "Uint64", ""},
		{"predicate", "Struct<id:Uint64,label:Utf8>", "SELECT id AS value FROM records WHERE id=$key.id AND label=$key.label;", "Uint64", ""},
		{"function", "Struct<label:Utf8>", "SELECT Unicode::GetLength($key.label) AS value;", "Uint64", ""},
		{"nested", "Struct<inner:Struct<id:Uint64>>", "SELECT $key.inner.id AS value;", "Uint64", ""},
		{"quoted field", "Struct<`record-id`:Uint64>", "SELECT $key.`record-id` AS value;", "Uint64", ""},
		{"optional field", "Struct<id:Optional<Uint64>>", "SELECT $key.id AS value;", "Optional<Uint64>", ""},
		{"optional struct", "Optional<Struct<id:Uint64>>", "SELECT $key.id AS value;", "Optional<Uint64>", ""},
		{"optional struct and field", "Optional<Struct<id:Optional<Uint64>>>", "SELECT $key.id AS value;", "Optional<Uint64>", ""},
		{"missing", "Struct<id:Uint64>", "SELECT id FROM records WHERE id=$key.missing;", "", "unknown struct field"},
		{"wrong type", "Struct<id:Utf8>", "SELECT id FROM records WHERE id=$key.id;", "", "incompatible"},
		{"wrong base", "Uint64", "SELECT $key.id AS value;", "", "requires Struct"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Lookup :many\nDECLARE $key AS " + tc.declaration + ";\n" + tc.sql}})
			if tc.failure != "" {
				require.ErrorContains(t, err, tc.failure)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.result, got.Queries[0].ResultSets[0].Columns[0].Type.String())
			require.Len(t, got.Queries[0].Parameters, 1)
			require.Equal(t, "key", got.Queries[0].Parameters[0].Name)
		})
	}
}

func TestFunctionReturningStructMemberAccessUsesFieldNames(t *testing.T) {
	resultType := model.Type{Kind: "Struct", Fields: []model.StructField{
		{Name: "label", Type: model.Type{Kind: "Utf8"}},
		{Name: "id", Type: model.Type{Kind: "Uint64"}},
	}}
	options := Options{Functions: []builtins.Signature{{Name: "MakeRecord", Returns: resultType}}}
	got, err := AnalyzeWithOptions(nil, []model.Source{{Name: "query.sql", Text: "-- name: Read :one\nSELECT MakeRecord().id AS value;"}}, options)
	require.NoError(t, err)
	require.Equal(t, "Uint64", got.Queries[0].ResultSets[0].Columns[0].Type.Kind)
}

func TestUnaryNotResolvesFunctionAtomically(t *testing.T) {
	options := Options{Functions: []builtins.Signature{{Name: "IsReady", Returns: model.Type{Kind: "Bool"}}}}
	{
		_, err := AnalyzeWithOptions(nil, []model.Source{{Name: "query.sql", Text: "-- name: Read :one\nSELECT true AS value WHERE NOT IsReady();"}}, options)
		require.NoError(t, err)
	}
	_, err := AnalyzeWithOptions(nil, []model.Source{{Name: "query.sql", Text: "-- name: Read :one\nSELECT true AS value WHERE NOT Unknown::Ready();"}}, options)
	require.ErrorContains(t, err, "unsupported YQL function")
}

func TestConfiguredStructTypeValidation(t *testing.T) {
	for _, typ := range []model.Type{
		{Kind: "Struct", Fields: []model.StructField{{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "id", Type: model.Type{Kind: "Utf8"}}}},
		{Kind: "Struct", Fields: []model.StructField{{Name: "", Type: model.Type{Kind: "Uint64"}}}},
		{Kind: "Uint64", Fields: []model.StructField{{Name: "hidden", Type: model.Type{Kind: "Utf8"}}}},
	} {
		_, err := AnalyzeWithOptions(nil, nil, Options{Functions: []builtins.Signature{{Name: "Broken", Returns: typ}}})
		require.Error(t, err)
	}
}

func TestUnknownFunctionsCannotHideInStructuredExpressions(t *testing.T) {
	for _, expression := range []string{
		"CAST(Unknown::Hash($id) AS Uint64)",
		"Unknown::Record($id).field",
	} {
		query := "-- name: Read :one\nDECLARE $id AS Uint64;\nSELECT " + expression + " AS value;"
		_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: query}})
		require.ErrorContains(t, err, "unsupported YQL function")
	}
}

func TestStructuredMemberBasesAndSuffixes(t *testing.T) {
	for _, test := range []struct{ name, query, want string }{
		{"undeclared base", "SELECT $missing.id AS value;", "cannot resolve type of parameter"},
		{"indexed suffix", "DECLARE $key AS Struct<id:Uint64>; SELECT $key.id[0] AS value;", "unsupported member access"},
		{"invocation after field", "DECLARE $key AS Struct<id:Uint64>; SELECT $key.id() AS value;", "unsupported member invocation"},
		{"literal base", "SELECT 1u.field AS value;", "unsupported member base"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Read :one\n" + test.query}})
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestStructLiteralDiagnostics(t *testing.T) {
	for _, tc := range []struct{ sql, want string }{
		{`SELECT <| status: 1ul, status: 2ul |> AS value;`, `duplicate field name "status"`},
		{`SELECT <| 1ul: "ready" |> AS value;`, `field name "1ul" must be an identifier`},
		{`SELECT <| status: $missing |> AS value;`, `struct literal field "status": cannot resolve type of parameter $missing`},
	} {
		_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Read :one\n" + tc.sql}})
		require.ErrorContains(t, err, tc.want)
	}
}

func TestStructuredTableColumnMemberAccess(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, payload Struct<label:Utf8,count:Uint64> NOT NULL, PRIMARY KEY(id));`}}
	got, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\nSELECT r.payload.count AS value FROM records r;"}})
	require.NoError(t, err)
	require.Equal(t, "Uint64", got.Queries[0].ResultSets[0].Columns[0].Type.Kind)
}

func TestParseTypeRejectsStatementInjectionAndMalformedInput(t *testing.T) {
	for _, text := range []string{
		"",
		"Uint64; DECLARE $other AS Utf8",
		"Uint64; SELECT 1",
		"List<Struct<id:Uint64>",
		"Uint64 trailing",
	} {
		{
			_, err := ParseType(text)
			require.Error(t, err)
		}
	}
}

func TestBytesAliasEveryTypePosition(t *testing.T) {
	for _, spelling := range []string{"Bytes", "String"} {
		got, err := Analyze([]model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL,payload " + spelling + ", PRIMARY KEY(id));"}}, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\nDECLARE $raw AS " + spelling + ";\nSELECT payload FROM records WHERE payload=$raw;"}})
		require.NoError(t, err)
		require.Equal(t, "String", got.Queries[0].Parameters[0].Type.Kind)
		typ, err := parseType("List<Struct<data:" + spelling + ">>")
		require.NoError(t, err)
		require.Equal(t, "String", typ.Elem.Fields[0].Type.Kind)
	}
}

func TestUnknownFunctionInPredicateRejected(t *testing.T) {
	_, err := Analyze([]model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));"}}, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\nDECLARE $id AS Uint64;\nSELECT id FROM records WHERE id=Unknown::Hash($id);"}})
	require.ErrorContains(t, err, "unsupported YQL function")
}
