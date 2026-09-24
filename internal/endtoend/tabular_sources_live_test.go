package endtoend

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/cli"
)

func TestTabularSourcesGeneratedGoCompiles(t *testing.T) {
	runTabularSources(t, "grpc://localhost:2136/local", true)
}

func TestLiveYDBTabularSources(t *testing.T) {
	dsn := os.Getenv("YDB_CONNECTION_STRING")
	if dsn == "" {
		t.Skip("set YDB_CONNECTION_STRING for tabular source validation")
	}
	runTabularSources(t, dsn, false)
}

func runTabularSources(t *testing.T, dsn string, compileOnly bool) {
	t.Helper()
	dir := t.TempDir()
	table := fmt.Sprintf("sqlc_tabular_%d", time.Now().UnixNano())
	replace := strings.NewReplacer("profiles", table, "entries", table+"_entries", "archive", table+"_archive")
	schema := replace.Replace(tabularSourcesSchema)
	queries := replace.Replace(tabularSourcesQueries)
	configuration := "version: '2'\nsql:\n"
	for _, runtime := range []string{"ydb", "database/sql"} {
		configuration += "- engine: ydb\n  schema: schema.sql\n  queries: queries.sql\n  gen:\n    go:\n      package: records\n      out: " + strings.ReplaceAll(runtime, "/", "_") + "\n      sql_package: " + runtime + "\n"
	}
	for name, contents := range map[string]string{
		"schema.sql":  schema,
		"queries.sql": queries,
		"sqlc.yaml":   configuration,
		"go.mod":      "module generated\n\ngo 1.26.0\n\nrequire github.com/ydb-platform/ydb-go-sdk/v3 v3.151.1\n",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(contents), 0600))
	}
	var stdout, stderr bytes.Buffer
	require.Zero(t, cli.Run([]string{"generate", "-f", filepath.Join(dir, "sqlc.yaml")}, &stdout, &stderr), "generate tabular sources: %s", stderr.String())
	for _, runtime := range []string{"ydb", "database/sql"} {
		t.Run(runtime, func(t *testing.T) {
			setup, sqlImport := "q := New(driver.Query())", ""
			if runtime == "database/sql" {
				setup = "db := sql.OpenDB(ydb.MustConnector(driver)); defer db.Close(); q := New(db)"
				sqlImport = "\"database/sql\""
			}
			source := strings.NewReplacer(
				"$DSN", strconv.Quote(dsn),
				"$SCHEMA", strconv.Quote(schema),
				"$TABLE", strconv.Quote(table),
				"$SETUP", setup,
				"$SQL_IMPORT", sqlImport,
			).Replace(tabularSourcesRuntime)
			compileTypedDMLPackage(t, dir, "./"+strings.ReplaceAll(runtime, "/", "_"), source, compileOnly)
		})
	}
}

const tabularSourcesSchema = `CREATE TABLE profiles (id Uint64 NOT NULL, name Utf8 NOT NULL, active Bool NOT NULL, PRIMARY KEY(id));
CREATE TABLE entries (id Uint64 NOT NULL, profile_id Uint64 NOT NULL, title Utf8 NOT NULL, score Int32 NOT NULL, PRIMARY KEY(id));
CREATE TABLE archive (id Uint64 NOT NULL, title Utf8 NOT NULL, PRIMARY KEY(id));`

const tabularSourcesQueries = `-- name: NamedGroups :many
DECLARE $minimum AS Int32;
$selected = (SELECT profile_id, title FROM entries WHERE score >= $minimum);
$grouped = (
    SELECT profile_id, AGGREGATE_LIST(title, 100u) AS titles
    FROM $selected
    GROUP BY profile_id
);
SELECT p.id, p.name, Yson::SerializeJson(Json::From(g.titles)) AS titles_json
FROM (SELECT id, name FROM profiles WHERE active) AS p
JOIN $grouped AS g ON p.id = g.profile_id
ORDER BY p.id;

-- name: DerivedLeftJoin :many
DECLARE $minimum AS Int32;
SELECT p.id, p.name, e.title
FROM (SELECT id, name FROM profiles WHERE active) AS p
LEFT JOIN (SELECT profile_id, title FROM entries WHERE score >= $minimum) AS e
    ON p.id = e.profile_id
ORDER BY p.id;

-- name: CopyRecentToArchive :exec
DECLARE $minimum AS Int32;
$recent = (SELECT id, title FROM entries WHERE score >= $minimum);
UPSERT INTO archive (id, title) SELECT id, title FROM $recent;

-- name: ListArchive :many
SELECT id, title FROM archive ORDER BY id;`

