package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const databaseConfigBase = "version: '2'\nsql:\n- engine: ydb\n  schema: s.sql\n  queries: q.sql\n"

func TestDatabaseAnalysisModes(t *testing.T) {
	for _, tc := range []struct {
		name, options string
		enabled       bool
	}{
		{"default offline", "", false},
		{"configured database", "  database: {uri: 'grpc://localhost:2136/local'}\n", true},
		{"explicit online", "  database: {uri: 'grpc://localhost:2136/local'}\n  analyzer: {database: true}\n", true},
		{"explicit offline", "  database: {uri: '${SQLC_YDB_TEST_UNUSED_URI}'}\n  analyzer: {database: false}\n", false},
		{"offline without database", "  analyzer: {database: false}\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Parse([]byte(databaseConfigBase + tc.options))
			if err != nil {
				t.Fatal(err)
			}
			if got := c.SQL[0].DatabaseEnabled(); got != tc.enabled {
				t.Fatalf("DatabaseEnabled() = %v, want %v", got, tc.enabled)
			}
		})
	}
}

func TestRejectInvalidDatabaseConfiguration(t *testing.T) {
	for _, tc := range []struct{ name, options, want string }{
		{"online without database", "  analyzer: {database: true}\n", "sql[0].analyzer.database requires database.uri"},
		{"missing URI", "  database: {}\n", "sql[0].database.uri is required"},
		{"blank URI", "  database: {uri: ' '}\n", "sql[0].database.uri is required"},
		{"unknown database option", "  database: {uri: 'grpc://localhost:2136/local', password: secret}\n", "field password"},
		{"literal auth token", "  database: {uri: 'grpc://localhost:2136/local', auth_token: secret}\n", "field auth_token"},
		{"unknown analyzer option", "  analyzer: {database: false, typo: true}\n", "field typo"},
		{"timeout syntax", "  database: {uri: 'grpc://localhost:2136/local', timeout: soon}\n", "database.timeout must be a positive duration"},
		{"zero timeout", "  database: {uri: 'grpc://localhost:2136/local', timeout: 0s}\n", "database.timeout must be a positive duration"},
		{"negative timeout", "  database: {uri: 'grpc://localhost:2136/local', timeout: -1s}\n", "database.timeout must be a positive duration"},
		{"invalid token env name", "  database: {uri: 'grpc://localhost:2136/local', auth_token_env: '${TOKEN}'}\n", "auth_token_env must be an environment variable name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(databaseConfigBase + tc.options))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v; want %q", err, tc.want)
			}
		})
	}
}

func TestLoadDatabaseDefersEnvironmentResolution(t *testing.T) {
	t.Setenv("SQLC_YDB_TEST_DEFERRED_URI", "")
	t.Setenv("SQLC_YDB_TEST_DEFERRED_TOKEN", "")
	path := filepath.Join(t.TempDir(), "sqlc.yaml")
	data := databaseConfigBase + "  database:\n    uri: '${SQLC_YDB_TEST_DEFERRED_URI}'\n    auth_token_env: SQLC_YDB_TEST_DEFERRED_TOKEN\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.SQL[0].Database.URI != "${SQLC_YDB_TEST_DEFERRED_URI}" {
		t.Fatalf("Load expanded URI: %q", c.SQL[0].Database.URI)
	}
}

func TestResolveDatabase(t *testing.T) {
	t.Setenv("SQLC_YDB_TEST_ENDPOINT", "localhost:2136")
	t.Setenv("SQLC_YDB_TEST_DATABASE", "local")
	t.Setenv("SQLC_YDB_TEST_TOKEN", "private-token-value")
	baseDir := t.TempDir()
	for _, tc := range []struct {
		name string
		in   Database
		want ResolvedDatabase
	}{
		{
			name: "defaults and URI substitutions",
			in:   Database{URI: "grpc://${SQLC_YDB_TEST_ENDPOINT}/${SQLC_YDB_TEST_DATABASE}"},
			want: ResolvedDatabase{Endpoint: "localhost:2136", Database: "/local", Timeout: 10 * time.Second},
		},
		{
			name: "TLS token CA and custom timeout",
			in:   Database{URI: "grpcs://localhost:2135/local", AuthTokenEnv: "SQLC_YDB_TEST_TOKEN", CAFile: "certs/root.pem", Timeout: "500ms"},
			want: ResolvedDatabase{Endpoint: "localhost:2135", Database: "/local", Secure: true, AuthToken: "private-token-value", CAFile: filepath.Join(baseDir, "certs", "root.pem"), Timeout: 500 * time.Millisecond},
		},
		{
			name: "absolute CA path and IPv6 endpoint",
			in:   Database{URI: "grpcs://[::1]:2135/local", CAFile: filepath.Join(baseDir, "root.pem")},
			want: ResolvedDatabase{Endpoint: "[::1]:2135", Database: "/local", Secure: true, CAFile: filepath.Join(baseDir, "root.pem"), Timeout: 10 * time.Second},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.in.Resolve(baseDir)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("resolved connection differs: %+v", got)
			}
		})
	}
}

