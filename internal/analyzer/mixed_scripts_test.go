package analyzer

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestMixedScriptPreservesOneResultInEveryPosition(t *testing.T) {
	for _, command := range []model.Command{model.One, model.Many} {
		for _, selectSQL := range []string{
			"SELECT id, payload FROM records WHERE id=$id;",
			"SELECT id, payload FROM records WHERE id=$id UNION ALL SELECT id, payload FROM copies WHERE id=$id;",
		} {
			for _, statements := range []string{
				selectSQL + " DELETE FROM copies WHERE id=$id;",
				"DELETE FROM copies WHERE id=$id; " + selectSQL,
				"UPDATE records SET payload='new'u WHERE id=$id; " + selectSQL + " DELETE FROM copies WHERE id=$id;",
			} {
				t.Run(string(command)+statements, func(t *testing.T) {
					sql := "-- name: Change " + string(command) + "\n" + statements
					result, err := Analyze(dmlScriptSchema, []model.Source{{Name: "query.sql", Text: sql}})
					if err != nil {
						t.Fatal(err)
					}
					if len(result.Queries) != 1 {
						t.Fatalf("queries=%d", len(result.Queries))
					}
					q := result.Queries[0]
					if q.SQL != sql || q.Command != command || len(q.ResultSets) != 1 || !q.MultipleStatements {
						t.Fatalf("query=%#v", q)
					}
					got := q.ResultSets[0].Columns
					if len(got) != 2 || got[0].Name != "id" || got[0].Type.Kind != "Uint64" || got[1].Name != "payload" || got[1].Type.Kind != "Utf8" {
						t.Fatalf("result columns=%#v", got)
					}
					want := []model.Parameter{{Name: "id", Type: model.Type{Kind: "Uint64"}}}
					if !reflect.DeepEqual(q.Parameters, want) {
						t.Fatalf("parameters=%#v", q.Parameters)
					}
				})
			}
		}
	}
}

func TestMixedScriptRejectsResultCountAndCommand(t *testing.T) {
	for _, tc := range []struct{ name, command, sql, want string }{
		{"two selects", ":many", "SELECT id FROM records; SELECT id FROM copies;", "multi-statement queries support at most one result-producing statement; found 2"},
		{"two results with writes", ":one", "DELETE FROM records; SELECT id FROM records; DELETE FROM copies RETURNING id;", "multi-statement queries support at most one result-producing statement; found 2"},
		{"exec result", ":exec", "SELECT id FROM records; DELETE FROM copies;", "command :exec cannot be used with a row-returning script; use :one or :many"},
		{"one no result", ":one", "DELETE FROM records; DELETE FROM copies;", "command :one requires exactly one result-producing statement in a script"},
		{"many no result", ":many", "DELETE FROM records; DELETE FROM copies;", "command :many requires exactly one result-producing statement in a script"},
		{"each writes", ":each", "SELECT id FROM records; DELETE FROM copies;", "multi-statement :each is unsupported; use :one or :many to consume the result before returning"},
		{"execrows", ":execrows", "SELECT id FROM records; DELETE FROM copies;", "multi-statement :execrows is unsupported; use :exec, :one, or :many"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Analyze(dmlScriptSchema, []model.Source{{Name: "query.sql", Text: "-- name: Change " + tc.command + "\n" + tc.sql}})
			if err == nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Message != tc.want {
				t.Fatalf("diagnostics=%#v, error=%v; want %q", result.Diagnostics, err, tc.want)
			}
		})
	}
}

func TestMixedScriptWildcardAndEmptyResultMetadata(t *testing.T) {
	const sql = "-- name: Change :many\nDECLARE $id AS Uint64;\nUPSERT INTO copies SELECT * FROM records;\nSELECT r.* FROM records AS r WHERE false;\nDELETE FROM copies WHERE id=$id;"
	result, err := Analyze(dmlScriptSchema, []model.Source{{Name: "query.sql", Text: sql}})
	if err != nil {
		t.Fatal(err)
	}
	q := result.Queries[0]
	if len(q.ResultSets) != 1 || len(q.ResultSets[0].Columns) != 2 || len(q.Syntax.Selects) != 2 || strings.Contains(q.SQL, "SELECT *") || strings.Contains(q.SQL, "r.*") || !strings.HasSuffix(q.SQL, "DELETE FROM copies WHERE id=$id;") {
		t.Fatalf("query=%#v", q)
	}
}

