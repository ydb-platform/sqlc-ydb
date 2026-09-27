package multi_results_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"example.com/sqlc-ydb-example-tests/internal/testdb"
	sq "example.com/sqlc-ydb-examples/multi_results/go/database/sql"
	native "example.com/sqlc-ydb-examples/multi_results/go/native"
	"github.com/ydb-platform/ydb-go-sdk/v3/query"
)

func TestGeneratedMultiResultSets(t *testing.T) {
	db := testdb.Open(t)
	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{"native/client", func() error { return checkNative(db.Context, db.Native) }},
		{"native/session", func() error {
			return db.Native.Do(db.Context, func(ctx context.Context, session query.Session) error { return checkNative(ctx, session) })
		}},
		{"native/transaction", func() error {
			return db.Native.DoTx(db.Context, func(ctx context.Context, tx query.TxActor) error { return checkNative(ctx, tx) })
		}},
		{"database/sql/client", func() error { return checkSQL(db.Context, db.SQL) }},
		{"database/sql/transaction", func() error {
			tx, err := db.SQL.BeginTx(db.Context, nil)
			if err != nil {
				return err
			}
			defer func() { _ = tx.Rollback() }()
			if err := checkSQL(db.Context, tx); err != nil {
				return err
			}
			return tx.Commit()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) { require.NoError(t, tc.run()) })
	}
}

func checkNative(ctx context.Context, executor native.DBTX) error {
	result, err := native.New(executor).FetchSummary(ctx, 42)
	if err != nil {
		return err
	}
	if len(result.Item) != 0 || len(result.Flags) != 1 || !result.Flags[0].Enabled || len(result.Result3) != 1 || result.Result3[0].Status != "ready" {
		return fmt.Errorf("unexpected native result: %+v", result)
	}
	literals, err := native.New(executor).BareLiterals(ctx)
	if err != nil {
		return err
	}
	if len(literals.Result1) != 1 || literals.Result1[0].Column0 != 1 || len(literals.Result2) != 1 || literals.Result2[0].Column0 != "2" || len(literals.Result3) != 1 || literals.Result3[0].Column0 {
		return fmt.Errorf("unexpected native literal results: %+v", literals)
	}
	return nil
}

func checkSQL(ctx context.Context, executor sq.DBTX) error {
	result, err := sq.New(executor).FetchSummary(ctx, 42)
	if err != nil {
		return err
	}
	if len(result.Item) != 0 || len(result.Flags) != 1 || !result.Flags[0].Enabled || len(result.Result3) != 1 || result.Result3[0].Status != "ready" {
		return fmt.Errorf("unexpected database/sql result: %+v", result)
	}
	literals, err := sq.New(executor).BareLiterals(ctx)
	if err != nil {
		return err
	}
	if len(literals.Result1) != 1 || literals.Result1[0].Column0 != 1 || len(literals.Result2) != 1 || literals.Result2[0].Column0 != "2" || len(literals.Result3) != 1 || literals.Result3[0].Column0 {
		return fmt.Errorf("unexpected database/sql literal results: %+v", literals)
	}
	return nil
}
