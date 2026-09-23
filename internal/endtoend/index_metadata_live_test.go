package endtoend

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/config"
	"github.com/ydb-platform/sqlc-ydb/internal/database"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestLiveYDBIndexMetadata(t *testing.T) {
	uri := os.Getenv("YDB_CONNECTION_STRING")
	if uri == "" {
		t.Skip("set YDB_CONNECTION_STRING for live secondary-index metadata checks")
	}
	dir := t.TempDir()
	table := fmt.Sprintf("sqlc_index_metadata_%d", time.Now().UnixNano())
	schemaSQL := "CREATE TABLE " + table + ` (
    id Uint64 NOT NULL,
    label Utf8 NOT NULL,
    rank Uint64 NOT NULL,
    payload Utf8,
    extra Utf8,
    PRIMARY KEY (id),
    INDEX by_label GLOBAL SYNC ON (label, rank) COVER (payload, extra),
    INDEX by_rank GLOBAL ASYNC ON (rank)
);`
	runDatabasePython(t, dir, indexMetadataFixturePython, schemaSQL)
	t.Cleanup(func() { runDatabasePython(t, dir, indexMetadataFixturePython, "DROP TABLE "+table+";") })
	settings, err := (config.Database{URI: uri, Timeout: "30s"}).Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	client, err := database.New(settings)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	described, err := client.DescribeTable(context.Background(), table)
	if err != nil {
		t.Fatal(err)
	}
	wantIndexes := map[string]model.Index{
		"by_label": {Name: "by_label", Kind: "GlobalSync", Columns: []string{"label", "rank"}, DataColumns: []string{"extra", "payload"}},
		"by_rank":  {Name: "by_rank", Kind: "GlobalAsync", Columns: []string{"rank"}},
	}
	if len(described.Indexes) != len(wantIndexes) {
		t.Fatalf("DescribeTable indexes = %#v", described.Indexes)
	}
	for _, index := range described.Indexes {
		index.DataColumns = slices.Sorted(slices.Values(index.DataColumns))
		if !reflect.DeepEqual(index, wantIndexes[index.Name]) {
			t.Fatalf("DescribeTable index = %#v, want %#v", index, wantIndexes[index.Name])
		}
	}

	schema := []model.Source{{Name: "schema.sql", Text: schemaSQL}}
	queries := []model.Source{{Name: "queries.sql", Text: `-- name: ByLabel :many
DECLARE $label AS Utf8;
SELECT r.* FROM ` + table + ` VIEW by_label AS r WHERE r.label = $label;
-- name: ByRank :many
DECLARE $rank AS Uint64;
SELECT id, label FROM ` + table + ` VIEW by_rank WHERE rank = $rank;
`}}
	offline, err := analyzer.Analyze(schema, queries)
	if err != nil {
		t.Fatal(err)
	}
	for _, localSchema := range []bool{false, true} {
		var local []model.Source
		if localSchema {
			local = schema
		}
		connected, err := analyzer.AnalyzeWithDatabase(context.Background(), local, queries, analyzer.Options{}, client)
		if err != nil {
			t.Fatal(err)
		}
		if len(connected.Queries) != len(offline.Queries) {
			t.Fatalf("query count changed: %d", len(connected.Queries))
		}
		for i, got := range connected.Queries {
			want := offline.Queries[i]
			if got.SQL != want.SQL || !reflect.DeepEqual(got.Parameters, want.Parameters) || !reflect.DeepEqual(got.ResultSets, want.ResultSets) {
				t.Fatalf("local schema=%v: query %s lost offline/connected parity", localSchema, got.Name)
			}
		}
	}

	for _, tc := range []struct {
		name, from, to, want string
	}{
		{"kind", "by_label GLOBAL SYNC", "by_label GLOBAL ASYNC", "local kind GlobalAsync and database kind GlobalSync"},
		{"key order", "ON (label, rank)", "ON (rank, label)", "local key columns [rank label] and database key columns [label rank]"},
		{"cover columns", "COVER (payload, extra)", "COVER (payload)", "covering columns"},
		{"extra remote index", ",\n    INDEX by_rank GLOBAL ASYNC ON (rank)", "", "database index \"by_rank\" is missing from the local schema"},
		{"missing remote index", "INDEX by_rank", "INDEX additional GLOBAL SYNC ON (extra),\n    INDEX by_rank", "index \"additional\" is missing from the database"},
		{"cover order ignored", "COVER (payload, extra)", "COVER (extra, payload)", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := []model.Source{{Name: "schema.sql", Text: strings.Replace(schemaSQL, tc.from, tc.to, 1)}}
			_, err := analyzer.AnalyzeWithDatabase(context.Background(), changed, queries, analyzer.Options{}, client)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "database schema drift") || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("drift = %v, want %q", err, tc.want)
			}
		})
	}
}

const indexMetadataFixturePython = databasePythonConnection + `import sys
with ydb.Driver(config) as driver:
    driver.wait(20)
    with ydb.QuerySessionPool(driver) as pool:
        pool.execute_with_retries(sys.argv[1])
`
