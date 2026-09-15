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

	"github.com/ydb-platform/sqlc-ydb/internal/cli"
)

func TestTypedDMLGeneratedGoCompiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.CopyFS(filepath.Join(dir, "db"), os.DirFS("testdata/typed_dml/expected/db")); err != nil {
		t.Fatal(err)
	}
	mod := "module generated\n\ngo 1.26.0\n\nrequire github.com/ydb-platform/ydb-go-sdk/v3 v3.151.1\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0600); err != nil {
		t.Fatal(err)
	}
	schema := string(mustRead(t, "testdata/typed_dml/schema.sql"))
	compileTypedDMLPackage(t, dir, "./db", typedDMLRuntimeSource("grpc://localhost:2136/local", "records", schema,
		`
	driver, err := ydb.Open(ctx, dsn, ydb.WithAnonymousCredentials())
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close(ctx)
	db := sql.OpenDB(ydb.MustConnector(driver))
	defer db.Close()
	if _, err = db.ExecContext(ctx, schema); err != nil {
		t.Fatal(err)
	}
	q := New(db)
`,
		`
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, dropErr := db.ExecContext(cleanup, "DROP TABLE "+table); dropErr != nil {
			t.Errorf("cleanup %s: %v", table, dropErr)
		}
	}()
`, true), true)
	compileTypedDMLPackage(t, dir, "./db/native", typedDMLRuntimeSource("grpc://localhost:2136/local", "records", schema,
		`
	driver, err := ydb.Open(ctx, dsn, ydb.WithAnonymousCredentials())
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close(ctx)
	client := driver.Query()
	if err = client.Exec(ctx, schema); err != nil {
		t.Fatal(err)
	}
	q := New(client)
`,
		`
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if dropErr := client.Exec(cleanup, "DROP TABLE "+table); dropErr != nil {
			t.Errorf("cleanup %s: %v", table, dropErr)
		}
	}()
`, false), true)
}

// TestLiveYDBTypedDML compiles the CLI-generated fixture against the pinned Go
// SDK and runs each adapter sequentially against its own disposable table.
func TestLiveYDBTypedDML(t *testing.T) {
	dsn := os.Getenv("YDB_CONNECTION_STRING")
	if dsn == "" {
		t.Skip("set YDB_CONNECTION_STRING for live typed DML validation")
	}
	dir := t.TempDir()
	copyFixture(t, "testdata/typed_dml", dir)
	table := fmt.Sprintf("sqlc_typed_dml_%d", time.Now().UnixNano())
	for _, name := range []string{"schema.sql", "queries.sql"} {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		data = []byte(strings.ReplaceAll(string(data), "records", table))
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"generate", "-f", filepath.Join(dir, "sqlc.yaml")}, &stdout, &stderr); code != 0 {
		t.Fatalf("generate typed DML fixture: %s", stderr.String())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("generate wrote output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	mod := "module generated\n\ngo 1.26.0\n\nrequire github.com/ydb-platform/ydb-go-sdk/v3 v3.151.1\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0600); err != nil {
		t.Fatal(err)
	}
	for _, runtime := range []struct {
		name, path, setup, cleanup string
	}{
		{
			name: "database/sql", path: "./db",
			setup: `
	driver, err := ydb.Open(ctx, dsn, ydb.WithAnonymousCredentials())
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close(ctx)
	db := sql.OpenDB(ydb.MustConnector(driver))
	defer db.Close()
	if _, err = db.ExecContext(ctx, schema); err != nil {
		t.Fatal(err)
	}
	q := New(db)
`,
			cleanup: `
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, dropErr := db.ExecContext(cleanup, "DROP TABLE "+table); dropErr != nil {
			t.Errorf("cleanup %s: %v", table, dropErr)
		}
	}()
`,
		},
		{
			name: "ydb", path: "./db/native",
			setup: `
	driver, err := ydb.Open(ctx, dsn, ydb.WithAnonymousCredentials())
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close(ctx)
	client := driver.Query()
	if err = client.Exec(ctx, schema); err != nil {
		t.Fatal(err)
	}
	q := New(client)
`,
			cleanup: `
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if dropErr := client.Exec(cleanup, "DROP TABLE "+table); dropErr != nil {
			t.Errorf("cleanup %s: %v", table, dropErr)
		}
	}()
`,
		},
	} {
		t.Run(runtime.name, func(t *testing.T) {
			source := typedDMLRuntimeSource(dsn, table, string(mustRead(t, filepath.Join(dir, "schema.sql"))), runtime.setup, runtime.cleanup, runtime.name == "database/sql")
			compileTypedDMLPackage(t, dir, runtime.path, source, false)
		})
	}
}

