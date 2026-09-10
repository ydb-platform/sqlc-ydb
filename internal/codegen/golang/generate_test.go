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

	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
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
			if !strings.Contains(string(source), "const queryGetUser = "+tc.wantLiteral) {
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

func TestGeneratedSQLUsesDeclarationFreeVariant(t *testing.T) {
	const executableSQL = "-- name: GetUser :one\n\nSELECT id, bio FROM users WHERE id = $id;"
	in := sample()
	in.Queries = in.Queries[:1]
	in.Queries[0].SQLWithoutDeclarations = "-- name: GetUser :one\n   \nSELECT id, bio FROM users WHERE id = $id;"

	for _, runtime := range []string{"database/sql", "ydb"} {
		source := generatedSQLSourceForAnalysis(t, runtime, in)
		if got := generatedSQLValue(t, source); got != executableSQL {
			t.Fatalf("%s executable SQL = %q, want declaration-free %q", runtime, got, executableSQL)
		}
	}
}

func TestGeneratedDatabaseSQLFormatsMultiParameterQueryRowCall(t *testing.T) {
	utf8 := model.Type{Kind: "Utf8"}
	u64 := model.Type{Kind: "Uint64"}
	in := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
		Name: "CreateAuthor", Command: model.One, SQL: "INSERT INTO authors VALUES ($author_id, $author_name, $biography) RETURNING id, name, bio;",
		Parameters: []model.Parameter{{Name: "author_id", Type: u64}, {Name: "author_name", Type: utf8}, {Name: "biography", Type: model.Optional(utf8)}},
		ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: u64}, {Name: "name", Type: utf8}, {Name: "bio", Type: model.Optional(utf8)}}}},
	}}}

	source := generatedSQLSourceForAnalysis(t, "database/sql", in)
	want := `err := q.db.QueryRowContext(ctx, queryCreateAuthor,
		sql.Named("author_id", arg.AuthorID),
		sql.Named("author_name", arg.AuthorName),
		sql.Named("biography", arg.Biography),
	).Scan(
		&row.ID,
		&row.Name,
		&row.Bio,
	)`
	if !strings.Contains(string(source), want) {
		t.Fatalf("multi-parameter QueryRowContext call was not formatted readably:\n%s", source)
	}
}

func TestGeneratedDatabaseSQLFormatsParameterizedCallsAndScans(t *testing.T) {
	utf8 := model.Type{Kind: "Utf8"}
	u64 := model.Type{Kind: "Uint64"}
	in := &model.AnalysisResult{Queries: []model.AnalyzedQuery{
		{Name: "DeleteUser", Command: model.Exec, SQL: "DELETE FROM users WHERE id = $id;", Parameters: []model.Parameter{{Name: "id", Type: u64}}},
		{Name: "FindUsers", Command: model.Many, SQL: "SELECT id, name FROM users WHERE name = $name;", Parameters: []model.Parameter{{Name: "name", Type: utf8}}, ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: u64}, {Name: "name", Type: utf8}}}}},
		{Name: "CountUsers", Command: model.One, SQL: "SELECT COUNT(*) AS count FROM users;", ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "count", Type: u64}}}}},
	}}
	source := string(generatedSQLSourceForAnalysis(t, "database/sql", in))
	for _, want := range []string{
		"q.db.ExecContext(ctx, queryDeleteUser,\n\t\tsql.Named(\"id\", arg),\n\t)",
		"q.db.QueryContext(ctx, queryFindUsers,\n\t\tsql.Named(\"name\", arg),\n\t)",
		"rows.Scan(\n\t\t\t&row.ID,\n\t\t\t&row.Name,\n\t\t)",
		"q.db.QueryRowContext(ctx, queryCountUsers).Scan(\n\t\t&row.Count,\n\t)",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("generated database/sql source misses %q:\n%s", want, source)
		}
	}
}

func TestGeneratedYDBManyUsesNamedColumnScans(t *testing.T) {
	in := sample()
	in.Queries = in.Queries[1:2]

	source := generatedSQLSourceForAnalysis(t, "ydb", in)
	want := `if err := r.ScanNamed(
			query.Named("id", &row.ID),
			query.Named("name", &row.Name),
		); err != nil {`
	if !strings.Contains(string(source), want) {
		t.Fatalf("native :many rows were not scanned by column name:\n%s", source)
	}
}

