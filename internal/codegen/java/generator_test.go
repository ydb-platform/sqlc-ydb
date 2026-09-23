package java

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestSQLLiteralRoundTripsThroughJava17(t *testing.T) {
	// The query has no parameters so the JDBC surface is entirely JDK types. This
	// makes the check a real javac/java 17 literal test without an SDK dependency.
	var c0 strings.Builder
	for r := rune(0); r < 32; r++ {
		c0.WriteRune(r)
	}
	c0.WriteRune(0x7f)
	cases := []struct {
		name string
		sql  string
	}{
		{"empty", ""},
		{"declare_indent", "DECLARE $books AS List<Struct<\n    id: Uint64,\n    data: Json\n>>;\n\nINSERT INTO books\nSELECT\n    id,\n    data\nFROM AS_TABLE($books);"},
		{"ordinary", "SELECT 1;"},
		{"leading_lf", "\nSELECT 1;"},
		{"trailing_lf", "SELECT 1;\n"},
		{"blank_lines", "\n\nSELECT 1;\n\n"},
		{"spaces_tabs", "  SELECT\t1;  \n\t  \n"},
		{"crlf", "SELECT 1;\r\n\r\nSELECT 2;\r\n"},
		{"quotes", "SELECT '\"', '\"\"\"', '''';"},
		{"literal_unicode_escape", `SELECT '\u000A', '\\u000A';`},
		{"trailing_backslash", "SELECT 'x';\\"},
		{"unicode_bom", "\ufeffSELECT 'Автор 中文 🚀 e\u0301 \u200d \u2028 \u2029';"},
		{"nul_and_all_c0", "SELECT '" + c0.String() + "';"},
	}
	queries := make([]model.AnalyzedQuery, len(cases))
	for i, tc := range cases {
		queries[i] = model.AnalyzedQuery{Name: fmt.Sprintf("Case%02d", i), Command: model.Exec, SQL: tc.sql}
	}
	files, err := Generate(&model.AnalysisResult{Queries: queries}, Options{Package: "literal", Runtime: "jdbc"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f.Name), f.Content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var program strings.Builder
	program.WriteString("package literal;\nimport java.lang.reflect.*; import java.nio.charset.StandardCharsets; import java.util.Base64;\npublic final class Main {\n")
	program.WriteString("  private static void check(String method, String expected) throws Exception { var client=(java.sql.Connection)Proxy.newProxyInstance(Main.class.getClassLoader(),new Class<?>[]{java.sql.Connection.class},(p,m,a)->{ if (!m.getName().equals(\"prepareStatement\")) throw new AssertionError(m); String actual=(String)a[0]; if (!Base64.getEncoder().encodeToString(actual.getBytes(StandardCharsets.UTF_8)).equals(expected)) throw new AssertionError(method+\" changed: \"+actual); throw new java.sql.SQLException(\"captured\"); }); try { Queries.class.getMethod(method).invoke(new Queries(client)); throw new AssertionError(\"no prepare\"); } catch (InvocationTargetException e) { if (!(e.getCause() instanceof java.sql.SQLException) || !e.getCause().getMessage().equals(\"captured\")) throw e; } }\n")
	program.WriteString("  public static void main(String[] args) throws Exception {\n")
	for i, tc := range cases {
		fmt.Fprintf(&program, "    check(\"case%02d\", \"%s\");\n", i, base64.StdEncoding.EncodeToString([]byte(tc.sql)))
	}
	program.WriteString("  }\n}\n")
	if err := os.WriteFile(filepath.Join(dir, "Main.java"), []byte(program.String()), 0600); err != nil {
		t.Fatal(err)
	}
	classes := filepath.Join(dir, "classes")
	compile := exec.Command("javac", "--release", "17", "-d", classes, "Queries.java", "Main.java")
	compile.Dir = dir
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("generated Java 17 source does not compile: %v\n%s\n%s", err, out, files[len(files)-1].Content)
	}
	run := exec.Command("java", "-cp", classes, "literal.Main")
	run.Dir = dir
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("generated Java SQL literal changed at runtime: %v\n%s", err, out)
	}
}

func TestJDBCUsesStandardPositionalParameters(t *testing.T) {
	querySQL := "-- name: GetAuthor :one\nSELECT id FROM authors WHERE id = $author_id;"
	files, err := Generate(&model.AnalysisResult{Queries: []model.AnalyzedQuery{{
		Name: "GetAuthor", Command: model.One, SQL: querySQL,
		Parameters: []model.Parameter{{Name: "author_id", Type: model.Type{Kind: "Uint64"}}},
		ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: model.Type{Kind: "Uint64"}}}}},
	}}}, Options{Package: "authors.jdbc", Runtime: "jdbc"})
	if err != nil {
		t.Fatal(err)
	}
	generated := string(files[len(files)-1].Content)
	for _, unwanted := range []string{"unwrap(", "DECLARE ", "wasNull("} {
		if strings.Contains(generated, unwanted) {
			t.Fatalf("unexpected %s in JDBC output", unwanted)
		}
	}
	wantPrepared := "client.prepareStatement(" + sqlLiteral("SELECT id FROM authors WHERE id = ?;") + ")"
	if !strings.Contains(generated, wantPrepared) {
		t.Fatalf("generated JDBC API did not use positional SQL:\n%s", generated)
	}
}

