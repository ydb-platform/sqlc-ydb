package runtime

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sqlc-dev/sqlc/pkg/engine"
	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/table"
)

func TestRegistry_IntegrationYDB(t *testing.T) {
	dsn := os.Getenv("YDB_DSN")
	if dsn == "" {
		dsn = "grpc://localhost:2136/local"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := ydb.Open(ctx, dsn)
	if err != nil {
		t.Skipf("YDB not available at %s: %v", dsn, err)
	}
	defer func() { _ = db.Close(ctx) }()

	runDDL := func(sql string) {
		err := db.Table().Do(ctx, func(ctx context.Context, s table.Session) error {
			return s.ExecuteSchemeQuery(ctx, sql)
		}, table.WithIdempotent())
		require.NoError(t, err)
	}

	// CREATE TABLE reg_t (id Uint64, a Utf8, PRIMARY KEY (id))
	runDDL(`CREATE TABLE reg_t (id Uint64, a Utf8, PRIMARY KEY (id));`)

	// ALTER TABLE reg_t ADD COLUMN b Utf8
	runDDL(`ALTER TABLE reg_t ADD COLUMN b Utf8;`)

	reg := Registry(&engine.ConnectionParams{Dsn: dsn})
	cols, ok := reg.Columns("reg_t")
	require.True(t, ok)
	require.Len(t, cols, 3)
	require.Equal(t, "id", cols[0].Name)
	require.Equal(t, "a", cols[1].Name)
	require.Equal(t, "b", cols[2].Name)

	// DROP TABLE reg_t
	runDDL(`DROP TABLE reg_t;`)

	// CREATE VIEW reg_v AS SELECT 1 AS x
	runDDL(`CREATE VIEW reg_v AS SELECT 1 AS x;`)
	defer func() {
		_ = db.Table().Do(ctx, func(ctx context.Context, s table.Session) error {
			return s.ExecuteSchemeQuery(ctx, `DROP VIEW reg_v;`)
		}, table.WithIdempotent())
	}()

	cols, ok = reg.Columns("reg_v")
	if ok {
		require.NotEmpty(t, cols, "view columns")
	}
}
