package analyzer

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestNamedTabularSource(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY (id));"}}
	const sql = "-- name: Read :many\n$selected = (SELECT id, label FROM records); SELECT s.id AS id, s.label AS label FROM $selected AS s WHERE s.id = $id;"
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
	if err != nil {
		t.Fatal(err)
	}
	q := result.Queries[0]
	if q.SQL != sql || len(q.ResultSets) != 1 || len(q.ResultSets[0].Columns) != 2 || q.ResultSets[0].Columns[0].Type.Kind != "Uint64" || len(q.Parameters) != 1 || q.Parameters[0].Name != "id" || q.Parameters[0].Type.Kind != "Uint64" {
		t.Fatalf("query = %#v", q)
	}
}

func TestNamedTabularSourceWithoutParentheses(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY (id));"}}
	const sql = "-- name: Read :many\n$selected = SELECT id FROM records; SELECT id FROM $selected;"
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
	if err != nil {
		t.Fatal(err)
	}
	if q := result.Queries[0]; q.SQL != sql || len(q.ResultSets) != 1 || len(q.ResultSets[0].Columns) != 1 || q.ResultSets[0].Columns[0].Type.Kind != "Uint64" {
		t.Fatalf("query = %#v", q)
	}
}

func TestTabularWildcardExpansion(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY (id));"}}
	const sql = "-- name: Read :many\n$selected = (SELECT * FROM records); SELECT s.* FROM $selected AS s;"
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
	if err != nil {
		t.Fatal(err)
	}
	q := result.Queries[0]
	if strings.Contains(q.SQL, "*") || len(q.ResultSets[0].Columns) != 2 || q.ResultSets[0].Columns[0].Name != "id" || q.ResultSets[0].Columns[1].Name != "label" {
		t.Fatalf("query = %#v", q)
	}
}

func TestChainedTabularBindings(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY (id));"}}
	const sql = "-- name: Read :many\n$first = (SELECT id, label FROM records WHERE id=$id); $second = (SELECT id, label FROM $first); SELECT label FROM $second;"
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
	if err != nil {
		t.Fatal(err)
	}
	if q := result.Queries[0]; q.SQL != sql || len(q.Parameters) != 1 || q.Parameters[0].Type.Kind != "Uint64" || len(q.ResultSets[0].Columns) != 1 || q.ResultSets[0].Columns[0].Type.UnwrapOptional().Kind != "Utf8" {
		t.Fatalf("query = %#v", q)
	}
}

func TestTabularBindingUsesComputedLocal(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY (id));"}}
	const sql = "-- name: Read :many\n$minimum = 1ul; $next = $minimum + 1ul; $selected = (SELECT id FROM records WHERE id > $next); SELECT id FROM $selected;"
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
	if err != nil {
		t.Fatal(err)
	}
	if q := result.Queries[0]; len(q.Parameters) != 0 || len(q.ResultSets) != 1 || q.ResultSets[0].Columns[0].Type.Kind != "Uint64" {
		t.Fatalf("query = %#v", q)
	}
}

func TestTabularBindingRetainsQualifiedResultName(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY (id));"}}
	const sql = "-- name: Read :many\n$selected = (SELECT r.id FROM records AS r JOIN records AS other ON r.id = other.id); SELECT s.`r.id` AS id FROM $selected AS s;"
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
	if err != nil {
		t.Fatal(err)
	}
	if columns := result.Queries[0].ResultSets[0].Columns; len(columns) != 1 || columns[0].Name != "id" || columns[0].Type.Kind != "Uint64" {
		t.Fatalf("columns = %#v", columns)
	}
}

func TestTabularBindingWithNestedMembership(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY (id));"}}
	const sql = "-- name: Read :many\n$selected = (SELECT id FROM records WHERE id IN (SELECT id FROM records)); SELECT id FROM $selected;"
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Queries[0].ResultSets[0].Columns) != 1 {
		t.Fatalf("query = %#v", result.Queries[0])
	}
}

func TestNamedTabularDMLSource(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY (id)); CREATE TABLE copies (id Uint64 NOT NULL, label Utf8, PRIMARY KEY (id));"}}
	const sql = "-- name: Copy :exec\n$selected = (SELECT id, label FROM records); UPSERT INTO copies SELECT id, label FROM $selected;"
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
	if err != nil {
		t.Fatal(err)
	}
	if q := result.Queries[0]; q.SQL != sql || len(q.ResultSets) != 0 || len(q.Parameters) != 0 {
		t.Fatalf("query = %#v", q)
	}
}

func TestTabularSourcesWithDatabaseDiscovery(t *testing.T) {
	const sql = "-- name: Read :many\n$selected = (SELECT id, label FROM records); SELECT s.id AS id, d.label AS label FROM $selected AS s JOIN (SELECT id, label FROM records) AS d ON s.id = d.id;"
	database := &fakeAnalysisDatabase{tables: map[string]model.Table{"records": databaseTestTable("label")}}
	result, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: sql}}, Options{}, database)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(database.validated, []string{sql}) || !reflect.DeepEqual(database.described, []string{"records"}) || len(result.Queries) != 1 || len(result.Queries[0].ResultSets[0].Columns) != 2 {
		t.Fatalf("validated = %q, described = %q, queries = %#v", database.validated, database.described, result.Queries)
	}
}