func TestGeneratedYDBManyValidatesOneResultSet(t *testing.T) {
	in := sample()
	in.Queries = in.Queries[1:2]

	source := string(generatedSQLSourceForAnalysis(t, "ydb", in))
	want := `result, err := q.db.Query(ctx, queryListUsers, opts...)
	if err != nil {
		return []ListUsersRow(nil), err
	}
	defer result.Close(ctx)

	resultSet, err := result.NextResultSet(ctx)
	if errors.Is(err, io.EOF) {
		return []ListUsersRow(nil), xerrors.WithStackTrace(query.ErrNoResultSets)
	}
	if err != nil {
		return []ListUsersRow(nil), xerrors.WithStackTrace(err)
	}

	items := []ListUsersRow(nil)
	for r, err := range resultSet.Rows(ctx) {
		if err != nil {
			return []ListUsersRow(nil), xerrors.WithStackTrace(err)
		}
		var row ListUsersRow
		if err := r.ScanNamed(
			query.Named("id", &row.ID),
			query.Named("name", &row.Name),
		); err != nil {
			return []ListUsersRow(nil), xerrors.WithStackTrace(err)
		}
		items = append(items, row)
	}

	_, err = result.NextResultSet(ctx)
	switch {
	case err == nil:
		return []ListUsersRow(nil), xerrors.WithStackTrace(query.ErrMoreThanOneResultSet)
	case errors.Is(err, io.EOF):
	case err != nil:
		return []ListUsersRow(nil), xerrors.WithStackTrace(err)
	}

	return items, nil`
	if !strings.Contains(source, want) {
		t.Fatalf("native :many query does not validate exactly one result set:\n%s", source)
	}
	for _, unwanted := range []string{"q.db.Do", "query.Session", "QueryResultSet", "attemptItems"} {
		if strings.Contains(source, unwanted) {
			t.Fatalf("native :many source contains caller-owned retry/materialization API %q:\n%s", unwanted, source)
		}
	}
	for _, wantImport := range []string{`"errors"`, `"io"`, `"github.com/ydb-platform/ydb-go-sdk/v3/pkg/xerrors"`} {
		if !strings.Contains(source, wantImport) {
			t.Fatalf("native :many source misses import %s:\n%s", wantImport, source)
		}
	}
}

func TestGeneratedYDBWithoutManyOmitsStreamingImports(t *testing.T) {
	in := sample()
	in.Queries = []model.AnalyzedQuery{in.Queries[0], in.Queries[2]}
	source := string(generatedSQLSourceForAnalysis(t, "ydb", in))
	for _, unwanted := range []string{`"errors"`, `"io"`, `pkg/xerrors`} {
		if strings.Contains(source, unwanted) {
			t.Fatalf("native source without :many imports %s:\n%s", unwanted, source)
		}
	}
}

func TestGeneratedYDBInterfaceSupportsClientsSessionsAndTransactions(t *testing.T) {
	files, err := Generate(sample(), Options{Package: "db", Runtime: "ydb"})
	if err != nil {
		t.Fatal(err)
	}
	var source string
	for _, file := range files {
		if file.Name == "db.go" {
			source = string(file.Content)
		}
	}
	for _, want := range []string{
		`Query(context.Context, string, ...query.ExecuteOption) (query.Result, error)`,
		"\t\"context\"\n\n\t\"github.com/ydb-platform/ydb-go-sdk/v3/query\"",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("native DBTX misses %q:\n%s", want, source)
		}
	}
	for _, unwanted := range []string{"QueryResultSet", "query.Operation", "query.DoOption"} {
		if strings.Contains(source, unwanted) {
			t.Fatalf("native DBTX exposes %q:\n%s", unwanted, source)
		}
	}
}

func TestGeneratedYDBManyPreservesEmptySliceContractOnErrors(t *testing.T) {
	in := sample()
	in.Queries = in.Queries[1:2]
	files, err := Generate(in, Options{Package: "db", Runtime: "ydb", EmitEmptySlices: true})
	if err != nil {
		t.Fatal(err)
	}
	var source string
	for _, file := range files {
		if file.Name == "query.sql.go" {
			source = string(file.Content)
		}
	}
	for _, want := range []string{
		"items := make([]ListUsersRow, 0)",
		"return make([]ListUsersRow, 0), xerrors.WithStackTrace(query.ErrNoResultSets)",
		"return make([]ListUsersRow, 0), xerrors.WithStackTrace(query.ErrMoreThanOneResultSet)",
		"return items, nil",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("emit_empty_slices error contract misses %q:\n%s", want, source)
		}
	}
}

func TestGeneratedYDBOneUsesNamedColumnScans(t *testing.T) {
	in := sample()
	in.Queries = in.Queries[:1]
	in.Queries[0].ResultSets[0].Columns[1].WireName = "users.bio"

	source := generatedSQLSourceForAnalysis(t, "ydb", in)
	want := `if err := result.ScanNamed(
		query.Named("id", &row.ID),
		query.Named("users.bio", &row.Bio),
	); err != nil {`
	if !strings.Contains(string(source), want) {
		t.Fatalf("native :one row was not scanned by wire name:\n%s", source)
	}
}

func TestGeneratedQueryImportsSeparateStandardLibraryAndExternalPackages(t *testing.T) {
	in := sample()
	in.Queries[2].Parameters[1].Type = model.Type{Kind: "Json"}
	for _, runtime := range []string{"database/sql", "ydb"} {
		source := string(generatedSQLSourceForAnalysis(t, runtime, in))
		want := "\t\"context\"\n\n\t\"github.com/ydb-platform/ydb-go-sdk/v3"
		if runtime == "database/sql" {
			want = "\t\"context\"\n\t\"database/sql\"\n\n\t\"github.com/ydb-platform/ydb-go-sdk/v3"
		} else {
			want = "\t\"context\"\n\t\"errors\"\n\t\"io\"\n\n\tydb \"github.com/ydb-platform/ydb-go-sdk/v3\""
		}
		if !strings.Contains(source, want) {
			t.Fatalf("%s imports do not separate stdlib and SDK packages:\n%s", runtime, source)
		}
		if strings.Contains(source, "import (\n\n") || strings.Contains(source, "\n\n)") {
			t.Fatalf("%s imports contain an empty group:\n%s", runtime, source)
		}
	}
}

