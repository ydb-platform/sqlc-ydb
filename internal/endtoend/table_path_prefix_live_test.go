package endtoend

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/cli"
	"github.com/ydb-platform/sqlc-ydb/internal/config"
	"github.com/ydb-platform/sqlc-ydb/internal/database"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

// Same-leaf tables have different schemas and rows, so resolving the wrong
// namespace cannot pass either metadata checks or generated runtime checks.
func TestLiveYDBTablePathPrefix(t *testing.T) {
	dsn := os.Getenv("YDB_CONNECTION_STRING")
	if dsn == "" {
		t.Skip("set YDB_CONNECTION_STRING for TablePathPrefix validation")
	}
	dir := t.TempDir()
	settings, err := (config.Database{URI: dsn, Timeout: "30s"}).Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	root := path.Join(settings.Database, fmt.Sprintf("sqlc_prefixx%d", time.Now().UnixNano()))
	a, b := root+"/a", root+"/b"
	schemas := []model.Source{
		{Name: "a.sql", Text: "PRAGMA TablePathPrefix(" + strconv.Quote(a) + ");\nCREATE TABLE users (id Uint64 NOT NULL, name Utf8 NOT NULL, note Utf8, PRIMARY KEY(id), INDEX by_name GLOBAL SYNC ON(name));"},
		{Name: "b.sql", Text: "PRAGMA TablePathPrefix(" + strconv.Quote(b) + ");\nCREATE TABLE users (active Bool NOT NULL, id Uint64 NOT NULL, name Utf8 NOT NULL, PRIMARY KEY(id));"},
	}
	runDatabasePython(t, dir, tablePathPrefixFixturePython, "create", root, schemas[0].Text, schemas[1].Text)
	t.Cleanup(func() { runDatabasePython(t, dir, tablePathPrefixFixturePython, "drop", root) })
	querySQL := strings.NewReplacer("$PREFIX_A", a, "$PREFIX_B", b, "$ROOT", root).Replace(tablePathPrefixQueries)
	queries := []model.Source{{Name: "queries.sql", Text: querySQL}}
	offline, err := analyzer.Analyze(schemas, queries)
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
	for _, localSchema := range []bool{false, true} {
		var local []model.Source
		if localSchema {
			local = schemas
		}
		connected, err := analyzer.AnalyzeWithDatabase(context.Background(), local, queries, analyzer.Options{}, client)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, table := range connected.Catalog.Tables {
			names = append(names, table.Name)
		}
		slices.Sort(names)
		if !slices.Equal(names, []string{a + "/users", b + "/users"}) {
			t.Fatalf("discovered table identities = %v", names)
		}
		if len(connected.Queries) != len(offline.Queries) {
			t.Fatalf("connected query count = %d", len(connected.Queries))
		}
		for i, got := range connected.Queries {
			want := offline.Queries[i]
			if got.SQL != want.SQL || !reflect.DeepEqual(got.Parameters, want.Parameters) || !reflect.DeepEqual(got.ResultSets, want.ResultSets) {
				t.Fatalf("local schema=%v: query %s lost offline/connected parity", localSchema, got.Name)
			}
		}
	}
	for _, change := range []struct {
		file     int
		from, to string
	}{{0, "note Utf8", "note Int32"}, {1, "active Bool", "active Utf8"}} {
		changed := slices.Clone(schemas)
		changed[change.file].Text = strings.Replace(changed[change.file].Text, change.from, change.to, 1)
		_, err := analyzer.AnalyzeWithDatabase(context.Background(), changed, queries, analyzer.Options{}, client)
		wantTable := []string{a, b}[change.file] + "/users"
		if err == nil || !strings.Contains(err.Error(), "database schema drift") || !strings.Contains(err.Error(), wantTable) {
			t.Fatalf("drift for %s = %v", wantTable, err)
		}
	}

	configuration := "version: '2'\nsql:\n"
	for _, runtime := range []string{"ydb", "database/sql"} {
		configuration += "- engine: ydb\n  schema: [a.sql, b.sql]\n  queries: queries.sql\n  gen:\n    go:\n      package: records\n      out: " + strings.ReplaceAll(runtime, "/", "_") + "\n      sql_package: " + runtime + "\n"
	}
	for name, content := range map[string]string{
		"a.sql": schemas[0].Text, "b.sql": schemas[1].Text, "queries.sql": querySQL, "sqlc.yaml": configuration,
		"go.mod": "module generated\n\ngo 1.26.0\n\nrequire github.com/ydb-platform/ydb-go-sdk/v3 v3.151.1\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"generate", "-f", filepath.Join(dir, "sqlc.yaml")}, &stdout, &stderr); code != 0 {
		t.Fatalf("generate namespace fixture: %s", stderr.String())
	}
	for _, runtime := range []string{"ydb", "database/sql"} {
		t.Run(runtime, func(t *testing.T) {
			setup, imports := "q := New(driver.Query())", ""
			if runtime == "database/sql" {
				setup = "db := sql.OpenDB(ydb.MustConnector(driver)); defer db.Close(); q := New(db)"
				imports = `"database/sql";`
			}
			source := `package records
import("context";"testing";"time";` + imports + `ydb "github.com/ydb-platform/ydb-go-sdk/v3")
func TestNamespaces(t *testing.T) {
 ctx,cancel:=context.WithTimeout(context.Background(),60*time.Second);defer cancel()
 driver,err:=ydb.Open(ctx,` + strconv.Quote(dsn) + `,ydb.WithAnonymousCredentials());if err!=nil {t.Fatal(err)};defer driver.Close(ctx)
 ` + setup + `
 first,err:=q.ReadA(ctx,1);if err!=nil || first.Name!="A" || first.Note==nil || *first.Note!="original" {t.Fatalf("namespace A: %v %v",first,err)}
 other,err:=q.ReadB(ctx,1);if err!=nil || other.Name!="B" || !other.Active {t.Fatalf("namespace B: %v %v",other,err)}
 absolute,err:=q.ReadAbsoluteB(ctx,1);if err!=nil || absolute.Name!="B" || !absolute.Active {t.Fatalf("absolute bypass: %v %v",absolute,err)}
 relative,err:=q.ReadRelativeA(ctx,1);if err!=nil || relative.Name!="A" {t.Fatalf("relative source: %v %v",relative,err)}
 indexed,err:=q.FindA(ctx,"A");if err!=nil || len(indexed)!=1 || indexed[0].ID!=1 {t.Fatalf("prefixed VIEW: %v %v",indexed,err)}
 if err:=q.InsertA(ctx,InsertAParams{ID:2,Name:"inserted"});err!=nil {t.Fatal(err)}
 if err:=q.UpdateA(ctx,UpdateAParams{ID:2,Name:"updated"});err!=nil {t.Fatal(err)}
 inserted,err:=q.ReadA(ctx,2);if err!=nil || inserted.Name!="updated" || inserted.Note!=nil {t.Fatalf("prefixed writes: %v %v",inserted,err)}
 if err:=q.CopyFromB(ctx,"B");err!=nil {t.Fatal(err)}
 copied,err:=q.ReadA(ctx,11);if err!=nil || copied.Name!="B" || copied.Note!=nil {t.Fatalf("cross-namespace UPSERT SELECT: %v %v",copied,err)}
 for _,id:=range []uint64{2,11} {if err:=q.DeleteA(ctx,id);err!=nil {t.Fatal(err)}}
 other,err=q.ReadB(ctx,1);if err!=nil || other.Name!="B" || !other.Active {t.Fatalf("other namespace changed: %v %v",other,err)}
}
`
			compileTypedDMLPackage(t, dir, "./"+strings.ReplaceAll(runtime, "/", "_"), source, false)
		})
	}
	t.Run("jooq", func(t *testing.T) { tablePathPrefixJooq(t, schemas, a, b) })
}

