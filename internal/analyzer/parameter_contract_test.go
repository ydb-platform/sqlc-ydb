package analyzer

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestConfiguredParameterTypeResolvesUndeclaredQuery(t *testing.T) {
	const sql = "-- name: Echo :one\nSELECT $value AS value;"
	options := Options{Parameters: map[string]map[string]model.Type{"Echo": {"value": {Kind: "Utf8"}}}}
	result, err := AnalyzeWithOptions(nil, []model.Source{{Name: "query.sql", Text: sql}}, options)
	require.NoError(t, err)
	require.Equal(t, sql, result.Queries[0].SQL)
	require.Empty(t, result.Queries[0].DeclaredParameters)
	require.Equal(t, []model.Parameter{{Name: "value", Type: model.Type{Kind: "Utf8"}}}, result.Queries[0].Parameters)
	require.Equal(t, model.Type{Kind: "Utf8"}, result.Queries[0].ResultSets[0].Columns[0].Type)
}

func TestConfiguredStructuredParameterResolvesASTABLE(t *testing.T) {
	const sql = "-- name: Read :many\nSELECT r.id, r.name FROM AS_TABLE($rows) AS r;"
	rows, err := ParseType("List<Struct<id:Uint64,name:Utf8>>")
	require.NoError(t, err)
	options := Options{Parameters: map[string]map[string]model.Type{"Read": {"rows": rows}}}
	result, err := AnalyzeWithOptions(nil, []model.Source{{Name: "query.sql", Text: sql}}, options)
	require.NoError(t, err)
	require.Equal(t, []model.Parameter{{Name: "rows", Type: rows}}, result.Queries[0].Parameters)
	require.Equal(t, []string{"id", "name"}, []string{result.Queries[0].ResultSets[0].Columns[0].Name, result.Queries[0].ResultSets[0].Columns[1].Name})
}

func TestConfiguredParameterSurvivesWildcardRewrite(t *testing.T) {
	const sql = "-- name: Read :many\nSELECT * FROM AS_TABLE($rows) AS r;"
	rows, err := ParseType("List<Struct<id:Uint64,name:Utf8>>")
	require.NoError(t, err)
	options := Options{Parameters: map[string]map[string]model.Type{"Read": {"rows": rows}}}

	result, err := AnalyzeWithOptions(nil, []model.Source{{Name: "query.sql", Text: sql}}, options)
	require.NoError(t, err)
	require.Len(t, result.Queries, 1)
	query := result.Queries[0]
	require.Equal(t, "-- name: Read :many\nSELECT `id`, `name` FROM AS_TABLE($rows) AS r;", query.SQL)
	require.Equal(t, []model.Parameter{{Name: "rows", Type: rows}}, query.Parameters)
	require.Equal(t, []model.Column{{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "name", Type: model.Type{Kind: "Utf8"}}}, query.ResultSets[0].Columns)
}

func TestConfiguredParameterTypesAreScopedByQuery(t *testing.T) {
	const sql = "-- name: Number :one\nSELECT $value AS value;\n-- name: Text :one\nSELECT $value AS value;"
	options := Options{Parameters: map[string]map[string]model.Type{
		"Number": {"value": {Kind: "Uint64"}},
		"Text":   {"value": {Kind: "Utf8"}},
	}}
	result, err := AnalyzeWithOptions(nil, []model.Source{{Name: "query.sql", Text: sql}}, options)
	require.NoError(t, err)
	require.Equal(t, model.Type{Kind: "Uint64"}, result.Queries[0].Parameters[0].Type)
	require.Equal(t, model.Type{Kind: "Utf8"}, result.Queries[1].Parameters[0].Type)
}