func TestGeneratedYDBNamedScanUsesWireNameAndGoFieldName(t *testing.T) {
	in := sample()
	in.Queries = in.Queries[1:2]
	in.Queries[0].ResultSets[0].Columns[1].WireName = "users.name"

	source := generatedSQLSourceForAnalysis(t, "ydb", in)
	want := `query.Named("users.name", &row.Name)`
	if !strings.Contains(string(source), want) {
		t.Fatalf("native named scan did not bind the wire name to the generated Go field:\n%s", source)
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
	return generatedSQLSourceForAnalysis(t, runtime, in)
}

func generatedSQLSourceForAnalysis(t *testing.T, runtime string, in *model.AnalysisResult) []byte {
	t.Helper()
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
		if !ok || len(decl.Names) != 1 || decl.Names[0].Name != "queryGetUser" {
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
// supplied development database. Example: YDB_CONNECTION_STRING=grpc://127.0.0.1:22136/local.
func TestLiveYDB(t *testing.T) {
	dsn := os.Getenv("YDB_CONNECTION_STRING")
	if dsn == "" {
		t.Skip("set YDB_CONNECTION_STRING to run against YDB")
	}
	table := fmt.Sprintf("sqlc_codegen_go_%d", time.Now().UnixNano())
	for _, runtime := range []string{"ydb", "database/sql"} {
		t.Run(runtime, func(t *testing.T) {
			runLiveGenerated(t, dsn, table, runtime)
			if runtime == "ydb" {
				runLiveTypedNative(t, dsn)
			} else {
				runLiveTypedDatabaseSQL(t, dsn)
			}
		})
	}
}

// runLiveTypedDatabaseSQL verifies the typed wrappers used by the database/sql
// adapter for Decimal and UUID, including nil Optional values.
func runLiveTypedDatabaseSQL(t *testing.T, dsn string) {
	t.Helper()
	decimal := model.Type{Kind: "Decimal", Precision: 22, Scale: 9}
	uuid := model.Type{Kind: "Uuid"}
	input := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
		Name: "RoundTripTyped", Command: model.One,
		SQL: "DECLARE $amount AS Optional<Decimal(22,9)>; DECLARE $id AS Optional<UUID>; SELECT $amount AS amount, $id AS id;",
		Parameters: []model.Parameter{
			{Name: "amount", Type: model.Optional(decimal)}, {Name: "id", Type: model.Optional(uuid)},
		},
		ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "amount", Type: model.Optional(decimal)}, {Name: "id", Type: model.Optional(uuid)}}}},
	}}}
	files, err := Generate(input, Options{Package: "db", Runtime: "database/sql"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f.Name), f.Content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module generated\n\ngo 1.26.0\n\nrequire github.com/ydb-platform/ydb-go-sdk/v3 v3.151.1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	source := `package db
import ("context"; "database/sql"; "testing"; "math/big"; "github.com/ydb-platform/ydb-go-sdk/v3/pkg/decimal"; "github.com/google/uuid"; ydb "github.com/ydb-platform/ydb-go-sdk/v3"; "github.com/ydb-platform/ydb-go-sdk/v3/types")
func TestTyped(t *testing.T) { ctx:=context.Background(); driver,err:=ydb.Open(ctx,` + strconv.Quote(dsn) + `,ydb.WithAnonymousCredentials());if err!=nil{t.Fatal(err)};defer driver.Close(ctx);db:=sql.OpenDB(ydb.MustConnector(driver));defer db.Close();q:=New(db);row,err:=q.RoundTripTyped(ctx,RoundTripTypedParams{Amount:nil,ID:nil});if err!=nil||row.Amount!=nil||row.ID!=nil{t.Fatalf("nil row=%#v err=%v",row,err)};amount:=&types.Decimal{Bytes:decimal.BigIntToByte(big.NewInt(123450000000),22),Precision:22,Scale:9};id:=uuid.MustParse("6e73b41c-4ede-4d08-9cfb-b7462d9e498b");row,err=q.RoundTripTyped(ctx,RoundTripTypedParams{Amount:amount,ID:&id});if err!=nil||row.Amount==nil||row.Amount.Precision!=22||row.Amount.Scale!=9||row.Amount.Bytes!=amount.Bytes||row.ID==nil||*row.ID!=id{t.Fatalf("typed row=%#v err=%v",row,err)} }
`
	if err := os.WriteFile(filepath.Join(dir, "typed_test.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	commandCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, "go", "test", "-mod=mod", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("typed database/sql adapter failed against %s:\n%s", dsn, out)
	}
}

// runLiveTypedNative covers bindings which require typed SDK values. It remains
// opt-in with TestLiveYDB because it sends queries to the supplied database.
func runLiveTypedNative(t *testing.T, dsn string) {
	t.Helper()
	u64 := model.Type{Kind: "Uint64"}
	decimal := model.Type{Kind: "Decimal", Precision: 22, Scale: 9}
	uuid := model.Type{Kind: "Uuid"}
	input := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
		Name: "RoundTripTyped", Command: model.One,
		SQL: "DECLARE $ids AS List<Uint64>; DECLARE $nullable_ids AS List<Optional<Uint64>>; DECLARE $amount AS Optional<Decimal(22,9)>; DECLARE $id AS UUID; SELECT $amount AS amount, $id AS id, ListLength($ids) AS size, ListHas($ids, 2ul) AS has_two, ListLength($nullable_ids) AS nullable_size, ListHas($nullable_ids, 1ul) AS has_one;",
		Parameters: []model.Parameter{
			{Name: "ids", Type: model.Type{Kind: "List", Elem: &u64}},
			{Name: "nullable_ids", Type: model.Type{Kind: "List", Elem: ptr(model.Optional(u64))}},
			{Name: "amount", Type: model.Optional(decimal)}, {Name: "id", Type: uuid},
		},
		ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "amount", Type: model.Optional(decimal)}, {Name: "id", Type: uuid}, {Name: "size", Type: u64}, {Name: "has_two", Type: model.Type{Kind: "Bool"}}, {Name: "nullable_size", Type: u64}, {Name: "has_one", Type: model.Type{Kind: "Bool"}}}}},
	}}}
	input.Queries = append(input.Queries, model.AnalyzedQuery{
		Name: "RoundTripLists", Command: model.One,
		SQL: "DECLARE $docs AS List<Yson>; DECLARE $maybe_docs AS List<Optional<Yson>>; DECLARE $days AS List<Optional<Date>>; SELECT ListLength($docs) AS size, ListLength(ListNotNull($maybe_docs)) AS present, ListHead(ListNotNull($days)) AS day;",
		Parameters: []model.Parameter{
			{Name: "docs", Type: model.Type{Kind: "List", Elem: ptr(model.Type{Kind: "Yson"})}},
			{Name: "maybe_docs", Type: model.Type{Kind: "List", Elem: ptr(model.Optional(model.Type{Kind: "Yson"}))}},
			{Name: "days", Type: model.Type{Kind: "List", Elem: ptr(model.Optional(model.Type{Kind: "Date"}))}},
		},
		ResultSets: []model.ResultSet{{Columns: []model.Column{
			{Name: "size", Type: u64}, {Name: "present", Type: u64}, {Name: "day", Type: model.Optional(model.Type{Kind: "Date"})},
		}}},
	})
	files, err := Generate(input, Options{Package: "db", Runtime: "ydb"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f.Name), f.Content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module generated\n\ngo 1.26.0\n\nrequire github.com/ydb-platform/ydb-go-sdk/v3 v3.151.1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	source := `package db
import ("context"; "testing"; "time"; "math/big"; "github.com/ydb-platform/ydb-go-sdk/v3/pkg/decimal"; "github.com/google/uuid"; ydb "github.com/ydb-platform/ydb-go-sdk/v3"; "github.com/ydb-platform/ydb-go-sdk/v3/types")
func TestTyped(t *testing.T) { ctx:=context.Background(); driver,err:=ydb.Open(ctx,` + strconv.Quote(dsn) + `,ydb.WithAnonymousCredentials());if err!=nil{t.Fatal(err)};defer driver.Close(ctx);q:=New(driver.Query());id:=uuid.MustParse("6e73b41c-4ede-4d08-9cfb-b7462d9e498b");row,err:=q.RoundTripTyped(ctx,RoundTripTypedParams{Ids:[]uint64{},NullableIds:[]*uint64{nil},Amount:nil,ID:id});if err!=nil||row.Amount!=nil||row.ID!=id||row.Size!=0||row.HasTwo||row.NullableSize!=1||row.HasOne{t.Fatalf("empty/nil row=%#v err=%v",row,err)};amount:=&types.Decimal{Bytes:decimal.BigIntToByte(big.NewInt(123450000000),22),Precision:22,Scale:9};one:=uint64(1);row,err=q.RoundTripTyped(ctx,RoundTripTypedParams{Ids:[]uint64{1,2},NullableIds:[]*uint64{nil,&one},Amount:amount,ID:id});if err!=nil||row.Amount==nil||row.Amount.Precision!=22||row.Amount.Scale!=9||row.Amount.Bytes!=amount.Bytes||row.ID!=id||row.Size!=2||!row.HasTwo||row.NullableSize!=2||!row.HasOne{t.Fatalf("typed row=%#v err=%v",row,err)} }

func TestTypedYsonAndDateLists(t *testing.T) {
 ctx := context.Background()
 driver, err := ydb.Open(ctx, ` + strconv.Quote(dsn) + `, ydb.WithAnonymousCredentials())
 if err != nil { t.Fatal(err) }; defer driver.Close(ctx)
 q := New(driver.Query())
 row, err := q.RoundTripLists(ctx, RoundTripListsParams{})
 if err != nil || row.Size != 0 || row.Present != 0 || row.Day != nil { t.Fatalf("empty lists: %#v %v", row, err) }
 doc := []byte("{}")
 day := time.Date(2001, 2, 3, 0, 0, 0, 0, time.UTC)
 row, err = q.RoundTripLists(ctx, RoundTripListsParams{Docs:[][]byte{doc}, MaybeDocs:[]*[]byte{nil,&doc}, Days:[]*time.Time{nil,&day}})
 if err != nil || row.Size != 1 || row.Present != 1 || row.Day == nil || !row.Day.Equal(day) { t.Fatalf("typed lists: %#v %v", row, err) }
}

`
	if err := os.WriteFile(filepath.Join(dir, "typed_test.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	commandCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, "go", "test", "-mod=mod", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("typed native adapter failed against %s:\n%s", dsn, out)
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
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module generated\n\ngo 1.26.0\n\nrequire github.com/ydb-platform/ydb-go-sdk/v3 v3.151.1\n"), 0600); err != nil {
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
func (conn) QueryContext(_ context.Context, q string, a []driver.NamedValue)(driver.Rows,error){ calls=a;lastSQL=q;if fail{return nil,errors.New("query failed")}; if q==queryGetUser {return &rows{data:[][]driver.Value{{uint64(7),nil}}},nil}; return &rows{data:[][]driver.Value{{uint64(8),"a"}}},nil }
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
	mod += "\nrequire github.com/ydb-platform/ydb-go-sdk/v3 v3.151.1\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-mod=mod", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated %s code does not compile:\n%s", opts.Runtime, out)
	}
	if opts.Runtime == "database/sql" && opts.EmitJSONTags && !strings.Contains(string(files[0].Content), "json:\"id\"") {
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

func TestGeneratedIdentifiersDoNotCollideWithMethodScope(t *testing.T) {
	u64 := model.Type{Kind: "Uint64"}
	row := []model.ResultSet{{Columns: []model.Column{{Name: "value", Type: u64}}}}
	databaseSQL := &model.AnalysisResult{Queries: []model.AnalyzedQuery{
		{Name: "ByContext", Command: model.Exec, Parameters: []model.Parameter{{Name: "ctx", Type: u64}}},
		{Name: "ByReceiver", Command: model.Exec, Parameters: []model.Parameter{{Name: "q", Type: u64}}},
		{Name: "BySQLImport", Command: model.Exec, Parameters: []model.Parameter{{Name: "sql", Type: u64}}},
		{Name: "ByRowLocal", Command: model.One, Parameters: []model.Parameter{{Name: "row", Type: u64}}, ResultSets: row},
	}}
	compileInput(t, databaseSQL, Options{Package: "db", Runtime: "database/sql"})

	ydb := &model.AnalysisResult{Queries: []model.AnalyzedQuery{
		{Name: "ByYDBImport", Command: model.Exec, Parameters: []model.Parameter{{Name: "ydb", Type: u64}}},
		{Name: "ByQueryImport", Command: model.Exec, Parameters: []model.Parameter{{Name: "query", Type: u64}}},
	}}
	compileInput(t, ydb, Options{Package: "db", Runtime: "ydb"})
}

func TestNoParameterQueryFilesCompileWithoutUnusedRuntimeImports(t *testing.T) {
	in := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Ping", Command: model.Exec, SQL: "SELECT 1;"}}}
	for _, runtime := range []string{"database/sql", "ydb"} {
		t.Run(runtime, func(t *testing.T) {
			compileInput(t, in, Options{Package: "db", Runtime: runtime})
		})
	}
}

func TestDatabaseSQLRejectsQueryNamedWithTx(t *testing.T) {
	_, err := Generate(&model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "WithTx", Command: model.Exec}}}, Options{Package: "db", Runtime: "database/sql"})
	if err == nil || !strings.Contains(err.Error(), `query name "WithTx" conflicts with generated Queries.WithTx`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestQueryConstantsPreserveCaseDistinctNames(t *testing.T) {
	in := &model.AnalysisResult{Queries: []model.AnalyzedQuery{
		{Name: "Foo", Command: model.Exec, SQL: "SELECT 1;"},
		{Name: "foo", Command: model.Exec, SQL: "SELECT 2;"},
	}}
	compileInput(t, in, Options{Package: "db", Runtime: "database/sql"})
}

func TestGenerateCompilesYDBJSONAndTimestampParameters(t *testing.T) {
	jsonType := model.Type{Kind: "Json"}
	jsonDocumentType := model.Type{Kind: "JsonDocument"}
	in := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
		Name:    "PutBook",
		Command: model.Exec,
		Parameters: []model.Parameter{
			{Name: "tags", Type: jsonType},
			{Name: "biography", Type: model.Optional(jsonType)},
			{Name: "document", Type: jsonDocumentType},
			{Name: "optional_document", Type: model.Optional(jsonDocumentType)},
			{Name: "available", Type: model.Type{Kind: "Timestamp"}},
		},
	}}}
	compileInput(t, in, Options{Package: "db", Runtime: "ydb"})
	compileInput(t, in, Options{Package: "db", Runtime: "database/sql"})
	files, err := Generate(in, Options{Package: "db", Runtime: "database/sql"})
	if err != nil {
		t.Fatal(err)
	}
	var source string
	for _, file := range files {
		if strings.Contains(file.Name, ".sql.go") {
			source = string(file.Content)
			break
		}
	}
	for _, call := range []string{
		`sql.Named("tags", types.JSONValue(arg.Tags))`,
		`sql.Named("biography", types.NullableJSONValue(arg.Biography))`,
		`sql.Named("document", types.JSONDocumentValue(arg.Document))`,
		`sql.Named("optional_document", types.NullableJSONDocumentValue(arg.OptionalDocument))`,
		`sql.Named("available", arg.Available)`,
	} {
		if !strings.Contains(source, call) {
			t.Fatalf("database/sql binding lacks %s:\n%s", call, source)
		}
	}
	if strings.Contains(source, `github.com/ydb-platform/ydb-go-sdk/v3/table`) || strings.Contains(source, `table.ValueParam`) {
		t.Fatalf("database/sql typed bindings retain the obsolete table wrapper:\n%s", source)
	}
}

