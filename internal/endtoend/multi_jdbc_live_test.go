package endtoend

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/codegen/java"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestLiveYDBJDBCMulti(t *testing.T) {
	if os.Getenv("YDB_CONNECTION_STRING") == "" {
		t.Skip("set YDB_CONNECTION_STRING for live JDBC multi-result validation")
	}
	cp := os.Getenv("SQLC_YDB_TEST_JAVA_CLASSPATH")
	maven := os.Getenv("SQLC_YDB_TEST_MAVEN")
	if cp == "" && maven == "" {
		t.Skip("set SQLC_YDB_TEST_MAVEN or SQLC_YDB_TEST_JAVA_CLASSPATH for live JDBC multi-result validation")
	}

	dir := t.TempDir()
	if cp == "" {
		classpath := filepath.Join(dir, "classpath")
		cmd := exec.Command(maven, "-q", "dependency:build-classpath", "-Dmdep.outputFile="+classpath)
		cmd.Dir = filepath.Join("..", "..", "tests", "examples", "java", "batch")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "JDBC SDK classpath:\n%s", out)
		contents, err := os.ReadFile(classpath)
		require.NoError(t, err)
		cp = strings.TrimSpace(string(contents))
	}

	queries, err := os.ReadFile(filepath.Join("..", "..", "examples", "multi_results", "queries.sql"))
	require.NoError(t, err)
	analysis, err := analyzer.Analyze(nil, []model.Source{{Name: "queries.sql", Text: string(queries)}})
	require.NoError(t, err)
	files, err := java.Generate(analysis, java.Options{Package: "multires.jdbc", Runtime: "jdbc"})
	require.NoError(t, err)

	compile := []string{"-cp", cp, "-d", dir}
	for _, file := range files {
		filename := filepath.Join(dir, file.Name)
		require.NoError(t, os.WriteFile(filename, file.Content, 0600))
		compile = append(compile, filename)
	}
	compile = append(compile, filepath.Join("..", "..", "tests", "examples", "java", "batch", "src", "test", "java", "MultiSmoke.java"))
	out, err := exec.Command("javac", compile...).CombinedOutput()
	require.NoError(t, err, "compile generated JDBC multi-result client:\n%s", out)
	out, err = exec.Command("java", "-cp", dir+string(os.PathListSeparator)+cp, "MultiSmoke").CombinedOutput()
	require.NoError(t, err, "execute generated JDBC multi-result client:\n%s", out)
}
