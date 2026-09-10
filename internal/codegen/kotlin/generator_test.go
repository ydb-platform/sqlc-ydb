package kotlin

import (
	"encoding/base64"
	"fmt"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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
		{"package_keyword", &model.AnalysisResult{}, Options{Package: "bad.class"}, "invalid Kotlin package"},
		{"package_empty_segment", &model.AnalysisResult{}, Options{Package: "bad..pkg"}, "invalid Kotlin package"},
		{"framework_type_collision", &model.AnalysisResult{Catalog: model.Catalog{Tables: []model.Table{{Name: "illegal_argument_exception"}}}}, Options{}, "type name collision"},
		{"package_java_namespace", &model.AnalysisResult{}, Options{Package: "java.sqlc"}, "namespaces are reserved"},
		{"runtime", &model.AnalysisResult{}, Options{Runtime: "unknown"}, "unsupported Kotlin runtime"},
		{"unsupported_parameter", &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Bad", Command: model.Exec, Parameters: []model.Parameter{{Name: "p", Type: model.Type{Kind: "Json"}}}}}}, Options{}, "unsupported Kotlin type"},
		{"unsupported_result", &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Bad", Command: model.One, ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "value", Type: model.Type{Kind: "List"}}}}}}}}, Options{}, "unsupported Kotlin type"},
		{"execrows", &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Bad", Command: model.ExecRows}}}, Options{}, "does not support"},
		{"one_no_results", &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Bad", Command: model.One}}}, Options{}, "requires one nonempty result set"},
		{"many_two_results", &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Bad", Command: model.Many, ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "value", Type: utf8}}}, {Columns: []model.Column{{Name: "other", Type: utf8}}}}}}}, Options{}, "requires one nonempty result set"},
		{"method_collision", &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Get_User", Command: model.Exec}, {Name: "getUser", Command: model.Exec}}}, Options{}, "method name collision"},
		{"parameter_collision", &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Bad", Command: model.Exec, Parameters: []model.Parameter{{Name: "a-b", Type: utf8}, {Name: "a_b", Type: utf8}}}}}, Options{}, "parameter name collision"},
		{"parameter_shadows_sql_constant", &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "GetAuthor", Command: model.Exec, Parameters: []model.Parameter{{Name: "get_author_sql", Type: utf8}}}}}, Options{}, "parameter name collision"},
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

func TestNullableScalarModelsAndRuntimeOwnership(t *testing.T) {
	a := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "GetAuthor", Command: model.One, SQL: "SELECT $id;", Parameters: []model.Parameter{{Name: "id", Type: model.Type{Kind: "Uint64"}}}, ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "id", Type: model.Type{Kind: "Uint64"}}, {Name: "bio", Type: model.Optional(model.Type{Kind: "Utf8"})}}}}}}}
	for _, runtime := range []string{"", "native", "ydb", "jdbc", "exposed"} {
		t.Run(runtime, func(t *testing.T) {
			files, err := Generate(a, Options{Runtime: runtime})
			if err != nil {
				t.Fatal(err)
			}
			row := string(files[0].Content)
			source := string(files[len(files)-1].Content)
			for _, want := range []string{"data class GetAuthorRow", "val id: Long", "val bio: String?"} {
				if !strings.Contains(row, want) {
					t.Errorf("missing %q in %s", want, row)
				}
			}
			if !strings.Contains(source, "fun getAuthor(id: Long): GetAuthorRow?") {
				t.Fatal(source)
			}
			if runtime == "jdbc" || runtime == "exposed" {
				for _, want := range []string{".use { _prepared", ".use { _rows", "setObject(\"id\", PrimitiveValue.newUint64(id))", "if (_rows.wasNull()) null"} {
					if !strings.Contains(source, want) {
						t.Errorf("missing %q", want)
					}
				}
			} else if !strings.Contains(source, "TxMode.SERIALIZABLE_RW") {
				t.Fatal(source)
			}
			for _, bad := range []string{"client.close", "client.commit", "client.rollback"} {
				if strings.Contains(source, bad) {
					t.Fatalf("borrowed resource ownership violated: %s", bad)
				}
			}
		})
	}
}

