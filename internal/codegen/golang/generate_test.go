package golang

import (
	"context"
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ydb-platform/sqlc-engine-ydb/internal/model"
)

func sample() *model.AnalysisResult {
	utf8 := model.Type{Kind: "Utf8"}
	return &model.AnalysisResult{Queries: []model.AnalyzedQuery{
		{Name: "GetUser", Command: model.One, SQL: "DECLARE $id AS Uint64; SELECT `id`, `bio` FROM `users` WHERE id = $id;", Parameters: []model.Parameter{{Name: "id", Type: model.Type{Kind: "Uint64"}}}, ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "bio", Type: model.Optional(utf8)}}}}},
		{Name: "ListUsers", Command: model.Many, SQL: "SELECT `id`, `name` FROM `users`;", ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "name", Type: utf8}}}}},
		{Name: "UpdateUser", Command: model.Exec, SQL: "DECLARE $name AS Utf8; DECLARE $bio AS Optional<Utf8>; UPDATE `users` SET name = $name, bio = $bio;", Parameters: []model.Parameter{{Name: "name", Type: utf8}, {Name: "bio", Type: model.Optional(utf8)}}},
	}}
}

func TestGeneratedSQLIsMultilineAndPreservesText(t *testing.T) {
	for _, tc := range []struct {
		sql, wantLiteral string
	}{
		{"-- name: GetUser :one\nDECLARE $id AS Uint64;\nSELECT id, bio FROM users WHERE id = $id;", "`-- name: GetUser :one\n"},
		{"-- name: GetUser :one\nSELECT `id`, `bio` FROM `users`\nWHERE name = 'Автор' AND path = 'C:\\data';", "\"-- name: GetUser :one\\n\" +\n"},
		{"-- name: GetUser :one\r\nSELECT id, bio FROM users;\r\n", "\"-- name: GetUser :one\\r\\n\" +\n"},
		{"-- name: GetUser :one\nSELECT '\x00' FROM users;", "\"-- name: GetUser :one\\n\" +\n"},
	} {
		for _, runtime := range []string{"database/sql", "ydb"} {
			source := generatedSQLSource(t, runtime, tc.sql)
			if !strings.Contains(string(source), "const getUser = "+tc.wantLiteral) {
				t.Fatalf("%s SQL is not a readable multiline literal:\n%s", runtime, source)
			}
			if strings.Contains(tc.sql, "`id`") && !strings.Contains(string(source), "\"SELECT `id`, `bio` FROM `users`\\n\"") {
				t.Fatalf("quoted identifiers split across Go literals:\n%s", source)
			}
			if got := generatedSQLValue(t, source); got != tc.sql {
				t.Fatalf("%s SQL changed: got %q, want %q", runtime, got, tc.sql)
			}
		}
	}
}

func TestGeneratedSQLSpecialCharacters(t *testing.T) {
	cases := []struct{ name, sql string }{
		{"quotes", "SELECT '\"\"\"', '```', '\\\"', '\\\\', '''', `id` FROM `users`;"},
		{"literal_escapes", `SELECT '\n\r\t\x00\u1234\U0001f680', 'C:\new\test' FROM users;`},
		{"trailing_backslash", "-- trailing backslash\\"},
		{"line_endings", "-- mixed\rSELECT id\r\nFROM users\n;\r"},
		{"indentation", "\n\nSELECT\n\t id,\n    bio\nFROM users;\n\n"},
		{"unicode", "SELECT 'Автор 中文 🚀 e\u0301 \u200d \u2028 \u2029' FROM users;"},
		{"bom", "SELECT '\ufeff' FROM users;"},
		{"invalid_utf8", "SELECT '\xff\xfe' FROM users;"},
		{"comment_symbols", "-- $id :param /* comment */ 100% \\\nSELECT `id` FROM users; -- \" + dangerous() + \""},
	}
	for ch := byte(0); ch < 32; ch++ {
		cases = append(cases, struct{ name, sql string }{fmt.Sprintf("control_%02x", ch), "SELECT '" + string(ch) + "' FROM users;"})
	}
	cases = append(cases, struct{ name, sql string }{"delete", "SELECT '\x7f' FROM users;"})
	for _, runtime := range []string{"database/sql", "ydb"} {
		for _, tc := range cases {
			t.Run(runtime+"/"+tc.name, func(t *testing.T) {
				got := generatedSQLValue(t, generatedSQLSource(t, runtime, tc.sql))
				if got != tc.sql {
					t.Fatalf("SQL changed: got %q, want %q", got, tc.sql)
				}
			})
		}
	}
}

