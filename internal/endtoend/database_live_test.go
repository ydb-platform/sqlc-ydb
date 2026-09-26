package endtoend

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/cli"
	"github.com/ydb-platform/sqlc-ydb/internal/config"
	"github.com/ydb-platform/sqlc-ydb/internal/database"
)

// This test deliberately runs sequentially: all runtimes share one disposable table.
func TestLiveYDBDatabaseAnalysis(t *testing.T) {
	if os.Getenv("YDB_CONNECTION_STRING") == "" {
		t.Skip("set YDB_CONNECTION_STRING for live database-assisted analysis")
	}
	dir := t.TempDir()
	table := fmt.Sprintf("sqlc_database_analysis_%d", time.Now().UnixNano())
	t.Cleanup(func() { runDatabasePython(t, dir, databaseFixturePython, "drop", table) })
	runDatabasePython(t, dir, databaseFixturePython, "create", table)
	before := runDatabasePython(t, dir, databaseFixturePython, "snapshot", table)
	// Discovery follows the descriptor order, which need not equal raw SELECT * order.
	settings, err := (config.Database{URI: os.Getenv("YDB_CONNECTION_STRING"), Timeout: "30s"}).Resolve(dir)
	require.NoError(t, err)
	metadataClient, err := database.New(settings)
	require.NoError(t, err)
	described, describeErr := metadataClient.DescribeTable(context.Background(), table)
	require.NoError(t, metadataClient.Close())
	require.NoError(t, describeErr)
	var columnNames, goFields []string
	fieldNames := map[string]string{"amount": "Amount", "id": "ID", "ztext": "Ztext"}
	for _, column := range described.Columns {
		columnNames = append(columnNames, column.Name)
		name, ok := fieldNames[column.Name]
		require.True(t, ok, "unexpected described column %q", column.Name)
		goFields = append(goFields, name)
	}

	write := func(name, contents string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(contents), 0600))
	}
	write("queries.sql", strings.ReplaceAll(`-- name: ReadRecord :one
DECLARE $id AS Uint64;
SELECT * FROM records WHERE id = $id;

-- name: ReadEmbeddedRecord :one
DECLARE $id AS Uint64;
SELECT sqlc.embed(r) FROM records AS r WHERE r.id = $id;

-- name: InsertRecord :exec
DECLARE $id AS Uint64;
DECLARE $ztext AS Optional<Utf8>;
DECLARE $amount AS Optional<Int32>;
INSERT INTO records (id, ztext, amount) VALUES ($id, $ztext, $amount);

-- name: UpdateRecord :exec
DECLARE $id AS Uint64;
DECLARE $ztext AS Optional<Utf8>;
UPDATE records SET ztext = $ztext WHERE id = $id;
`, "records", table))
	const set = `- engine: ydb
  queries: queries.sql
  database:
    uri: ${YDB_CONNECTION_STRING}
  gen:
`
	cfg := "version: '2'\nsql:\n" + set + `    go:
      package: db
      out: stdlib
      sql_package: database/sql
    python:
      out: pydb
      runtime: ydb
` + set + `    go:
      package: db
      out: native
      sql_package: ydb
`
	write("sqlc.yaml", cfg)
	invoke := func(command string, flags ...string) (int, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		args := append([]string{command, "--no-remote", "-f", filepath.Join(dir, "sqlc.yaml")}, flags...)
		code := cli.Run(args, &stdout, &stderr)
		return code, stderr.String()
	}
	// The last query set fails after earlier sets are ready to generate. No
	// outputs may be written until server validation succeeds for every set.
	rejectedConfig := cfg + strings.Replace(set, "queries.sql", "rejected.sql", 1) + `    go:
      package: db
      out: rejected
`
	write("sqlc.yaml", rejectedConfig)
	for _, tc := range []struct{ name, sql string }{
		{"undeclared_parameter", "SELECT * FROM " + table + " WHERE id = $id;"},
		{"server_before_local_semantics", "SELECT id + 1ul AS incremented FROM " + table + " WHERE id = $id;"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			write("rejected.sql", "-- name: MissingDeclaration :one\n"+tc.sql)
			code, stderr := invoke("generate")
			require.False(t, code == 0 || !strings.Contains(stderr, "YDB ") || !strings.Contains(stderr, "Unknown name: $id") || !strings.Contains(stderr, "DECLARE"), "undeclared parameter must return a YDB error with a DECLARE suggestion: %d %s", code, stderr)
			require.False(t, strings.Contains(stderr, "computed result expression") || strings.Contains(stderr, "unsupported result expression"), "local expression rejection preceded server validation: %s", stderr)
			for _, output := range []string{"stdlib", "native", "pydb", "rejected"} {
				_, err := os.Stat(filepath.Join(dir, output))
				require.ErrorIs(t, err, os.ErrNotExist, "failed generation left output %s", output)
			}
		})
	}
	write("sqlc.yaml", cfg)
	for _, command := range []string{"compile", "generate", "diff"} {
		code, stderr := invoke(command)
		require.Zero(t, code, "%s: %s", command, stderr)
		after := runDatabasePython(t, dir, databaseFixturePython, "snapshot", table)
		require.Equal(t, before, after, "%s changed database rows: before=%s after=%s", command, before, after)
	}
	code, stderr := invoke("compile", "--no-database")
	require.NotZero(t, code, stderr)
	require.Contains(t, stderr, "schema is required")
	write("schema.sql", "CREATE TABLE "+table+" (ztext Utf8, id Uint64 NOT NULL, amount Int64, PRIMARY KEY(id));")
	write("sqlc.yaml", strings.ReplaceAll(cfg, "  queries: queries.sql\n", "  schema: schema.sql\n  queries: queries.sql\n"))
	code, stderr = invoke("compile")
	require.NotZero(t, code, stderr)
	require.Contains(t, stderr, "amount")
	write("sqlc.yaml", cfg)

	write("go.mod", "module generated\n\ngo 1.26.0\n\nrequire github.com/ydb-platform/ydb-go-sdk/v3 v3.151.1\n")
	for _, runtime := range []struct {
		name string
		id   uint64
	}{
		{"stdlib", 100}, {"native", 101},
	} {
		t.Run(runtime.name, func(t *testing.T) {
			compileTypedDMLPackage(t, dir, "./"+runtime.name, databaseGeneratedGo(runtime.name == "stdlib", runtime.id, goFields), false)
		})
	}
	runDatabasePython(t, dir, databaseGeneratedPython, columnNames...)
}