func TestConfiguredParameterTypeRejectsMismatchAndUnusedNames(t *testing.T) {
	const schema = "CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));"
	for _, tc := range []struct {
		name    string
		sql     string
		options Options
		want    string
	}{
		{"SQL constraint", "-- name: Read :many\nSELECT id FROM records WHERE id=$id;", Options{Parameters: map[string]map[string]model.Type{"Read": {"id": {Kind: "Utf8"}}}}, "parameter $id"},
		{"DECLARE conflict", "-- name: Read :one\nDECLARE $id AS Uint64; SELECT $id AS id;", Options{Parameters: map[string]map[string]model.Type{"Read": {"id": {Kind: "Utf8"}}}}, "conflicts with DECLARE"},
		{"unknown query", "-- name: Read :one\nSELECT 1 AS value;", Options{Parameters: map[string]map[string]model.Type{"Other": {"id": {Kind: "Uint64"}}}}, "unknown query"},
		{"unused parameter", "-- name: Read :one\nSELECT 1 AS value;", Options{Parameters: map[string]map[string]model.Type{"Read": {"id": {Kind: "Uint64"}}}}, "unused parameter"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := AnalyzeWithOptions([]model.Source{{Name: "schema.sql", Text: schema}}, []model.Source{{Name: "query.sql", Text: tc.sql}}, tc.options)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestConfiguredParameterDoesNotHideUnknownInsertColumn(t *testing.T) {
	const schema = "CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));"
	const sql = "-- name: Insert :exec\nINSERT INTO records (missing) VALUES ($value);"
	options := Options{Parameters: map[string]map[string]model.Type{"Insert": {"value": {Kind: "Uint64"}}}}

	result, err := AnalyzeWithOptions([]model.Source{{Name: "schema.sql", Text: schema}}, []model.Source{{Name: "query.sql", Text: sql}}, options)
	require.ErrorContains(t, err, `unknown column "missing"`)
	require.Empty(t, result.Queries)
}

func TestConfiguredParameterUnknownQueryPreservesPreviousDiagnostics(t *testing.T) {
	queries := []model.Source{
		{Name: "first.sql", Text: "-- name: Read :one\nSELECT 1 AS value;"},
		{Name: "second.sql", Text: "-- name: Read :one\nSELECT 2 AS value;"},
	}
	options := Options{Parameters: map[string]map[string]model.Type{"Other": {"id": {Kind: "Uint64"}}}}

	result, err := AnalyzeWithOptions(nil, queries, options)
	require.ErrorContains(t, err, `analyzer.parameters references unknown query "Other"`)
	require.ErrorContains(t, err, `query "Read" is declared more than once`)
	require.Len(t, result.Diagnostics, 1)
	require.ErrorContains(t, result.Diagnostics[0], `query "Read" is declared more than once`)
}

func TestDatabaseAnalysisValidatesConfiguredParametersWithoutChangingExecutableSQL(t *testing.T) {
	const sql = "-- name: Read :one\nSELECT id FROM records WHERE id = $id;"
	database := &fakeAnalysisDatabase{tables: map[string]model.Table{"records": databaseTestTable("name")}}
	options := Options{Parameters: map[string]map[string]model.Type{"Read": {"id": {Kind: "Uint64"}}}}
	result, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: sql}}, options, database)
	require.NoError(t, err)
	require.Equal(t, []string{"DECLARE $`id` AS Uint64; " + sql}, database.validated)
	require.Equal(t, sql, result.Queries[0].SQL)
	require.Empty(t, result.Queries[0].DeclaredParameters)
}

func TestDatabaseAnalysisDoesNotDuplicateSourceDeclaration(t *testing.T) {
	const sql = "-- name: Read :one\nDECLARE $id AS Uint64;\nSELECT $id AS id, $`имя` AS name;"
	database := &fakeAnalysisDatabase{}
	options := Options{Parameters: map[string]map[string]model.Type{"Read": {"id": {Kind: "Uint64"}, "имя": {Kind: "Utf8"}}}}
	result, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: sql}}, options, database)
	require.NoError(t, err)
	require.Equal(t, []string{"DECLARE $`имя` AS Utf8; " + sql}, database.validated)
	require.Equal(t, []string{"id"}, result.Queries[0].DeclaredParameters)
	require.Equal(t, sql, result.Queries[0].SQL)
}

