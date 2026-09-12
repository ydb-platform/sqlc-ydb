// Package testdb connects example tests to an explicitly selected disposable YDB.
package testdb

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/query"
)

type DB struct {
	Context context.Context
	Driver  *ydb.Driver
	Native  query.Client
	SQL     *sql.DB
}

func Open(t *testing.T) *DB {
	t.Helper()
	dsn := os.Getenv("YDB_CONNECTION_STRING")
	if dsn == "" {
		t.Skip("set YDB_CONNECTION_STRING for live acceptance")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	driver, err := ydb.Open(ctx, dsn, ydb.WithAnonymousCredentials())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := driver.Close(ctx); err != nil {
			t.Errorf("close driver: %v", err)
		}
	})
	standard := sql.OpenDB(ydb.MustConnector(driver))
	t.Cleanup(func() {
		if err := standard.Close(); err != nil {
			t.Errorf("close database/sql: %v", err)
		}
	})
	return &DB{Context: ctx, Driver: driver, Native: driver.Query(), SQL: standard}
}

// Apply uses the example's actual schema. Cleanup is registered only after a
// successful file, so a failed CREATE never causes an existing table to be dropped.
// A partially applied file can leave new tables in the disposable test database.
func (db *DB) Apply(t *testing.T, path string, cleanup ...string) {
	t.Helper()
	schema, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Native.Exec(db.Context, string(schema)); err != nil {
		t.Fatalf("apply %s: %v", path, err)
	}
	if len(cleanup) == 0 {
		return
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, statement := range cleanup {
			if err := db.Native.Exec(ctx, statement); err != nil {
				t.Errorf("cleanup %s: %v", statement, err)
			}
		}
	})
}
