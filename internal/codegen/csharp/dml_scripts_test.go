package csharp

import (
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestOneScriptConsumesStreamBeforeReturningFirstRow(t *testing.T) {
	for _, tc := range []struct {
		name  string
		sql   string
		multi bool
	}{
		{name: "single", sql: "SELECT id FROM records ORDER BY id;"},
		{name: "script", sql: "SELECT id FROM records ORDER BY id; DELETE FROM records WHERE id=0ul;", multi: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			analysis, err := analyzer.Analyze(
				[]model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));"}},
				[]model.Source{{Name: "query.sql", Text: "-- name: ReadRecords :one\n" + tc.sql}},
			)
			if err != nil {
				t.Fatal(err)
			}
			_, queries := generated(t, analysis)
			want := "        return ReadRecordsRowFrom(reader);\n"
			if tc.multi {
				want = "        var row = ReadRecordsRowFrom(reader);\n        while (await reader.ReadAsync(cancellationToken).ConfigureAwait(false))\n        {\n        }\n        return row;\n"
			}
			if !strings.Contains(queries, want) {
				t.Fatalf("generated first-row path missing %q:\n%s", want, queries)
			}
		})
	}
}
