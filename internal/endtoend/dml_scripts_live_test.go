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

	"github.com/ydb-platform/sqlc-ydb/internal/cli"
)

func TestDMLScriptsGeneratedGoCompiles(t *testing.T) {
	runDMLScripts(t, "grpc://localhost:2136/local", true)
}

func TestLiveYDBDMLScripts(t *testing.T) {
	dsn := os.Getenv("YDB_CONNECTION_STRING")
	if dsn == "" {
		t.Skip("set YDB_CONNECTION_STRING for DML script validation")
	}
	runDMLScripts(t, dsn, false)
	t.Run("jooq", dmlScriptsJooq)
	t.Run("csharp", dmlScriptsCsharp)
}

func runDMLScripts(t *testing.T, dsn string, compileOnly bool) {
	t.Helper()
	dir := t.TempDir()
	table := fmt.Sprintf("sqlc_dml_scripts_%d", time.Now().UnixNano())
	replace := strings.NewReplacer("records", table, "copies", table+"_copies")
	schema := replace.Replace(dmlScriptsSchema)
	configuration := "version: '2'\nsql:\n"
	for _, runtime := range []string{"ydb", "database/sql"} {
		configuration += "- engine: ydb\n  schema: schema.sql\n  queries: queries.sql\n  gen:\n    go:\n      package: records\n      out: " + strings.ReplaceAll(runtime, "/", "_") + "\n      sql_package: " + runtime + "\n"
	}
	for name, contents := range map[string]string{"schema.sql": schema, "queries.sql": replace.Replace(dmlScriptsQueries + dmlScriptsResultQueries), "sqlc.yaml": configuration, "go.mod": "module generated\n\ngo 1.26.0\n\nrequire github.com/ydb-platform/ydb-go-sdk/v3 v3.151.1\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"generate", "-f", filepath.Join(dir, "sqlc.yaml")}, &stdout, &stderr); code != 0 {
		t.Fatalf("generate DML scripts: %s", stderr.String())
	}
	for _, runtime := range []string{"ydb", "database/sql"} {
		t.Run(runtime, func(t *testing.T) {
			source := strings.NewReplacer("$DSN", strconv.Quote(dsn), "$SCHEMA", strconv.Quote(schema)).Replace(dmlScriptsGoRuntime(runtime == "ydb"))
			compileTypedDMLPackage(t, dir, "./"+strings.ReplaceAll(runtime, "/", "_"), source, compileOnly)
		})
	}
}

const dmlScriptsSchema = `CREATE TABLE records (id Uint64 NOT NULL, value Int64 NOT NULL, label Utf8 NOT NULL, PRIMARY KEY(id));
CREATE TABLE copies (id Uint64 NOT NULL, value Int64 NOT NULL, label Utf8 NOT NULL, PRIMARY KEY(id));`

const dmlScriptsQueries = `-- name: MutateTogether :exec
DECLARE $id AS Uint64;
DECLARE $value AS Int64;
DECLARE $label AS Utf8;
DECLARE $obsolete_id AS Uint64;
INSERT INTO records (id, value, label) VALUES ($id, $value, $label);
UPSERT INTO copies (id, value, label)
SELECT id, value, label FROM records WHERE id = $id;
UPDATE records SET value = value + 1 WHERE id = $id;
DELETE FROM copies WHERE id = $obsolete_id;

-- name: FailLate :exec
DECLARE $id AS Uint64;
DECLARE $existing_id AS Uint64;
DECLARE $value AS Int64;
DECLARE $label AS Utf8;
INSERT INTO records (id, value, label) VALUES ($id, $value, $label);
INSERT INTO copies (id, value, label) VALUES ($existing_id, $value, $label);

-- name: DeleteTogether :exec
DECLARE $id AS Uint64;
DELETE FROM records WHERE id = $id;
DELETE FROM copies WHERE id = $id;

-- name: SeedCopy :exec
DECLARE $id AS Uint64;
UPSERT INTO copies (id, value, label) VALUES ($id, 0l, 'obsolete'u);

-- name: ListRecords :many
SELECT id, value, label FROM records ORDER BY id;

-- name: ListCopies :many
SELECT id, value, label FROM copies ORDER BY id;
`

