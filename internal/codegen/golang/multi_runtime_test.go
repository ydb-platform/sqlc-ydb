package golang

import (
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestMultiNativeRuntime(t *testing.T) {
	runGeneratedRuntimeTest(t, runtimeMultiInput(), Options{Runtime: "ydb"}, nativeMultiRuntime)
}

func TestMultiDatabaseSQLRuntime(t *testing.T) {
	runGeneratedRuntimeTest(t, runtimeMultiInput(), Options{Runtime: "database/sql"}, sqlMultiRuntime)
}

func runtimeMultiInput() *model.AnalysisResult {
	return &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
		Name: "ReadSummary", Command: model.Multi, SQL: "SELECT CAST(1 AS Uint64) AS value; SELECT false AS value;",
		ResultSets: []model.ResultSet{
			{Name: "Result1", Columns: []model.Column{{Name: "value", Type: model.Type{Kind: "Uint64"}}}},
			{Name: "Result2", Columns: []model.Column{{Name: "value", Type: model.Type{Kind: "Bool"}}}},
		},
	}}}
}

const nativeMultiRuntime = `package db

import (
 "context"
 "errors"
 "io"
 "strings"
 "testing"

 "github.com/ydb-platform/ydb-go-sdk/v3/query"
 "github.com/ydb-platform/ydb-go-sdk/v3/types"
)

var fault = errors.New("fault")

type stream struct {
 query.Result
 sets []*set
 at, calls, closed int
 lateErr, closeErr error
}
func (s *stream) Query(context.Context,string,...query.ExecuteOption)(query.Result,error){s.calls++;return s,nil}
func (*stream) QueryRow(context.Context,string,...query.ExecuteOption)(query.Row,error){panic("unexpected QueryRow")}
func (*stream) Exec(context.Context,string,...query.ExecuteOption)error{panic("unexpected Exec")}
func (s *stream) NextResultSet(ctx context.Context)(query.ResultSet,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if s.at<len(s.sets){next:=s.sets[s.at];s.at++;return next,nil}
 if s.lateErr!=nil{return nil,s.lateErr}
 return nil,io.EOF
}
func (s *stream) Close(context.Context)error{s.closed++;return s.closeErr}

type set struct {
 query.ResultSet
 names []string
 typ types.Type
 values []any
 read int
 rowErr, scanErr error
}
func (s *set) Columns()[]string{return s.names}
func (s *set) ColumnTypes()[]types.Type{return []types.Type{s.typ}}
func (s *set) NextRow(ctx context.Context)(query.Row,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if s.read<len(s.values){v:=s.values[s.read];s.read++;return row{value:v,err:s.scanErr},nil}
 if s.rowErr!=nil{return nil,s.rowErr}
 return nil,io.EOF
}
type row struct {query.Row;value any;err error}
func (r row) ScanNamed(dst ...query.NamedDestination)error{
 if r.err!=nil{return r.err}
 if len(dst)!=1||dst[0].Name()!="value"{return errors.New("wrong destination")}
 switch target:=dst[0].Ref().(type){
 case *uint64:*target=r.value.(uint64)
 case *bool:*target=r.value.(bool)
 default:return errors.New("wrong destination type")
 }
 return nil
}
func newStream()*stream{return &stream{sets:[]*set{
 {names:[]string{"value"},typ:types.TypeUint64},
 {names:[]string{"value"},typ:types.TypeBool,values:[]any{true}},
}}}
func TestResultSets(t *testing.T){
 for _,tc:=range []struct{name string;change func(*stream);want string}{
  {"empty-first",func(*stream){},""},
  {"missing",func(s *stream){s.sets=s.sets[:1]},"missing result set 2"},
  {"extra",func(s *stream){s.sets=append(s.sets,&set{names:[]string{"value"},typ:types.TypeBool})},"unexpected extra result set"},
  {"schema",func(s *stream){s.sets[1].typ=types.TypeUint64},"result set 2 column 1"},
  {"row",func(s *stream){s.sets[1].rowErr=fault},"fault"},
  {"decode",func(s *stream){s.sets[1].scanErr=fault},"fault"},
  {"late",func(s *stream){s.lateErr=fault},"fault"},
  {"close",func(s *stream){s.closeErr=fault},"fault"},
 }{
  t.Run(tc.name,func(t *testing.T){
   s:=newStream();tc.change(s)
   got,err:=New(s).ReadSummary(context.Background())
   if tc.want==""{
    if err!=nil||len(got.Result1)!=0||len(got.Result2)!=1||!got.Result2[0].Value{t.Fatalf("result=%+v err=%v",got,err)}
   }else if err==nil||!strings.Contains(err.Error(),tc.want)||len(got.Result1)!=0||len(got.Result2)!=0{t.Fatalf("result=%+v err=%v",got,err)}
   if s.calls!=1||s.closed!=1{t.Fatalf("calls=%d closed=%d",s.calls,s.closed)}
  })
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 s:=newStream();got,err:=New(s).ReadSummary(ctx)
 if !errors.Is(err,context.Canceled)||len(got.Result2)!=0||s.closed!=1{t.Fatalf("cancellation: %+v %v closed=%d",got,err,s.closed)}
}
`

