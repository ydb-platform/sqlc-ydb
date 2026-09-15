package analyzer

import (
	"strings"
	"testing"

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
		{"optional struct", "Optional<Struct<id:Uint64>>", "SELECT $key.id AS value;", "", "Optional<Struct>"},
		{"missing", "Struct<id:Uint64>", "SELECT id FROM records WHERE id=$key.missing;", "", "unknown struct field"},
		{"wrong type", "Struct<id:Utf8>", "SELECT id FROM records WHERE id=$key.id;", "", "incompatible"},
		{"wrong base", "Uint64", "SELECT $key.id AS value;", "", "requires Struct"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Lookup :many\nDECLARE $key AS " + tc.declaration + ";\n" + tc.sql}})
			if tc.failure != "" {
				if err == nil || !strings.Contains(err.Error(), tc.failure) {
					t.Fatalf("error = %v, want %s", err, tc.failure)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Queries[0].ResultSets[0].Columns[0].Type.String() != tc.result {
				t.Fatal(got.Queries[0])
			}
			if len(got.Queries[0].Parameters) != 1 || got.Queries[0].Parameters[0].Name != "key" {
				t.Fatal(got.Queries[0].Parameters)
			}
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
	if err != nil {
		t.Fatal(err)
	}
	if got.Queries[0].ResultSets[0].Columns[0].Type.Kind != "Uint64" {
		t.Fatal(got.Queries[0])
	}
}

func TestUnaryNotResolvesFunctionAtomically(t *testing.T) {
	options := Options{Functions: []builtins.Signature{{Name: "IsReady", Returns: model.Type{Kind: "Bool"}}}}
	if _, err := AnalyzeWithOptions(nil, []model.Source{{Name: "query.sql", Text: "-- name: Read :one\nSELECT true AS value WHERE NOT IsReady();"}}, options); err != nil {
		t.Fatal(err)
	}
	_, err := AnalyzeWithOptions(nil, []model.Source{{Name: "query.sql", Text: "-- name: Read :one\nSELECT true AS value WHERE NOT Unknown::Ready();"}}, options)
	if err == nil || !strings.Contains(err.Error(), "unsupported YQL function") {
		t.Fatalf("unknown function under NOT: %v", err)
	}
}

func TestConfiguredStructTypeValidation(t *testing.T) {
	for _, typ := range []model.Type{
		{Kind: "Struct", Fields: []model.StructField{{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "id", Type: model.Type{Kind: "Utf8"}}}},
		{Kind: "Struct", Fields: []model.StructField{{Name: "", Type: model.Type{Kind: "Uint64"}}}},
		{Kind: "Uint64", Fields: []model.StructField{{Name: "hidden", Type: model.Type{Kind: "Utf8"}}}},
	} {
		_, err := AnalyzeWithOptions(nil, nil, Options{Functions: []builtins.Signature{{Name: "Broken", Returns: typ}}})
		if err == nil {
			t.Fatalf("accepted invalid configured type: %#v", typ)
		}
	}
}

func TestUnknownFunctionsCannotHideInStructuredExpressions(t *testing.T) {
	for _, expression := range []string{
		"CAST(Unknown::Hash($id) AS Uint64)",
		"Unknown::Record($id).field",
	} {
		query := "-- name: Read :one\nDECLARE $id AS Uint64;\nSELECT " + expression + " AS value;"
		_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: query}})
		if err == nil || !strings.Contains(err.Error(), "unsupported YQL function") {
			t.Fatalf("%s: %v", expression, err)
		}
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
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestStructuredTableColumnMemberAccess(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, payload Struct<label:Utf8,count:Uint64> NOT NULL, PRIMARY KEY(id));`}}
	got, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\nSELECT r.payload.count AS value FROM records r;"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Queries[0].ResultSets[0].Columns[0].Type.Kind != "Uint64" {
		t.Fatal(got.Queries[0])
	}
}

func TestParseTypeRejectsStatementInjectionAndMalformedInput(t *testing.T) {
	for _, text := range []string{
		"",
		"Uint64; DECLARE $other AS Utf8",
		"Uint64; SELECT 1",
		"List<Struct<id:Uint64>",
		"Uint64 trailing",
	} {
		if typ, err := ParseType(text); err == nil {
			t.Fatalf("ParseType(%q) = %v, want error", text, typ)
		}
	}
}

func TestBytesAliasEveryTypePosition(t *testing.T) {
	for _, spelling := range []string{"Bytes", "String"} {
		got, err := Analyze([]model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL,payload " + spelling + ", PRIMARY KEY(id));"}}, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\nDECLARE $raw AS " + spelling + ";\nSELECT payload FROM records WHERE payload=$raw;"}})
		if err != nil {
			t.Fatal(err)
		}
		if got.Queries[0].Parameters[0].Type.Kind != "String" {
			t.Fatal(got.Queries[0].Parameters)
		}
		typ, err := parseType("List<Struct<data:" + spelling + ">>")
		if err != nil || typ.Elem.Fields[0].Type.Kind != "String" {
			t.Fatalf("type=%v error=%v", typ, err)
		}
	}
}

func TestUnknownFunctionInPredicateRejected(t *testing.T) {
	_, err := Analyze([]model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));"}}, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\nDECLARE $id AS Uint64;\nSELECT id FROM records WHERE id=Unknown::Hash($id);"}})
	if err == nil || !strings.Contains(err.Error(), "unsupported YQL function") {
		t.Fatalf("error=%v", err)
	}
}
