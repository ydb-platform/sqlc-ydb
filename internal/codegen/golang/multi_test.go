package golang

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func multiInput(t *testing.T) *model.AnalysisResult {
	t.Helper()
	const sql = "-- name: ReadSummary :multi\nDECLARE $id AS Uint64;\n-- result: Count\nSELECT $id AS value;\n-- result: Status\nSELECT false AS value;\nSELECT \"2\"u AS value;"
	in, err := analyzer.Analyze(nil, []model.Source{{Name: "query.sql", Text: sql}})
	require.NoError(t, err)
	return in
}

func TestMultiGoGeneration(t *testing.T) {
	in := multiInput(t)
	for _, runtime := range []string{"ydb", "database/sql"} {
		t.Run(runtime, func(t *testing.T) {
			options := Options{Runtime: runtime, EmitInterface: true, EmitEmptySlices: true, Rename: map[string]string{"value": "ValueRenamed", "Count": "RenamedCount"}, QueryParameterLimit: intPtr(0)}
			files, err := Generate(in, options)
			require.NoError(t, err)
			var models, query string
			for _, file := range files {
				if file.Name == "models.go" {
					models = string(file.Content)
				} else if file.Name == "query.sql.go" {
					query = string(file.Content)
				}
			}
			for _, want := range []string{"type ReadSummaryResult struct", "Count   []ReadSummaryCountRow", "Status  []ReadSummaryStatusRow", "Result3 []ReadSummaryResult3Row", "ValueRenamed uint64", "ValueRenamed bool", "ValueRenamed string", "ReadSummary(ctx context.Context, arg ReadSummaryParams"} {
				require.Contains(t, models, want)
			}
			require.Contains(t, query, "out.Count = make([]ReadSummaryCountRow, 0)")
			require.Contains(t, query, "out.Status = make([]ReadSummaryStatusRow, 0)")
			require.Equal(t, in.Queries[0].SQL[len("-- name: ReadSummary :multi\n"):], generatedSQLValue(t, []byte(query)))
			compileInput(t, in, options)
		})
	}
}

func TestMultiGeneratedTypeCollision(t *testing.T) {
	in := multiInput(t)
	in.Queries = append(in.Queries, model.AnalyzedQuery{
		Name: "ReadSummaryCount", Command: model.One, SQL: "SELECT 1 AS value;",
		ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "value", Type: model.Type{Kind: "Int32"}}}}},
	})
	_, err := Generate(in, Options{Runtime: "ydb"})
	require.ErrorContains(t, err, "generated declaration ReadSummaryCountRow collides with another declaration")
}

func TestMultiRejectsMalformedResultSets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*model.AnalyzedQuery)
		want   string
	}{
		{"one result set", func(q *model.AnalyzedQuery) { q.ResultSets = q.ResultSets[:1] }, ":multi requires at least two result sets"},
		{"empty columns", func(q *model.AnalyzedQuery) { q.ResultSets[1].Columns = nil }, ":multi result \"Status\" requires at least one column"},
		{"invalid name", func(q *model.AnalyzedQuery) { q.ResultSets[1].Name = "bad-name" }, "invalid :multi result name \"bad-name\""},
		{"unexported name", func(q *model.AnalyzedQuery) { q.ResultSets[1].Name = "status" }, "invalid :multi result name \"status\""},
		{"duplicate name", func(q *model.AnalyzedQuery) { q.ResultSets[1].Name = q.ResultSets[0].Name }, "generated declaration ReadSummaryCountRow collides"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := multiInput(t)
			tc.change(&in.Queries[0])
			_, err := Generate(in, Options{Runtime: "ydb"})
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestMultiUnicodeResultNameCompiles(t *testing.T) {
	const sql = "-- name: Read :multi\n-- result: Имя\nSELECT 1 AS value;\nSELECT false AS enabled;"
	in, err := analyzer.Analyze(nil, []model.Source{{Name: "query.sql", Text: sql}})
	require.NoError(t, err)
	for _, runtime := range []string{"ydb", "database/sql"} {
		t.Run(runtime, func(t *testing.T) {
			compileInput(t, in, Options{Runtime: runtime})
		})
	}
}