func TestGenerateCompilesNativeTypedListsDecimalAndUUID(t *testing.T) {
	u64 := model.Type{Kind: "Uint64"}
	decimal := model.Type{Kind: "Decimal", Precision: 22, Scale: 9}
	uuid := model.Type{Kind: "Uuid"}
	in := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
		Name:    "BindTypedValues",
		Command: model.One,
		Parameters: []model.Parameter{
			{Name: "ids", Type: model.Type{Kind: "List", Elem: &u64}},
			{Name: "nullable_ids", Type: model.Type{Kind: "List", Elem: ptr(model.Optional(u64))}},
			{Name: "nullable_amounts", Type: model.Type{Kind: "List", Elem: ptr(model.Optional(decimal))}},
			{Name: "nullable_uuids", Type: model.Type{Kind: "List", Elem: ptr(model.Optional(uuid))}},
			{Name: "amount", Type: decimal},
			{Name: "optional_amount", Type: model.Optional(decimal)},
			{Name: "id", Type: uuid},
			{Name: "optional_id", Type: model.Optional(uuid)},
		},
		ResultSets: []model.ResultSet{{Columns: []model.Column{
			{Name: "amount", Type: decimal}, {Name: "optional_amount", Type: model.Optional(decimal)},
			{Name: "id", Type: uuid}, {Name: "optional_id", Type: model.Optional(uuid)},
		}}},
	}}}
	compileInput(t, in, Options{Package: "db", Runtime: "ydb", EmitInterface: true})
	files, err := Generate(in, Options{Package: "db", Runtime: "ydb", EmitInterface: true})
	if err != nil {
		t.Fatal(err)
	}
	var source string
	for _, file := range files {
		if file.Name == "query.sql.go" {
			source = string(file.Content)
		}
	}
	for _, want := range []string{
		"func (q *Queries) BindTypedValues(ctx context.Context, arg BindTypedValuesParams, opts ...query.ExecuteOption)",
		"types.ZeroValue(types.List(types.TypeUint64))",
		"types.ZeroValue(types.List(types.Optional(types.TypeUint64)))",
		"list = list.Add().Uint64(listItem0)",
		"types.NullableUint64Value(listItem1)",
		"types.NullableDecimalValue(nil, 22, 9)",
		"types.NullableUUIDTypedValue(listItem3)",
		".Decimal(arg.Amount.Bytes, 22, 9)",
		"if arg.OptionalAmount != nil",
		".Uuid(arg.ID)",
		".BeginOptional().Uuid(arg.OptionalID).EndOptional()",
		"callOptions = append(callOptions, query.WithParameters(parameters.Build()))",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("generated native binding lacks %q:\n%s", want, source)
		}
	}
}

