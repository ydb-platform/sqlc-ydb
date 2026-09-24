package records_test

import (
	"bytes"
	"testing"
	"time"

	"example.com/sqlc-ydb-example-tests/internal/testdb"
	sq "example.com/sqlc-ydb-examples/records/go/database/sql"
	native "example.com/sqlc-ydb-examples/records/go/native"
	"github.com/stretchr/testify/require"
)

func TestRecordsNative(t *testing.T) {
	db := testdb.Open(t)
	db.Apply(t, "../../../../examples/records/schema.sql", "DROP TABLE records;")
	ctx := db.Context
	q := native.New(db.Native)
	const ownerID = ^uint64(0)
	createdAt := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	updatedAt := createdAt.Add(time.Hour)
	payload := []byte{0, 1, 255}
	err := q.InsertRecords(ctx, native.InsertRecordsParams{
		OwnerID: ownerID, CreatedAt: createdAt,
		Rows: []native.InsertRecordsRowsItem{
			{RecordID: "a", GroupID: "new", Payload: payload, Attributes: `["red"]`},
			{RecordID: "b", GroupID: "new", Payload: []byte("second"), Attributes: `["blue"]`},
		},
	})
	require.NoError(t, err)
	rows, err := q.ListRecords(ctx, ownerID)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	key := native.GetRecordKey{OwnerHash: rows[0].OwnerHash, RecordID: "a"}
	row, err := q.GetRecord(ctx, key)
	require.NoError(t, err)
	require.Equal(t, ownerID, row.OwnerID)
	require.True(t, bytes.Equal(row.Payload, payload))
	require.Equal(t, `["red"]`, row.Attributes)
	require.True(t, row.CreatedAt.Equal(createdAt))
	err = q.UpdateRecords(ctx, native.UpdateRecordsParams{
		OwnerID: ownerID, UpdatedAt: updatedAt,
		Rows: []native.UpdateRecordsRowsItem{
			{RecordID: "a", GroupID: "changed", Payload: []byte("updated"), Attributes: `["green"]`},
			{RecordID: "missing", GroupID: "changed", Payload: payload, Attributes: `[]`},
		},
	})
	require.NoError(t, err)
	row, err = q.GetRecord(ctx, key)
	require.NoError(t, err)
	require.Equal(t, "changed", row.GroupID)
	require.Equal(t, "updated", string(row.Payload))
	require.True(t, row.CreatedAt.Equal(createdAt))
	require.True(t, row.UpdatedAt.Equal(updatedAt))
	rows, err = q.ListRecords(ctx, ownerID)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "new", rows[1].GroupID)
	filtered, err := q.FilterRecords(ctx, native.FilterRecordsParams{OwnerID: ownerID, RecordIds: []string{"a", "b"}, GroupID: "changed"})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	require.Equal(t, "a", filtered[0].RecordID)
	tagged, err := q.FindRecordsByTags(ctx, [][]byte{[]byte("green")})
	require.NoError(t, err)
	require.Len(t, tagged, 1)
	require.Equal(t, "a", tagged[0].RecordID)
	tagged, err = q.FindRecordsByTags(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, tagged)
	err = q.DeleteRecords(ctx, native.DeleteRecordsParams{OwnerID: ownerID, RecordIds: []string{"a", "b"}, GroupID: "changed"})
	require.NoError(t, err)
	rows, err = q.ListRecords(ctx, ownerID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "b", rows[0].RecordID)
	err = q.UpsertRecords(ctx, native.UpsertRecordsParams{
		OwnerID: ownerID, CreatedAt: updatedAt,
		Rows: []native.UpsertRecordsRowsItem{
			{RecordID: "b", GroupID: "replaced", Payload: payload, Attributes: `[]`},
			{RecordID: "c", GroupID: "inserted", Payload: payload, Attributes: `[]`},
		},
	})
	require.NoError(t, err)
	rows, err = q.ListRecords(ctx, ownerID)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "replaced", rows[0].GroupID)
	require.Equal(t, "inserted", rows[1].GroupID)
	require.NoError(t, q.InsertRecords(ctx, native.InsertRecordsParams{OwnerID: ownerID, CreatedAt: createdAt}), "empty batch")
	label := "Москва"
	reversed, err := q.ReverseGroupLabel(ctx, &label)
	require.NoError(t, err)
	require.NotNil(t, reversed.ReversedLabel)
	require.Equal(t, "авксоМ", *reversed.ReversedLabel)
	reversed, err = q.ReverseGroupLabel(ctx, nil)
	require.NoError(t, err)
	require.Nil(t, reversed.ReversedLabel)
}

