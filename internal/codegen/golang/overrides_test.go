package golang

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/config"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func overrideInput() *model.AnalysisResult {
	id := model.Column{Name: "id", Table: "customers", Type: model.Type{Kind: "Uint64"}}
	name := model.Column{Name: "name", Table: "customers", Type: model.Type{Kind: "Utf8"}}
	note := model.Column{Name: "note", Table: "customers", Type: model.Optional(model.Type{Kind: "Utf8"})}
	return &model.AnalysisResult{Queries: []model.AnalyzedQuery{
		{Name: "GetCustomer", Command: model.One, SQL: "DECLARE $id AS Uint64; SELECT id, name, note FROM customers WHERE id = $id;", Parameters: []model.Parameter{{Name: "id", Type: id.Type}}, ParameterColumns: map[string][]model.Column{"id": {id}}, ResultSets: []model.ResultSet{{Columns: []model.Column{id, name, note}}}},
		{Name: "PutCustomer", Command: model.Exec, SQL: "DECLARE $id AS Uint64; DECLARE $name AS Utf8; DECLARE $note AS Optional<Utf8>; UPSERT INTO customers (id, name, note) VALUES ($id, $name, $note);", Parameters: []model.Parameter{{Name: "id", Type: id.Type}, {Name: "name", Type: name.Type}, {Name: "note", Type: note.Type}}, ParameterColumns: map[string][]model.Column{"id": {id}, "name": {name}, "note": {note}}},
	}}
}

func overrideOptions(runtime string) Options {
	return Options{Package: "db", Runtime: runtime, Overrides: []config.GoOverride{
		{DBType: "Utf8", GoType: config.GoType{Import: "generated/domain", Package: "domain", Type: "CustomerName"}},
		{DBType: "Utf8", Nullable: true, GoType: config.GoType{Type: "Note"}},
		{Column: "customers.id", GoType: config.GoType{Type: "CustomerID"}},
	}}
}

func TestGoOverridesGeneratedRuntime(t *testing.T) {
	for _, runtime := range []string{"database/sql", "ydb"} {
		t.Run(runtime, func(t *testing.T) {
			files, err := Generate(overrideInput(), overrideOptions(runtime))
			require.NoError(t, err)
			dir := t.TempDir()
			for _, file := range files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, file.Name), file.Content, 0600))
			}
			require.NoError(t, os.Mkdir(filepath.Join(dir, "domain"), 0700))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "domain", "domain.go"), []byte("package domain\ntype CustomerName string\n"), 0600))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module generated\n\ngo 1.26.0\n\nrequire github.com/ydb-platform/ydb-go-sdk/v3 v3.151.1\n"), 0600))
			source := overrideSQLRuntime
			if runtime == "ydb" {
				source = overrideNativeRuntime
			}
			require.NoError(t, os.WriteFile(filepath.Join(dir, "runtime_test.go"), []byte(source), 0600))
			cmd := exec.Command("go", "test", "-mod=mod", ".")
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, "generated %s runtime failed:\n%s", runtime, out)
		})
	}
}

const overrideSQLRuntime = `package db
import ("context"; "database/sql"; "database/sql/driver"; "io"; "testing"; "generated/domain")
type CustomerID uint64
type Note string
var captured []driver.NamedValue
type testDriver struct{}
func (testDriver) Open(string)(driver.Conn,error){return testConn{},nil}
type testConn struct{}
func (testConn) Prepare(string)(driver.Stmt,error){return nil,driver.ErrSkip}
func (testConn) Close()error{return nil}
func (testConn) Begin()(driver.Tx,error){return nil,driver.ErrSkip}
func (testConn) QueryContext(_ context.Context,_ string,args []driver.NamedValue)(driver.Rows,error){captured=args;return &testRows{},nil}
func (testConn) ExecContext(_ context.Context,_ string,args []driver.NamedValue)(driver.Result,error){captured=args;return driver.RowsAffected(1),nil}
type testRows struct{done bool}
func (*testRows) Columns()[]string{return []string{"id","name","note"}}
func (*testRows) Close()error{return nil}
func (r *testRows) Next(dst []driver.Value)error{if r.done{return io.EOF};r.done=true;dst[0]=int64(7);dst[1]="Ada";dst[2]="memo";return nil}
func TestOverrideValues(t *testing.T){sql.Register("override",testDriver{});db,err:=sql.Open("override","");if err!=nil{t.Fatal(err)};defer db.Close();q:=New(db);row,err:=q.GetCustomer(context.Background(),CustomerID(7));if err!=nil||row.ID!=7||row.Name!=domain.CustomerName("Ada")||row.Note==nil||*row.Note!="memo"{t.Fatalf("row=%#v err=%v",row,err)};if captured[0].Value!=int64(7){t.Fatalf("read arg=%#v",captured)};note:=Note("changed");err=q.PutCustomer(context.Background(),PutCustomerParams{ID:7,Name:"Ada",Note:&note});if err!=nil{t.Fatal(err)};if captured[0].Value!=int64(7)||captured[1].Value!="Ada"||captured[2].Value!="changed"{t.Fatalf("write args=%#v",captured)};err=q.PutCustomer(context.Background(),PutCustomerParams{ID:8,Name:"Grace"});if err!=nil||captured[2].Value!=nil{t.Fatalf("nullable write args=%#v err=%v",captured,err)}}
`