func TestGenerateRejectsInvalidContracts(t *testing.T) {
	utf8 := model.Type{Kind: "Utf8"}
	for _, tc := range []struct {
		name string
		in   *model.AnalysisResult
		opts Options
		want string
	}{
		{"nil", nil, Options{}, "nil analysis result"},
		{"diagnostics", &model.AnalysisResult{Diagnostics: []model.Diagnostic{{Message: "bad"}}}, Options{}, "analysis diagnostics"},
		{"package_keyword", &model.AnalysisResult{}, Options{Package: "bad.class"}, "invalid Java package"},
		{"package_empty_segment", &model.AnalysisResult{}, Options{Package: "bad..pkg"}, "invalid Java package"},
		{"framework_type_collision", &model.AnalysisResult{Catalog: model.Catalog{Tables: []model.Table{{Name: "illegal_argument_exception"}}}}, Options{}, "type name collision"},
		{"package_java_namespace", &model.AnalysisResult{}, Options{Package: "java.sqlc"}, "java packages are reserved"},
		{"runtime", &model.AnalysisResult{}, Options{Runtime: "unknown"}, "unsupported Java runtime"},
		{"removed_spring_runtime", &model.AnalysisResult{}, Options{Runtime: "spring"}, "unsupported Java runtime"},
		{"removed_hibernate_runtime", &model.AnalysisResult{}, Options{Runtime: "hibernate"}, "unsupported Java runtime"},
		{"unsupported_parameter", &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Bad", Command: model.Exec, Parameters: []model.Parameter{{Name: "p", Type: model.Type{Kind: "Tuple"}}}}}}, Options{}, "unsupported Java type"},
		{"unsupported_result", &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Bad", Command: model.One, ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "value", Type: model.Type{Kind: "List"}}}}}}}}, Options{}, "unsupported Java type"},
		{"execrows", &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Bad", Command: model.ExecRows}}}, Options{}, "does not support"},
		{"one_no_results", &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Bad", Command: model.One}}}, Options{}, "requires one nonempty result set"},
		{"many_two_results", &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Bad", Command: model.Many, ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "value", Type: utf8}}}, {Columns: []model.Column{{Name: "other", Type: utf8}}}}}}}, Options{}, "requires one nonempty result set"},
		{"method_collision", &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Get_User", Command: model.Exec}, {Name: "getUser", Command: model.Exec}}}, Options{}, "method name collision"},
		{"parameter_collision", &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Bad", Command: model.Exec, Parameters: []model.Parameter{{Name: "a-b", Type: utf8}, {Name: "a_b", Type: utf8}}}}}, Options{}, "parameter name collision"},
		{"parameter_shadows_client", &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "GetAuthor", Command: model.Exec, Parameters: []model.Parameter{{Name: "client", Type: utf8}}}}}, Options{}, "parameter name collision"},
		{"field_collision", &model.AnalysisResult{Catalog: model.Catalog{Tables: []model.Table{{Name: "items", Columns: []model.Column{{Name: "a-b", Type: utf8}, {Name: "a_b", Type: utf8}}}}}}, Options{}, "field name collision"},
		{"record_collision", &model.AnalysisResult{Catalog: model.Catalog{Tables: []model.Table{{Name: "get_author_row", Columns: []model.Column{{Name: "id", Type: utf8}}}}}, Queries: []model.AnalyzedQuery{{Name: "get_author", Command: model.One, ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: utf8}}}}}}}, Options{}, "type name collision"},
		{"invalid_utf8", &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Bad", Command: model.Exec, SQL: string([]byte{0xff})}}}, Options{}, "must be valid UTF-8"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Generate(tc.in, tc.opts)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Generate() error = %v, want %q", err, tc.want)
			}
		})
	}
}

