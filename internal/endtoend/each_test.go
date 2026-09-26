package endtoend

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/cli"
)

func TestEachUnsupportedTargets(t *testing.T) {
	for _, profile := range []struct{ target, runtime string }{
		{"python", "ydb"}, {"python", "dbapi"}, {"python", "sqlalchemy"},
		{"cpp", "ydb"}, {"cpp", "userver"}, {"csharp", "adonet"}, {"csharp", "dapper"},
		{"java", "ydb"}, {"java", "jdbc"}, {"java", "jooq"},
		{"kotlin", "ydb"}, {"kotlin", "jdbc"}, {"kotlin", "exposed"},
		{"typescript", "ydb"}, {"rust", "ydb"}, {"php", "ydb"},
	} {
		t.Run(profile.target+"/"+profile.runtime, func(t *testing.T) {
			dir := t.TempDir()
			copyFixture(t, "testdata/each", dir)
			cfg := "version: \"2\"\nsql:\n  - engine: ydb\n    schema: schema.sql\n    queries: queries.sql\n    gen:\n      go:\n        out: db\n      " + profile.target + ":\n        out: other\n        runtime: " + profile.runtime + "\n"
			require.NoError(t, os.WriteFile(filepath.Join(dir, "sqlc.yaml"), []byte(cfg), 0600))
			var stdout, stderr bytes.Buffer
			code := cli.Run([]string{"generate", "--no-remote", "-f", filepath.Join(dir, "sqlc.yaml")}, &stdout, &stderr)
			require.NotZero(t, code, stderr.String())
			require.Contains(t, stderr.String(), ":each")
			_, err := os.Stat(filepath.Join(dir, "db"))
			require.ErrorIs(t, err, os.ErrNotExist, "partial Go output despite unsupported target")
		})
	}
}

func TestEachGeneratedGoCompiles(t *testing.T) { runEachFixture(t, false) }
func TestLiveYDBEach(t *testing.T) {
	if os.Getenv("YDB_CONNECTION_STRING") == "" {
		t.Skip("set YDB_CONNECTION_STRING for streaming callback validation")
	}
	runEachFixture(t, true)
}

func runEachFixture(t *testing.T, live bool) {
	t.Helper()
	dir := t.TempDir()
	copyFixture(t, "testdata/each", dir)
	table := fmt.Sprintf("sqlc_each_%d", time.Now().UnixNano())
	for _, name := range []string{"schema.sql", "queries.sql"} {
		path := filepath.Join(dir, name)
		data := strings.ReplaceAll(string(mustRead(t, path)), "devices", table)
		// The parameter's public name stays stable while the physical table is isolated.
		data = strings.ReplaceAll(data, "$"+table, "$devices")
		require.NoError(t, os.WriteFile(path, []byte(data), 0600))
	}
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"generate", "--no-remote", "-f", filepath.Join(dir, "sqlc.yaml")}, &stdout, &stderr)
	require.Zero(t, code, stderr.String())
	mod := "module generated\n\ngo 1.26.0\n\nrequire github.com/ydb-platform/ydb-go-sdk/v3 v3.151.1\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0600))
	schema := string(mustRead(t, filepath.Join(dir, "schema.sql")))
	for _, native := range []bool{false, true} {
		path := "./db"
		if native {
			path += "/native"
		}
		t.Run(path, func(t *testing.T) { compileTypedDMLPackage(t, dir, path, eachLiveSource(native, schema, table), !live) })
	}
}

