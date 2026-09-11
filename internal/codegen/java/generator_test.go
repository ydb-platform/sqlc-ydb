package java

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
		Name: "GetAuthor", Command: model.One, SQL: querySQL, SQLWithoutDeclarations: querySQL,
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
		{"unsupported_parameter", &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Bad", Command: model.Exec, Parameters: []model.Parameter{{Name: "p", Type: model.Type{Kind: "Json"}}}}}}, Options{}, "unsupported Java type"},
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
		{Kind: "Double"}, {Kind: "Utf8"}, {Kind: "String"},
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
			}}}, Options{Package: profile.pkg, Runtime: profile.runtime})
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
        Queries guarded = new Queries(refusingConnection());
        expectRange(() -> guarded.bad8(-1));
        expectRange(() -> guarded.bad8(256));
        expectRange(() -> guarded.bad16(-1));
        expectRange(() -> guarded.bad16(65536));
        expectRange(() -> guarded.bad32(-1L));
        expectRange(() -> guarded.bad32(4294967296L));

        YdbTypes types = new YdbTypes(false, DecimalType.getDefault());
        YdbQuery query = YdbQuery.parseQuery(new QueryKey("SELECT ?, ?, ?, ?;"), new YdbQueryProperties(new Properties()), types);
        tech.ydb.jdbc.query.params.InMemoryQuery bound = new tech.ydb.jdbc.query.params.InMemoryQuery(query, false);
        new Queries(bindingConnection(bound)).bind(-1L, null, "typed text", new byte[] { 0, 1, (byte) 255 });

        Params values = bound.getCurrentParams();
        check(values.values().size() == 4, "wrong parameter count");
        check(PrimitiveValue.newUint64(-1L).equals(values.values().get("$jp1")), "Uint64 lost its type or name");
        check(OptionalType.of(PrimitiveType.Uint16).emptyValue().equals(values.values().get("$jp2")), "optional null lost its declared type");
        check(PrimitiveValue.newText("typed text").equals(values.values().get("$jp3")), "Utf8 lost its type");
        check(PrimitiveValue.newBytes(new byte[] { 0, 1, (byte) 255 }).equals(values.values().get("$jp4")), "String lost its binary type");
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

func TestJDBCParameterOccurrencesPreserveSQLText(t *testing.T) {
	q := model.AnalyzedQuery{
		SQLWithoutDeclarations: "-- name: Check :one\n$local = 'Привет $value';\nSELECT `$value`, $local, $value, $other, $value; -- $value\n",
		Parameters:             []model.Parameter{{Name: "other"}, {Name: "value"}},
	}
	sql, bindings := jdbcSQL(q)
	want := "$local = 'Привет $value';\nSELECT `$value`, $local, ?, ?, ?; -- $value\n"
	if sql != want || fmt.Sprint(bindings) != "[1 0 1]" {
		t.Fatalf("SQL=%q bindings=%v", sql, bindings)
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