func TestDatabaseAnalysisNeedsTypesForAllUndeclaredParameters(t *testing.T) {
	const sql = "-- name: Read :many\nSELECT $opaque AS value FROM records WHERE id = $id;"
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));"}}
	queries := []model.Source{{Name: "query.sql", Text: sql}}
	partial := Options{Parameters: map[string]map[string]model.Type{"Read": {"opaque": {Kind: "Utf8"}}}}
	_, err := AnalyzeWithOptions(schema, queries, partial)
	require.NoError(t, err)

	database := &fakeAnalysisDatabase{validateError: errors.New("Unknown name: $id"), tables: map[string]model.Table{"records": databaseTestTable("name")}}
	_, err = AnalyzeWithDatabase(context.Background(), nil, queries, partial, database)
	require.ErrorContains(t, err, "database query validation failed: Unknown name: $id")
	require.Equal(t, []string{"DECLARE $`opaque` AS Utf8; " + sql}, database.validated)

	database.validateError = nil
	database.validated = nil
	complete := Options{Parameters: map[string]map[string]model.Type{"Read": {"opaque": {Kind: "Utf8"}, "id": {Kind: "Uint64"}}}}
	result, err := AnalyzeWithDatabase(context.Background(), nil, queries, complete, database)
	require.NoError(t, err)
	require.Equal(t, []string{"DECLARE $`id` AS Uint64; DECLARE $`opaque` AS Utf8; " + sql}, database.validated)
	require.Equal(t, []model.Parameter{{Name: "opaque", Type: model.Type{Kind: "Utf8"}}, {Name: "id", Type: model.Type{Kind: "Uint64"}}}, result.Queries[0].Parameters)
}

func TestDatabaseAnalysisRejectsMalformedQueryBeforeValidationWithConfiguredParameter(t *testing.T) {
	const sql = "-- name: Read :one\nSELECT ($value AS value;"
	database := &fakeAnalysisDatabase{}
	options := Options{Parameters: map[string]map[string]model.Type{"Read": {"value": {Kind: "Utf8"}}}}

	result, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: sql}}, options, database)
	require.Error(t, err)
	require.NotEmpty(t, result.Diagnostics)
	require.Equal(t, "query.sql", result.Diagnostics[0].Position.File)
	require.Equal(t, 2, result.Diagnostics[0].Position.Line)
	require.Empty(t, database.validated)
}

func TestDatabaseAnalysisRejectsConflictingParameterTypeBeforeValidation(t *testing.T) {
	const sql = "-- name: Read :one\nDECLARE $value AS Uint64; SELECT $value AS value;"
	database := &fakeAnalysisDatabase{}
	options := Options{Parameters: map[string]map[string]model.Type{"Read": {"value": {Kind: "Utf8"}}}}

	result, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: sql}}, options, database)
	require.ErrorContains(t, err, "conflicts with DECLARE")
	require.Len(t, result.Diagnostics, 1)
	require.Equal(t, model.Position{File: "query.sql", Line: 1, Column: 1}, result.Diagnostics[0].Position)
	require.Empty(t, database.validated)
}

func TestConfiguredParameterRejectsInvalidProgrammaticTypeBeforeDatabaseValidation(t *testing.T) {
	const sql = "-- name: Echo :one\nSELECT $value AS value;"
	for _, tc := range []struct {
		name      string
		typeValue model.Type
	}{
		{name: "unknown type", typeValue: model.Type{Kind: "UnknownYQLType"}},
		{name: "invalid type structure", typeValue: model.Type{Kind: "Utf8", Elem: &model.Type{Kind: "Uint64"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := Options{Parameters: map[string]map[string]model.Type{"Echo": {"value": tc.typeValue}}}
			queries := []model.Source{{Name: "query.sql", Text: sql}}

			_, err := AnalyzeWithOptions(nil, queries, options)
			require.ErrorContains(t, err, "configured parameter $value has unsupported YQL type")

			database := &fakeAnalysisDatabase{}
			_, err = AnalyzeWithDatabase(context.Background(), nil, queries, options, database)
			require.ErrorContains(t, err, "configured parameter $value has unsupported YQL type")
			require.Empty(t, database.validated)
		})
	}
}