func TestMixedScriptDatabaseValidatesOriginalSQLOnce(t *testing.T) {
	const sql = "-- name: Change :many\nPRAGMA TablePathPrefix='/local/tenant';\nDECLARE $id AS Uint64;\nUPDATE records SET name='changed'u WHERE id=$id;\nSELECT * FROM records WHERE id=$id;\nDELETE FROM copies WHERE id=$id;"
	database := &fakeAnalysisDatabase{tables: map[string]model.Table{"/local/tenant/records": databaseTestTable("name"), "/local/tenant/copies": databaseTestTable("name")}}
	result, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: sql}}, Options{}, database)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(database.validated, []string{sql}) || !reflect.DeepEqual(database.described, []string{"/local/tenant/records", "/local/tenant/copies"}) {
		t.Fatalf("validated=%q described=%q", database.validated, database.described)
	}
	if len(result.Queries) != 1 || len(result.Queries[0].ResultSets) != 1 || result.Queries[0].ResultSets[0].Columns[0].Table != "/local/tenant/records" {
		t.Fatalf("queries=%#v", result.Queries)
	}
}

func TestMixedScriptRejectsLateParameterRefinementOfResult(t *testing.T) {
	schema := append([]model.Source{}, dmlScriptSchema...)
	schema = append(schema, model.Source{Name: "loose.sql", Text: "CREATE TABLE loose(id Uint64 NOT NULL,payload Utf8,PRIMARY KEY(id));"})
	const read = "SELECT $p AS projected FROM loose WHERE payload=$p;"
	const write = "UPDATE records SET payload=$p;"
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Change :many\n" + read + write}})
	const want = "parameter $p changes inferred type from Optional<Utf8> to Utf8 after the result statement; add DECLARE before the script to keep its result type stable"
	if err == nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Message != want {
		t.Fatalf("diagnostics=%#v error=%v; want %q", result.Diagnostics, err, want)
	}
	for _, sql := range []string{"DECLARE $p AS Utf8; " + read + write, write + read} {
		result, err = Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Change :many\n" + sql}})
		if err != nil {
			t.Fatal(err)
		}
		q := result.Queries[0]
		if len(q.Parameters) != 1 || q.Parameters[0].Type.Kind != "Utf8" || q.ResultSets[0].Columns[0].Type.Kind != "Utf8" {
			t.Fatalf("parameter/result type mismatch: %#v", q)
		}
	}
	sql := "UPDATE loose SET payload=$p; SELECT id FROM copies WHERE id=$id; " + write
	result, err = Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Change :many\n" + sql}})
	if err != nil {
		t.Fatalf("unrelated parameter refinement changed result: %v", err)
	}
	wantParameters := []model.Parameter{{Name: "p", Type: model.Type{Kind: "Utf8"}}, {Name: "id", Type: model.Type{Kind: "Uint64"}}}
	if result.Queries[0].ResultSets[0].Columns[0].Type.Kind != "Uint64" || !reflect.DeepEqual(result.Queries[0].Parameters, wantParameters) {
		t.Fatalf("result=%#v", result.Queries[0])
	}
}

func TestMixedScriptRejectsIncompatibleSharedParameters(t *testing.T) {
	for _, sql := range []string{
		"SELECT id FROM records WHERE id=$id; DELETE FROM texts WHERE id=$id;",
		"DELETE FROM texts WHERE id=$id; SELECT id FROM records WHERE id=$id;",
	} {
		result, err := Analyze(dmlScriptSchema, []model.Source{{Name: "query.sql", Text: "-- name: Change :many\n" + sql}})
		if err == nil {
			t.Fatalf("accepted %s", sql)
		}
		found := false
		for _, d := range result.Diagnostics {
			if d.Message == "external parameter $id has incompatible inferred types; add DECLARE to specify its intended type" {
				found = true
			}
		}
		if !found {
			t.Fatalf("diagnostics=%#v", result.Diagnostics)
		}
	}
}

