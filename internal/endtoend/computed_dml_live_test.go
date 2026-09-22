package endtoend

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ydb-platform/sqlc-ydb/internal/cli"
)

func TestComputedDMLGeneratedGoCompiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.CopyFS(filepath.Join(dir, "db"), os.DirFS("testdata/computed_dml/expected/db")); err != nil {
		t.Fatal(err)
	}
	writeComputedDMLModule(t, dir)
	for _, native := range []bool{false, true} {
		path := "./db"
		if native {
			path += "/native"
		}
		compileTypedDMLPackage(t, dir, path, computedDMLGoRuntime(native), true)
	}
}

// All adapters run sequentially against one disposable table. Generated calls
// participate in SDK-owned retrying transactions, including rollback and contention.
func TestLiveYDBComputedDML(t *testing.T) {
	if os.Getenv("YDB_CONNECTION_STRING") == "" {
		t.Skip("set YDB_CONNECTION_STRING for live computed DML validation")
	}
	dir := t.TempDir()
	copyFixture(t, "testdata/computed_dml", dir)
	table := fmt.Sprintf("sqlc_computed_dml_%d", time.Now().UnixNano())
	for _, name := range []string{"schema.sql", "queries.sql"} {
		contents := strings.ReplaceAll(string(mustRead(t, filepath.Join(dir, name))), "counters", table)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	runDatabasePython(t, dir, databasePythonConnection+`from pathlib import Path
with ydb.Driver(config) as driver:
    driver.wait(20)
    with ydb.QuerySessionPool(driver) as pool:
        pool.execute_with_retries(Path("schema.sql").read_text())
`)
	t.Cleanup(func() { runDatabasePython(t, dir, databaseFixturePython, "drop", table) })
	invoke := func(command string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := cli.Run([]string{command, "--no-remote", "-f", filepath.Join(dir, "sqlc.yaml")}, &stdout, &stderr); code != 0 {
			t.Fatalf("%s: %s", command, stderr.String())
		}
	}
	invoke("generate")
	// Connected validation must produce the same generated API and SQL as offline
	// analysis; EXPLAIN must not execute the INSERT used by each runtime below.
	cfgPath := filepath.Join(dir, "sqlc.yaml")
	cfg := strings.ReplaceAll(string(mustRead(t, cfgPath)), "    schema: schema.sql", "    schema: schema.sql\n    database:\n      uri: ${YDB_CONNECTION_STRING}")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	invoke("diff")
	writeComputedDMLModule(t, dir)
	for _, native := range []bool{false, true} {
		name, path := "database_sql", "./db"
		if native {
			name, path = "native", "./db/native"
		}
		t.Run(name, func(t *testing.T) { compileTypedDMLPackage(t, dir, path, computedDMLGoRuntime(native), false) })
	}
	t.Run("python", func(t *testing.T) { runDatabasePython(t, dir, computedDMLPythonRuntime) })
}

func writeComputedDMLModule(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module generated\n\ngo 1.26.0\n\nrequire github.com/ydb-platform/ydb-go-sdk/v3 v3.151.1\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func computedDMLGoRuntime(native bool) string {
	imports := `"database/sql"
 "github.com/ydb-platform/ydb-go-sdk/v3/retry"`
	setup := `db := sql.OpenDB(ydb.MustConnector(driver))
 defer db.Close()
 q := New(db)
 const id = "database_sql"
 withTx := func(fn func(context.Context, *Queries) error) error {
  return retry.DoTx(ctx, db, func(ctx context.Context, tx *sql.Tx) error { return fn(ctx, New(tx)) })
 }`
	if native {
		imports = `"github.com/ydb-platform/ydb-go-sdk/v3/query"`
		setup = `q := New(driver.Query())
 const id = "native"
 withTx := func(fn func(context.Context, *Queries) error) error {
  return driver.Query().DoTx(ctx, func(ctx context.Context, tx query.TxActor) error { return fn(ctx, New(tx)) })
 }`
	}
	return `package counters
import (
 "context"
 "errors"
 "fmt"
 "os"
 "testing"
 "time"
 ` + imports + `
 ydb "github.com/ydb-platform/ydb-go-sdk/v3"
)
func TestComputedDMLRuntime(t *testing.T) {
 ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
 defer cancel()
 driver, err := ydb.Open(ctx, os.Getenv("YDB_CONNECTION_STRING"), ydb.WithAnonymousCredentials())
 if err != nil { t.Fatal(err) }; defer driver.Close(ctx)
 ` + setup + `
 created, err := q.CreateCounter(ctx, id)
 if err != nil || created.Value != 0 || created.OptionalValue != nil || created.Label == nil || *created.Label != "pending" || !created.Enabled { t.Fatalf("create: %+v %v",created,err) }
 incremented, err := q.IncrementCounter(ctx, IncrementCounterParams{ID:id, Delta:5})
 if err != nil || incremented.Value != 5 { t.Fatalf("increment: %+v %v",incremented,err) }
 transformed, err := q.TransformCounter(ctx,id)
 if err != nil || transformed.Value != 17 || transformed.OptionalValue == nil || *transformed.OptionalValue != 1 || transformed.Label == nil || *transformed.Label != "done" || transformed.Enabled { t.Fatalf("transform: %+v %v",transformed,err) }
 cleared, err := q.ClearOptional(ctx,id)
 if err != nil || cleared.Value != 17 || cleared.OptionalValue != nil || cleared.Label != nil { t.Fatalf("clear: %+v %v",cleared,err) }
 if err := q.WidenCounterFromSelect(ctx,id); err != nil { t.Fatal(err) }
 widened, err := q.ReadCounter(ctx,id)
 if err != nil || widened.Value != 7 || widened.OptionalValue != nil || widened.Label == nil || *widened.Label != "wide" || !widened.Enabled { t.Fatalf("widen SELECT: %+v %v",widened,err) }
 if err := q.UpsertCounter(ctx, UpsertCounterParams{ID:id,Seed:2}); err != nil { t.Fatal(err) }
 reset, err := q.ReadCounter(ctx,id)
 if err != nil || reset.Value != 12 || reset.OptionalValue == nil || *reset.OptionalValue != 5 || reset.Label == nil || *reset.Label != "reset" || !reset.Enabled { t.Fatalf("upsert: %+v %v",reset,err) }
 aborted := errors.New("rollback computed assignment")
 err = withTx(func(ctx context.Context, tx *Queries) error {
  row, err := tx.IncrementCounter(ctx,IncrementCounterParams{ID:id,Delta:100})
  if err != nil { return err }; if row.Value != 112 { return fmt.Errorf("transaction value: %d",row.Value) }
  read, err := tx.ReadCounter(ctx,id)
  if err != nil { return err }; if read.Value != 112 { return fmt.Errorf("read-your-writes: %d",read.Value) }
  return aborted
 })
 if !errors.Is(err,aborted) { t.Fatalf("rollback: %v",err) }
 row, err := q.ReadCounter(ctx,id)
 if err != nil || row.Value != 12 { t.Fatalf("after rollback: %+v %v",row,err) }
 const workers = 16
 type outcome struct {value int64; err error}
 results := make(chan outcome,workers)
 for range workers {
  go func() {
   var value int64
   err := withTx(func(ctx context.Context, tx *Queries) error {
    row, err := tx.IncrementCounter(ctx,IncrementCounterParams{ID:id,Delta:1})
    value = row.Value
    return err
   })
   results <- outcome{value,err}
  }()
 }
 seen := make(map[int64]bool)
 for range workers {
  got := <-results
  if got.err != nil { t.Errorf("concurrent increment: %v",got.err); continue }
  if got.value < 13 || got.value > 12+workers || seen[got.value] { t.Errorf("invalid RETURNING value: %d",got.value) }
  seen[got.value] = true
 }
 row, err = q.ReadCounter(ctx,id)
 if err != nil || row.Value != 12+workers { t.Fatalf("lost increment: %+v %v",row,err) }
}
`
}

const computedDMLPythonRuntime = databasePythonConnection + `from concurrent.futures import ThreadPoolExecutor
from py.queries import Querier
with ydb.Driver(config) as driver:
    driver.wait(20)
    with ydb.QuerySessionPool(driver) as pool:
        q = Querier(pool)
        row = q.create_counter("python")
        assert (row.value, row.optional_value, row.label, row.enabled) == (0, None, "pending", True), row
        assert q.increment_counter("python", 5).value == 5
        row = q.transform_counter("python")
        assert (row.value, row.optional_value, row.label, row.enabled) == (17, 1, "done", False), row
        row = q.clear_optional("python")
        assert (row.value, row.optional_value, row.label) == (17, None, None), row
        q.widen_counter_from_select("python")
        row = q.read_counter("python")
        assert (row.value, row.optional_value, row.label, row.enabled) == (7, None, "wide", True), row
        q.upsert_counter("python", 2)
        row = q.read_counter("python")
        assert (row.value, row.optional_value, row.label, row.enabled) == (12, 5, "reset", True), row
        def rollback(tx):
            tq = Querier(tx)
            assert tq.increment_counter("python", 100).value == 112
            assert tq.read_counter("python").value == 112
            tx.rollback()
        pool.retry_tx_sync(rollback)
        assert q.read_counter("python").value == 12
        def increment(_):
            # The default retry budget can be exhausted by sixteen hot-key updates.
            return pool.retry_tx_sync(
                lambda tx: Querier(tx).increment_counter("python", 1).value,
                retry_settings=ydb.RetrySettings(max_retries=64),
            )
        with ThreadPoolExecutor(max_workers=8) as executor:
            values = list(executor.map(increment, range(16)))
        assert sorted(values) == list(range(13,29)), values
        assert q.read_counter("python").value == 28
`