func TestGenerateCompilesDatabaseSQLDecimalAndUUID(t *testing.T) {
	decimal := model.Type{Kind: "Decimal", Precision: 35, Scale: 12}
	uuid := model.Type{Kind: "Uuid"}
	in := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
		Name: "BindValues", Command: model.One,
		Parameters: []model.Parameter{{Name: "amount", Type: decimal}, {Name: "id", Type: model.Optional(uuid)}},
		ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "amount", Type: decimal}, {Name: "id", Type: model.Optional(uuid)}}}},
	}}}
	compileInput(t, in, Options{Package: "db", Runtime: "database/sql"})
	files, err := Generate(in, Options{Package: "db", Runtime: "database/sql"})
	if err != nil {
		t.Fatal(err)
	}
	var source string
	for _, file := range files {
		if file.Name == "query.sql.go" {
			source = string(file.Content)
		}
	}
	for _, want := range []string{
		`sql.Named("amount", types.DecimalValue(&types.Decimal{Bytes: arg.Amount.Bytes, Precision: 35, Scale: 12}))`,
		`sql.Named("id", types.NullableUUIDTypedValue(arg.ID))`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("generated database/sql binding lacks %q:\n%s", want, source)
		}
	}
}

func TestSingleTypedExecParametersCompileWithoutUnusedImports(t *testing.T) {
	decimal := model.Type{Kind: "Decimal", Precision: 22, Scale: 9}
	uuid := model.Type{Kind: "Uuid"}
	in := &model.AnalysisResult{Queries: []model.AnalyzedQuery{
		{Name: "PutAmount", Command: model.Exec, Source: model.Position{File: "amount.sql"}, Parameters: []model.Parameter{{Name: "amount", Type: decimal}}},
		{Name: "PutID", Command: model.Exec, Source: model.Position{File: "id.sql"}, Parameters: []model.Parameter{{Name: "id", Type: uuid}}},
	}}
	for _, runtime := range []string{"database/sql", "ydb"} {
		t.Run(runtime, func(t *testing.T) {
			compileInput(t, in, Options{Package: "db", Runtime: runtime})
		})
	}
}

