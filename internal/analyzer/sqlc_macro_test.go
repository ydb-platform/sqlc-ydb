package analyzer

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestAnalyzeRejectsUnsupportedSQLCMacrosInEveryQueryContext(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE foo (
 id Uint64 NOT NULL,
 name Utf8 NOT NULL,
 PRIMARY KEY (id)
);`}}
	tests := []struct {
		name    string
		command model.Command
		query   string
	}{
		{name: "slice in nested select", command: model.Many, query: "SELECT id FROM (SELECT id FROM foo WHERE id IN sqlc.slice(ids)) nested;"},
		{name: "slice in update where", command: model.Exec, query: "UPDATE foo SET name = $name WHERE id IN sqlc.slice(ids);"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := "-- name: Invalid " + string(tt.command) + "\n" + tt.query
			result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
			require.Error(t, err)
			require.NotNil(t, result)
			require.NotEqual(t, 0, len(result.Diagnostics))
			require.Contains(t, err.Error(), "sqlc.slice is unsupported")
		})
	}
}

func TestAnalyzeSQLCArguments(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE foo (id Uint64 NOT NULL, name Utf8 NOT NULL, PRIMARY KEY (id));"}}
	query := "-- name: Read :many\r\nSELECT id FROM foo WHERE id = sqlc.arg(id) OR name = sqlc.narg('display-name') OR name = sqlc.narg(`имя`);"
	got, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
	require.NoError(t, err)
	require.Equal(t, "-- name: Read :many\r\nSELECT id FROM foo WHERE id = $id OR name = $`display-name` OR name = $`имя`;", got.Queries[0].SQL)
	require.Equal(t, []model.Parameter{
		{Name: "id", Type: model.Type{Kind: "Uint64"}},
		{Name: "display-name", Type: model.Optional(model.Type{Kind: "Utf8"})},
		{Name: "имя", Type: model.Optional(model.Type{Kind: "Utf8"})},
	}, got.Queries[0].Parameters)
}

func TestAnalyzeSQLCNargKeepsResultNullable(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE foo (id Uint64 NOT NULL, PRIMARY KEY (id));"}}
	query := "-- name: Read :many\nSELECT sqlc.narg(id) AS requested FROM foo WHERE id = sqlc.narg(id);"
	got, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
	require.NoError(t, err)
	require.Equal(t, model.Optional(model.Type{Kind: "Uint64"}), got.Queries[0].Parameters[0].Type)
	require.Equal(t, model.Optional(model.Type{Kind: "Uint64"}), got.Queries[0].ResultSets[0].Columns[0].Type)
}

func TestAnalyzeSQLCArgumentsShareExternalParameter(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE foo (id Uint64 NOT NULL, PRIMARY KEY (id));"}}
	query := "-- name: Read :many\nSELECT id FROM foo WHERE id = $id OR id = sqlc.arg(id) OR id = sqlc.arg('id');"
	got, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
	require.NoError(t, err)
	require.Equal(t, []model.Parameter{{Name: "id", Type: model.Type{Kind: "Uint64"}}}, got.Queries[0].Parameters)
	require.Equal(t, "-- name: Read :many\nSELECT id FROM foo WHERE id = $id OR id = $id OR id = $`id`;", got.Queries[0].SQL)
}

func TestAnalyzeSQLCArgumentDiagnosticsHaveStableOrder(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE foo (id Uint64 NOT NULL, PRIMARY KEY (id));"}}
	query := "-- name: Read :many\n$z = 1ul; $a = 2ul; SELECT id FROM foo WHERE id = sqlc.arg(z) OR id = sqlc.arg(a);"
	for range 20 {
		result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
		require.Error(t, err)
		var conflicts []string
		for _, diagnostic := range result.Diagnostics {
			if strings.Contains(diagnostic.Message, "conflicts with local") {
				conflicts = append(conflicts, diagnostic.Message)
			}
		}
		require.Equal(t, []string{`sqlc argument "a" conflicts with local $a`, `sqlc argument "z" conflicts with local $z`}, conflicts)
	}
}

func TestAnalyzeSQLCArgumentConflictPointsToFirstCall(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE foo (id Uint64 NOT NULL, PRIMARY KEY (id));"}}
	query := "-- name: Read :many\nDECLARE $id AS Uint64; SELECT id FROM foo WHERE id = sqlc.narg(id) OR id = sqlc.narg(id);"
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
	require.Error(t, err)
	var positions []model.Position
	for _, diagnostic := range result.Diagnostics {
		if strings.Contains(diagnostic.Message, "requires an Optional parameter type") {
			positions = append(positions, diagnostic.Position)
		}
	}
	require.Equal(t, []model.Position{{File: "query.sql", Line: 2, Column: strings.Index(query[strings.IndexByte(query, '\n')+1:], "sqlc.narg") + 1}}, positions)
}

func TestAnalyzeSQLCArgumentWithWhitespaceAroundDot(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE foo (id Uint64 NOT NULL, PRIMARY KEY (id));"}}
	query := "-- name: Read :many\nSELECT id FROM foo WHERE id = SQLC /* comment */ . Arg(id);"
	got, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
	require.NoError(t, err)
	require.Equal(t, "-- name: Read :many\nSELECT id FROM foo WHERE id = $id;", got.Queries[0].SQL)
}

func TestAnalyzeSQLCArgumentDiagnosticsMapExpandedSQL(t *testing.T) {
	query := "-- name: Read :many\nSELECT sqlc.embed(b) FROM books AS b WHERE b.book_id = sqlc.arg(id);"
	catalog, diagnostics := buildCatalog(embedSchema)
	require.Empty(t, diagnostics)
	blocks, diagnostics := queryBlocks(model.Source{Name: "query.sql", Text: query})
	require.Empty(t, diagnostics)
	require.Len(t, blocks, 1)
	block := blocks[0]
	require.Empty(t, lowerSQLCArguments(&block))
	result, diagnostics := analyzeExecutableQuery(catalog, &block)
	require.Empty(t, diagnostics)
	for _, target := range []struct{ expanded, original string }{
		{expanded: "__sqlc_embed_0_0", original: "sqlc.embed(b)"},
		{expanded: "WHERE", original: "WHERE"},
		{expanded: "$id", original: "sqlc.arg(id)"},
	} {
		gotIndex := strings.Index(result.SQL, target.expanded)
		require.NotEqual(t, -1, gotIndex)
		got := block.originalDiagnostics([]model.Diagnostic{{Position: positionInSQL("query.sql", block.line, result.SQL, gotIndex)}})
		wantIndex := strings.Index(query, target.original)
		require.Equal(t, positionInSQL("query.sql", block.line, query, wantIndex), got[0].Position)
	}
}

func TestAnalyzeSQLCArgumentPreservesOriginalDiagnosticPosition(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE foo (id Uint64 NOT NULL, PRIMARY KEY (id));"}}
	for _, query := range []string{
		"-- name: Read :many\r\nSELECT id FROM foo WHERE id = sqlc.arg(id) AND missing = 1u;",
		"-- name: Read :many\r\nSELECT id FROM foo WHERE id = sqlc.arg(\r\n  id\r\n) AND missing = 1u;",
		"-- name: Read :many\r\nSELECT id FROM foo WHERE id = sqlc.arg(id) AND 'Ж' = 'Ж' AND missing = 1u;",
	} {
		_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
		require.ErrorContains(t, err, `unknown column "missing"`)
		line := 1 + strings.Count(query[:strings.Index(query, "missing")], "\n")
		lastNewline := strings.LastIndex(query[:strings.Index(query, "missing")], "\n")
		column := len([]rune(query[lastNewline+1:strings.Index(query, "missing")])) + 1
		require.ErrorContains(t, err, "query.sql:"+strconv.Itoa(line)+":"+strconv.Itoa(column))
	}
}

func TestAnalyzeSQLCArgumentPreservesEndOfInputDiagnosticPosition(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE foo (id Uint64 NOT NULL, PRIMARY KEY (id));"}}
	query := "-- name: Read :many\nSELECT id FROM foo WHERE id = sqlc.arg(id) +"
	_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
	require.ErrorContains(t, err, "mismatched input '<EOF>'")
	require.ErrorContains(t, err, "query.sql:2:"+strconv.Itoa(len([]rune(query[strings.IndexByte(query, '\n')+1:]))+1))
}

func TestAnalyzeSQLCNargConflictUsesOriginalMacroPosition(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE foo (id Uint64 NOT NULL, PRIMARY KEY (id));"}}
	query := "-- name: Read :many\nDECLARE $id AS Uint64; SELECT id FROM foo WHERE id = sqlc.arg(other) OR id = sqlc.narg(id);"
	_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
	require.ErrorContains(t, err, "sqlc.narg(id) requires an Optional parameter type")
	column := strings.Index(query[strings.IndexByte(query, '\n')+1:], "sqlc.narg") + 1
	require.ErrorContains(t, err, "query.sql:2:"+strconv.Itoa(column))
}

func TestAnalyzeSQLCArgumentPreservesLiteralsAndComments(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE foo (id Uint64 NOT NULL, PRIMARY KEY (id));"}}
	query := "-- name: Read :many\r\n-- 🪄 sqlc.arg(ghost)\r\nSELECT 'sqlc.narg(ghost)' AS literal, id FROM foo /* sqlc.arg(ghost) */ WHERE id = sqlc.arg(id);"
	got, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
	require.NoError(t, err)
	require.Equal(t, strings.Replace(query, "sqlc.arg(id);", "$id;", 1), got.Queries[0].SQL)
}

func TestDatabaseAnalysisLowersSQLCArgumentsBeforeValidation(t *testing.T) {
	query := "-- name: Read :one\nSELECT sqlc.narg('display-name') AS value;"
	optional := model.Optional(model.Type{Kind: "Utf8"})
	options := Options{Parameters: map[string]map[string]model.Type{"Read": {"display-name": optional}}}
	database := &fakeAnalysisDatabase{}
	got, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: query}}, options, database)
	require.NoError(t, err)
	require.Equal(t, []string{"DECLARE $`display-name` AS Optional<Utf8>; -- name: Read :one\nSELECT $`display-name` AS value;"}, database.validated)
	require.Equal(t, []model.Parameter{{Name: "display-name", Type: optional}}, got.Queries[0].Parameters)
}

func TestDatabaseAnalysisRequiresTypeForSQLCArgument(t *testing.T) {
	query := "-- name: Read :one\nSELECT sqlc.arg(id) AS value;"
	database := &fakeAnalysisDatabase{validateError: errors.New("Unknown name: $id")}
	_, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: query}}, Options{}, database)
	require.ErrorContains(t, err, "database query validation failed: Unknown name: $id")
	require.Equal(t, []string{"-- name: Read :one\nSELECT $id AS value;"}, database.validated)
}

func TestAnalyzeSQLCNargDMLAssignment(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE foo (id Uint64 NOT NULL, name Utf8, PRIMARY KEY (id));"}}
	for _, statement := range []string{
		"INSERT INTO foo (id, name) VALUES (sqlc.narg(id), sqlc.arg(name));",
		"UPDATE foo SET id = sqlc.narg(id) WHERE id = 1ul;",
	} {
		_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Write :exec\n" + statement}})
		require.ErrorContains(t, err, "cannot assign Optional<Uint64> to column \"id\" of type Uint64")
	}
	got, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Write :exec\nINSERT INTO foo (id, name) VALUES (sqlc.arg(id), sqlc.narg(name));"}})
	require.NoError(t, err)
	require.Equal(t, []model.Parameter{{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "name", Type: model.Optional(model.Type{Kind: "Utf8"})}}, got.Queries[0].Parameters)
}

func TestAnalyzeSQLCArgumentErrors(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE foo (id Uint64 NOT NULL, name Utf8 NOT NULL, PRIMARY KEY (id));"}}
	for _, tt := range []struct{ name, sql, want string }{
		{"missing name", "SELECT id FROM foo WHERE id = sqlc.arg();", "expects exactly one parameter name"},
		{"two names", "SELECT id FROM foo WHERE id = sqlc.arg(id, name);", "expects exactly one parameter name"},
		{"expression", "SELECT id FROM foo WHERE id = sqlc.arg(id + 1u);", "expects exactly one parameter name"},
		{"mixed forms", "SELECT id FROM foo WHERE id = sqlc.arg(id) OR id = sqlc.narg(id);", "cannot use both sqlc.arg and sqlc.narg"},
		{"nonoptional declaration", "DECLARE $id AS Uint64; SELECT id FROM foo WHERE id = sqlc.narg(id);", "requires an Optional parameter type"},
		{"unresolved", "SELECT sqlc.narg(value) AS value;", "cannot resolve type of external parameter"},
		{"local collision", "$id = 1u; SELECT id FROM foo WHERE id = sqlc.arg(id);", "conflicts with local"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Invalid :many\n" + tt.sql}})
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestAnalyzeSQLCArgumentRejectsInvalidNamesWithoutCascadingErrors(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE foo (id Uint64 NOT NULL, PRIMARY KEY (id));"}}
	for _, argument := range []string{"42", "''", `'a\b'`} {
		t.Run(argument, func(t *testing.T) {
			query := "-- name: Invalid :many\nSELECT id FROM foo WHERE id = sqlc.arg(" + argument + ");\n\n-- name: Valid :many\nSELECT id FROM foo;"
			result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
			require.ErrorContains(t, err, "sqlc.arg expects a nonempty identifier or quoted string parameter name")
			require.Len(t, result.Diagnostics, 1)
			require.Len(t, result.Queries, 1)
			require.Equal(t, "Valid", result.Queries[0].Name)
		})
	}
}

func TestAnalyzeIgnoresSQLCMacroTextInStringsCommentsAndQuotedIdentifiers(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE foo (`sqlc.arg` Utf8 NOT NULL, PRIMARY KEY (`sqlc.arg`));"}}
	queries := []model.Source{{Name: "query.sql", Text: `-- name: LiteralText :many
-- sqlc.slice(ids) is documentation, not a call.
SELECT 'sqlc.narg(name)' AS macro_text, ` + "`sqlc.arg`" + `
FROM foo /* sqlc.arg(name) */;`}}

	got, err := Analyze(schema, queries)
	require.NoError(t, err)
	require.Len(t, got.Queries, 1)
	require.Len(t, got.Queries[0].ResultSets, 1)
}