// This opt-in integration test compiles one all-scalar generated query against
// the exact dependency pins in each authors Maven profile. It intentionally
// uses the real provider APIs, not local stubs.
func TestAllSupportedScalarsCompileAgainstAuthorsMavenProfiles(t *testing.T) {
	maven := os.Getenv("SQLC_YDB_TEST_MAVEN")
	if maven == "" {
		t.Skip("set SQLC_YDB_TEST_MAVEN to compile against the authors Maven profiles")
	}
	types := []model.Type{
		{Kind: "Bool"}, {Kind: "Int8"}, {Kind: "Uint8"}, {Kind: "Int16"}, {Kind: "Uint16"},
		{Kind: "Int32"}, {Kind: "Uint32"}, {Kind: "Int64"}, {Kind: "Uint64"}, {Kind: "Float"},
		{Kind: "Double"}, {Kind: "Utf8"}, {Kind: "String"}, {Kind: "Json"}, {Kind: "Timestamp"},
	}
	var parameters []model.Parameter
	var columns []model.Column
	for _, typ := range types {
		name := strings.ToLower(typ.Kind)
		parameters = append(parameters, model.Parameter{Name: name, Type: typ}, model.Parameter{Name: "optional_" + name, Type: model.Optional(typ)})
		columns = append(columns, model.Column{Name: name, Type: typ}, model.Column{Name: "optional_" + name, Type: model.Optional(typ)})
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	parentPom, err := os.ReadFile(filepath.Join(repoRoot, "examples", "authors", "java", "pom.xml"))
	if err != nil {
		t.Fatal(err)
	}
	profiles := []struct{ runtime, module, pkg string }{
		{"ydb", "native", "synthetic.nativeapi"},
		{"jdbc", "jdbc", "synthetic.jdbc"},
	}
	for _, profile := range profiles {
		t.Run(profile.runtime, func(t *testing.T) {
			files, err := Generate(&model.AnalysisResult{Queries: []model.AnalyzedQuery{{
				Name: "AllScalars", Command: model.One, SQL: "SELECT 1;", Parameters: parameters, ResultSets: []model.ResultSet{{Columns: columns}},
			}, batchQuery(), optionalBatchQuery(), listBooksQuery(), declaredBatchQuery(), declaredMixedQuery()}}, Options{Package: profile.pkg, Runtime: profile.runtime})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "pom.xml"), parentPom, 0600); err != nil {
				t.Fatal(err)
			}
			modulePom, err := os.ReadFile(filepath.Join(repoRoot, "examples", "authors", "java", profile.module, "pom.xml"))
			if err != nil {
				t.Fatal(err)
			}
			moduleDir := filepath.Join(dir, profile.module)
			if err := os.MkdirAll(filepath.Join(moduleDir, "src", "main", "java", filepath.FromSlash(strings.ReplaceAll(profile.pkg, ".", "/"))), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(moduleDir, "pom.xml"), modulePom, 0600); err != nil {
				t.Fatal(err)
			}
			for _, file := range files {
				if err := os.WriteFile(filepath.Join(moduleDir, "src", "main", "java", filepath.FromSlash(strings.ReplaceAll(profile.pkg, ".", "/")), file.Name), file.Content, 0600); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command(maven, "-q", "-DskipTests", "compile")
			cmd.Dir = moduleDir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("generated %s all-scalar API does not compile against authors Maven pins: %v\n%s", profile.runtime, err, out)
			}
		})
	}
}

// This test executes generated JDBC code without a YDB server. Its proxy only
// supplies the standard JDBC surface; parameter binding is delegated to the
// published driver's InMemoryQuery to verify actual positional types.
func TestGenerateMixedScripts(t *testing.T) {
	analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));"}}, []model.Source{{Name: "queries.sql", Text: `-- name: ReadAndClear :one
