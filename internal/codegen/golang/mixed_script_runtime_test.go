package golang

import (
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestDatabaseSQLMixedScriptReportsCompletionErrors(t *testing.T) {
	analysis, err := analyzer.Analyze(
		[]model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));"}},
		[]model.Source{{Name: "queries.sql", Text: "-- name: ReadAndDelete :many\nSELECT id FROM records;\nDELETE FROM records WHERE id = 9ul;"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	runGeneratedRuntimeTest(t, analysis, Options{Package: "db", Runtime: "database/sql"}, mixedScriptDatabaseSQLRuntime)
}

const mixedScriptDatabaseSQLRuntime = `package db

import (
 "context"
 "database/sql"
 "database/sql/driver"
 "errors"
 "io"
 "strings"
 "testing"
)

var completionError = errors.New("trailing mutation failed")
var currentRows *scriptRows
var queryCalls int

type scriptDriver struct{}
func (scriptDriver) Open(string) (driver.Conn,error) {return scriptConn{},nil}
type scriptConn struct{}
func (scriptConn) Prepare(string) (driver.Stmt,error) {return nil,driver.ErrSkip}
func (scriptConn) Close() error {return nil}
func (scriptConn) Begin() (driver.Tx,error) {return nil,driver.ErrSkip}
func (scriptConn) QueryContext(_ context.Context,statement string,_ []driver.NamedValue) (driver.Rows,error) {
 queryCalls++
 if !strings.Contains(statement,"SELECT id FROM records;") || !strings.Contains(statement,"DELETE FROM records") {return nil,errors.New("script was split")}
 return currentRows,nil
}

type scriptRows struct { remaining,closes int; completion error }
func (*scriptRows) Columns() []string {return []string{"id"}}
func (r *scriptRows) Next(values []driver.Value) error {
 if r.remaining==0 {return io.EOF}
 r.remaining--
 values[0]=int64(7)
 return nil
}
// Streaming drivers cannot know whether another result exists before consuming
// terminal status. Rows.Next therefore does not implicitly close this result.
func (*scriptRows) HasNextResultSet() bool {return true}
func (r *scriptRows) NextResultSet() error {
 if r.completion!=nil {return r.completion}
 return io.EOF
}
func (r *scriptRows) Close() error {r.closes++;return r.completion}

func TestCompletion(t *testing.T) {
 sql.Register("mixed-script",scriptDriver{})
 db,err:=sql.Open("mixed-script","")
 if err!=nil {t.Fatal(err)}
 defer db.Close()
 q:=New(db)
 for _,test:=range []struct{name string;count int;completion error}{
  {"rows-success",1,nil},{"empty-success",0,nil},
  {"rows-late-error",1,completionError},{"empty-late-error",0,completionError},
 } {
  t.Run(test.name,func(t *testing.T){
   currentRows=&scriptRows{remaining:test.count,completion:test.completion}
   queryCalls=0
   rows,err:=q.ReadAndDelete(context.Background())
   if test.completion!=nil {
    if !errors.Is(err,test.completion) || rows!=nil {t.Fatalf("partial result escaped before completion: rows=%v error=%v",rows,err)}
   } else if err!=nil || len(rows)!=test.count || (len(rows)>0 && rows[0].ID!=7) {t.Fatalf("rows=%v error=%v",rows,err)}
   if queryCalls!=1 || currentRows.closes!=1 {t.Fatalf("calls=%d closes=%d",queryCalls,currentRows.closes)}
  })
 }
}
`