const sqlMultiRuntime = `package db

import (
 "context"
 "database/sql"
 "database/sql/driver"
 "errors"
 "io"
 "strings"
 "testing"
)

var fault=errors.New("fault")
var current *stream
type connector struct{}
func (connector) Connect(context.Context)(driver.Conn,error){return conn{},nil}
func (connector) Driver()driver.Driver{panic("unused")}
type conn struct{}
func (conn) Prepare(string)(driver.Stmt,error){panic("unused")}
func (conn) Close()error{return nil}
func (conn) Begin()(driver.Tx,error){panic("unused")}
func (conn) QueryContext(context.Context,string,[]driver.NamedValue)(driver.Rows,error){current.calls++;return current,nil}
type set struct{typ,name string;values []driver.Value;read int;rowErr error}
type stream struct{sets []*set;at,calls,closed int;lateErr,closeErr error}
func (s *stream) Columns()[]string{return []string{s.sets[s.at].name}}
func (s *stream) ColumnTypeDatabaseTypeName(int)string{return s.sets[s.at].typ}
func (s *stream) Next(dst []driver.Value)error{
 set:=s.sets[s.at]
 if set.read<len(set.values){dst[0]=set.values[set.read];set.read++;return nil}
 if set.rowErr!=nil{return set.rowErr}
 return io.EOF
}
func (s *stream) HasNextResultSet()bool{return true}
func (s *stream) NextResultSet()error{
 if s.at+1<len(s.sets){s.at++;return nil}
 if s.lateErr!=nil{return s.lateErr}
 return io.EOF
}
func (s *stream) Close()error{s.closed++;return s.closeErr}
func newStream()*stream{return &stream{sets:[]*set{
 {typ:"Uint64",name:"value"},
 {typ:"Bool",name:"value",values:[]driver.Value{true}},
}}}
func TestResultSets(t *testing.T){
 for _,tc:=range []struct{name string;change func(*stream);want string}{
  {"empty-first",func(*stream){},""},
  {"missing",func(s *stream){s.sets=s.sets[:1]},"missing result set 2"},
  {"extra",func(s *stream){s.sets=append(s.sets,&set{typ:"Bool",name:"value"})},"unexpected extra result set"},
  {"schema",func(s *stream){s.sets[1].typ="Uint64"},"result set 2 column 1"},
  {"row",func(s *stream){s.sets[1].rowErr=fault},"fault"},
  {"late",func(s *stream){s.lateErr=fault},"fault"},
  {"close",func(s *stream){s.closeErr=fault},"fault"},
 }{
  t.Run(tc.name,func(t *testing.T){
   current=newStream();tc.change(current)
   db:=sql.OpenDB(connector{});db.SetMaxOpenConns(1)
   got,err:=New(db).ReadSummary(context.Background());_ = db.Close()
   if tc.want==""{
    if err!=nil||len(got.Result1)!=0||len(got.Result2)!=1||!got.Result2[0].Value{t.Fatalf("result=%+v err=%v",got,err)}
   }else if err==nil||!strings.Contains(err.Error(),tc.want)||len(got.Result1)!=0||len(got.Result2)!=0{t.Fatalf("result=%+v err=%v",got,err)}
   if current.calls!=1||current.closed!=1{t.Fatalf("calls=%d closed=%d",current.calls,current.closed)}
  })
 }
}
`
