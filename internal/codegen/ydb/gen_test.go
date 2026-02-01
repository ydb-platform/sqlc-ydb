package ydb

import (
	"context"
	"strings"
	"testing"

	"github.com/sqlc-dev/sqlc-engine-ydb/internal/codegen/pb"
)

// TestGenerate_authors runs the generator with a request mimicking sqlc output
// for examples/authors (empty catalog, one query file with GetAuthor/ListAuthors/etc).
func TestGenerate_authors(t *testing.T) {
	req := &pb.GenerateRequest{
		Settings: &pb.Settings{
			Codegen: &pb.Codegen{Out: "db"},
		},
		PluginOptions: []byte(`{"package":"db","sql_package":"github.com/ydb-platform/ydb-go-sdk/v3"}`),
		Catalog:       &pb.Catalog{}, // empty — plugin derives row types from queries
		Queries: []*pb.Query{
			{
				Name:     "GetAuthor",
				Cmd:      ":one",
				Text:     "SELECT * FROM authors WHERE id = $id LIMIT 1",
				Filename: "queries.sql",
				Params: []*pb.Parameter{
					{Number: 1, Column: &pb.Column{Name: "id", Type: &pb.Identifier{Name: "uint64"}, NotNull: true}},
				},
				Columns: []*pb.Column{
					{Name: "id", Type: &pb.Identifier{Name: "uint64"}, NotNull: true, Table: &pb.Identifier{Name: "authors"}},
					{Name: "name", Type: &pb.Identifier{Name: "utf8"}, NotNull: true, Table: &pb.Identifier{Name: "authors"}},
					{Name: "bio", Type: &pb.Identifier{Name: "utf8"}, NotNull: false, Table: &pb.Identifier{Name: "authors"}},
				},
			},
			{
				Name:     "ListAuthors",
				Cmd:      ":many",
				Text:     "SELECT * FROM authors ORDER BY name",
				Filename: "queries.sql",
				Columns: []*pb.Column{
					{Name: "id", Type: &pb.Identifier{Name: "uint64"}, NotNull: true},
					{Name: "name", Type: &pb.Identifier{Name: "utf8"}, NotNull: true},
					{Name: "bio", Type: &pb.Identifier{Name: "utf8"}, NotNull: false},
				},
			},
			{
				Name:     "CreateAuthor",
				Cmd:      ":one",
				Text:     "INSERT INTO authors (name, bio) VALUES ($name, $bio) RETURNING *",
				Filename: "queries.sql",
				Params: []*pb.Parameter{
					{Number: 1, Column: &pb.Column{Name: "name", Type: &pb.Identifier{Name: "utf8"}, NotNull: true}},
					{Number: 2, Column: &pb.Column{Name: "bio", Type: &pb.Identifier{Name: "utf8"}, NotNull: false}},
				},
				Columns: []*pb.Column{
					{Name: "id", Type: &pb.Identifier{Name: "uint64"}, NotNull: true},
					{Name: "name", Type: &pb.Identifier{Name: "utf8"}, NotNull: true},
					{Name: "bio", Type: &pb.Identifier{Name: "utf8"}, NotNull: false},
				},
			},
			{
				Name:     "UpdateAuthor",
				Cmd:      ":exec",
				Text:     "UPDATE authors SET name = $name, bio = $bio WHERE id = $id",
				Filename: "queries.sql",
				Params: []*pb.Parameter{
					{Number: 1, Column: &pb.Column{Name: "name", Type: &pb.Identifier{Name: "utf8"}, NotNull: true}},
					{Number: 2, Column: &pb.Column{Name: "bio", Type: &pb.Identifier{Name: "utf8"}, NotNull: false}},
					{Number: 3, Column: &pb.Column{Name: "id", Type: &pb.Identifier{Name: "uint64"}, NotNull: true}},
				},
			},
			{
				Name:     "DeleteAuthor",
				Cmd:      ":exec",
				Text:     "DELETE FROM authors WHERE id = $id",
				Filename: "queries.sql",
				Params: []*pb.Parameter{
					{Number: 1, Column: &pb.Column{Name: "id", Type: &pb.Identifier{Name: "uint64"}, NotNull: true}},
				},
			},
		},
	}

	resp, err := Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp == nil || len(resp.Files) == 0 {
		t.Fatal("expected non-empty Files")
	}

	byName := make(map[string][]byte)
	for _, f := range resp.Files {
		byName[f.Name] = f.Contents
	}

	for _, name := range []string{"models.go", "db.go", "queries.sql.go"} {
		if byName[name] == nil {
			t.Errorf("missing file %q", name)
			continue
		}
		body := string(byName[name])
		if body == "" {
			t.Errorf("file %q is empty", name)
		}
		if !strings.Contains(body, "package db") {
			t.Errorf("file %q does not contain package db", name)
		}
	}

	models := string(byName["models.go"])
	for _, want := range []string{"GetAuthorRow", "ListAuthorsRow", "CreateAuthorRow"} {
		if !strings.Contains(models, want) {
			t.Errorf("models.go: expected to contain %q", want)
		}
	}

	qgo := string(byName["queries.sql.go"])
	for _, want := range []string{"GetAuthor", "ListAuthors", "CreateAuthor", "UpdateAuthor", "DeleteAuthor", "Queries", "QueryRow", "QueryResultSet", "Exec"} {
		if !strings.Contains(qgo, want) {
			t.Errorf("queries.sql.go: expected to contain %q", want)
		}
	}
}
