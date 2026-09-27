package analyzer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestRowNumberWindows(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, category Utf8, PRIMARY KEY(id));`}}
	for _, statement := range []string{
		`SELECT id, ROW_NUMBER() OVER () AS row_num FROM records;`,
		`SELECT id, ROW_NUMBER() OVER (PARTITION BY category ORDER BY id) AS row_num FROM records;`,
		`SELECT id, ROW_NUMBER() OVER w AS row_num FROM records WINDOW w AS (PARTITION BY category ORDER BY id);`,
		`SELECT id, ROW_NUMBER() OVER (ORDER BY id DESC) AS row_num FROM records;`,
		`SELECT r.id, ROW_NUMBER() OVER (PARTITION BY r.category ORDER BY r.id) AS row_num FROM records AS r;`,
	} {
		t.Run(statement, func(t *testing.T) {
			result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: RankRecords :many\n" + statement}})
			require.NoError(t, err)
			require.Equal(t, model.Type{Kind: "Uint64"}, result.Queries[0].ResultSets[0].Columns[1].Type)
			require.Equal(t, "row_num", result.Queries[0].ResultSets[0].Columns[1].Name)
		})
	}
}

func TestRowNumberWindowDiagnostics(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, category Utf8, PRIMARY KEY(id));`}}
	for _, tc := range []struct{ statement, want string }{
		{`SELECT ROW_NUMBER() AS row_num FROM records;`, "requires OVER"},
		{`SELECT ROW_NUMBER(id) OVER (ORDER BY id) AS row_num FROM records;`, "no arguments"},
		{`SELECT ROW_NUMBER(*) OVER (ORDER BY id) AS row_num FROM records;`, "no arguments"},
		{`SELECT ROW_NUMBER() OVER missing AS row_num FROM records;`, "unknown window"},
		{`SELECT ROW_NUMBER() OVER w AS row_num FROM records WINDOW w AS (ORDER BY id), w AS (ORDER BY category);`, "duplicate window"},
		{`SELECT ROW_NUMBER() OVER (PARTITION BY absent ORDER BY id) AS row_num FROM records;`, "unknown column"},
		{`SELECT ROW_NUMBER() OVER (PARTITION BY id + 1 ORDER BY id) AS row_num FROM records;`, "PARTITION BY requires direct columns"},
		{`SELECT ROW_NUMBER() OVER (PARTITION BY id AS key ORDER BY id) AS row_num FROM records;`, "PARTITION BY requires direct columns"},
		{`SELECT ROW_NUMBER() OVER (PARTITION COMPACT BY category ORDER BY id) AS row_num FROM records;`, "PARTITION COMPACT"},
		{`SELECT ROW_NUMBER() OVER (ORDER BY absent) AS row_num FROM records;`, "unknown column"},
		{`SELECT ROW_NUMBER() OVER (ORDER BY id + 1) AS row_num FROM records;`, "window ORDER BY requires direct columns"},
		{`SELECT ROW_NUMBER() OVER (ORDER BY id ROWS BETWEEN CURRENT ROW AND UNBOUNDED FOLLOWING) AS row_num FROM records;`, "window frames"},
		{`SELECT ROW_NUMBER() OVER child AS row_num FROM records WINDOW base AS (ORDER BY id), child AS (base);`, "inherited window"},
		{`SELECT SUM(id) OVER (ORDER BY id) AS total FROM records;`, "window function"},
		{`SELECT id FROM records WHERE ROW_NUMBER() OVER (ORDER BY id) = 1;`, "SELECT projection"},
		{`$rank = ROW_NUMBER() OVER (); SELECT $rank AS row_num FROM records;`, "SELECT projection"},
	} {
		t.Run(tc.statement, func(t *testing.T) {
			_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: RankRecords :many\n" + tc.statement}})
			require.ErrorContains(t, err, tc.want)
		})
	}
}
