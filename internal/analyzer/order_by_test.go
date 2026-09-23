package analyzer

import (
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestOrderByUsesProjectedTypesAndBindings(t *testing.T) {
	schema := `CREATE TABLE records (id Uint64 NOT NULL, column0 Utf8, flag Bool NOT NULL, PRIMARY KEY(id));`
	for _, tc := range []struct {
		statement, typ, boundColumn string
	}{
		{`SELECT id > 0ul FROM records ORDER BY column0;`, "", ""},
		{`SELECT id > 0ul AS id FROM records ORDER BY id;`, "", ""},
		{`SELECT column0 AS id FROM records ORDER BY (id) DESC;`, "", "column0"},
		{`SELECT column0 AS renamed FROM records ORDER BY renamed;`, "", "column0"},
		{`SELECT id FROM records ORDER BY id = $p;`, "Uint64", "id"},
		{`SELECT id AS id FROM records ORDER BY id = $p;`, "Uint64", "id"},
		{`SELECT flag FROM records ORDER BY column0 = $p;`, "Optional<Utf8>", "column0"},
		{`SELECT id > 0ul AS id FROM records ORDER BY records.id = $p;`, "Uint64", "id"},
		{`SELECT r.*, id > 0ul FROM records AS r ORDER BY column1;`, "", ""},
	} {
		t.Run(tc.statement, func(t *testing.T) {
			result, err := Analyze([]model.Source{{Name: "schema.sql", Text: schema}}, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\n" + tc.statement}})
			if err != nil {
				t.Fatal(err)
			}
			query := result.Queries[0]
			if (tc.typ == "" && len(query.Parameters) != 0) || (tc.typ != "" && (len(query.Parameters) != 1 || query.Parameters[0].Type.String() != tc.typ)) {
				t.Fatalf("parameters = %+v; want $p %s", query.Parameters, tc.typ)
			}
			for _, ref := range columnRefs(query.Syntax.Root) {
				if !isOrderByReference(ref.ctx) {
					continue
				}
				binding, exists := query.Syntax.Columns[ref.ctx.GetStart().GetTokenIndex()]
				if exists != (tc.boundColumn != "") || binding.Column.Name != tc.boundColumn {
					t.Fatalf("ORDER BY binding = %+v, exists %v; want source column %q", binding, exists, tc.boundColumn)
				}
			}
		})
	}
	for _, statement := range []string{
		`SELECT id > 0ul FROM records ORDER BY column0 = $p;`,
		`SELECT id > 0ul AS id FROM records ORDER BY id = $p;`,
		`SELECT column0 AS id FROM records ORDER BY id = $p;`,
		`SELECT column0 AS renamed FROM records ORDER BY renamed = $p;`,
		`SELECT id > 0ul FROM records ORDER BY column0 IN $p;`,
		`DECLARE $p AS Bool; SELECT id > 0ul AS id FROM records ORDER BY id = $p;`,
	} {
		_, err := Analyze([]model.Source{{Name: "schema.sql", Text: schema}}, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\n" + statement}})
		if err == nil || !strings.Contains(err.Error(), "ORDER BY expressions referencing projection aliases") {
			t.Fatalf("unsafe ORDER BY scope accepted for %s: %v", statement, err)
		}
	}
}