DELETE FROM records; SELECT 42 AS answer; DELETE FROM records;
-- name: ReadManyAndClear :many
DELETE FROM records; SELECT 42 AS answer; DELETE FROM records;`}})
	if err != nil {
		t.Fatal(err)
	}
	for _, runtime := range []string{"ydb", "jdbc"} {
		t.Run(runtime, func(t *testing.T) {
			files, err := Generate(analysis, Options{Package: "scripts", Runtime: runtime})
			if err != nil {
				t.Fatal(err)
			}
			code := string(files[len(files)-1].Content)
			if strings.Count(code, "DELETE FROM records; SELECT 42 AS answer; DELETE FROM records;") != 2 {
				t.Fatalf("mixed script text changed: %s", code)
			}
			if runtime != "ydb" && strings.Contains(code, ".executeQuery()") {
				t.Fatalf("script execution must traverse JDBC update counts: %s", code)
			}
			if runtime != "ydb" && !strings.Contains(code, "Expected one result set") {
				t.Fatalf("script execution must reject a mismatched result count: %s", code)
			}
		})
	}
}

func TestGeneratedJDBCUsesTypedDriverValuesAndGuardsUnsignedRanges(t *testing.T) {
	maven := os.Getenv("SQLC_YDB_TEST_MAVEN")
	if maven == "" {
		t.Skip("set SQLC_YDB_TEST_MAVEN to execute the published JDBC binding regression")
	}
	queries := []model.AnalyzedQuery{
		{
			Name: "Bind", Command: model.Exec, SQL: "SELECT $author_id, $maybe_id, $title, $payload;",
			Parameters: []model.Parameter{
				{Name: "author_id", Type: model.Type{Kind: "Uint64"}},
				{Name: "maybe_id", Type: model.Optional(model.Type{Kind: "Uint16"})},
				{Name: "title", Type: model.Type{Kind: "Utf8"}},
				{Name: "payload", Type: model.Type{Kind: "String"}},
			},
		},
		{Name: "Bad8", Command: model.Exec, SQL: "SELECT 1;", Parameters: []model.Parameter{{Name: "value", Type: model.Type{Kind: "Uint8"}}}},
		{Name: "Bad16", Command: model.Exec, SQL: "SELECT 1;", Parameters: []model.Parameter{{Name: "value", Type: model.Optional(model.Type{Kind: "Uint16"})}}},
		{Name: "Bad32", Command: model.Exec, SQL: "SELECT 1;", Parameters: []model.Parameter{{Name: "value", Type: model.Type{Kind: "Uint32"}}}},
	}
	queries = append(queries, model.AnalyzedQuery{
		Name: "Nullable", Command: model.One, SQL: "SELECT $number AS number, $flag AS flag, $payload AS payload;",
		Parameters: []model.Parameter{
			{Name: "number", Type: model.Optional(model.Type{Kind: "Int64"})},
			{Name: "flag", Type: model.Optional(model.Type{Kind: "Bool"})},
			{Name: "payload", Type: model.Optional(model.Type{Kind: "String"})},
		},
		ResultSets: []model.ResultSet{{Columns: []model.Column{
			{Name: "number", Type: model.Optional(model.Type{Kind: "Int64"})},
			{Name: "flag", Type: model.Optional(model.Type{Kind: "Bool"})},
			{Name: "payload", Type: model.Optional(model.Type{Kind: "String"})},
		}}},
	})
	analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));"}}, []model.Source{{Name: "queries.sql", Text: "-- name: ReadAndClear :one\nDELETE FROM records; SELECT 42 AS value; DELETE FROM records;\n-- name: ReadManyAndClear :many\nDELETE FROM records; SELECT 42 AS value; DELETE FROM records;"}})
	if err != nil {
		t.Fatal(err)
	}
	queries = append(queries, analysis.Queries...)
	queries = append(queries, batchQuery(), optionalBatchQuery(), listBooksQuery(), declaredBatchQuery(), declaredMixedQuery())
	files, err := Generate(&model.AnalysisResult{Queries: queries}, Options{Package: "synthetic.jdbc", Runtime: "jdbc"})
	if err != nil {
		t.Fatal(err)
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	parentPom, err := os.ReadFile(filepath.Join(repoRoot, "examples", "authors", "java", "pom.xml"))
	if err != nil {
		t.Fatal(err)
	}
	jdbcPom, err := os.ReadFile(filepath.Join(repoRoot, "examples", "authors", "java", "jdbc", "pom.xml"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pom.xml"), parentPom, 0600); err != nil {
		t.Fatal(err)
	}
	moduleDir := filepath.Join(dir, "jdbc")
	packageDir := filepath.Join(moduleDir, "src", "main", "java", "synthetic", "jdbc")
	if err := os.MkdirAll(packageDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "pom.xml"), jdbcPom, 0600); err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(packageDir, file.Name), file.Content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	const program = `package synthetic.jdbc;

import java.lang.reflect.Proxy;
import java.sql.Connection;
import java.sql.PreparedStatement;
import java.sql.Types;
import java.util.Properties;

import tech.ydb.jdbc.common.YdbTypes;
import tech.ydb.jdbc.query.QueryKey;
import tech.ydb.jdbc.query.YdbQuery;
import tech.ydb.jdbc.settings.YdbQueryProperties;
import tech.ydb.table.query.Params;
import tech.ydb.table.values.DecimalType;
import tech.ydb.table.values.OptionalType;
import tech.ydb.table.values.PrimitiveType;
import tech.ydb.table.values.PrimitiveValue;

public final class Main {
    private Main() { }

