package streaming_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"example.com/sqlc-ydb-example-tests/internal/testdb"
	sq "example.com/sqlc-ydb-examples/streaming/go/database/sql"
	native "example.com/sqlc-ydb-examples/streaming/go/native"
	"github.com/ydb-platform/ydb-go-sdk/v3/query"
	"github.com/ydb-platform/ydb-go-sdk/v3/retry/budget"
)

func exportNative(ctx context.Context, executor native.DBTX, dst io.Writer, minID, maxID uint64) error {
	encoder := json.NewEncoder(dst)
	return native.New(executor).VisitDevices(ctx, native.VisitDevicesParams{MinID: minID, MaxID: maxID}, func(row native.VisitDevicesRow) error {
		return encoder.Encode(row)
	})
}

func exportSQL(ctx context.Context, executor sq.DBTX, dst io.Writer, minID, maxID uint64) error {
	encoder := json.NewEncoder(dst)
	return sq.New(executor).VisitDevices(ctx, sq.VisitDevicesParams{MinID: minID, MaxID: maxID}, func(row sq.VisitDevicesRow) error {
		return encoder.Encode(row)
	})
}

type failingWriter struct {
	calls int
}

func (w *failingWriter) Write([]byte) (int, error) {
	w.calls++
	return 0, io.ErrClosedPipe
}

func TestCallbackExports(t *testing.T) {
	db := testdb.Open(t)
	db.Apply(t, "../../../../examples/streaming/schema.sql", "DROP TABLE streaming_devices;")
	nq := native.New(db.Native)
	names := []*string{nil, new("Датчик"), new("Lamp")}
	for i, name := range names {
		require.NoError(t, nq.UpsertDevice(db.Context, native.UpsertDeviceParams{ID: uint64(i + 1), Name: name}))
	}
	sqlQueries := sq.New(db.SQL)
	require.NoError(t, sqlQueries.UpsertDevice(db.Context, sq.UpsertDeviceParams{ID: 4, Name: new("SQL device")}))
	for _, tc := range []struct {
		name   string
		export func(io.Writer, uint64, uint64) error
	}{
		{"native/client", func(dst io.Writer, minID, maxID uint64) error {
			return exportNative(db.Context, db.Native, dst, minID, maxID)
		}},
		{"native/session", func(dst io.Writer, minID, maxID uint64) error {
			return db.Native.Do(db.Context, func(ctx context.Context, s query.Session) error {
				return exportNative(ctx, s, dst, minID, maxID)
			}, query.WithRetryBudget(budget.Percent(0)))
		}},
		{"native/transaction", func(dst io.Writer, minID, maxID uint64) error {
			return db.Native.DoTx(db.Context, func(ctx context.Context, tx query.TxActor) error {
				return exportNative(ctx, tx, dst, minID, maxID)
			}, query.WithRetryBudget(budget.Percent(0)))
		}},
		{"database_sql/client", func(dst io.Writer, minID, maxID uint64) error {
			return exportSQL(db.Context, db.SQL, dst, minID, maxID)
		}},
		{"database_sql/transaction", func(dst io.Writer, minID, maxID uint64) error {
			tx, err := db.SQL.BeginTx(db.Context, nil)
			if err != nil {
				return err
			}
			defer func() { _ = tx.Rollback() }()
			if err := exportSQL(db.Context, tx, dst, minID, maxID); err != nil {
				return err
			}
			return tx.Commit()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			require.NoError(t, tc.export(&out, 1, 3))
			want := "{\"id\":1,\"name\":null}\n{\"id\":2,\"name\":\"Датчик\"}\n{\"id\":3,\"name\":\"Lamp\"}\n"
			require.Equal(t, want, out.String())
			out.Reset()
			require.NoError(t, tc.export(&out, 99, 100))
			require.Empty(t, out.String(), "empty export")
			failed := new(failingWriter)
			err := tc.export(failed, 1, 3)
			require.ErrorIs(t, err, io.ErrClosedPipe)
			require.Equal(t, 1, failed.calls)
		})
	}
	// No-parameter queries use the same callback API in both runtime profiles.
	var ids []uint64
	require.NoError(t, nq.VisitAllDevices(db.Context, func(row native.VisitAllDevicesRow) error { ids = append(ids, row.ID); return nil }))
	require.Len(t, ids, 4)
	require.Equal(t, uint64(1), ids[0])
	require.Equal(t, uint64(4), ids[3])
	ids = nil
	require.NoError(t, sqlQueries.VisitAllDevices(db.Context, func(row sq.VisitAllDevicesRow) error { ids = append(ids, row.ID); return nil }))
	require.Len(t, ids, 4)
	require.Equal(t, uint64(1), ids[0])
	require.Equal(t, uint64(4), ids[3])
}
