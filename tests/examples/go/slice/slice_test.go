package slice_test

import (
	"testing"

	"example.com/sqlc-ydb-example-tests/internal/testdb"
	sq "example.com/sqlc-ydb-examples/slice/go/database/sql"
	native "example.com/sqlc-ydb-examples/slice/go/native"
	"github.com/stretchr/testify/require"
)

func TestSQLCSliceLists(t *testing.T) {
	db := testdb.Open(t)
	db.Apply(t, "../../../../examples/slice/schema.sql", "DROP TABLE records;")
	require.NoError(t, db.Native.Exec(db.Context, `UPSERT INTO records (id, label) VALUES (1ul, "one"u), (2ul, "two"u), (3ul, "three"u);`))

	t.Run("native", func(t *testing.T) {
		queries := native.New(db.Native)
		rows, err := queries.FindRecords(db.Context, []uint64{1, 3})
		require.NoError(t, err)
		require.Equal(t, []native.FindRecordsRow{{ID: 1, Label: "one"}, {ID: 3, Label: "three"}}, rows)
		rows, err = queries.FindRecords(db.Context, nil)
		require.NoError(t, err)
		require.Empty(t, rows)
		excluded, err := queries.ExcludeRecords(db.Context, []uint64{1, 3})
		require.NoError(t, err)
		require.Equal(t, []native.ExcludeRecordsRow{{ID: 2, Label: "two"}}, excluded)
		excluded, err = queries.ExcludeRecords(db.Context, nil)
		require.NoError(t, err)
		require.Equal(t, []native.ExcludeRecordsRow{{ID: 1, Label: "one"}, {ID: 2, Label: "two"}, {ID: 3, Label: "three"}}, excluded)
	})

	t.Run("database/sql", func(t *testing.T) {
		queries := sq.New(db.SQL)
		rows, err := queries.FindRecords(db.Context, []uint64{1, 3})
		require.NoError(t, err)
		require.Equal(t, []sq.FindRecordsRow{{ID: 1, Label: "one"}, {ID: 3, Label: "three"}}, rows)
		rows, err = queries.FindRecords(db.Context, nil)
		require.NoError(t, err)
		require.Empty(t, rows)
		excluded, err := queries.ExcludeRecords(db.Context, []uint64{1, 3})
		require.NoError(t, err)
		require.Equal(t, []sq.ExcludeRecordsRow{{ID: 2, Label: "two"}}, excluded)
		excluded, err = queries.ExcludeRecords(db.Context, nil)
		require.NoError(t, err)
		require.Equal(t, []sq.ExcludeRecordsRow{{ID: 1, Label: "one"}, {ID: 2, Label: "two"}, {ID: 3, Label: "three"}}, excluded)
	})
}