    public static void main(String[] args) throws Exception {
        verifyTerminalStatus();
        Queries guarded = new Queries(refusingConnection());
        expectRange(() -> guarded.bad8(-1));
        expectRange(() -> guarded.bad8(256));
        expectRange(() -> guarded.bad16(-1));
        expectRange(() -> guarded.bad16(65536));
        expectRange(() -> guarded.bad32(-1L));
        expectRange(() -> guarded.bad32(4294967296L));

        YdbTypes types = new YdbTypes(false, DecimalType.getDefault());
        var scalarQuery = YdbQuery.parseQuery(new QueryKey("DECLARE $j AS Json; DECLARE $u AS Uint8; SELECT $j, $u;"), new YdbQueryProperties(new Properties()), types);
        var scalarPrepared = new tech.ydb.jdbc.query.params.PreparedQuery(types, scalarQuery, java.util.Map.of("$j", PrimitiveType.Json, "$u", PrimitiveType.Uint8));
        scalarPrepared.setParam("j", "{}", Types.VARCHAR);
        scalarPrepared.setParam("u", 255, Types.INTEGER);
        check(scalarPrepared.getCurrentParams().values().get("$j").equals(PrimitiveValue.newJson("{}")), "declared Json rejected setString");
        check(scalarPrepared.getCurrentParams().values().get("$u").equals(PrimitiveValue.newUint8(255)), "declared Uint8 rejected setInt");
        var setterQuery = YdbQuery.parseQuery(new QueryKey("SELECT ?, ?, ?;"), new YdbQueryProperties(new Properties()), types);
        var setters = new tech.ydb.jdbc.query.params.InMemoryQuery(setterQuery, false);
        setters.setParam(1, "{}", Types.VARCHAR);
        setters.setParam(2, 255, Types.INTEGER);
        setters.setParam(3, java.sql.Timestamp.from(java.time.Instant.EPOCH), Types.TIMESTAMP);
        check(setters.getCurrentParams().values().get("$jp1").getType().equals(PrimitiveType.Text), "setString does not infer Json");
        check(setters.getCurrentParams().values().get("$jp2").getType().equals(PrimitiveType.Int32), "setInt does not infer Uint8");
        check(setters.getCurrentParams().values().get("$jp3").equals(PrimitiveValue.newTimestamp(java.time.Instant.EPOCH)), "setTimestamp lost the timestamp type");
        for (PrimitiveType optionalType : new PrimitiveType[]{PrimitiveType.Text, PrimitiveType.Bytes, PrimitiveType.Json, PrimitiveType.Timestamp}) {
            var empty = OptionalType.of(optionalType).emptyValue();
            var reader = tech.ydb.table.result.impl.ProtoValueReaders.forTypedValue(tech.ydb.proto.ValueProtos.TypedValue.newBuilder().setType(empty.getType().toPb()).setValue(empty.toPb()).build());
            Object actual = optionalType == PrimitiveType.Text ? reader.getText() : optionalType == PrimitiveType.Bytes ? reader.getBytes() : optionalType == PrimitiveType.Json ? reader.getJson() : reader.getTimestamp();
            check(actual == null, "optional reference getter did not return null");
        }

        YdbQuery query = YdbQuery.parseQuery(new QueryKey("SELECT ?, ?, ?, ?;"), new YdbQueryProperties(new Properties()), types);
        tech.ydb.jdbc.query.params.InMemoryQuery bound = new tech.ydb.jdbc.query.params.InMemoryQuery(query, false);
        new Queries(bindingConnection(bound)).bind(-1L, null, "typed text", new byte[] { 0, 1, (byte) 255 });

        Params values = bound.getCurrentParams();
        check(values.values().size() == 4, "wrong parameter count");
        check(PrimitiveValue.newUint64(-1L).equals(values.values().get("$jp1")), "Uint64 lost its type or name");
        check(OptionalType.of(PrimitiveType.Uint16).emptyValue().equals(values.values().get("$jp2")), "optional null lost its declared type");
        check(PrimitiveValue.newText("typed text").equals(values.values().get("$jp3")), "Utf8 lost its type");
        check(PrimitiveValue.newBytes(new byte[] { 0, 1, (byte) 255 }).equals(values.values().get("$jp4")), "String lost its binary type");
        YdbQuery batch = YdbQuery.parseQuery(new QueryKey("INSERT INTO books SELECT * FROM AS_TABLE(?);"), new YdbQueryProperties(new Properties()), types);
        tech.ydb.jdbc.query.params.InMemoryQuery batchBound = new tech.ydb.jdbc.query.params.InMemoryQuery(batch, false);
        Queries batchQueries = new Queries(bindingConnection(batchBound));
        batchQueries.createBooks(java.util.List.of());
        tech.ydb.table.values.ListValue emptyBatch = (tech.ydb.table.values.ListValue) batchBound.getCurrentParams().values().get("$jp1");
        check(emptyBatch.size() == 0, "emptyBatch batch contains rows");
        tech.ydb.table.values.StructType itemType = (tech.ydb.table.values.StructType) emptyBatch.getType().getItemType();
        check(itemType.getMembersCount() == 8, "emptyBatch batch lost struct schema");
        check(itemType.getMemberType(itemType.getMemberIndex("tags")).equals(PrimitiveType.Json), "Json field lost type");
        batchQueries.createBooks(java.util.List.of(new CreateBooksBooksItem(-1L, 42L, "isbn", "paper", "Book", 2026, java.time.Instant.EPOCH, "{\"ok\":true}")));
        tech.ydb.table.values.ListValue filledBatch = (tech.ydb.table.values.ListValue) batchBound.getCurrentParams().values().get("$jp1");
        check(filledBatch.size() == 1 && filledBatch.getType().equals(emptyBatch.getType()), "batch changed declared type");
        tech.ydb.table.values.StructValue book = (tech.ydb.table.values.StructValue) filledBatch.get(0);
        check(book.getMemberValue(itemType.getMemberIndex("book_id")).equals(PrimitiveValue.newUint64(-1L)), "batch Uint64 lost unsigned bits");
        check(book.getMemberValue(itemType.getMemberIndex("tags")).equals(PrimitiveValue.newJson("{\"ok\":true}")), "batch Json lost bytes");
        java.util.Map<String, tech.ydb.table.values.Value<?>> expectedBook = java.util.Map.ofEntries(
            java.util.Map.entry("book_id", PrimitiveValue.newUint64(-1L)), java.util.Map.entry("author_id", PrimitiveValue.newUint64(42L)),
            java.util.Map.entry("isbn", PrimitiveValue.newText("isbn")), java.util.Map.entry("book_type", PrimitiveValue.newText("paper")),
            java.util.Map.entry("title", PrimitiveValue.newText("Book")), java.util.Map.entry("year", PrimitiveValue.newInt32(2026)),
            java.util.Map.entry("available", PrimitiveValue.newTimestamp(java.time.Instant.EPOCH)), java.util.Map.entry("tags", PrimitiveValue.newJson("{\"ok\":true}")));
        String previousName = "";
        for (int i = 0; i < itemType.getMembersCount(); i++) {
            String field = itemType.getMemberName(i);
            check(previousName.compareTo(field) < 0, "SDK struct type is not canonical by name");
            check(itemType.toPb().getStructType().getMembers(i).getName().equals(field), "wire type changed field order");
            check(book.toPb().getItems(i).equals(expectedBook.get(field).toPb()), "wire value mismatched field " + field);
            previousName = field;
        }
        expectRange(() -> guarded.optionalBooks(java.util.List.of(new OptionalBooksBooksItem(-1, null))));
        YdbQuery optionalBatch = YdbQuery.parseQuery(new QueryKey("SELECT * FROM AS_TABLE(?);"), new YdbQueryProperties(new Properties()), types);
        tech.ydb.jdbc.query.params.InMemoryQuery optionalBound = new tech.ydb.jdbc.query.params.InMemoryQuery(optionalBatch, false);
        new Queries(bindingConnection(optionalBound)).optionalBooks(java.util.List.of(new OptionalBooksBooksItem(null, null)));
        tech.ydb.table.values.ListValue optionalRows = (tech.ydb.table.values.ListValue) optionalBound.getCurrentParams().values().get("$jp1");
        tech.ydb.table.values.StructValue optionalRow = (tech.ydb.table.values.StructValue) optionalRows.get(0);
        check(optionalRow.getMemberValue(optionalRow.getType().getMemberIndex("rank")).equals(OptionalType.of(PrimitiveType.Uint8).emptyValue()), "optional batch null lost type");
        String declaredSQL = "DECLARE $books AS List<Struct<book_id:Uint64,author_id:Uint64,isbn:Utf8,book_type:Utf8,title:Utf8,year:Int32,available:Timestamp,tags:Json>>;\nINSERT INTO books SELECT * FROM AS_TABLE($books);";
        var declaredQuery = YdbQuery.parseQuery(new QueryKey(declaredSQL), new YdbQueryProperties(new Properties()), types);
        var declared = new tech.ydb.jdbc.query.params.PreparedQuery(types, declaredQuery, java.util.Map.of("$books", emptyBatch.getType()));
        var autoBatch = tech.ydb.jdbc.query.params.BatchedQuery.tryCreateBatched(types, declaredQuery, java.util.Map.of("$books", emptyBatch.getType()));
        check(autoBatch != null && autoBatch.parametersCount() == 8, "AUTO did not flatten struct members into parameters");
        try { autoBatch.setParam("books", emptyBatch, Types.JAVA_OBJECT); throw new AssertionError("AUTO unexpectedly accepted the named list"); } catch (java.sql.SQLException expected) { }
        try { autoBatch.setParam(1, emptyBatch, Types.JAVA_OBJECT); throw new AssertionError("AUTO unexpectedly accepted a positional list"); } catch (java.sql.SQLException expected) { }

        var declaredQueries = new Queries(declaredConnection(declared, declaredSQL));
        declaredQueries.declaredBooks(java.util.List.of());
        check(declared.getQueryText(declared.getCurrentParams()).equals(declaredSQL), "driver changed explicit DECLARE");
        check(declared.getCurrentParams().values().get("$books").getType().equals(emptyBatch.getType()), "declared empty list lost schema or name");
        declaredQueries.declaredBooks(java.util.List.of(new DeclaredBooksBooksItem(-1L,42L,"isbn","paper","Book",2026,java.time.Instant.EPOCH,"[]")));
        check(((tech.ydb.table.values.ListValue)declared.getCurrentParams().values().get("$books")).size()==1,"declared row missing");
        String mixedSQL = "DECLARE $a AS Utf8;\nDECLARE $z AS Uint64;\nSELECT $z, $a, $z;";
        var mixedQuery = YdbQuery.parseQuery(new QueryKey(mixedSQL), new YdbQueryProperties(new Properties()), types);
        var mixed = new tech.ydb.jdbc.query.params.PreparedQuery(types, mixedQuery, java.util.Map.of("$a", PrimitiveType.Text, "$z",PrimitiveType.Uint64));
        new Queries(declaredConnection(mixed,mixedSQL)).declaredMixed("text",-1L);
        check(mixed.getQueryText(mixed.getCurrentParams()).equals(mixedSQL),"mixed declarations changed");
        check(mixed.getCurrentParams().values().get("$z").equals(PrimitiveValue.newUint64(-1L)),"named parameter lost type");



        String endpoint = System.getenv("YDB_CONNECTION_STRING");
        if (endpoint != null && !endpoint.isBlank()) {
            try (Connection connection = java.sql.DriverManager.getConnection("jdbc:ydb:" + endpoint)) {
                Queries live = new Queries(connection);
                NullableRow empty = live.nullable(null, null, null).orElseThrow();
                check(empty.number() == null && empty.flag() == null && empty.payload() == null, "nullable getters lost NULL");
                NullableRow filled = live.nullable(42L, false, new byte[]{0, (byte)255}).orElseThrow();
                check(filled.number() == 42L && !filled.flag() && java.util.Arrays.equals(filled.payload(), new byte[]{0, (byte)255}), "nullable getters lost values");
            }
        }
    }