const overrideNativeRuntime = `package db
import ("context"; "reflect"; "testing"; "generated/domain"; "github.com/ydb-platform/ydb-go-sdk/v3/query"; "github.com/ydb-platform/ydb-go-sdk/v3/types")
type CustomerID uint64
type Note string
type customBool bool
type customInt32 int32
type customDouble float64
type customBytes []byte
type testDB struct{}
func (testDB) Exec(context.Context,string,...query.ExecuteOption)error{return nil}
func (testDB) Query(context.Context,string,...query.ExecuteOption)(query.Result,error){return nil,nil}
func (testDB) QueryRow(context.Context,string,...query.ExecuteOption)(query.Row,error){return testRow{},nil}
type testRow struct{query.Row}
func (testRow) ScanNamed(dst ...query.NamedDestination)error{for _,d:=range dst{switch d.Name(){case "id":*d.Ref().(*CustomerID)=7;case "name":*d.Ref().(*domain.CustomerName)="Ada";case "note":v:=Note("memo");*d.Ref().(**Note)=&v}};return nil}
func TestOverrideValues(t *testing.T){q:=New(testDB{});row,err:=q.GetCustomer(context.Background(),CustomerID(7));if err!=nil||row.ID!=7||row.Name!="Ada"||row.Note==nil||*row.Note!="memo"{t.Fatalf("row=%#v err=%v",row,err)};note:=Note("changed");if err:=q.PutCustomer(context.Background(),PutCustomerParams{ID:7,Name:"Ada",Note:&note});err!=nil{t.Fatal(err)};if err:=q.PutCustomer(context.Background(),PutCustomerParams{ID:8,Name:"Grace"});err!=nil{t.Fatal(err)}}
func TestSDKDerivedScans(t *testing.T){var id CustomerID;if err:=types.CastTo(types.Uint64Value(7),&id);err!=nil||id!=7{t.Fatalf("id=%d err=%v",id,err)};var name domain.CustomerName;if err:=types.CastTo(types.TextValue("Ada"),&name);err!=nil||name!="Ada"{t.Fatalf("name=%q err=%v",name,err)};var note *Note;raw:="memo";if err:=types.CastTo(types.NullableTextValue(&raw),&note);err!=nil||note==nil||*note!="memo"{t.Fatalf("note=%v err=%v",note,err)};if err:=types.CastTo(types.NullableTextValue(nil),&note);err!=nil||note!=nil{t.Fatalf("null note=%v err=%v",note,err)}}
func checkDerivedCast[T any](t *testing.T, name string, value, present, null types.Value, want T){t.Helper();var got T;if err:=types.CastTo(value,&got);err!=nil||!reflect.DeepEqual(got,want){t.Fatalf("%s required: got=%v want=%v err=%v",name,got,want,err)};var optional *T;if err:=types.CastTo(present,&optional);err!=nil||optional==nil||!reflect.DeepEqual(*optional,want){t.Fatalf("%s present: got=%v want=%v err=%v",name,optional,want,err)};if err:=types.CastTo(null,&optional);err!=nil||optional!=nil{t.Fatalf("%s null: got=%v err=%v",name,optional,err)}}
func TestSDKSupportedOverrideKinds(t *testing.T){b:=true;i:=int32(7);u:=uint64(8);d:=float64(1.5);s:="Ada";raw:=[]byte("data");checkDerivedCast(t,"Bool",types.BoolValue(b),types.NullableBoolValue(&b),types.NullableBoolValue(nil),customBool(b));checkDerivedCast(t,"Int32",types.Int32Value(i),types.NullableInt32Value(&i),types.NullableInt32Value(nil),customInt32(i));checkDerivedCast(t,"Uint64",types.Uint64Value(u),types.NullableUint64Value(&u),types.NullableUint64Value(nil),CustomerID(u));checkDerivedCast(t,"Double",types.DoubleValue(d),types.NullableDoubleValue(&d),types.NullableDoubleValue(nil),customDouble(d));checkDerivedCast(t,"Utf8",types.TextValue(s),types.NullableTextValue(&s),types.NullableTextValue(nil),domain.CustomerName(s));checkDerivedCast(t,"String",types.BytesValue(raw),types.NullableBytesValue(&raw),types.NullableBytesValue(nil),customBytes(raw))}
`