func generatedSQLSource(t *testing.T, runtime, sql string) []byte {
	t.Helper()
	in := sample()
	in.Queries = in.Queries[:1]
	in.Queries[0].SQL = sql
	files, err := Generate(in, Options{Package: "db", Runtime: runtime})
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.Name == "query.sql.go" {
			return file.Content
		}
	}
	t.Fatal("generated query file missing")
	return nil
}

func generatedSQLValue(t *testing.T, source []byte) string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "query.sql.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	var result string
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		decl, ok := node.(*ast.ValueSpec)
		if !ok || len(decl.Names) != 1 || decl.Names[0].Name != "getUser" {
			return true
		}
		found = true
		expr := decl.Values[0]
		start, end := fset.Position(expr.Pos()).Offset, fset.Position(expr.End()).Offset
		value, err := types.Eval(fset, nil, token.NoPos, string(source[start:end]))
		if err != nil {
			t.Fatal(err)
		}
		result = constant.StringVal(value.Value)
		return false
	})
	if !found {
		t.Fatal("generated SQL constant missing")
	}
	return result
}

// TestLiveYDB is deliberately opt-in: it creates and drops its own table on the
// supplied development database. Example: SQLC_YDB_TEST_DSN=grpc://127.0.0.1:22136/local.
func TestLiveYDB(t *testing.T) {
	dsn := os.Getenv("SQLC_YDB_TEST_DSN")
	if dsn == "" {
		t.Skip("set SQLC_YDB_TEST_DSN to run against YDB")
	}
	table := fmt.Sprintf("sqlc_codegen_go_%d", time.Now().UnixNano())
	for _, runtime := range []string{"ydb", "database/sql"} {
		t.Run(runtime, func(t *testing.T) { runLiveGenerated(t, dsn, table, runtime) })
	}
}

func runLiveGenerated(t *testing.T, dsn, table, runtime string) {
	t.Helper()
	utf8, bytes := model.Type{Kind: "Utf8"}, model.Type{Kind: "String"}
	input := &model.AnalysisResult{Queries: []model.AnalyzedQuery{
		{Name: "PutUser", Command: model.Exec, SQL: "DECLARE $id AS Uint64; DECLARE $name AS Utf8; DECLARE $payload AS String; DECLARE $bio AS Optional<Utf8>; UPSERT INTO `" + table + "` (`id`,`name`,`payload`,`bio`) VALUES ($id,$name,$payload,$bio);", Parameters: []model.Parameter{{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "name", Type: utf8}, {Name: "payload", Type: bytes}, {Name: "bio", Type: model.Optional(utf8)}}},
		{Name: "GetUser", Command: model.One, SQL: "DECLARE $id AS Uint64; SELECT `id`,`name`,`payload`,`bio` FROM `" + table + "` WHERE `id` = $id;", Parameters: []model.Parameter{{Name: "id", Type: model.Type{Kind: "Uint64"}}}, ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "name", Type: utf8}, {Name: "payload", Type: bytes}, {Name: "bio", Type: model.Optional(utf8)}}}}},
		{Name: "ListUsers", Command: model.Many, SQL: "SELECT `id`,`name`,`payload`,`bio` FROM `" + table + "` ORDER BY `id`;", ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "name", Type: utf8}, {Name: "payload", Type: bytes}, {Name: "bio", Type: model.Optional(utf8)}}}}},
		{Name: "GetList", Command: model.One, SQL: "SELECT AsList(CAST(1 AS Uint64), CAST(2 AS Uint64)) AS values;", ResultSets: []model.ResultSet{
			{Columns: []model.Column{{Name: "values", Type: model.Type{Kind: "List", Elem: &model.Type{Kind: "Uint64"}}}}},
		}},
	}}
	if runtime == "database/sql" {
		_, listErr := Generate(&model.AnalysisResult{Queries: []model.AnalyzedQuery{input.Queries[3]}}, Options{Package: "db", Runtime: runtime})
		if listErr == nil || !strings.Contains(listErr.Error(), "List results") {
			t.Fatalf("database/sql List result: %v", listErr)
		}
		input.Queries = input.Queries[:3]
	}
	files, err := Generate(input, Options{Package: "db", Runtime: runtime})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f.Name), f.Content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module generated\n\ngo 1.26.0\n\nrequire github.com/ydb-platform/ydb-go-sdk/v3 v3.125.1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	test := liveTestSource(dsn, table, runtime)
	if err := os.WriteFile(filepath.Join(dir, "live_test.go"), []byte(test), 0600); err != nil {
		t.Fatal(err)
	}
	commandCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, "go", "test", "-mod=mod", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("live %s adapter failed against %s:\n%s", runtime, dsn, out)
	}
}

