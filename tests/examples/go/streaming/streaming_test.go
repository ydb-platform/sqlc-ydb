package streaming_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

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
		if err := nq.UpsertDevice(db.Context, native.UpsertDeviceParams{ID: uint64(i + 1), Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	sqlQueries := sq.New(db.SQL)
	if err := sqlQueries.UpsertDevice(db.Context, sq.UpsertDeviceParams{ID: 4, Name: new("SQL device")}); err != nil {
		t.Fatal(err)
	}
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
			if err := tc.export(&out, 1, 3); err != nil {
				t.Fatal(err)
			}
			want := "{\"id\":1,\"name\":null}\n{\"id\":2,\"name\":\"Датчик\"}\n{\"id\":3,\"name\":\"Lamp\"}\n"
			if out.String() != want {
				t.Fatalf("export = %q, want %q", out.String(), want)
			}
			out.Reset()
			if err := tc.export(&out, 99, 100); err != nil || out.Len() != 0 {
				t.Fatalf("empty export: %q, %v", out.String(), err)
			}
			failed := new(failingWriter)
			if err := tc.export(failed, 1, 3); !errors.Is(err, io.ErrClosedPipe) || failed.calls != 1 {
				t.Fatalf("failed export: writes=%d, error=%v", failed.calls, err)
			}
		})
	}
	// No-parameter queries use the same callback API in both runtime profiles.
	var ids []uint64
	if err := nq.VisitAllDevices(db.Context, func(row native.VisitAllDevicesRow) error { ids = append(ids, row.ID); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(ids) != 4 || ids[0] != 1 || ids[3] != 4 {
		t.Fatalf("native all: %v", ids)
	}
	ids = nil
	if err := sqlQueries.VisitAllDevices(db.Context, func(row sq.VisitAllDevicesRow) error { ids = append(ids, row.ID); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(ids) != 4 || ids[0] != 1 || ids[3] != 4 {
		t.Fatalf("SQL all: %v", ids)
	}
}