    private static Connection declaredConnection(tech.ydb.jdbc.query.params.PreparedQuery query, String expectedSQL) {
        var statement = (tech.ydb.jdbc.YdbPreparedStatement) Proxy.newProxyInstance(Main.class.getClassLoader(), new Class<?>[]{tech.ydb.jdbc.YdbPreparedStatement.class}, (proxy,method,args) -> {
            if (method.getName().equals("setObject") || method.getName().equals("setString")) { query.setParam((String)args[0],args[1],method.getName().equals("setString") ? Types.VARCHAR : Types.JAVA_OBJECT); return null; }
            if (method.getName().equals("execute")) return false;
            if (method.getName().equals("close")) return null;
            throw new AssertionError(method);
        });
        return (Connection) Proxy.newProxyInstance(Main.class.getClassLoader(),new Class<?>[]{tech.ydb.jdbc.YdbConnection.class},(proxy,method,args) -> {
            if (method.getName().equals("unwrap")) return proxy;
            if (method.getName().equals("prepareStatement")) {
                check(args[0].equals(expectedSQL),"source SQL was rewritten: " + args[0]);
                check(args.length==2 && args[1]==tech.ydb.jdbc.YdbPrepareMode.DATA_QUERY,"auto-batch mode was not disabled");
                return statement;
            }
            throw new AssertionError(method);
        });
    }