func TestRejectsMalformedAndNestedOptionalTypes(t *testing.T) {
	for _, typ := range []model.Type{{Kind: "Optional"}, model.Optional(model.Optional(model.Type{Kind: "Utf8"})), {Kind: "Int64", Elem: &model.Type{Kind: "Utf8"}}} {
		_, err := Generate(&model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Bad", Command: model.Exec, Parameters: []model.Parameter{{Name: "v", Type: typ}}}}}, Options{})
		if err == nil || !strings.Contains(err.Error(), "unsupported Kotlin type") {
			t.Fatalf("type %s: %v", typ, err)
		}
	}
}

// This check evaluates the actual generated properties through Kotlin's compiler
// and JVM; string containment alone cannot detect interpolation or newline loss.
func TestSQLLiteralRoundTripsThroughKotlin(t *testing.T) {
	maven := os.Getenv("SQLC_YDB_TEST_MAVEN")
	if maven == "" {
		t.Skip("set SQLC_YDB_TEST_MAVEN for Kotlin compiler literal verification")
	}
	var controls strings.Builder
	for r := rune(0); r < 32; r++ {
		controls.WriteRune(r)
	}
	controls.WriteRune(127)
	cases := []string{"", "SELECT 1;", "\nSELECT 1;", "SELECT 1;\n", "\n\nSELECT 1;\n\n", "  SELECT\t1;  \n\t  \n", "SELECT 1;\r\n\r\n", `SELECT $value, '${name}', '$', '\u000A', '"""';`, "SELECT 'x';\\", "\ufeffSELECT 'Автор 中文 🚀 e\u0301 \u200d \u2028 \u2029';", controls.String()}
	queries := make([]model.AnalyzedQuery, len(cases))
	for i, sql := range cases {
		queries[i] = model.AnalyzedQuery{Name: fmt.Sprintf("Case%02d", i), Command: model.Exec, SQL: sql}
	}
	files, err := Generate(&model.AnalysisResult{Queries: queries}, Options{Package: "literal", Runtime: "jdbc"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "main", "kotlin")
	if err := os.MkdirAll(src, 0700); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		writeFile(t, filepath.Join(src, f.Name), f.Content)
	}
	var b strings.Builder
	b.WriteString("package literal\nimport java.lang.reflect.Proxy\nimport java.sql.Connection\nimport java.util.Base64\nfun main() {\n    val client = Proxy.newProxyInstance(Connection::class.java.classLoader, arrayOf(Connection::class.java)) { _, _, _ -> error(\"No SQL execution expected\") } as Connection\n    val queries = Queries(client)\n")
	for i, sql := range cases {
		fmt.Fprintf(&b, "    run { val field = Queries::class.java.getDeclaredField(\"case%02dSql\"); field.isAccessible = true; check(Base64.getEncoder().encodeToString((field.get(queries) as String).toByteArray(Charsets.UTF_8)) == \"%s\") { \"case%02d SQL bytes changed\" } }\n", i, base64.StdEncoding.EncodeToString([]byte(sql)), i)
	}
	b.WriteString("}\n")
	writeFile(t, filepath.Join(src, "Main.kt"), []byte(b.String()))
	writeFile(t, filepath.Join(dir, "pom.xml"), []byte(`
<project xmlns="http://maven.apache.org/POM/4.0.0"><modelVersion>4.0.0</modelVersion><groupId>test</groupId><artifactId>kotlin-literals</artifactId><version>1</version>
<properties><project.build.sourceEncoding>UTF-8</project.build.sourceEncoding></properties>
<dependencies><dependency><groupId>org.jetbrains.kotlin</groupId><artifactId>kotlin-stdlib</artifactId><version>2.2.20</version></dependency></dependencies>
<build><sourceDirectory>src/main/kotlin</sourceDirectory><plugins><plugin><groupId>org.jetbrains.kotlin</groupId><artifactId>kotlin-maven-plugin</artifactId><version>2.2.20</version><executions><execution><id>compile</id><phase>compile</phase><goals><goal>compile</goal></goals></execution></executions><configuration><jvmTarget>17</jvmTarget></configuration></plugin></plugins></build></project>`))
	runMaven(t, maven, dir)
	cp, err := os.ReadFile(filepath.Join(dir, "classpath"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("java", "-cp", filepath.Join(dir, "target", "classes")+string(os.PathListSeparator)+strings.TrimSpace(string(cp)), "literal.MainKt")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Kotlin literal round-trip: %v\n%s", err, out)
	}
}
func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
}
func runMaven(t *testing.T, maven, dir string) {
	t.Helper()
	cmd := exec.Command(maven, "-q", "-DskipTests", "compile", "dependency:build-classpath", "-Dmdep.outputFile="+filepath.Join(dir, "classpath"))
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Kotlin compilation failed: %v\n%s", err, out)
	}
}