func liveTestSource(dsn, table, runtime string) string {
	constructor := "New(driver.Query())"
	imports := `"github.com/ydb-platform/ydb-go-sdk/v3"`
	listCheck := `list,err:=q.GetList(ctx);if err!=nil||len(list.Values)!=2||list.Values[0]!=1||list.Values[1]!=2{t.Fatalf("list=%#v err=%v",list,err)};`
	if runtime == "database/sql" {
		constructor = "New(sql.OpenDB(ydb.MustConnector(driver)))"
		imports += `; "database/sql"`
		listCheck = ""
	}
	return `package db
import ("context"; "testing"; "time"; ` + imports + `)
func TestLive(t *testing.T) { ctx,cancel:=context.WithTimeout(context.Background(),45*time.Second);defer cancel(); driver,err:=ydb.Open(ctx,` + strconv.Quote(dsn) + `,ydb.WithAnonymousCredentials());if err!=nil{t.Fatal(err)};defer func(){cleanup,cancel:=context.WithTimeout(context.Background(),45*time.Second);defer cancel();_ = driver.Query().Exec(cleanup,"DROP TABLE IF EXISTS ` + "`" + table + "`" + `");_ = driver.Close(cleanup)}(); if err=driver.Query().Exec(ctx,"DROP TABLE IF EXISTS ` + "`" + table + "`" + `");err!=nil{t.Fatal(err)}; if err=driver.Query().Exec(ctx,"CREATE TABLE ` + "`" + table + "`" + ` (id Uint64 NOT NULL, name Utf8 NOT NULL, payload String NOT NULL, bio Utf8, PRIMARY KEY(id))");err!=nil{t.Fatal(err)}; q:=` + constructor + `; high:=uint64(^uint64(0)); if err=q.PutUser(ctx,PutUserParams{ID:high,Name:"текст",Payload:[]byte{0,1,2},Bio:nil});err!=nil{t.Fatal(err)}; row,err:=q.GetUser(ctx,high);if err!=nil||row.ID!=high||row.Bio!=nil||string(row.Payload)!="\x00\x01\x02"{t.Fatalf("row=%#v err=%v",row,err)}; bio:="present"; if err=q.PutUser(ctx,PutUserParams{ID:1,Name:"one",Payload:[]byte("x"),Bio:&bio});err!=nil{t.Fatal(err)}; rows,err:=q.ListUsers(ctx);if err!=nil||len(rows)!=2{t.Fatalf("rows=%#v err=%v",rows,err)}; ` + listCheck + `_,err=q.GetUser(ctx,2);if err==nil{t.Fatal("missing :one row did not return error")} }
`
}

func TestGenerateCompilesDatabaseSQL(t *testing.T) {
	compile(t, Options{Package: "db", Runtime: "database/sql", EmitJSONTags: true, EmitInterface: true, EmitEmptySlices: true})
}

func TestAbsoluteSourceIsCompilableQueryFile(t *testing.T) {
	in := sample()
	for i := range in.Queries {
		in.Queries[i].Source.File = "/private/project/queries.sql"
	}
	files, err := Generate(in, Options{Package: "db", Runtime: "database/sql"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f.Name), f.Content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "use.go"), []byte("package db\nvar _ = (*Queries).GetUser\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module generated\n\ngo 1.26.0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("absolute source query file was ignored or failed to compile:\n%s", out)
	}
	for _, f := range files {
		if f.Name == "queries.sql.go" {
			return
		}
	}
	t.Fatal("expected basename output queries.sql.go")
}
func TestGenerateCompilesYDB(t *testing.T) {
	in := sample()
	in.Queries = in.Queries[:3]
	compileInput(t, in, Options{Package: "db", Runtime: "ydb", EmitEmptySlices: true})
}

func TestGeneratedDatabaseSQLRuntime(t *testing.T) {
	files, err := Generate(sample(), Options{Package: "db", Runtime: "database/sql", EmitEmptySlices: true})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f.Name), f.Content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module generated\n\ngo 1.26.0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "runtime_test.go"), []byte(runtimeTest), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-mod=mod", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated runtime behavior failed:\n%s", out)
	}
}