    private static void verifyTerminalStatus() throws Exception {
        for (boolean many : new boolean[]{false, true}) for (boolean leading : new boolean[]{false, true}) for (int mode = 0; mode < 8; mode++) {
            int selectedMode = mode;
            int[] next = {0};
            int[] position = {leading ? 0 : 1};
            boolean[] closed = {false, false};
            var terminal = new java.sql.SQLException("late DML failure");
            var rows = (java.sql.ResultSet) Proxy.newProxyInstance(Main.class.getClassLoader(), new Class<?>[]{java.sql.ResultSet.class}, (proxy, method, args) -> {
                if (method.getName().equals("next")) {
                    next[0]++;
                    if (selectedMode == 2 || selectedMode == 1 && next[0] > 1) throw terminal;
                    return selectedMode == 0 ? next[0] <= 2 : selectedMode != 3 && selectedMode != 5 && next[0] == 1;
                }
                if (method.getName().equals("getInt")) return 42;
                if (method.getName().equals("close")) { closed[0] = true; return null; }
                throw new AssertionError(method);
            });
            var statement = (PreparedStatement) Proxy.newProxyInstance(Main.class.getClassLoader(), new Class<?>[]{PreparedStatement.class}, (proxy, method, args) -> {
                if (method.getName().equals("execute")) return !leading && selectedMode != 7;
                if (method.getName().equals("getResultSet")) return selectedMode != 7 && (position[0] == 1 || selectedMode == 6 && position[0] == 2) ? rows : null;
                if (method.getName().equals("getUpdateCount")) return position[0] >= 3 ? -1 : 0;
                if (method.getName().equals("getMoreResults")) {
                    position[0]++;
                    if (position[0] == 2 && (selectedMode == 4 || selectedMode == 5)) throw terminal;
                    return selectedMode != 7 && (position[0] == 1 || selectedMode == 6 && position[0] == 2);
                }
                if (method.getName().equals("close")) { closed[1] = true; return null; }
                throw new AssertionError(method);
            });
            var connection = (Connection) Proxy.newProxyInstance(Main.class.getClassLoader(), new Class<?>[]{Connection.class}, (proxy, method, args) -> {
                if (method.getName().equals("prepareStatement")) {
                    check(args[0].equals("DELETE FROM records; SELECT 42 AS value; DELETE FROM records;"), "script changed");
                    return statement;
                }
                throw new AssertionError("borrowed connection: " + method);
            });
            try {
                if (many) {
                    var values = new Queries(connection).readManyAndClear();
                    check(mode == 0 || mode == 3, "late execution failure was ignored");
                    if (mode == 0) check(values.size() == 2 && values.get(0).value() == 42, "many result changed");
                    else check(values.isEmpty(), "empty many result changed");
                } else {
                    var value = new Queries(connection).readAndClear();
                    check(mode == 0 || mode == 3, "late execution failure was ignored");
                    if (mode == 0) check(value.orElseThrow().value() == 42 && next[0] == 3, "first-row result returned before terminal status");
                    else check(value.isEmpty(), "empty result contract changed");
                }
                check(position[0] == 3, "trailing update count was not consumed");
            } catch (java.sql.SQLException error) {
                if (mode == 6 || mode == 7) check(error.getMessage().equals("Expected one result set"), "wrong result count error");
                else check((mode == 1 || mode == 2 || mode == 4 || mode == 5) && error == terminal, "wrong terminal error");
            }
            check((closed[0] || mode == 7) && closed[1], "owned JDBC resources leaked");
        }
    }