func eachLiveSource(native bool, schema, table string) string {
	imports := `"database/sql"`
	setup := `db:=sql.OpenDB(ydb.MustConnector(driver)); defer db.Close()
 db.SetMaxOpenConns(1)
 q:=New(db)
 scoped:=func(fn func(*Queries) error) error {
  tx,err:=db.BeginTx(ctx,nil);if err!=nil{return err};defer tx.Rollback()
  if err:=fn(q.WithTx(tx));err!=nil{return err}
  return tx.Commit()
 }
 run:=func(q *Queries,ctx context.Context,consume func(VisitDevicesRow)error)error{return q.VisitDevices(ctx,VisitDevicesParams{MinID:0,MaxID:2047},consume)}
 streaming:=func(ctx context.Context,consume func(VisitDevicesRow)error)error{return run(q,ctx,consume)}`
	if native {
		imports = `"github.com/ydb-platform/ydb-go-sdk/v3/query"
 "github.com/ydb-platform/ydb-go-sdk/v3/retry/budget"`
		setup = `q:=New(driver.Query())
 scoped:=func(fn func(*Queries)error)error{
  if err:=driver.Query().Do(ctx,func(ctx context.Context,s query.Session)error{return fn(New(s))});err!=nil{return err}
  return driver.Query().DoTx(ctx,func(ctx context.Context,tx query.TxActor)error{return fn(New(tx))})
 }
 run:=func(q *Queries,ctx context.Context,consume func(VisitDevicesRow)error)error{return q.VisitDevices(ctx,VisitDevicesParams{MinID:0,MaxID:2047},consume,query.WithResponsePartLimitSizeBytes(4096))}
 streaming:=func(ctx context.Context,consume func(VisitDevicesRow)error)error{
  // The caller owns this operation and chooses its retry policy. Returning the
  // query error lets the SDK discard a session whose canceled query is pending.
  return driver.Query().Do(ctx,func(ctx context.Context,s query.Session)error{
   return run(New(s),ctx,consume)
  },query.WithRetryBudget(budget.Percent(0)))
 }`
	}
	return `package devices
import (
 "context"
 "errors"
 "fmt"
 "os"
 "runtime"
 "strings"
 "testing"
 "time"
 ` + imports + `
 ydb "github.com/ydb-platform/ydb-go-sdk/v3"
 "github.com/ydb-platform/ydb-go-sdk/v3/retry"
)
func TestEachLive(t *testing.T){
 ctx,cancel:=context.WithTimeout(context.Background(),90*time.Second);defer cancel()
 driver,err:=ydb.Open(ctx,os.Getenv("YDB_CONNECTION_STRING"),ydb.WithAnonymousCredentials())
 if err!=nil{t.Fatal(err)};defer driver.Close(ctx)
 if err:=driver.Query().Exec(ctx,` + fmt.Sprintf("%q", schema) + `);err!=nil{t.Fatal(err)}
 defer func(){cleanup,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel();if err:=driver.Query().Exec(cleanup,` + fmt.Sprintf("%q", "DROP TABLE "+table) + `);err!=nil{t.Error(err)}}()
 ` + setup + `
 calls:=0
 if err:=q.VisitAll(ctx,func(VisitAllRow)error{calls++;return nil});err!=nil||calls!=0{t.Fatalf("empty: calls=%d err=%v",calls,err)}
 name:=strings.Repeat("x",8192)
 for batch:=0;batch<16;batch++{
  items:=make([]PutDevicesDevicesItem,128)
  for i:=range items{items[i]=PutDevicesDevicesItem{ID:uint64(batch*128+i),Name:&name};if items[i].ID==1{items[i].Name=nil}}
  if err:=q.PutDevices(ctx,items);err!=nil{t.Fatal(err)}
 }
 // Client-backed native Queries remain valid and retain the SDK's materialization behavior.
 calls=0
 if err:=q.VisitFrom(ctx,2047,func(VisitFromRow)error{calls++;return nil});err!=nil||calls!=1{t.Fatalf("client: calls=%d err=%v",calls,err)}
 calls=0
 err=q.VisitNamedDevices(ctx,VisitNamedDevicesParams{MinName:&name,MaxName:&name},func(VisitNamedDevicesRow)error{calls++;return nil})
 if err!=nil||calls!=2047{t.Fatalf("inferred nullable bounds: calls=%d err=%v",calls,err)}
 calls=0
 err=q.VisitNamedDevices(ctx,VisitNamedDevicesParams{MaxName:&name},func(VisitNamedDevicesRow)error{calls++;return nil})
 if err!=nil||calls!=0{t.Fatalf("nil inferred bound: calls=%d err=%v",calls,err)}
 runtime.GC();var before runtime.MemStats;runtime.ReadMemStats(&before)
 calls=0
 err=streaming(ctx,func(row VisitDevicesRow)error{
  if row.ID!=uint64(calls)||(row.Name==nil)!=(calls==1){return fmt.Errorf("row %d: id=%d nil=%v",calls,row.ID,row.Name==nil)}
  if calls==0{
   runtime.GC();var now runtime.MemStats;runtime.ReadMemStats(&now)
   // The caller selected a streaming executor; the generated method must not retain the 16 MiB result.
   if now.HeapAlloc>before.HeapAlloc+12<<20{return fmt.Errorf("result buffered before callback: %d bytes",now.HeapAlloc-before.HeapAlloc)}
  }
  calls++;return nil
 })
 if err!=nil||calls!=2048{t.Fatalf("full: calls=%d err=%v",calls,err)}
 stop:=errors.New("consumer stop")
 for _,cause:=range []error{stop,retry.RetryableError(stop)}{
  calls=0;err=streaming(ctx,func(VisitDevicesRow)error{calls++;return cause})
  if !errors.Is(err,stop)||calls!=1{t.Fatalf("stop/retry: calls=%d err=%v",calls,err)}
 }
 canceled,stopContext:=context.WithCancel(ctx);calls=0
 err=streaming(canceled,func(VisitDevicesRow)error{calls++;stopContext();return nil})
 if !errors.Is(err,context.Canceled)||calls!=1{t.Fatalf("cancel: calls=%d err=%v",calls,err)}
 // Reuse the client/pool after early exit; a one-connection SQL pool exposes leaked rows.
 calls=0
 err=q.VisitFrom(ctx,2047,func(row VisitFromRow)error{calls++;if row.ID!=2047{return fmt.Errorf("id=%d",row.ID)};return nil})
 if err!=nil||calls!=1{t.Fatalf("reuse: calls=%d err=%v",calls,err)}
 if err:=scoped(func(q *Queries)error{
  calls:=0
  if err:=q.VisitFrom(ctx,2047,func(VisitFromRow)error{calls++;return nil});err!=nil{return err}
  if calls!=1{return fmt.Errorf("scoped calls=%d",calls)}
  return nil
 });err!=nil{t.Fatalf("session/transaction: %v",err)}
}
`
}