func runDatabasePython(t *testing.T, dir, script string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", append([]string{"-c", script}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "Python database acceptance: %v\n%s", err, out)
	return strings.TrimSpace(string(out))
}

const databasePythonConnection = `import os, urllib.parse, ydb
u = urllib.parse.urlsplit(os.environ["YDB_CONNECTION_STRING"])
config = ydb.DriverConfig(u.scheme + "://" + u.netloc, u.path, credentials=ydb.AnonymousCredentials(), disable_discovery=True)
`

const databaseFixturePython = databasePythonConnection + `import json, sys
mode, table = sys.argv[1:]
with ydb.Driver(config) as driver:
    driver.wait(20)
    with ydb.QuerySessionPool(driver) as pool:
        if mode == "create":
            pool.execute_with_retries("CREATE TABLE " + table + " (ztext Utf8, id Uint64 NOT NULL, amount Int32, PRIMARY KEY(id));")
            pool.execute_with_retries("UPSERT INTO " + table + " (id, ztext, amount) VALUES (18446744073709551615ul, 'original'u, -7), (42ul, NULL, NULL);")
        elif mode == "drop":
            pool.execute_with_retries("DROP TABLE IF EXISTS " + table + ";")
        else:
            result = pool.execute_with_retries("SELECT * FROM " + table + " ORDER BY id;")[0]
            names = [column.name for column in result.columns]
            assert names == ["amount", "id", "ztext"], names
            rows = [{name: row[name] for name in names} for row in result.rows]
            assert rows == [{"amount": None, "id": 42, "ztext": None}, {"amount": -7, "id": 18446744073709551615, "ztext": "original"}], rows
            print(json.dumps(rows, sort_keys=True))
`