func TestGeneratedDecimalBindingsRejectMismatchedCarrierMetadata(t *testing.T) {
	decimal := model.Type{Kind: "Decimal", Precision: 22, Scale: 9}
	in := &model.AnalysisResult{Queries: []model.AnalyzedQuery{
		{Name: "PutAmount", Command: model.Exec, Parameters: []model.Parameter{{Name: "amount", Type: decimal}}},
		{Name: "PutOptionalAmount", Command: model.Exec, Parameters: []model.Parameter{{Name: "amount", Type: model.Optional(decimal)}}},
		{Name: "PutAmounts", Command: model.Exec, Parameters: []model.Parameter{{Name: "amounts", Type: model.Type{Kind: "List", Elem: &decimal}}}},
		{Name: "PutOptionalAmounts", Command: model.Exec, Parameters: []model.Parameter{{Name: "amounts", Type: model.Type{Kind: "List", Elem: ptr(model.Optional(decimal))}}}},
	}}
	for _, runtime := range []string{"database/sql", "ydb"} {
		t.Run(runtime, func(t *testing.T) {
			input := in
			if runtime == "database/sql" {
				input = &model.AnalysisResult{Queries: in.Queries[:2]}
			}
			runGeneratedRuntimeTest(t, input, Options{Package: "db", Runtime: runtime}, decimalMismatchRuntimeTest(runtime))
		})
	}
}

