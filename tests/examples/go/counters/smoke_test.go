package counters_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"example.com/sqlc-ydb-example-tests/internal/testdb"
	sq "example.com/sqlc-ydb-examples/counters/go/database/sql"
	native "example.com/sqlc-ydb-examples/counters/go/native"
	"github.com/ydb-platform/ydb-go-sdk/v3/query"
	"github.com/ydb-platform/ydb-go-sdk/v3/retry"
)

func TestCountersNative(t *testing.T) {
	db := testdb.Open(t)
	db.Apply(t, "../../../../examples/counters/schema.sql", "DROP TABLE counters;")
	ctx := db.Context
	q := native.New(db.Native)
	const id = "requests"
	created, err := q.CreateCounter(ctx, id)
	if err != nil || created.ID != id || created.Value != 0 || created.OptionalValue != nil || created.Label == nil || *created.Label != "pending" || !created.Enabled {
		t.Fatalf("constant INSERT and RETURNING *: %+v, %v", created, err)
	}
	incremented, err := q.IncrementCounter(ctx, native.IncrementCounterParams{ID: id, Delta: 5})
	if err != nil || incremented.Value != 5 {
		t.Fatalf("increment: %+v, %v", incremented, err)
	}
	transformed, err := q.TransformCounter(ctx, id)
	// Every SET expression sees the original row: enabled uses 5, not 17.
	if err != nil || transformed.Value != 17 || transformed.OptionalValue == nil || *transformed.OptionalValue != 1 || transformed.Label == nil || *transformed.Label != "done" || transformed.Enabled {
		t.Fatalf("computed assignments: %+v, %v", transformed, err)
	}
	cleared, err := q.ClearOptional(ctx, id)
	if err != nil || cleared.Value != 17 || cleared.OptionalValue != nil || cleared.Label != nil {
		t.Fatalf("NULL assignments: %+v, %v", cleared, err)
	}
	if err := q.UpsertCounter(ctx, native.UpsertCounterParams{ID: id, Seed: 2}); err != nil {
		t.Fatal(err)
	}
	row, err := q.ReadCounter(ctx, id)
	if err != nil || row.Value != 12 || row.OptionalValue == nil || *row.OptionalValue != 5 || row.Label == nil || *row.Label != "reset" || !row.Enabled {
		t.Fatalf("computed UPSERT: %+v, %v", row, err)
	}
	aborted := errors.New("rollback counter increment")
	err = db.Native.DoTx(ctx, func(ctx context.Context, tx query.TxActor) error {
		txq := native.New(tx)
		changed, err := txq.IncrementCounter(ctx, native.IncrementCounterParams{ID: id, Delta: 100})
		if err != nil {
			return err
		}
		if changed.Value != 112 {
			return fmt.Errorf("transaction increment: %d", changed.Value)
		}
		read, err := txq.ReadCounter(ctx, id)
		if err != nil {
			return err
		}
		if read.Value != 112 {
			return fmt.Errorf("transaction read: %d", read.Value)
		}
		return aborted
	})
	if !errors.Is(err, aborted) {
		t.Fatalf("rollback: %v", err)
	}
	row, err = q.ReadCounter(ctx, id)
	if err != nil || row.Value != 12 {
		t.Fatalf("after rollback: %+v, %v", row, err)
	}
	// The committed generated projections remain valid after an unrelated column is added.
	if err := db.Native.Exec(ctx, "ALTER TABLE counters ADD COLUMN extra Utf8;"); err != nil {
		t.Fatal(err)
	}
	rows, err := q.ListCounters(ctx)
	if err != nil || len(rows) != 1 || rows[0].ID != id || rows[0].Value != 12 {
		t.Fatalf("SELECT * after schema evolution: %+v, %v", rows, err)
	}
	row, err = q.ReadCounter(ctx, id)
	if err != nil || row.ID != id || row.Value != 12 {
		t.Fatalf("alias.* after schema evolution: %+v, %v", row, err)
	}
	incremented, err = q.IncrementCounter(ctx, native.IncrementCounterParams{ID: id, Delta: 1})
	if err != nil || incremented.ID != id || incremented.Value != 13 {
		t.Fatalf("RETURNING * after schema evolution: %+v, %v", incremented, err)
	}
}