func dmlScriptsGoRuntime(native bool) string {
	imports := `"database/sql"
 "github.com/ydb-platform/ydb-go-sdk/v3/retry"`
	noRows := "sql.ErrNoRows"
	counted := `func (c *countedDB) QueryContext(ctx context.Context, statement string, args ...interface{}) (*sql.Rows, error) {
 c.queries++
 return c.DBTX.QueryContext(ctx, statement, args...)
}
func (c *countedDB) QueryRowContext(ctx context.Context, statement string, args ...interface{}) *sql.Row {
 c.queries++
 return c.DBTX.QueryRowContext(ctx, statement, args...)
}
func (c *countedDB) ExecContext(ctx context.Context, statement string, args ...interface{}) (sql.Result, error) {
 c.execs++
 return c.DBTX.ExecContext(ctx, statement, args...)
}`
	setup := `db := sql.OpenDB(ydb.MustConnector(driver))
 defer db.Close()
 counter := &countedDB{DBTX:db}
 q := New(counter)
 withTx := func(fn func(context.Context, *Queries) error) error {
  return retry.DoTx(ctx, db, func(ctx context.Context, tx *sql.Tx) error { return fn(ctx, New(tx)) })
 }`
	if native {
		noRows = "query.ErrNoRows"
		imports = `"github.com/ydb-platform/ydb-go-sdk/v3/query"`
		counted = `func (c *countedDB) Query(ctx context.Context, statement string, opts ...query.ExecuteOption) (query.Result, error) {
 c.queries++
 return c.DBTX.Query(ctx, statement, opts...)
}
func (c *countedDB) QueryRow(ctx context.Context, statement string, opts ...query.ExecuteOption) (query.Row, error) {
 c.queries++
 return c.DBTX.QueryRow(ctx, statement, opts...)
}
func (c *countedDB) Exec(ctx context.Context, statement string, opts ...query.ExecuteOption) error {
 c.execs++
 return c.DBTX.Exec(ctx, statement, opts...)
}`
		setup = `counter := &countedDB{DBTX:driver.Query()}
 q := New(counter)
 withTx := func(fn func(context.Context, *Queries) error) error {
  return driver.Query().DoTx(ctx, func(ctx context.Context, tx query.TxActor) error { return fn(ctx, New(tx)) })
 }`
	}
	return `package records

import (
 "context"
 "errors"
 "fmt"
 "strings"
 "testing"
 "time"
 ` + imports + `
 ydb "github.com/ydb-platform/ydb-go-sdk/v3"
 "github.com/ydb-platform/ydb-go-genproto/protos/Ydb"
)

type countedDB struct { DBTX; execs,queries int }
` + counted + `

func TestDMLScripts(t *testing.T) {
 ctx,cancel := context.WithTimeout(context.Background(),90*time.Second)
 defer cancel()
 driver,err := ydb.Open(ctx,$DSN,ydb.WithAnonymousCredentials())
 if err != nil {t.Fatal(err)}
 defer driver.Close(ctx)
 for _,ddl := range strings.Split($SCHEMA,";") {
  if strings.TrimSpace(ddl)=="" {continue}
  if err := driver.Query().Exec(ctx,ddl); err != nil {t.Fatal(err)}
  created := strings.Fields(ddl)[2]
  defer func() {
   cleanup,done := context.WithTimeout(context.Background(),10*time.Second)
   defer done()
   if err := driver.Query().Exec(cleanup,"DROP TABLE "+created);err != nil {t.Error(err)}
  }()
 }
 ` + setup + `
 if err := q.SeedCopy(ctx,99);err != nil {t.Fatal(err)}
 before := counter.execs
 if err := q.MutateTogether(ctx,MutateTogetherParams{ID:1,Value:40,Label:"shared → Привет",ObsoleteID:99});err != nil {t.Fatal(err)}
 if counter.execs != before+1 {t.Fatalf("script used %d Exec calls, want one",counter.execs-before)}
 if err := checkScriptRows(ctx,q,1,41,40,"shared → Привет");err != nil {t.Fatal(err)}

 aborted := errors.New("caller requests rollback")
 attempts := 0
 err = withTx(func(ctx context.Context,tx *Queries) error {
  attempts++
  if err := tx.DeleteTogether(ctx,1);err != nil {return err}
  if err := checkEmptyScriptTables(ctx,tx);err != nil {return err}
  return aborted
 })
 if !errors.Is(err,aborted) || attempts != 1 {t.Fatalf("caller rollback/retry: attempts=%d error=%v",attempts,err)}
 if err := checkScriptRows(ctx,q,1,41,40,"shared → Привет");err != nil {t.Fatal(err)}

 err = withTx(func(ctx context.Context,tx *Queries) error {
  if err := tx.DeleteTogether(ctx,1);err != nil {return err}
  if err := tx.MutateTogether(ctx,MutateTogetherParams{ID:2,Value:7,Label:"committed",ObsoleteID:99});err != nil {return err}
  return checkScriptRows(ctx,tx,2,8,7,"committed")
 })
 if err != nil {t.Fatal(err)}
 if err := checkScriptRows(ctx,q,2,8,7,"committed");err != nil {t.Fatal(err)}

 before = counter.execs
 err = q.FailLate(ctx,FailLateParams{ID:3,ExistingID:2,Value:100,Label:"must roll back"})
 if !ydb.IsOperationError(err,Ydb.StatusIds_PRECONDITION_FAILED) {t.Fatalf("direct script expected duplicate-key failure in its second statement, got %v",err)}
 if counter.execs != before+1 {t.Fatal("failing script was split into multiple Exec calls")}
 if err := checkScriptRows(ctx,q,2,8,7,"committed");err != nil {t.Fatalf("direct script partially committed: %v",err)}

 err = withTx(func(ctx context.Context,tx *Queries) error {
  return tx.FailLate(ctx,FailLateParams{ID:3,ExistingID:2,Value:100,Label:"must roll back"})
 })
 if !ydb.IsOperationError(err,Ydb.StatusIds_PRECONDITION_FAILED) {t.Fatalf("transaction expected duplicate-key failure in its second statement, got %v",err)}
 if err := checkScriptRows(ctx,q,2,8,7,"committed");err != nil {t.Fatalf("later failure did not roll back: %v",err)}

 cancelled,stop := context.WithCancel(ctx)
 stop()
 err = q.MutateTogether(cancelled,MutateTogetherParams{ID:4,Value:10,Label:"cancelled",ObsoleteID:99})
 if err == nil {t.Fatal("cancelled script succeeded")}
 if err := checkScriptRows(ctx,q,2,8,7,"committed");err != nil {t.Fatal(err)}
 before = counter.execs
 if err := q.DeleteTogether(ctx,500);err != nil {t.Fatal(err)}
 if counter.execs != before+1 {t.Fatal("empty mutation script was split into multiple Exec calls")}
 if err := checkScriptRows(ctx,q,2,8,7,"committed");err != nil {t.Fatal(err)}
 if err := q.DeleteTogether(ctx,2);err != nil {t.Fatal(err)}
 if err := q.MutateTogether(ctx,MutateTogetherParams{ID:4,Value:10,Label:"recovered",ObsoleteID:99});err != nil {t.Fatal(err)}
 if err := checkScriptRows(ctx,q,4,11,10,"recovered");err != nil {t.Fatal(err)}
 checkMixedDMLScripts(t,ctx,q,withTx,counter)
}

func checkScriptRows(ctx context.Context,q *Queries,id uint64,value,copyValue int64,label string) error {
 rows,err := q.ListRecords(ctx)
 if err != nil {return err}
 if len(rows)!=1 || rows[0].ID!=id || rows[0].Value!=value || rows[0].Label!=label {return fmt.Errorf("records: %+v",rows)}
 copies,err := q.ListCopies(ctx)
 if err != nil {return err}
 if len(copies)!=1 || copies[0].ID!=id || copies[0].Value!=copyValue || copies[0].Label!=label {return fmt.Errorf("copies/read-your-writes: %+v",copies)}
 return nil
}

func checkEmptyScriptTables(ctx context.Context,q *Queries) error {
 rows,err := q.ListRecords(ctx)
 if err != nil {return err}
 if len(rows)!=0 {return fmt.Errorf("records not empty in transaction: %+v",rows)}
 copies,err := q.ListCopies(ctx)
 if err != nil {return err}
 if len(copies)!=0 {return fmt.Errorf("copies not empty in transaction: %+v",copies)}
 return nil
}
` + strings.ReplaceAll(dmlScriptsResultGoRuntime, "$NO_ROWS", noRows)
}
