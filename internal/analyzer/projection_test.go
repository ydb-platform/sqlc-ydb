package analyzer

import (
	"reflect"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestAnalyzeProjectionOrderAndAliases(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY (id));`}}
	id := model.Column{Name: "id", Type: model.Type{Kind: "Uint64"}, Table: "records"}
	label := model.Column{Name: "label", Type: model.Optional(model.Type{Kind: "Utf8"}), Table: "records"}
	for _, tt := range []struct {
		name string
		sql  string
		want []model.Column
	}{
		{"star", "SELECT * FROM records;", []model.Column{id, label}},
		{"repeated stars", "SELECT *, *, r.* FROM records r;", []model.Column{id, label, id, label, id, label}},
		{"distinct", "SELECT DISTINCT label, id FROM records;", []model.Column{label, id}},
		{"column alias without AS", "SELECT label caption FROM records;", []model.Column{{Name: "caption", Type: label.Type, Table: "records"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Rows :many\n" + tt.sql}})
			if err != nil {
				t.Fatal(err)
			}
			if columns := got.Queries[0].ResultSets[0].Columns; !reflect.DeepEqual(columns, tt.want) {
				t.Fatalf("columns = %#v, want %#v", columns, tt.want)
			}
		})
	}
}

func TestAnalyzeJoinKindsAndOrder(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, label Utf8, PRIMARY KEY (id));`}}
	for _, tt := range []struct {
		name     string
		from     string
		optional []bool
	}{
		{"right", "records a RIGHT JOIN records b ON a.id = b.id", []bool{true, false}},
		{"full", "records a FULL JOIN records b ON a.id = b.id", []bool{true, true}},
		{"self left", "records a LEFT JOIN records b ON a.id = b.id", []bool{false, true}},
		{"comma", "records a, records b", []bool{false, false}},
		{"cross", "records a CROSS JOIN records b", []bool{false, false}},
		{"left then inner", "records a LEFT JOIN records b ON a.id = b.id INNER JOIN records c ON a.id = c.id", []bool{false, true, false}},
		{"inner then left", "records a INNER JOIN records b ON a.id = b.id LEFT JOIN records c ON a.id = c.id", []bool{false, false, true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			projection := "a.id, b.id, b.label"
			if len(tt.optional) == 3 {
				projection += ", c.id"
			}
			got, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Joined :many\nSELECT " + projection + " FROM " + tt.from + ";"}})
			if err != nil {
				t.Fatal(err)
			}
			var want []model.Column
			for i, optional := range tt.optional {
				typ := model.Type{Kind: "Uint64"}
				if optional {
					typ = model.Optional(typ)
				}
				want = append(want, model.Column{Name: "id", WireName: string(rune('a'+i)) + ".id", Type: typ, Table: "records"})
				if i == 1 {
					want = append(want, model.Column{Name: "label", WireName: "b.label", Type: model.Optional(model.Type{Kind: "Utf8"}), Table: "records"})
				}
			}
			if columns := got.Queries[0].ResultSets[0].Columns; !reflect.DeepEqual(columns, want) {
				t.Fatalf("columns = %#v, want %#v", columns, want)
			}
		})
	}
}

func TestAnalyzeJoinStarOrder(t *testing.T) {
	got, err := Analyze([]model.Source{{Name: "schema.sql", Text: `
CREATE TABLE first (id Uint64 NOT NULL, PRIMARY KEY (id));
CREATE TABLE second (label Utf8 NOT NULL, PRIMARY KEY (label));`}},
		[]model.Source{{Name: "query.sql", Text: "-- name: Rows :many\nSELECT * FROM second, first;"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Column{{Name: "label", Type: model.Type{Kind: "Utf8"}, Table: "second"}, {Name: "id", Type: model.Type{Kind: "Uint64"}, Table: "first"}}
	if columns := got.Queries[0].ResultSets[0].Columns; !reflect.DeepEqual(columns, want) {
		t.Fatalf("columns = %#v, want %#v", columns, want)
	}
}
