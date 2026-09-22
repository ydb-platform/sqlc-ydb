package counters_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"example.com/sqlc-ydb-example-tests/internal/testdb"
	native "example.com/sqlc-ydb-examples/counters/go/native"
)

func TestDatabaseAnalysis(t *testing.T) {
	db := testdb.Open(t)
	db.Apply(t, "../../../../examples/counters/schema.sql", "DROP TABLE counters;")
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "bin", "sqlc-ydb")
	if _, err := os.Stat(binary); err != nil {
		t.Fatalf("run make generate before live example tests: %v", err)
	}
	// Discovery writes only into this copy, leaving committed example outputs intact.
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Join(root, "examples", "counters"))); err != nil {
		t.Fatal(err)
	}
	run := func(config, command string, extra ...string) (string, error) {
		t.Helper()
		args := append([]string{command, "--no-remote", "-f", filepath.Join(dir, config)}, extra...)
		cmd := exec.CommandContext(db.Context, binary, args...)
		output, err := cmd.CombinedOutput()
		return string(output), err
	}
	for _, tc := range []struct{ config, command string }{
		{"sqlc.database.yaml", "compile"},
		{"sqlc.database.yaml", "diff"},
		{"sqlc.discovery.yaml", "generate"},
		{"sqlc.discovery.yaml", "diff"},
	} {
		if output, err := run(tc.config, tc.command); err != nil {
			t.Fatalf("%s %s: %v\n%s", tc.command, tc.config, err, output)
		}
	}
	rows, err := native.New(db.Native).ListCounters(db.Context)
	if err != nil || len(rows) != 0 {
		t.Fatalf("analysis must not execute INSERT/UPSERT queries: %+v, %v", rows, err)
	}
	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(root, "examples", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "build", "discovered", "smoke_test.go"), []byte(discoverySmoke), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(db.Context, "go", "test", "-p", "1", "-count=1", "./build/discovered")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("discovered client: %v\n%s", err, output)
	}
	if err := db.Native.Exec(db.Context, "ALTER TABLE counters ADD COLUMN extra Utf8;"); err != nil {
		t.Fatal(err)
	}
	if output, err := run("sqlc.database.yaml", "compile"); err == nil || !strings.Contains(output, "schema drift") {
		t.Fatalf("expected schema drift, got %v\n%s", err, output)
	}
	if output, err := run("sqlc.database.yaml", "diff", "--no-database"); err != nil {
		t.Fatalf("offline override: %v\n%s", err, output)
	}
}

const discoverySmoke = `package counters

import (
    "context"
    "os"
    "testing"
    "time"

    "github.com/ydb-platform/ydb-go-sdk/v3"
)

func TestDiscoveredClient(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
    defer cancel()
    driver, err := ydb.Open(ctx, os.Getenv("YDB_CONNECTION_STRING"), ydb.WithAnonymousCredentials())
    if err != nil { t.Fatal(err) }
    defer func() { if err := driver.Close(ctx); err != nil { t.Error(err) } }()
    q := New(driver.Query())
    created, err := q.CreateCounter(ctx, "discovered")
    if err != nil || created.ID != "discovered" || created.Value != 0 || created.OptionalValue != nil || !created.Enabled {
        t.Fatalf("discovered INSERT: %+v, %v", created, err)
    }
    row, err := q.ReadCounter(ctx, "discovered")
    if err != nil || row.ID != created.ID || row.Value != created.Value || row.Label == nil || *row.Label != "pending" {
        t.Fatalf("discovered SELECT: %+v, %v", row, err)
    }
}
`