func TestGoOverrideConflictAndUnsupportedTypes(t *testing.T) {
	input := overrideInput()
	input.Queries[1].ParameterColumns["name"] = append(input.Queries[1].ParameterColumns["name"], model.Column{Name: "other", Table: "customers", Type: model.Type{Kind: "Utf8"}})
	options := overrideOptions("ydb")
	options.Overrides = append(options.Overrides, config.GoOverride{Column: "customers.other", GoType: config.GoType{Type: "OtherName"}})
	_, err := Generate(input, options)
	require.ErrorContains(t, err, "different Go type overrides")

	options = overrideOptions("ydb")
	options.Overrides[0].DBType = "Uuid"
	_, err = Generate(overrideInput(), options)
	require.ErrorContains(t, err, "Go type overrides support only")

	for _, kind := range []string{"Int8", "Int16", "Int64", "Uint8", "Uint16", "Uint32", "Float", "Json", "JsonDocument", "Yson"} {
		t.Run(kind, func(t *testing.T) {
			options := overrideOptions("ydb")
			options.Overrides[0].DBType = kind
			_, err := Generate(overrideInput(), options)
			require.ErrorContains(t, err, "Go type overrides support only")
		})
	}
	for _, kind := range []string{"Bool", "Int32", "Uint64", "Double", "Utf8", "String"} {
		t.Run(kind, func(t *testing.T) {
			options := overrideOptions("ydb")
			options.Overrides[0].DBType = kind
			_, err := Generate(overrideInput(), options)
			require.NoError(t, err)
		})
	}

	options = overrideOptions("ydb")
	options.Overrides[0].GoType.Type = "map[string]string"
	_, err = Generate(overrideInput(), options)
	require.ErrorContains(t, err, "must name one Go type")

	options = overrideOptions("ydb")
	options.Overrides[0].GoType.Import = ""
	options.Overrides[0].GoType.Type = "generated/domain.CustomerName"
	files, err := Generate(overrideInput(), options)
	require.NoError(t, err)
	var models string
	for _, file := range files {
		if file.Name == "models.go" {
			models = string(file.Content)
		}
	}
	require.True(t, strings.Contains(models, `"generated/domain"`))
}

func TestColumnOverridePrecedesDBTypeForInputAndOutput(t *testing.T) {
	options := overrideOptions("database/sql")
	options.Overrides = append([]config.GoOverride{{DBType: "Uint64", GoType: config.GoType{Type: "GenericID"}}}, options.Overrides...)
	files, err := Generate(overrideInput(), options)
	require.NoError(t, err)
	for _, file := range files {
		if file.Name == "models.go" || file.Name == "query.sql.go" {
			require.NotContains(t, string(file.Content), "GenericID")
			require.Contains(t, string(file.Content), "CustomerID")
		}
	}
}

func TestRepeatedParameterAcceptsEquivalentGoTypes(t *testing.T) {
	input := overrideInput()
	input.Queries[1].ParameterColumns["name"] = append(input.Queries[1].ParameterColumns["name"], model.Column{Name: "other", Table: "customers", Type: model.Type{Kind: "Utf8"}})
	options := overrideOptions("ydb")
	options.Overrides = append(options.Overrides,
		config.GoOverride{Column: "customers.name", GoType: config.GoType{Type: "generated/domain.CustomerName"}},
		config.GoOverride{Column: "customers.other", GoType: config.GoType{Import: "generated/domain", Package: "domain", Type: "CustomerName"}},
	)
	_, err := Generate(input, options)
	require.NoError(t, err)
}