func TestRejectsInvalidDecimalInsideListResult(t *testing.T) {
	invalid := model.Type{Kind: "Decimal", Precision: 0, Scale: 0}
	list := model.Type{Kind: "List", Elem: &invalid}
	_, err := Generate(&model.AnalysisResult{Queries: []model.AnalyzedQuery{{
		Name: "ListAmounts", Command: model.Many,
		ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "amounts", Type: list}}}},
	}}}, Options{Runtime: "ydb"})
	if err == nil || !strings.Contains(err.Error(), "Decimal requires precision") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func runGeneratedRuntimeTest(t *testing.T, input *model.AnalysisResult, opts Options, source string) {
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
	mod := "module generated\n\ngo 1.26.0\n\nrequire github.com/ydb-platform/ydb-go-sdk/v3 v3.151.1\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "decimal_test.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-mod=mod", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated %s decimal validation failed:\n%s", opts.Runtime, out)
	}
}

func decimalMismatchRuntimeTest(runtime string) string {
	methods := `if err := q.PutAmount(ctx, wrong); err == nil || !strings.Contains(err.Error(), "Decimal parameter $amount expects Decimal(22,9)") { t.Fatalf("scalar err=%v", err) }
if err := q.PutOptionalAmount(ctx, &wrong); err == nil || !strings.Contains(err.Error(), "Decimal parameter $amount expects Decimal(22,9)") { t.Fatalf("optional err=%v", err) }`
	if runtime == "ydb" {
		methods += `
if err := q.PutAmounts(ctx, []types.Decimal{wrong}); err == nil || !strings.Contains(err.Error(), "Decimal parameter $amounts expects Decimal(22,9)") { t.Fatalf("list err=%v", err) }
if err := q.PutOptionalAmounts(ctx, []*types.Decimal{&wrong}); err == nil || !strings.Contains(err.Error(), "Decimal parameter $amounts expects Decimal(22,9)") { t.Fatalf("optional list err=%v", err) }`
	}
	return `package db
import ("context"; "strings"; "testing"; "github.com/ydb-platform/ydb-go-sdk/v3/types")
func TestDecimalMetadata(t *testing.T) { ctx:=context.Background(); q:=New(nil); wrong:=types.Decimal{Precision:21,Scale:9}; ` + methods + ` }
`
}

func TestNativeOptionsAreForwardedAndCannotReplaceTypedArguments(t *testing.T) {
	u64 := model.Type{Kind: "Uint64"}
	in := &model.AnalysisResult{Queries: []model.AnalyzedQuery{
		{Name: "Ping", Command: model.Exec},
		{Name: "Put", Command: model.Exec, Parameters: []model.Parameter{{Name: "id", Type: u64}}},
	}}
	files, err := Generate(in, Options{Package: "db", Runtime: "ydb"})
	if err != nil {
		t.Fatal(err)
	}
	var source string
	for _, file := range files {
		if file.Name == "query.sql.go" {
			source = string(file.Content)
		}
	}
	for _, want := range []string{
		"func (q *Queries) Ping(ctx context.Context, opts ...query.ExecuteOption) error",
		"return q.db.Exec(ctx, queryPing, opts...)",
		"func (q *Queries) Put(ctx context.Context, arg uint64, opts ...query.ExecuteOption) error",
		"callOptions := append([]query.ExecuteOption(nil), opts...)",
		"callOptions = append(callOptions, query.WithParameters(parameters.Build()))",
		"return q.db.Exec(ctx, queryPut, callOptions...)",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("native option forwarding lacks %q:\n%s", want, source)
		}
	}
}