func TestRecordsDatabaseSQL(t *testing.T) {
	db := testdb.Open(t)
	db.Apply(t, "../../../../examples/records/schema.sql", "DROP TABLE records;")
	ctx := db.Context
	q := sq.New(db.SQL)
	const ownerID = ^uint64(0)
	createdAt := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	updatedAt := createdAt.Add(time.Hour)
	payload := []byte{0, 1, 255}
	err := q.InsertRecords(ctx, sq.InsertRecordsParams{
		OwnerID: ownerID, CreatedAt: createdAt,
		Rows: []sq.InsertRecordsRowsItem{
			{RecordID: "a", GroupID: "new", Payload: payload, Attributes: `["red"]`},
			{RecordID: "b", GroupID: "new", Payload: []byte("second"), Attributes: `["blue"]`},
		},
	})
	require.NoError(t, err)
	rows, err := q.ListRecords(ctx, ownerID)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	key := sq.GetRecordKey{OwnerHash: rows[0].OwnerHash, RecordID: "a"}
	row, err := q.GetRecord(ctx, key)
	require.NoError(t, err)
	require.Equal(t, ownerID, row.OwnerID)
	require.True(t, bytes.Equal(row.Payload, payload))
	require.Equal(t, `["red"]`, row.Attributes)
	require.True(t, row.CreatedAt.Equal(createdAt))
	err = q.UpdateRecords(ctx, sq.UpdateRecordsParams{
		OwnerID: ownerID, UpdatedAt: updatedAt,
		Rows: []sq.UpdateRecordsRowsItem{
			{RecordID: "a", GroupID: "changed", Payload: []byte("updated"), Attributes: `["green"]`},
			{RecordID: "missing", GroupID: "changed", Payload: payload, Attributes: `[]`},
		},
	})
	require.NoError(t, err)
	row, err = q.GetRecord(ctx, key)
	require.NoError(t, err)
	require.Equal(t, "changed", row.GroupID)
	require.Equal(t, "updated", string(row.Payload))
	require.True(t, row.CreatedAt.Equal(createdAt))
	require.True(t, row.UpdatedAt.Equal(updatedAt))
	rows, err = q.ListRecords(ctx, ownerID)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "new", rows[1].GroupID)
	filtered, err := q.FilterRecords(ctx, sq.FilterRecordsParams{OwnerID: ownerID, RecordIds: []string{"a", "b"}, GroupID: "changed"})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	require.Equal(t, "a", filtered[0].RecordID)
	tagged, err := q.FindRecordsByTags(ctx, [][]byte{[]byte("green")})
	require.NoError(t, err)
	require.Len(t, tagged, 1)
	require.Equal(t, "a", tagged[0].RecordID)
	tagged, err = q.FindRecordsByTags(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, tagged)
	err = q.DeleteRecords(ctx, sq.DeleteRecordsParams{OwnerID: ownerID, RecordIds: []string{"a", "b"}, GroupID: "changed"})
	require.NoError(t, err)
	rows, err = q.ListRecords(ctx, ownerID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "b", rows[0].RecordID)
	err = q.UpsertRecords(ctx, sq.UpsertRecordsParams{
		OwnerID: ownerID, CreatedAt: updatedAt,
		Rows: []sq.UpsertRecordsRowsItem{
			{RecordID: "b", GroupID: "replaced", Payload: payload, Attributes: `[]`},
			{RecordID: "c", GroupID: "inserted", Payload: payload, Attributes: `[]`},
		},
	})
	require.NoError(t, err)
	rows, err = q.ListRecords(ctx, ownerID)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "replaced", rows[0].GroupID)
	require.Equal(t, "inserted", rows[1].GroupID)
	require.NoError(t, q.InsertRecords(ctx, sq.InsertRecordsParams{OwnerID: ownerID, CreatedAt: createdAt}), "empty batch")
	label := "Москва"
	reversed, err := q.ReverseGroupLabel(ctx, &label)
	require.NoError(t, err)
	require.NotNil(t, reversed.ReversedLabel)
	require.Equal(t, "авксоМ", *reversed.ReversedLabel)
	reversed, err = q.ReverseGroupLabel(ctx, nil)
	require.NoError(t, err)
	require.Nil(t, reversed.ReversedLabel)
}