    private static Connection refusingConnection() {
        return (Connection) Proxy.newProxyInstance(Main.class.getClassLoader(), new Class<?>[] { Connection.class },
                (proxy, method, args) -> { throw new AssertionError("range guard reached Connection." + method.getName()); });
    }

    private static Connection bindingConnection(tech.ydb.jdbc.query.params.InMemoryQuery query) {
        PreparedStatement statement = (PreparedStatement) Proxy.newProxyInstance(
                Main.class.getClassLoader(), new Class<?>[] { PreparedStatement.class }, (proxy, method, args) -> {
                    if (method.getName().startsWith("set")) {
                        int type = switch (method.getName()) {
                            case "setString" -> Types.VARCHAR;
                            case "setBytes" -> Types.VARBINARY;
                            case "setObject" -> Types.JAVA_OBJECT;
                            default -> throw new AssertionError(method);
                        };
                        query.setParam((int) args[0], args[1], type);
                        return null;
                    }
                    if (method.getName().equals("execute")) return false;
                    if (method.getName().equals("close")) return null;
                    throw new AssertionError("unexpected PreparedStatement." + method.getName());
                });
        return (Connection) Proxy.newProxyInstance(Main.class.getClassLoader(), new Class<?>[] { Connection.class }, (proxy, method, args) -> {
            if (method.getName().equals("prepareStatement")) return statement;
            if (method.getName().equals("close")) return null;
            throw new AssertionError("unexpected Connection." + method.getName());
        });
    }

    private static void expectRange(ThrowingRun run) throws Exception {
        try { run.run(); } catch (IllegalArgumentException expected) { return; }
        throw new AssertionError("expected IllegalArgumentException");
    }

    private static void check(boolean condition, String message) {
        if (!condition) throw new AssertionError(message);
    }

    @FunctionalInterface private interface ThrowingRun { void run() throws Exception; }
}
`
	if err := os.WriteFile(filepath.Join(packageDir, "Main.java"), []byte(program), 0600); err != nil {
		t.Fatal(err)
	}
	classpathFile := filepath.Join(moduleDir, "classpath")
	cmd := exec.Command(maven, "-q", "-DskipTests", "compile", "dependency:build-classpath", "-Dmdep.outputFile="+classpathFile)
	cmd.Dir = moduleDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated JDBC binding fixture did not compile: %v\n%s", err, out)
	}
	classpath, err := os.ReadFile(classpathFile)
	if err != nil {
		t.Fatal(err)
	}
	run := exec.Command("java", "-cp", filepath.Join(moduleDir, "target", "classes")+string(os.PathListSeparator)+strings.TrimSpace(string(classpath)), "synthetic.jdbc.Main")
	run.Dir = moduleDir
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("generated JDBC binding fixture failed against the published driver: %v\n%s", err, out)
	}
}

func TestNullableJavaGettersAndTextBinding(t *testing.T) {
	q := model.AnalyzedQuery{Name: "Read", Command: model.One, SQL: "SELECT $bio;", Parameters: []model.Parameter{{Name: "bio", Type: model.Optional(model.Type{Kind: "Utf8"})}}, ResultSets: []model.ResultSet{{Columns: []model.Column{
		{Name: "bio", Type: model.Optional(model.Type{Kind: "Utf8"})},
		{Name: "number", Type: model.Optional(model.Type{Kind: "Int64"})},
	}}}}
	for _, runtime := range []string{"jdbc", "ydb"} {
		files, err := Generate(&model.AnalysisResult{Queries: []model.AnalyzedQuery{q}}, Options{Package: "nullable", Runtime: runtime})
		if err != nil {
			t.Fatal(err)
		}
		output := string(files[len(files)-1].Content)
		expected := []string{"_prepared.setString(1, bio)", "_rows.getObject(2, Long.class)"}
		if runtime == "ydb" {
			expected = []string{"PrimitiveValue.newText(bio).makeOptional()", "String _value0 = _rows.getColumn(0).getText();"}
		}
		for _, fragment := range expected {
			if !strings.Contains(output, fragment) {
				t.Fatalf("missing %s in %s", fragment, output)
			}
		}
	}
}

func TestNativeExecDoesNotMaterializeResults(t *testing.T) {
	files, err := Generate(&model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Delete", Command: model.Exec, SQL: "DELETE FROM authors;"}}}, Options{Package: "authors", Runtime: "ydb"})
	if err != nil {
		t.Fatal(err)
	}
	output := string(files[len(files)-1].Content)
	if strings.Contains(output, "QueryReader") || !strings.Contains(output, ".execute().join().getStatus().expectSuccess();") {
		t.Fatalf("native exec must execute without collecting results and check status:\n%s", output)
	}
}