func TestMixedScriptReturningPreservesOneResult(t *testing.T) {
	for _, returning := range []string{
		"INSERT INTO records(id,payload) VALUES($id,'new'u) RETURNING *;",
		"UPSERT INTO records(id,payload) VALUES($id,'new'u) RETURNING *;",
		"UPDATE records SET payload='new'u WHERE id=$id RETURNING *;",
		"DELETE FROM records WHERE id=$id RETURNING *;",
	} {
		for _, sql := range []string{returning + "DELETE FROM copies WHERE id=$id;", "DELETE FROM copies WHERE id=$id;" + returning, "DELETE FROM texts;" + returning + "DELETE FROM copies WHERE id=$id;"} {
			for _, command := range []string{":one", ":many"} {
				result, err := Analyze(dmlScriptSchema, []model.Source{{Name: "query.sql", Text: "-- name: Change " + command + "\n" + sql}})
				if err != nil {
					t.Fatalf("%s: %v", sql, err)
				}
				q := result.Queries[0]
				if len(q.ResultSets) != 1 || !reflect.DeepEqual(q.ResultSets[0].Columns, result.Catalog.Tables[0].Columns) || !strings.Contains(q.SQL, "RETURNING `id`, `payload`") {
					t.Fatalf("query=%#v", q)
				}
			}
		}
	}
}

func TestMixedScriptReturningTypeDoesNotDependOnParameterRefinement(t *testing.T) {
	schema := append([]model.Source{}, dmlScriptSchema...)
	schema = append(schema, model.Source{Name: "loose.sql", Text: "CREATE TABLE loose(id Uint64 NOT NULL,payload Utf8,PRIMARY KEY(id));"})
	const sql = "-- name: Change :many\nUPDATE loose SET payload=$p RETURNING payload; UPDATE records SET payload=$p;"
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
	if err != nil {
		t.Fatal(err)
	}
	q := result.Queries[0]
	if len(q.Parameters) != 1 || q.Parameters[0].Type.Kind != "Utf8" || !q.ResultSets[0].Columns[0].Type.Equal(model.Optional(model.Type{Kind: "Utf8"})) {
		t.Fatalf("query=%#v", q)
	}
}

func TestMixedScriptDoesNotEnableNamedTabularBindings(t *testing.T) {
	const sql = "-- name: Change :many\n$selection=(SELECT id FROM records); DELETE FROM copies; SELECT id FROM $selection;"
	result, err := Analyze(dmlScriptSchema, []model.Source{{Name: "query.sql", Text: sql}})
	if err == nil || len(result.Diagnostics) == 0 || !strings.HasPrefix(result.Diagnostics[0].Message, "cannot resolve type of local $selection:") {
		t.Fatalf("diagnostics=%#v error=%v", result.Diagnostics, err)
	}
}

func TestMixedScriptInferenceGuardPreservesSingleQueryTypes(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE loose(id Uint64 NOT NULL,payload Utf8,PRIMARY KEY(id));"}}
	for _, tc := range []struct {
		sql string
		typ model.Type
	}{
		{"SELECT $p AS projected FROM loose WHERE payload=$p;", model.Optional(model.Type{Kind: "Utf8"})},
		{"DECLARE $p AS Utf8; SELECT $p AS projected FROM loose WHERE payload=$p;", model.Type{Kind: "Utf8"}},
		{"DECLARE $p AS Utf8?; $local=$p; SELECT $local AS projected FROM loose WHERE payload=$local;", model.Optional(model.Type{Kind: "Utf8"})},
	} {
		result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\n" + tc.sql}})
		if err != nil {
			t.Fatal(err)
		}
		q := result.Queries[0]
		if q.MultipleStatements || len(q.Parameters) != 1 || q.Parameters[0].Name != "p" || !q.Parameters[0].Type.Equal(tc.typ) || len(q.ResultSets) != 1 || !q.ResultSets[0].Columns[0].Type.Equal(tc.typ) {
			t.Fatalf("query=%#v, want parameter/result %s", q, tc.typ.String())
		}
	}
}

func TestMixedScriptUnresolvedResultParameterKeepsOriginalDiagnostic(t *testing.T) {
	schema := append([]model.Source{}, dmlScriptSchema...)
	schema = append(schema, model.Source{Name: "loose.sql", Text: "CREATE TABLE loose(id Uint64 NOT NULL,payload Utf8,PRIMARY KEY(id));"})
	for _, projection := range []string{"$p AS projected", "id, $p AS projected"} {
		t.Run(projection, func(t *testing.T) {
			sql := "-- name: Change :many\nSELECT " + projection + " FROM loose; UPDATE records SET payload=$p;"
			result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
			const want = "cannot resolve type of parameter $p; add DECLARE"
			if err == nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Message != want {
				t.Fatalf("diagnostics=%#v error=%v; want only %q", result.Diagnostics, err, want)
			}
		})
	}
}