func compileTypedDMLPackage(t *testing.T, root, packagePath, source string, compileOnly bool) {
	t.Helper()
	pkgDir := filepath.Join(root, strings.TrimPrefix(packagePath, "./"))
	if err := os.WriteFile(filepath.Join(pkgDir, "typed_dml_live_test.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"test", "-mod=mod", "-count=1"}
	if compileOnly {
		args = append(args, "-run", "^$")
	}
	args = append(args, ".")
	commandCtx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, "go", args...)
	cmd.Dir = pkgDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated package %s: %v\n%s", packagePath, err, out)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func typedDMLRuntimeSource(dsn, table, schema, setup, cleanup string, databaseSQL bool) string {
	sqlImport := ""
	if databaseSQL {
		sqlImport = "\"database/sql\"\n"
	}
	return `package records
import (
	"bytes"
	"context"
	` + sqlImport + `"testing"
	"time"
	ydb "github.com/ydb-platform/ydb-go-sdk/v3"
)

func TestTypedDML(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	dsn := ` + strconv.Quote(dsn) + `
	table := ` + strconv.Quote(table) + `
	schema := ` + strconv.Quote(schema) + `
` + setup + cleanup + `
	owner := uint64(^uint64(0))
	created := time.Date(2026, 9, 15, 1, 2, 3, 456000000, time.UTC)
	updated := created.Add(7 * time.Minute)
	if err := q.InsertRecords(ctx, InsertRecordsParams{OwnerID: owner, Rows: nil, CreatedAt: created}); err != nil {
		t.Fatal(err)
	}
	if rows, err := q.ListRecords(ctx, owner); err != nil || len(rows) != 0 {
		t.Fatalf("nil insert rows=%#v err=%v", rows, err)
	}

	inserted := []InsertRecordsRowsItem{
		{RecordID: "r1", GroupID: "g1", Payload: []byte{0, 1, 255}, Attributes: "{\"kind\":\"one\"}"},
		{RecordID: "r2", GroupID: "g2", Payload: []byte("two"), Attributes: "{\"kind\":\"two\"}"},
		{RecordID: "r3", GroupID: "g2", Payload: []byte("three"), Attributes: "{\"kind\":\"three\"}"},
	}
	if err := q.InsertRecords(ctx, InsertRecordsParams{OwnerID: owner, Rows: inserted, CreatedAt: created}); err != nil {
		t.Fatal(err)
	}
	rows, err := q.ListRecords(ctx, owner)
	if err != nil || len(rows) != 3 {
		t.Fatalf("insert rows=%#v err=%v", rows, err)
	}
	if rows[0].OwnerID != owner ||
		!bytes.Equal(rows[0].Payload, inserted[0].Payload) ||
		rows[0].Attributes != inserted[0].Attributes ||
		!rows[0].CreatedAt.Equal(created) || !rows[0].UpdatedAt.Equal(created) {
		t.Fatalf("preservation=%#v", rows[0])
	}

	ownerHash := rows[0].OwnerHash
	if err := q.UpdateRecords(ctx, UpdateRecordsParams{OwnerID: owner, Rows: []UpdateRecordsRowsItem{}, UpdatedAt: updated}); err != nil {
		t.Fatal(err)
	}
	changes := []UpdateRecordsRowsItem{
		{RecordID: "r1", GroupID: "g2", Payload: []byte("changed"), Attributes: "{\"changed\":true}"},
		{RecordID: "missing", GroupID: "g2", Payload: []byte("missing"), Attributes: "{}"},
	}
	if err := q.UpdateRecords(ctx, UpdateRecordsParams{OwnerID: owner, Rows: changes, UpdatedAt: updated}); err != nil {
		t.Fatal(err)
	}
	rows, err = q.ListRecords(ctx, owner)
	if err != nil || len(rows) != 3 {
		t.Fatalf("update inserted missing row: rows=%#v err=%v", rows, err)
	}
	if rows[0].OwnerID != owner || rows[0].GroupID != "g2" ||
		!bytes.Equal(rows[0].Payload, changes[0].Payload) ||
		rows[0].Attributes != changes[0].Attributes ||
		!rows[0].CreatedAt.Equal(created) || !rows[0].UpdatedAt.Equal(updated) {
		t.Fatalf("update preservation=%#v", rows[0])
	}

	for _, ids := range [][]string{nil, {}} {
		filtered, err := q.FilterRecords(ctx, FilterRecordsParams{OwnerID: owner, RecordIds: ids, GroupID: "g2"})
		if err != nil || len(filtered) != 0 {
			t.Fatalf("empty filter ids=%#v rows=%#v err=%v", ids, filtered, err)
		}
	}
	filtered, err := q.FilterRecords(ctx, FilterRecordsParams{OwnerID: owner, RecordIds: []string{"r1", "r2", "not-present"}, GroupID: "g2"})
	if err != nil || len(filtered) != 2 || filtered[0].RecordID != "r1" || filtered[1].RecordID != "r2" {
		t.Fatalf("filtered=%#v err=%v", filtered, err)
	}

	got, err := q.GetRecord(ctx, GetRecordKey{OwnerHash: ownerHash, RecordID: "r1"})
	if err != nil || got.OwnerID != owner || !bytes.Equal(got.Payload, changes[0].Payload) {
		t.Fatalf("get=%#v err=%v", got, err)
	}

	for _, ids := range [][]string{nil, {}} {
		if err := q.DeleteRecords(ctx, DeleteRecordsParams{OwnerID: owner, RecordIds: ids, GroupID: "g2"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := q.DeleteRecords(ctx, DeleteRecordsParams{OwnerID: owner, RecordIds: []string{"r1", "r3", "not-present"}, GroupID: "g2"}); err != nil {
		t.Fatal(err)
	}
	rows, err = q.ListRecords(ctx, owner)
	if err != nil || len(rows) != 1 || rows[0].RecordID != "r2" {
		t.Fatalf("delete predicate rows=%#v err=%v", rows, err)
	}
}
`
}