func TestLiveYDBGoOverrides(t *testing.T) {
	dsn := os.Getenv("YDB_CONNECTION_STRING")
	if dsn == "" {
		t.Skip("set YDB_CONNECTION_STRING to run against YDB")
	}
	table := fmt.Sprintf("sqlc_go_overrides_%d", time.Now().UnixNano())
	for _, runtime := range []string{"ydb", "database/sql"} {
		t.Run(runtime, func(t *testing.T) {
			input := overrideInput()
			for qi := range input.Queries {
				query := &input.Queries[qi]
				query.SQL = strings.ReplaceAll(query.SQL, "customers", table)
				for i := range query.Parameters {
					for j := range query.ParameterColumns[query.Parameters[i].Name] {
						query.ParameterColumns[query.Parameters[i].Name][j].Table = table
					}
				}
				for ri := range query.ResultSets {
					for ci := range query.ResultSets[ri].Columns {
						query.ResultSets[ri].Columns[ci].Table = table
					}
				}
			}
			options := overrideOptions(runtime)
			options.Overrides[2].Column = table + ".id"
			files, err := Generate(input, options)
			require.NoError(t, err)
			dir := t.TempDir()
			for _, file := range files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, file.Name), file.Content, 0600))
			}
			require.NoError(t, os.Mkdir(filepath.Join(dir, "domain"), 0700))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "domain", "domain.go"), []byte("package domain\ntype CustomerName string\n"), 0600))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module generated\n\ngo 1.26.0\n\nrequire github.com/ydb-platform/ydb-go-sdk/v3 v3.151.1\n"), 0600))
			constructor := "New(driver.Query())"
			importSQL := ""
			if runtime == "database/sql" {
				constructor = "New(sql.OpenDB(ydb.MustConnector(driver)))"
				importSQL = `; "database/sql"`
			}
			source := `package db
import ("context"; "testing"; "time"; "generated/domain"; ydb "github.com/ydb-platform/ydb-go-sdk/v3"` + importSQL + `)
type CustomerID uint64
type Note string
func TestLiveOverride(t *testing.T){ctx,cancel:=context.WithTimeout(context.Background(),40*time.Second);defer cancel();driver,err:=ydb.Open(ctx,` + fmt.Sprintf("%q", dsn) + `,ydb.WithAnonymousCredentials());if err!=nil{t.Fatal(err)};defer driver.Close(ctx);defer func(){cleanup,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel();if err:=driver.Query().Exec(cleanup,"DROP TABLE IF EXISTS ` + "`" + table + "`" + `");err!=nil{t.Error(err)}}();if err:=driver.Query().Exec(ctx,"CREATE TABLE ` + "`" + table + "`" + ` (id Uint64 NOT NULL, name Utf8 NOT NULL, note Utf8, PRIMARY KEY(id))");err!=nil{t.Fatal(err)};q:=` + constructor + `;if err:=q.PutCustomer(ctx,PutCustomerParams{ID:7,Name:domain.CustomerName("Ада")});err!=nil{t.Fatal(err)};row,err:=q.GetCustomer(ctx,CustomerID(7));if err!=nil||row.ID!=7||row.Name!="Ада"||row.Note!=nil{t.Fatalf("nil row=%#v err=%v",row,err)};note:=Note("memo");if err:=q.PutCustomer(ctx,PutCustomerParams{ID:8,Name:"Grace",Note:&note});err!=nil{t.Fatal(err)};row,err=q.GetCustomer(ctx,CustomerID(8));if err!=nil||row.ID!=8||row.Name!="Grace"||row.Note==nil||*row.Note!="memo"{t.Fatalf("filled row=%#v err=%v",row,err)}}
`
			require.NoError(t, os.WriteFile(filepath.Join(dir, "live_test.go"), []byte(source), 0600))
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "go", "test", "-mod=mod", "-count=1", ".")
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, "live %s overrides failed:\n%s", runtime, out)
		})
	}
}