func TestGroupedTabularBindingOuterJoin(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY (id));"}}
	const sql = "-- name: Read :many\n$grouped = (SELECT id, AGGREGATE_LIST(label) AS labels FROM records GROUP BY id); SELECT g.id AS id, g.labels AS labels FROM records AS r LEFT JOIN $grouped AS g ON r.id = g.id;"
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
	if err != nil {
		t.Fatal(err)
	}
	columns := result.Queries[0].ResultSets[0].Columns
	if len(columns) != 2 || !columns[0].Type.IsOptional() || !columns[1].Type.IsOptional() || columns[1].Type.UnwrapOptional().Kind != "List" {
		t.Fatalf("columns = %#v", columns)
	}
	_, err = Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Read :one\nSELECT AGGREGATE_LIST(1ul) AS values;"}})
	if err == nil || !strings.Contains(err.Error(), "aggregate functions require a FROM source") {
		t.Fatalf("aggregate without source: %v", err)
	}
}

func TestYsonResourceRequiresSerialization(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY (id));"}}
	_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Read :one\nSELECT Json::From(AGGREGATE_LIST(label)) AS payload FROM records;"}})
	if err == nil || !strings.Contains(err.Error(), "nonpersistable type Resource<'Yson2.Node'>") {
		t.Fatalf("direct resource result: %v", err)
	}
	_, err = Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Read :one\nSELECT AGGREGATE_LIST(Json::From(label)) AS payload FROM records;"}})
	if err == nil || !strings.Contains(err.Error(), "nonpersistable type List<Resource<'Yson2.Node'>>") {
		t.Fatalf("nested resource result: %v", err)
	}
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Read :one\nSELECT Yson::SerializeJson(Json::From(AGGREGATE_LIST(label))) AS payload FROM records;"}})
	if err != nil {
		t.Fatal(err)
	}
	if columns := result.Queries[0].ResultSets[0].Columns; len(columns) != 1 || columns[0].Type.String() != "Optional<Json>" {
		t.Fatalf("columns = %#v", columns)
	}
}

func TestDerivedJoinSource(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY (id));"}}
	const sql = "-- name: Read :many\nSELECT l.id AS id, r.label AS label FROM records AS l JOIN (SELECT id, label FROM records) AS r ON l.id = r.id;"
	result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: sql}})
	if err != nil {
		t.Fatal(err)
	}
	if columns := result.Queries[0].ResultSets[0].Columns; len(columns) != 2 || columns[0].Type.Kind != "Uint64" || columns[1].Type.UnwrapOptional().Kind != "Utf8" {
		t.Fatalf("columns = %#v", columns)
	}
}

func TestTabularSourceDiagnostics(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY (id));"}}
	for _, tc := range []struct{ sql, want string }{
		{"SELECT id FROM $missing;", "unknown tabular binding $missing"},
		{"$n = 1ul; SELECT id FROM $n;", "local $n is scalar"},
		{"$selected = (SELECT id FROM records); SELECT id FROM records AS s JOIN $selected AS s ON s.id = s.id;", "duplicate source alias"},
		{"$selected = (SELECT id FROM $selected); SELECT id FROM $selected;", "unknown tabular binding $selected"},
		{"$selected = (SELECT id AS value, id AS value FROM records); SELECT value FROM $selected;", "duplicate or unnamed result column"},
		{"$selected = (SELECT id FROM records); $selected = (SELECT id FROM records); SELECT id FROM $selected;", "assigned more than once"},
		{"DECLARE $selected AS Uint64; $selected = (SELECT id FROM records); SELECT id FROM $selected;", "conflicts with a DECLARE parameter"},
		{"$selected = (SELECT id FROM records); DECLARE $id AS Uint64; SELECT id FROM $selected;", "DECLARE statements must precede local assignments"},
		{"SELECT id FROM records; $selected = (SELECT id FROM records);", "local assignments must precede all data statements"},
		{"$selected = (SELECT id FROM records UNION ALL SELECT id FROM records); SELECT id FROM $selected;", "tabular local assignments support one SELECT input"},
		{"$selected = SELECT id FROM records UNION ALL SELECT id FROM records; SELECT id FROM $selected;", "tabular local assignments support one SELECT input"},
		{"$selected = WITH r AS (SELECT id FROM records) SELECT id FROM r; SELECT id FROM $selected;", "CTEs in tabular local assignments are not yet supported"},
		{"$selected = (DISCARD SELECT id FROM records); SELECT id FROM $selected;", "tabular local assignments require SELECT without DISCARD or INTO RESULT"},
		{"$first, $second = (SELECT id FROM records); SELECT id FROM $first;", "only single local assignments are supported"},
		{"$step = 1ul; $next = $step + \"invalid\"; $selected = (SELECT id FROM records WHERE id > $next); SELECT id FROM $selected;", "cannot resolve type of local $next"},
		{"SELECT d.id FROM (SELECT missing AS id FROM records) AS d;", "unknown column"},
		{"SELECT d.id FROM (SELECT id AS id, id AS id FROM records) AS d;", "duplicate or unnamed result column"},
		{"SELECT r.id FROM records AS r JOIN (SELECT id FROM records) ON r.id = id;", "derived SELECT requires an explicit alias"},
		{"SELECT r.id FROM records AS r JOIN (VALUES (1u)) AS v ON r.id = v.column0;", "unsupported FROM or JOIN source"},
		{"SELECT id FROM records WITH (FORCE_INDEX = idx);", "table hints and sampling are not yet supported"},
		{"SELECT id WITHOUT id FROM records;", "SELECT WITHOUT is not yet supported"},
	} {
		result, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\n" + tc.sql}})
		if err == nil || len(result.Diagnostics) == 0 || !strings.Contains(result.Diagnostics[0].Message, tc.want) {
			t.Fatalf("%s: diagnostics = %#v, error = %v; want %q", tc.sql, result.Diagnostics, err, tc.want)
		}
	}
}