// Compile every supported scalar and nullable scalar against the same pinned
// dependencies used by the runnable authors example. No provider stubs are used.
func TestAllSupportedScalarsCompileAgainstAuthorsMaven(t *testing.T) {
	maven := os.Getenv("SQLC_YDB_TEST_MAVEN")
	if maven == "" {
		t.Skip("set SQLC_YDB_TEST_MAVEN for Kotlin SDK compilation")
	}
	pom, err := os.ReadFile(filepath.Join("..", "..", "..", "examples", "authors", "kotlin", "pom.xml"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "pom.xml"), pom)
	var params []model.Parameter
	var columns []model.Column
	for _, kind := range []string{"Bool", "Int8", "Uint8", "Int16", "Uint16", "Int32", "Uint32", "Int64", "Uint64", "Float", "Double", "Utf8", "String"} {
		for _, optional := range []bool{false, true} {
			typ := model.Type{Kind: kind}
			n := strings.ToLower(kind)
			if optional {
				typ = model.Optional(typ)
				n = "optional_" + n
			}
			params = append(params, model.Parameter{Name: n, Type: typ})
			columns = append(columns, model.Column{Name: n, Type: typ})
		}
	}
	for _, runtime := range []string{"ydb", "jdbc", "exposed"} {
		queries := []model.AnalyzedQuery{}
		for _, command := range []model.Command{model.One, model.Many, model.Exec} {
			q := model.AnalyzedQuery{Name: "All" + strings.TrimPrefix(string(command), ":"), SQL: "SELECT 1;", Command: command, Parameters: params}
			if command != model.Exec {
				q.ResultSets = []model.ResultSet{{Columns: columns}}
			}
			queries = append(queries, q)
		}
		files, err := Generate(&model.AnalysisResult{Queries: queries}, Options{Package: "synthetic." + runtime, Runtime: runtime})
		if err != nil {
			t.Fatal(err)
		}
		src := filepath.Join(dir, "src", "generated", "kotlin", "synthetic", runtime)
		if err := os.MkdirAll(src, 0700); err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			writeFile(t, filepath.Join(src, f.Name), f.Content)
		}
	}
	runMaven(t, maven, dir)
}