const tabularSourcesRuntime = `package records

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
	$SQL_IMPORT
	ydb "github.com/ydb-platform/ydb-go-sdk/v3"
)

func TestTabularSources(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	driver, err := ydb.Open(ctx, $DSN, ydb.WithAnonymousCredentials())
	if err != nil { t.Fatal(err) }
	defer driver.Close(ctx)
	for _, ddl := range strings.Split($SCHEMA, ";") {
		if strings.TrimSpace(ddl) == "" { continue }
		if err := driver.Query().Exec(ctx, ddl); err != nil { t.Fatal(err) }
		name := strings.Fields(ddl)[2]
		defer func() {
			cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
			defer done()
			if err := driver.Query().Exec(cleanup, "DROP TABLE "+name); err != nil { t.Error(err) }
		}()
	}
	table := $TABLE
	if err := driver.Query().Exec(ctx, "UPSERT INTO "+table+" (id,name,active) VALUES (1ul,'Alice'u,true),(2ul,'Bob'u,true),(3ul,'Cara'u,false),(4ul,'Dana'u,true);"); err != nil { t.Fatal(err) }
	if err := driver.Query().Exec(ctx, "UPSERT INTO "+table+"_entries (id,profile_id,title,score) VALUES (10ul,1ul,'first'u,2020),(11ul,1ul,'second'u,2025),(12ul,2ul,'third'u,2023),(13ul,3ul,'hidden'u,2024);"); err != nil { t.Fatal(err) }
	$SETUP
	grouped, err := q.NamedGroups(ctx, 2020)
	if err != nil || len(grouped) != 2 || grouped[0].ID != 1 || grouped[0].Name != "Alice" || grouped[1].ID != 2 || grouped[1].Name != "Bob" {
		t.Fatalf("named grouped sources: %+v, %v", grouped, err)
	}
	checkTitles(t, grouped[0].TitlesJson, []string{"first", "second"})
	checkTitles(t, grouped[1].TitlesJson, []string{"third"})
	empty, err := q.NamedGroups(ctx, 2030)
	if err != nil || len(empty) != 0 { t.Fatalf("empty named sources: %+v, %v", empty, err) }
	joined, err := q.DerivedLeftJoin(ctx, 2024)
	if err != nil || len(joined) != 3 || joined[0].ID != 1 || joined[0].Title == nil || *joined[0].Title != "second" || joined[1].ID != 2 || joined[1].Title != nil || joined[2].ID != 4 || joined[2].Title != nil {
		t.Fatalf("derived LEFT JOIN/nullability: %+v, %v", joined, err)
	}
	if err := q.CopyRecentToArchive(ctx, 2024); err != nil { t.Fatal(err) }
	archived, err := q.ListArchive(ctx)
	if err != nil || len(archived) != 2 || archived[0].ID != 11 || archived[0].Title != "second" || archived[1].ID != 13 || archived[1].Title != "hidden" {
		t.Fatalf("named source in UPSERT SELECT: %+v, %v", archived, err)
	}
}

func checkTitles(t *testing.T, value *string, want []string) {
	t.Helper()
	if value == nil { t.Fatal("missing JSON titles") }
	var got []string
	if err := json.Unmarshal([]byte(*value), &got); err != nil { t.Fatal(err) }
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) { t.Fatalf("JSON titles = %v, want %v", got, want) }
}
`