func TestRejectsUnsupportedListAndDecimalShapes(t *testing.T) {
	u64 := model.Type{Kind: "Uint64"}
	cases := []struct {
		name, runtime, want string
		type_               model.Type
	}{
		{"database list", "database/sql", "List parameters are unsupported", model.Type{Kind: "List", Elem: &u64}},
		{"optional list", "ydb", "Optional<List> parameters", model.Optional(model.Type{Kind: "List", Elem: &u64})},
		{"nested list", "ydb", "nested List parameters", model.Type{Kind: "List", Elem: ptr(model.Type{Kind: "List", Elem: &u64})}},
		{"invalid decimal", "ydb", "Decimal requires precision", model.Type{Kind: "Decimal", Precision: 0, Scale: 0}},
		{"nested optional decimal", "ydb", "nested Optional<Decimal>", model.Optional(model.Optional(model.Type{Kind: "Decimal", Precision: 22, Scale: 9}))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Generate(&model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Bad", Command: model.Exec, Parameters: []model.Parameter{{Name: "p", Type: tc.type_}}}}}, Options{Runtime: tc.runtime})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestRejectsNestedOptionalNativeListElementFromYQL(t *testing.T) {
	result, err := analyzer.Analyze(nil, []model.Source{{Name: "query.sql", Text: `-- name: Bind :one
DECLARE $values AS List<Optional<Optional<Uint64>>>;
SELECT $values AS values;
`}})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	_, err = Generate(result, Options{Runtime: "ydb"})
	if err == nil || !strings.Contains(err.Error(), "List element must be a scalar or Optional<scalar>") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func ptr(t model.Type) *model.Type { return &t }

func TestGenerateCompilesDatabaseSQLJSONOnlyQueryFile(t *testing.T) {
	jsonType := model.Type{Kind: "Json"}
	in := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{
		Name:       "FindByDocument",
		Command:    model.Exec,
		Source:     model.Position{File: "json.sql"},
		Parameters: []model.Parameter{{Name: "document", Type: jsonType}},
	}}}
	compileInput(t, in, Options{Package: "db", Runtime: "database/sql"})
}

func TestRejectsBlankIdentifierPackage(t *testing.T) {
	_, err := Generate(&model.AnalysisResult{}, Options{Package: "_", Runtime: "database/sql"})
	if err == nil || !strings.Contains(err.Error(), `invalid Go package "_"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompileTypedParametersAcrossSources(t *testing.T) {
	for _, runtime := range []string{"ydb", "database/sql"} {
		for _, kind := range []model.Type{{Kind: "Decimal", Precision: 22, Scale: 9}, model.Optional(model.Type{Kind: "Uuid"})} {
			t.Run(runtime+"/"+kind.Kind, func(t *testing.T) {
				in := &model.AnalysisResult{}
				for i, name := range []string{"First", "Second"} {
					in.Queries = append(in.Queries, model.AnalyzedQuery{Name: name, Command: model.Exec, SQL: "SELECT $value;", Source: model.Position{File: fmt.Sprintf("query%d.sql", i)}, Parameters: []model.Parameter{{Name: "value", Type: kind}}})
				}
				compileInput(t, in, Options{Package: "db", Runtime: runtime})
			})
		}
	}
}

func TestCompileNativeListParameterMatrix(t *testing.T) {
	decimal := model.Type{Kind: "Decimal", Precision: 22, Scale: 9}
	supported := []model.Type{
		{Kind: "Bool"}, {Kind: "Int8"}, {Kind: "Int16"}, {Kind: "Int32"}, {Kind: "Int64"},
		{Kind: "Uint8"}, {Kind: "Uint16"}, {Kind: "Uint32"}, {Kind: "Uint64"},
		{Kind: "Float"}, {Kind: "Double"}, {Kind: "String"}, {Kind: "Utf8"}, {Kind: "Json"}, {Kind: "JsonDocument"}, {Kind: "Yson"},
		{Kind: "Date"}, {Kind: "Datetime"}, {Kind: "Timestamp"}, {Kind: "Interval"}, decimal, {Kind: "Uuid"},
	}
	in := &model.AnalysisResult{}
	for _, element := range supported {
		for _, optional := range []bool{false, true} {
			item := element
			prefix := "List"
			if optional {
				item = model.Optional(item)
				prefix = "OptionalList"
			}
			in.Queries = append(in.Queries, model.AnalyzedQuery{
				Name: prefix + goName(element.Kind), Command: model.Exec,
				Parameters: []model.Parameter{{Name: "values", Type: model.Type{Kind: "List", Elem: &item}}},
			})
		}
	}
	compileInput(t, in, Options{Package: "db", Runtime: "ydb"})
	files, err := Generate(in, Options{Package: "db", Runtime: "ydb"})
	if err != nil {
		t.Fatal(err)
	}
	var source string
	for _, file := range files {
		if file.Name == "query.sql.go" {
			source = string(file.Content)
		}
	}
	for _, want := range []string{".YSON(listItem", "types.NullableYSONValueFromBytes(listItem"} {
		if !strings.Contains(source, want) {
			t.Fatalf("native YSON list binding lacks %q:\n%s", want, source)
		}
	}
}

func TestRejectsExtendedTemporalNativeListParameters(t *testing.T) {
	for _, kind := range []string{"Date32", "Datetime64", "Timestamp64", "Interval64"} {
		for _, optional := range []bool{false, true} {
			t.Run(kind+fmt.Sprintf("/optional=%t", optional), func(t *testing.T) {
				element := model.Type{Kind: kind}
				if optional {
					element = model.Optional(element)
				}
				_, err := Generate(&model.AnalysisResult{Queries: []model.AnalyzedQuery{{
					Name: "Bind", Command: model.Exec,
					Parameters: []model.Parameter{{Name: "values", Type: model.Type{Kind: "List", Elem: &element}}},
				}}}, Options{Runtime: "ydb"})
				if err == nil || !strings.Contains(err.Error(), "SDK list builder has no "+kind+" method") {
					t.Fatalf("unexpected error: %v", err)
				}
			})
		}
	}
}
