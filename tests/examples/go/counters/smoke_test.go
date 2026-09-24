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
	"github.com/stretchr/testify/require"
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
	require.NoError(t, err)
	require.Equal(t, id, created.ID)
	require.Zero(t, created.Value)
	require.Nil(t, created.OptionalValue)
	require.NotNil(t, created.Label)
	require.Equal(t, "pending", *created.Label)
	require.True(t, created.Enabled)
	incremented, err := q.IncrementCounter(ctx, native.IncrementCounterParams{ID: id, Delta: 5})
	require.NoError(t, err)
	require.Equal(t, int64(5), incremented.Value)
	transformed, err := q.TransformCounter(ctx, id)
	// Every SET expression sees the original row: enabled uses 5, not 17.
	require.NoError(t, err)
	require.Equal(t, int64(17), transformed.Value)
	require.NotNil(t, transformed.OptionalValue)
	require.Equal(t, int64(1), *transformed.OptionalValue)
	require.NotNil(t, transformed.Label)
	require.Equal(t, "done", *transformed.Label)
	require.False(t, transformed.Enabled)
	cleared, err := q.ClearOptional(ctx, id)
	require.NoError(t, err)
	require.Equal(t, int64(17), cleared.Value)
	require.Nil(t, cleared.OptionalValue)
	require.Nil(t, cleared.Label)
	require.NoError(t, q.UpsertCounter(ctx, native.UpsertCounterParams{ID: id, Seed: 2}))
	row, err := q.ReadCounter(ctx, id)
	require.NoError(t, err)
	require.Equal(t, int64(12), row.Value)
	require.NotNil(t, row.OptionalValue)
	require.Equal(t, int64(5), *row.OptionalValue)
	require.NotNil(t, row.Label)
	require.Equal(t, "reset", *row.Label)
	require.True(t, row.Enabled)
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
	require.ErrorIs(t, err, aborted)
	row, err = q.ReadCounter(ctx, id)
	require.NoError(t, err)
	require.Equal(t, int64(12), row.Value)
	// The committed generated projections remain valid after an unrelated column is added.
	require.NoError(t, db.Native.Exec(ctx, "ALTER TABLE counters ADD COLUMN extra Utf8;"))
	rows, err := q.ListCounters(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, id, rows[0].ID)
	require.Equal(t, int64(12), rows[0].Value)
	row, err = q.ReadCounter(ctx, id)
	require.NoError(t, err)
	require.Equal(t, id, row.ID)
	require.Equal(t, int64(12), row.Value)
	incremented, err = q.IncrementCounter(ctx, native.IncrementCounterParams{ID: id, Delta: 1})
	require.NoError(t, err)
	require.Equal(t, id, incremented.ID)
	require.Equal(t, int64(13), incremented.Value)
}

func TestCountersDatabaseSQL(t *testing.T) {
	db := testdb.Open(t)
	db.Apply(t, "../../../../examples/counters/schema.sql", "DROP TABLE counters;")
	ctx := db.Context
	q := sq.New(db.SQL)
	const id = "requests"
	created, err := q.CreateCounter(ctx, id)
	require.NoError(t, err)
	require.Equal(t, id, created.ID)
	require.Zero(t, created.Value)
	require.Nil(t, created.OptionalValue)
	require.NotNil(t, created.Label)
	require.Equal(t, "pending", *created.Label)
	require.True(t, created.Enabled)
	incremented, err := q.IncrementCounter(ctx, sq.IncrementCounterParams{ID: id, Delta: 5})
	require.NoError(t, err)
	require.Equal(t, int64(5), incremented.Value)
	transformed, err := q.TransformCounter(ctx, id)
	// Every SET expression sees the original row: enabled uses 5, not 17.
	require.NoError(t, err)
	require.Equal(t, int64(17), transformed.Value)
	require.NotNil(t, transformed.OptionalValue)
	require.Equal(t, int64(1), *transformed.OptionalValue)
	require.NotNil(t, transformed.Label)
	require.Equal(t, "done", *transformed.Label)
	require.False(t, transformed.Enabled)
	cleared, err := q.ClearOptional(ctx, id)
	require.NoError(t, err)
	require.Equal(t, int64(17), cleared.Value)
	require.Nil(t, cleared.OptionalValue)
	require.Nil(t, cleared.Label)
	require.NoError(t, q.UpsertCounter(ctx, sq.UpsertCounterParams{ID: id, Seed: 2}))
	row, err := q.ReadCounter(ctx, id)
	require.NoError(t, err)
	require.Equal(t, int64(12), row.Value)
	require.NotNil(t, row.OptionalValue)
	require.Equal(t, int64(5), *row.OptionalValue)
	require.NotNil(t, row.Label)
	require.Equal(t, "reset", *row.Label)
	require.True(t, row.Enabled)
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
	require.ErrorIs(t, err, aborted)
	row, err = q.ReadCounter(ctx, id)
	require.NoError(t, err)
	require.Equal(t, int64(12), row.Value)
	// The committed generated projections remain valid after an unrelated column is added.
	require.NoError(t, db.Native.Exec(ctx, "ALTER TABLE counters ADD COLUMN extra Utf8;"))
	rows, err := q.ListCounters(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, id, rows[0].ID)
	require.Equal(t, int64(12), rows[0].Value)
	row, err = q.ReadCounter(ctx, id)
	require.NoError(t, err)
	require.Equal(t, id, row.ID)
	require.Equal(t, int64(12), row.Value)
	incremented, err = q.IncrementCounter(ctx, sq.IncrementCounterParams{ID: id, Delta: 1})
	require.NoError(t, err)
	require.Equal(t, id, incremented.ID)
	require.Equal(t, int64(13), incremented.Value)
}