const runtimeTest = `package db
import ("context"; "database/sql"; "database/sql/driver"; "errors"; "io"; "strings"; "testing")
var calls []driver.NamedValue
var closed bool
var lastSQL string
var fail bool
type drv struct{}; func (drv) Open(string)(driver.Conn,error){return conn{},nil}
type conn struct{}; func (conn) Prepare(string)(driver.Stmt,error){return nil,driver.ErrSkip}; func (conn) Close()error{return nil}; func (conn) Begin()(driver.Tx,error){return nil,driver.ErrSkip}
func (conn) QueryContext(_ context.Context, q string, a []driver.NamedValue)(driver.Rows,error){ calls=a;lastSQL=q;if fail{return nil,errors.New("query failed")}; if q==getUser {return &rows{data:[][]driver.Value{{uint64(7),nil}}},nil}; return &rows{data:[][]driver.Value{{uint64(8),"a"}}},nil }
func (conn) ExecContext(_ context.Context, _ string, a []driver.NamedValue)(driver.Result,error){calls=a; return result(3),nil}
type rows struct{data [][]driver.Value; i int}; func (r *rows) Columns()[]string{return []string{"id","bio"}}; func (r *rows) Close()error{closed=true;return nil}; func (r *rows) Next(dst []driver.Value)error{if r.i==len(r.data){return io.EOF};copy(dst,r.data[r.i]);r.i++;return nil}
type result int64; func (r result) LastInsertId()(int64,error){return 0,nil};func(r result) RowsAffected()(int64,error){return int64(r),nil}
func TestRuntime(t *testing.T){sql.Register("mock",drv{}); db,err:=sql.Open("mock","");if err!=nil{t.Fatal(err)};q:=New(db); one,err:=q.GetUser(context.Background(),7);if err!=nil||one.ID!=7||one.Bio!=nil{t.Fatalf("one: %#v %v",one,err)};if len(calls)!=1||calls[0].Name!="id"||calls[0].Value.(int64)!=7||!strings.Contains(lastSQL,"$id"){t.Fatalf("args/sql: %#v %q",calls,lastSQL)}; many,err:=q.ListUsers(context.Background());if err!=nil||len(many)!=1||many[0].Name!="a"||!closed{t.Fatalf("many: %#v %v close=%v",many,err,closed)};bio:="b";if err:=q.UpdateUser(context.Background(),UpdateUserParams{Name:"n",Bio:&bio});err!=nil{t.Fatal(err)};if len(calls)!=2||calls[0].Name!="name"||calls[1].Name!="bio"{t.Fatalf("named: %#v",calls)};fail=true;if _,err:=q.GetUser(context.Background(),7);err==nil{t.Fatal("query error was swallowed")}}
`

func compile(t *testing.T, opts Options) {
	compileInput(t, sample(), opts)
}

func compileInput(t *testing.T, input *model.AnalysisResult, opts Options) {
	t.Helper()
	files, err := Generate(input, opts)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f.Name), f.Content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	mod := "module generated\n\ngo 1.26.0\n"
	if opts.Runtime == "ydb" {
		mod += "\nrequire github.com/ydb-platform/ydb-go-sdk/v3 v3.125.1\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-mod=mod", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated %s code does not compile:\n%s", opts.Runtime, out)
	}
	if opts.Runtime == "database/sql" && !strings.Contains(string(files[0].Content), "json:\"id\"") {
		t.Fatal("JSON tags were not emitted")
	}
}

func TestRejectsUnsupportedType(t *testing.T) {
	_, err := Generate(&model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Bad", Command: model.Exec, Parameters: []model.Parameter{{Name: "p", Type: model.Type{Kind: "Struct"}}}}}}, Options{Runtime: "ydb"})
	if err == nil || !strings.Contains(err.Error(), "unsupported YQL type") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRejectsExecRows(t *testing.T) {
	for _, runtime := range []string{"ydb", "database/sql"} {
		_, err := Generate(&model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Delete", Command: model.ExecRows}}}, Options{Runtime: runtime})
		if err == nil || !strings.Contains(err.Error(), ":execrows") {
			t.Fatalf("%s: %v", runtime, err)
		}
	}
}

func TestGoNameInitialismID(t *testing.T) {
	if got := goName("author_id"); got != "AuthorID" {
		t.Fatalf("author_id => %q", got)
	}
}