func databaseGeneratedGo(databaseSQL bool, insertedID uint64, fieldNames []string) string {
	extraImport, setup := "", "q := New(driver.Query())"
	if databaseSQL {
		extraImport = `"database/sql"`
		setup = `db := sql.OpenDB(ydb.MustConnector(driver))
	defer db.Close()
	q := New(db)`
	}
	return `package db
import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"
	` + extraImport + `
	ydb "github.com/ydb-platform/ydb-go-sdk/v3"
)
func TestDatabaseGeneratedRuntime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	driver, err := ydb.Open(ctx, os.Getenv("YDB_CONNECTION_STRING"), ydb.WithAnonymousCredentials())
	if err != nil { t.Fatal(err) }
	defer driver.Close(ctx)
	` + setup + `
	const highID = ^uint64(0)
	row, err := q.ReadRecord(ctx, highID)
	if err != nil { t.Fatal(err) }
	if row.ID != highID || row.Amount == nil || *row.Amount != -7 || row.Ztext == nil || *row.Ztext != "original" {
		t.Fatalf("wildcard values: %+v", row)
	}
	typ := reflect.TypeOf(row)
	names := []string{typ.Field(0).Name, typ.Field(1).Name, typ.Field(2).Name}
	if !reflect.DeepEqual(names, ` + fmt.Sprintf("%#v", fieldNames) + `) { t.Fatalf("wildcard order: %v", names) }
	row, err = q.ReadRecord(ctx, 42)
	if err != nil || row.ID != 42 || row.Amount != nil || row.Ztext != nil { t.Fatalf("nullable row: %+v %v", row, err) }
	const id uint64 = ` + strconv.FormatUint(insertedID, 10) + `
	amount, text := int32(9), "inserted"
	if err := q.InsertRecord(ctx, InsertRecordParams{ID: id, Ztext: &text, Amount: &amount}); err != nil { t.Fatal(err) }
	row, err = q.ReadRecord(ctx, id)
	if err != nil || row.ID != id || row.Amount == nil || *row.Amount != 9 || row.Ztext == nil || *row.Ztext != text { t.Fatalf("inserted row: %+v %v", row, err) }
	if err := q.UpdateRecord(ctx, UpdateRecordParams{ID: id, Ztext: nil}); err != nil { t.Fatal(err) }
	row, err = q.ReadRecord(ctx, id)
	if err != nil || row.Ztext != nil || row.Amount == nil || *row.Amount != 9 { t.Fatalf("updated row: %+v %v", row, err) }
}
`
}

const databaseGeneratedPython = databasePythonConnection + `import dataclasses, sys
from pydb.queries import Querier
with ydb.Driver(config) as driver:
    driver.wait(20)
    with ydb.QuerySessionPool(driver) as pool:
        q = Querier(pool)
        row = q.read_record(18446744073709551615)
        assert row.id == 18446744073709551615 and row.amount == -7 and row.ztext == "original", row
        assert [field.name for field in dataclasses.fields(row)] == sys.argv[1:]
        row = q.read_record(42)
        assert row.id == 42 and row.amount is None and row.ztext is None, row
        q.insert_record(id=102, ztext="inserted", amount=9)
        row = q.read_record(102)
        assert row.id == 102 and row.amount == 9 and row.ztext == "inserted", row
        q.update_record(id=102, ztext=None)
        row = q.read_record(102)
        assert row.ztext is None and row.amount == 9, row
`