const tablePathPrefixQueries = `-- name: ReadA :one
PRAGMA TablePathPrefix("$PREFIX_A");
DECLARE $id AS Uint64;
SELECT users.* FROM users WHERE users.id = $id;

-- name: ReadB :one
PRAGMA TablePathPrefix("$PREFIX_B");
DECLARE $id AS Uint64;
SELECT users.* FROM users WHERE id = $id;

-- name: ReadAbsoluteB :one
PRAGMA TablePathPrefix("$PREFIX_A");
DECLARE $id AS Uint64;
SELECT u.* FROM ` + "`$PREFIX_B/users`" + ` AS u WHERE u.id = $id;

-- name: ReadRelativeA :one
PRAGMA TablePathPrefix("$ROOT");
DECLARE $id AS Uint64;
SELECT u.* FROM ` + "`a/users`" + ` AS u WHERE u.id = $id;

-- name: FindA :many
PRAGMA TablePathPrefix("$PREFIX_A");
DECLARE $name AS Utf8;
SELECT u.* FROM users VIEW by_name AS u WHERE u.name = $name ORDER BY u.id;

-- name: InsertA :exec
PRAGMA TablePathPrefix("$PREFIX_A");
DECLARE $id AS Uint64;
DECLARE $name AS Utf8;
INSERT INTO users (id,name) VALUES ($id,$name);

-- name: UpdateA :exec
PRAGMA TablePathPrefix("$PREFIX_A");
DECLARE $id AS Uint64;
DECLARE $name AS Utf8;
UPDATE users SET name = $name WHERE users.id = $id;

-- name: CopyFromB :exec
PRAGMA TablePathPrefix("$PREFIX_A");
DECLARE $name AS Utf8;
UPSERT INTO users SELECT id+10ul AS id, name FROM ` + "`$PREFIX_B/users`" + ` WHERE name = $name;

-- name: DeleteA :exec
PRAGMA TablePathPrefix("$PREFIX_A");
DECLARE $id AS Uint64;
DELETE FROM users WHERE users.id = $id;
`

const tablePathPrefixFixturePython = databasePythonConnection + `import sys
mode, root = sys.argv[1:3]
with ydb.Driver(config) as driver:
    driver.wait(20)
    with ydb.QuerySessionPool(driver) as pool:
        if mode == "create":
            for directory in [root, root+"/a", root+"/b"]:
                driver.scheme_client.make_directory(directory)
            for schema in sys.argv[3:]:
                pool.execute_with_retries(schema)
            pool.execute_with_retries(sys.argv[3].replace("CREATE TABLE users", "CREATE TABLE mapped_users"))
            pool.execute_with_retries('UPSERT INTO '+chr(96)+root+'/a/users'+chr(96)+' (id,name,note) VALUES (1ul,"A"u,"original"u);')
            pool.execute_with_retries('UPSERT INTO '+chr(96)+root+'/b/users'+chr(96)+' (active,id,name) VALUES (true,1ul,"B"u);')
        else:
            for table in [root+"/a/mapped_users", root+"/a/users", root+"/b/users"]:
                pool.execute_with_retries("DROP TABLE IF EXISTS "+chr(96)+table+chr(96)+";")
            for directory in [root+"/b", root+"/a", root]:
                driver.scheme_client.remove_directory(directory)
`