// Execute generated Kotlin through a Java caller and the real driver parameter binder.
func TestGeneratedJDBCUsesTypedDriverValuesAndGuardsUnsignedRanges(t *testing.T) {
	maven := os.Getenv("SQLC_YDB_TEST_MAVEN")
	if maven == "" {
		t.Skip("set SQLC_YDB_TEST_MAVEN to execute the published JDBC binding regression")
	}
	queries := []model.AnalyzedQuery{
		{
			Name: "Bind", Command: model.Exec, SQL: "SELECT 1;",
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
	files, err := Generate(&model.AnalysisResult{Queries: queries}, Options{Package: "synthetic.jdbc", Runtime: "jdbc"})
	if err != nil {
		t.Fatal(err)
	}
	pom, err := os.ReadFile(filepath.Join("..", "..", "..", "examples", "authors", "kotlin", "pom.xml"))
	if err != nil {
		t.Fatal(err)
	}
	moduleDir := t.TempDir()
	writeFile(t, filepath.Join(moduleDir, "pom.xml"), pom)
	packageDir := filepath.Join(moduleDir, "src", "generated", "kotlin", "synthetic", "jdbc")
	if err := os.MkdirAll(packageDir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		writeFile(t, filepath.Join(packageDir, file.Name), file.Content)
	}
	const program = `package synthetic.jdbc;

import java.lang.reflect.InvocationHandler;
import java.lang.reflect.Method;
import java.lang.reflect.Proxy;
import java.sql.Connection;
import java.sql.PreparedStatement;
import java.sql.Types;
import java.util.HashMap;
import java.util.Map;
import java.util.Properties;

import tech.ydb.jdbc.YdbPreparedStatement;
import tech.ydb.jdbc.common.YdbTypes;
import tech.ydb.jdbc.query.QueryKey;
import tech.ydb.jdbc.query.YdbQuery;
import tech.ydb.jdbc.query.params.PreparedQuery;
import tech.ydb.jdbc.settings.YdbQueryProperties;
import tech.ydb.table.query.Params;
import tech.ydb.table.values.DecimalType;
import tech.ydb.table.values.OptionalType;
import tech.ydb.table.values.PrimitiveType;
import tech.ydb.table.values.PrimitiveValue;
import tech.ydb.table.values.Type;

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
        YdbQuery query = YdbQuery.parseQuery(new QueryKey("SELECT 1;"), new YdbQueryProperties(new Properties()), types);
        Map<String, Type> declared = new HashMap<>();
        declared.put("$author_id", PrimitiveType.Uint64);
        declared.put("$maybe_id", OptionalType.of(PrimitiveType.Uint16));
        declared.put("$title", PrimitiveType.Text);
        declared.put("$payload", PrimitiveType.Bytes);
        PreparedQuery bound = new PreparedQuery(types, query, declared);
        new Queries(bindingConnection(bound)).bind(-1L, null, "typed text", new byte[] { 0, 1, (byte) 255 });

        Params values = bound.getCurrentParams();
        check(values.values().size() == 4, "wrong parameter count");
        check(PrimitiveValue.newUint64(-1L).equals(values.values().get("$author_id")), "Uint64 lost its type or name");
        check(OptionalType.of(PrimitiveType.Uint16).emptyValue().equals(values.values().get("$maybe_id")), "optional null lost its declared type");
        check(PrimitiveValue.newText("typed text").equals(values.values().get("$title")), "Utf8 lost its type");
        check(PrimitiveValue.newBytes(new byte[] { 0, 1, (byte) 255 }).equals(values.values().get("$payload")), "String lost its binary type");
    }

    private static Connection refusingConnection() {
        return (Connection) Proxy.newProxyInstance(Main.class.getClassLoader(), new Class<?>[] { Connection.class },
                (proxy, method, args) -> { throw new AssertionError("range guard reached Connection." + method.getName()); });
    }

    private static Connection bindingConnection(PreparedQuery query) {
        YdbPreparedStatement named = (YdbPreparedStatement) Proxy.newProxyInstance(
                Main.class.getClassLoader(), new Class<?>[] { YdbPreparedStatement.class }, new InvocationHandler() {
                    @Override public Object invoke(Object proxy, Method method, Object[] args) throws Throwable {
                        if (method.getName().equals("setObject") && args != null && args.length == 2 && args[0] instanceof String) {
                            query.setParam((String) args[0], args[1], Types.JAVA_OBJECT);
                            return null;
                        }
                        if (method.getName().equals("close")) return null;
                        throw new AssertionError("unexpected YdbPreparedStatement." + method.getName());
                    }
                });
        PreparedStatement statement = (PreparedStatement) Proxy.newProxyInstance(
                Main.class.getClassLoader(), new Class<?>[] { PreparedStatement.class }, (proxy, method, args) -> {
                    if (method.getName().equals("unwrap") && args != null && args.length == 1 && args[0] == YdbPreparedStatement.class) return named;
                    if (method.getName().equals("isWrapperFor")) return args != null && args.length == 1 && args[0] == YdbPreparedStatement.class;
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
	if err := os.WriteFile(filepath.Join(moduleDir, "Main.java"), []byte(program), 0600); err != nil {
		t.Fatal(err)
	}
	runMaven(t, maven, moduleDir)
	classpath, err := os.ReadFile(filepath.Join(moduleDir, "classpath"))
	if err != nil {
		t.Fatal(err)
	}
	cp := filepath.Join(moduleDir, "target", "classes") + string(os.PathListSeparator) + strings.TrimSpace(string(classpath))
	compile := exec.Command("javac", "--release", "17", "-cp", cp, "-d", filepath.Join(moduleDir, "target", "classes"), filepath.Join(moduleDir, "Main.java"))
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("Java caller of Kotlin bindings failed to compile: %v\n%s", err, out)
	}

	run := exec.Command("java", "-cp", filepath.Join(moduleDir, "target", "classes")+string(os.PathListSeparator)+strings.TrimSpace(string(classpath)), "synthetic.jdbc.Main")
	run.Dir = moduleDir
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("generated JDBC binding fixture failed against the published driver: %v\n%s", err, out)
	}
}

func TestReservedNamesAndImportedTypeShadowing(t *testing.T) {
	files, err := Generate(&model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "When", Command: model.Exec, Parameters: []model.Parameter{{Name: "class", Type: model.Type{Kind: "Utf8"}}}}}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if source := string(files[len(files)-1].Content); !strings.Contains(source, "fun when_(class_: String): Unit") {
		t.Fatal(source)
	}
	_, err = Generate(&model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Bad", Command: model.Exec, Parameters: []model.Parameter{{Name: "_params", Type: model.Type{Kind: "Utf8"}}}}}}, Options{})
	if err == nil || !strings.Contains(err.Error(), "parameter name collision") {
		t.Fatalf("import shadowing: %v", err)
	}
}