func TestResolveDatabaseRejectsInvalidConnections(t *testing.T) {
	t.Setenv("SQLC_YDB_TEST_EMPTY_URI", "")
	t.Setenv("SQLC_YDB_TEST_EMPTY_TOKEN", "")
	for _, tc := range []struct {
		name string
		in   Database
		want string
	}{
		{"missing URI env", Database{URI: "${SQLC_YDB_TEST_MISSING_URI}"}, "environment variable SQLC_YDB_TEST_MISSING_URI is unset or empty"},
		{"empty URI env", Database{URI: "${SQLC_YDB_TEST_EMPTY_URI}"}, "environment variable SQLC_YDB_TEST_EMPTY_URI is unset or empty"},
		{"unterminated URI env", Database{URI: "${SQLC_YDB_TEST_URI"}, "use ${NAME}"},
		{"invalid URI env name", Database{URI: "${bad-name}"}, "use ${NAME}"},
		{"missing scheme", Database{URI: "localhost:2136/local"}, "grpc or grpcs"},
		{"unsupported scheme", Database{URI: "https://localhost:2136/local"}, "grpc or grpcs"},
		{"missing host", Database{URI: "grpc:///local"}, "host"},
		{"missing hostname", Database{URI: "grpc://:2136/local"}, "host"},
		{"missing database", Database{URI: "grpc://localhost:2136"}, "database path"},
		{"userinfo", Database{URI: "grpc://name:private-secret@localhost:2136/local"}, "userinfo"},
		{"query", Database{URI: "grpc://localhost:2136/local?token=private-secret"}, "query"},
		{"empty query", Database{URI: "grpc://localhost:2136/local?"}, "query"},
		{"fragment", Database{URI: "grpc://localhost:2136/local#private-secret"}, "fragment"},
		{"empty fragment", Database{URI: "grpc://localhost:2136/local#"}, "fragment"},
		{"invalid URI", Database{URI: "grpc://private-secret:bad/local"}, "invalid database.uri"},
		{"plaintext CA", Database{URI: "grpc://localhost:2136/local", CAFile: "ca.pem"}, "ca_file requires a grpcs URI"},
		{"missing token env", Database{URI: "grpc://localhost:2136/local", AuthTokenEnv: "SQLC_YDB_TEST_MISSING_TOKEN"}, "environment variable SQLC_YDB_TEST_MISSING_TOKEN is unset or empty"},
		{"empty token env", Database{URI: "grpc://localhost:2136/local", AuthTokenEnv: "SQLC_YDB_TEST_EMPTY_TOKEN"}, "environment variable SQLC_YDB_TEST_EMPTY_TOKEN is unset or empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.in.Resolve(t.TempDir())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v; want %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "private-secret") {
				t.Fatalf("error exposes URI contents: %v", err)
			}
		})
	}
}

func TestDatabaseAnalysisSchemaRequirement(t *testing.T) {
	withoutSchema := strings.Replace(databaseConfigBase, "  schema: s.sql\n", "", 1)
	for _, tc := range []struct {
		name, options string
		wantError     bool
	}{
		{"discover schema from configured database", "  database: {uri: 'grpc://localhost:2136/local'}\n", false},
		{"discover schema with explicit online mode", "  database: {uri: 'grpc://localhost:2136/local'}\n  analyzer: {database: true}\n", false},
		{"no database", "", true},
		{"explicit offline mode", "  database: {uri: 'grpc://localhost:2136/local'}\n  analyzer: {database: false}\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(withoutSchema + tc.options))
			if !tc.wantError && err != nil {
				t.Fatal(err)
			}
			if tc.wantError && (err == nil || !strings.Contains(err.Error(), "schema and queries paths are required")) {
				t.Fatalf("got %v; want required schema error", err)
			}
		})
	}
	withoutQueries := strings.Replace(databaseConfigBase, "  queries: q.sql\n", "", 1)
	_, err := Parse([]byte(withoutQueries + "  database: {uri: 'grpc://localhost:2136/local'}\n"))
	if err == nil || !strings.Contains(err.Error(), "queries paths are required") {
		t.Fatalf("got %v; want required queries error", err)
	}
}