func TestCountersDatabaseSQL(t *testing.T) {
	db := testdb.Open(t)
	db.Apply(t, "../../../../examples/counters/schema.sql", "DROP TABLE counters;")
	ctx := db.Context
	q := sq.New(db.SQL)
	const id = "requests"
	created, err := q.CreateCounter(ctx, id)
	if err != nil || created.ID != id || created.Value != 0 || created.OptionalValue != nil || created.Label == nil || *created.Label != "pending" || !created.Enabled {
		t.Fatalf("constant INSERT and RETURNING *: %+v, %v", created, err)
	}
	incremented, err := q.IncrementCounter(ctx, sq.IncrementCounterParams{ID: id, Delta: 5})
	if err != nil || incremented.Value != 5 {
		t.Fatalf("increment: %+v, %v", incremented, err)
	}
	transformed, err := q.TransformCounter(ctx, id)
	// Every SET expression sees the original row: enabled uses 5, not 17.
	if err != nil || transformed.Value != 17 || transformed.OptionalValue == nil || *transformed.OptionalValue != 1 || transformed.Label == nil || *transformed.Label != "done" || transformed.Enabled {
		t.Fatalf("computed assignments: %+v, %v", transformed, err)
	}
	cleared, err := q.ClearOptional(ctx, id)
	if err != nil || cleared.Value != 17 || cleared.OptionalValue != nil || cleared.Label != nil {
		t.Fatalf("NULL assignments: %+v, %v", cleared, err)
	}
	if err := q.UpsertCounter(ctx, sq.UpsertCounterParams{ID: id, Seed: 2}); err != nil {
		t.Fatal(err)
	}
	row, err := q.ReadCounter(ctx, id)
	if err != nil || row.Value != 12 || row.OptionalValue == nil || *row.OptionalValue != 5 || row.Label == nil || *row.Label != "reset" || !row.Enabled {
		t.Fatalf("computed UPSERT: %+v, %v", row, err)
	}
	aborted := errors.New("rollback counter increment")
	err = retry.DoTx(ctx, db.SQL, func(ctx context.Context, tx *sql.Tx) error {
		txq := sq.New(tx)
		changed, err := txq.IncrementCounter(ctx, sq.IncrementCounterParams{ID: id, Delta: 100})
		if err != nil {
			return err
		}
		if changed.Value != 112 {
			return fmt.Errorf("transaction increment: %d", changed.Value)
		}
		read, err := txq.ReadCounter(ctx, id)
		if err != nil {
			return err
		}
		if read.Value != 112 {
			return fmt.Errorf("transaction read: %d", read.Value)
		}
		return aborted
	})
	if !errors.Is(err, aborted) {
		t.Fatalf("rollback: %v", err)
	}
	row, err = q.ReadCounter(ctx, id)
	if err != nil || row.Value != 12 {
		t.Fatalf("after rollback: %+v, %v", row, err)
	}
	// The committed generated projections remain valid after an unrelated column is added.
	if err := db.Native.Exec(ctx, "ALTER TABLE counters ADD COLUMN extra Utf8;"); err != nil {
		t.Fatal(err)
	}
	rows, err := q.ListCounters(ctx)
	if err != nil || len(rows) != 1 || rows[0].ID != id || rows[0].Value != 12 {
		t.Fatalf("SELECT * after schema evolution: %+v, %v", rows, err)
	}
	row, err = q.ReadCounter(ctx, id)
	if err != nil || row.ID != id || row.Value != 12 {
		t.Fatalf("alias.* after schema evolution: %+v, %v", row, err)
	}
	incremented, err = q.IncrementCounter(ctx, sq.IncrementCounterParams{ID: id, Delta: 1})
	if err != nil || incremented.ID != id || incremented.Value != 13 {
		t.Fatalf("RETURNING * after schema evolution: %+v, %v", incremented, err)
	}
}
