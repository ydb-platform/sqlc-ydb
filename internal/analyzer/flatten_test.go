package analyzer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestAnalyzeFlattenListBy(t *testing.T) {
	for _, tc := range []struct {
		name string
		sql  string
		want []model.Column
	}{
		{
			name: "replace list column",
			sql:  `SELECT tags FROM (SELECT AsList("a", "b") AS tags) FLATTEN LIST BY tags;`,
			want: []model.Column{{Name: "tags", Type: model.Type{Kind: "String"}}},
		},
		{
			name: "named element keeps list",
			sql:  `SELECT item FROM (SELECT AsList("a", "b") AS tags) FLATTEN LIST BY tags AS item;`,
			want: []model.Column{{Name: "item", Type: model.Type{Kind: "String"}}},
		},
		{
			name: "derived alias and predicate",
			sql:  `SELECT x.item FROM (SELECT AsList("a", "b") AS tags) AS x FLATTEN LIST BY tags AS item WHERE x.item = "a";`,
			want: []model.Column{{Name: "item", Type: model.Type{Kind: "String"}}},
		},
		{
			name: "multiple lists",
			sql:  `SELECT tags, ids FROM (SELECT AsList("a", "b") AS tags, AsList(1ul, 2ul) AS ids) FLATTEN LIST BY (tags, ids);`,
			want: []model.Column{{Name: "tags", Type: model.Type{Kind: "String"}}, {Name: "ids", Type: model.Type{Kind: "Uint64"}}},
		},
		{
			name: "parenthesized aliases",
			sql:  `SELECT tag, id FROM (SELECT AsList("a") AS tags, AsList(1ul) AS ids) FLATTEN LIST BY (tags AS tag, ids AS id);`,
			want: []model.Column{{Name: "tag", Type: model.Type{Kind: "String"}}, {Name: "id", Type: model.Type{Kind: "Uint64"}}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Flatten :many\n" + tc.sql}})
			require.NoError(t, err)
			require.Equal(t, tc.want, got.Queries[0].ResultSets[0].Columns)
		})
	}
}

func TestAnalyzeFlattenOptionalListBy(t *testing.T) {
	query := `-- name: Flatten :many
DECLARE $rows AS List<Struct<tags:Optional<List<Utf8>>>>;
SELECT item FROM AS_TABLE($rows) AS r FLATTEN LIST BY tags AS item;`
	got, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: query}})
	require.NoError(t, err)
	require.Equal(t, []model.Column{{Name: "item", Type: model.Type{Kind: "Utf8"}}}, got.Queries[0].ResultSets[0].Columns)
}

func TestAnalyzeFlattenListByRejectsNonList(t *testing.T) {
	_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: `-- name: Flatten :many
SELECT item FROM (SELECT "a" AS value) FLATTEN LIST BY value AS item;`}})
	require.ErrorContains(t, err, "FLATTEN LIST BY column \"value\" requires List<T>")
}

func TestAnalyzeFlattenListByRejectsUnknownAndCollidingColumns(t *testing.T) {
	for _, tc := range []struct {
		sql  string
		want string
	}{
		{`SELECT item FROM (SELECT AsList("a") AS tags) FLATTEN LIST BY missing AS item;`, `unknown FLATTEN column "missing"`},
		{`SELECT item FROM (SELECT AsList("a") AS tags) FLATTEN LIST BY tags AS tags;`, `FLATTEN LIST BY result column "tags" already exists`},
		{`SELECT item FROM (SELECT AsList("a") AS tags) AS x FLATTEN LIST BY y.tags AS item;`, `unknown FLATTEN source alias "y"`},
		{`SELECT item FROM (SELECT AsList("a") AS tags) FLATTEN LIST BY (AsList("b") AS item);`, `FLATTEN LIST BY expressions require a source column`},
	} {
		t.Run(tc.want, func(t *testing.T) {
			_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Flatten :many\n" + tc.sql}})
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestAnalyzeFlattenListByRejectsEmbedOfTransformedTable(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE authors (id Uint64 NOT NULL, tags List<Utf8>, PRIMARY KEY(id));`}}
	query := []model.Source{{Name: "query.sql", Text: `-- name: Read :many
SELECT sqlc.embed(a) FROM authors AS a FLATTEN LIST BY tags;`}}
	_, err := Analyze(schema, query)
	require.ErrorContains(t, err, "sqlc.embed requires a physical catalog table")
	require.ErrorContains(t, err, "flattened")
}

func TestAnalyzeFlattenListByRejectsIgnoredSourceColumnList(t *testing.T) {
	for _, sql := range []string{
		`SELECT x.n FROM (SELECT AsList("a") AS tags, 1 AS n) AS x(n);`,
		`SELECT x.n FROM (SELECT AsList("a") AS tags, 1 AS n) AS x(n) FLATTEN LIST BY tags;`,
	} {
		_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\n" + sql}})
		require.ErrorContains(t, err, "source column lists are unsupported")
	}
}
