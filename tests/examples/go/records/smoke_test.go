package records_test

import (
	"bytes"
	"testing"
	"time"

	"example.com/sqlc-ydb-example-tests/internal/testdb"
	sq "example.com/sqlc-ydb-examples/records/go/database/sql"
	native "example.com/sqlc-ydb-examples/records/go/native"
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
	if err != nil {
		t.Fatal(err)
	}
	rows, err := q.ListRecords(ctx, ownerID)
	if err != nil || len(rows) != 2 {
		t.Fatalf("insert/list: %+v, %v", rows, err)
	}
	key := native.GetRecordKey{OwnerHash: rows[0].OwnerHash, RecordID: "a"}
	row, err := q.GetRecord(ctx, key)
	if err != nil || row.OwnerID != ownerID || !bytes.Equal(row.Payload, payload) || row.Attributes != `["red"]` || !row.CreatedAt.Equal(createdAt) {
		t.Fatalf("Struct key and scalar values: %+v, %v", row, err)
	}
	err = q.UpdateRecords(ctx, native.UpdateRecordsParams{
		OwnerID: ownerID, UpdatedAt: updatedAt,
		Rows: []native.UpdateRecordsRowsItem{
			{RecordID: "a", GroupID: "changed", Payload: []byte("updated"), Attributes: `["green"]`},
			{RecordID: "missing", GroupID: "changed", Payload: payload, Attributes: `[]`},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err = q.GetRecord(ctx, key)
	if err != nil || row.GroupID != "changed" || string(row.Payload) != "updated" || !row.CreatedAt.Equal(createdAt) || !row.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("partial UPDATE preserves created_at: %+v, %v", row, err)
	}
	rows, err = q.ListRecords(ctx, ownerID)
	if err != nil || len(rows) != 2 || rows[1].GroupID != "new" {
		t.Fatalf("UPDATE must not insert missing keys or change other rows: %+v, %v", rows, err)
	}
	filtered, err := q.FilterRecords(ctx, native.FilterRecordsParams{OwnerID: ownerID, RecordIds: []string{"a", "b"}, GroupID: "changed"})
	if err != nil || len(filtered) != 1 || filtered[0].RecordID != "a" {
		t.Fatalf("typed IN list: %+v, %v", filtered, err)
	}
	tagged, err := q.FindRecordsByTags(ctx, [][]byte{[]byte("green")})
	if err != nil || len(tagged) != 1 || tagged[0].RecordID != "a" {
		t.Fatalf("JSON tag intersection: %+v, %v", tagged, err)
	}
	tagged, err = q.FindRecordsByTags(ctx, nil)
	if err != nil || len(tagged) != 0 {
		t.Fatalf("empty tag list: %+v, %v", tagged, err)
	}
	err = q.DeleteRecords(ctx, native.DeleteRecordsParams{OwnerID: ownerID, RecordIds: []string{"a", "b"}, GroupID: "changed"})
	if err != nil {
		t.Fatal(err)
	}
	rows, err = q.ListRecords(ctx, ownerID)
	if err != nil || len(rows) != 1 || rows[0].RecordID != "b" {
		t.Fatalf("filtered DELETE ON: %+v, %v", rows, err)
	}
	err = q.UpsertRecords(ctx, native.UpsertRecordsParams{
		OwnerID: ownerID, CreatedAt: updatedAt,
		Rows: []native.UpsertRecordsRowsItem{
			{RecordID: "b", GroupID: "replaced", Payload: payload, Attributes: `[]`},
			{RecordID: "c", GroupID: "inserted", Payload: payload, Attributes: `[]`},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, err = q.ListRecords(ctx, ownerID)
	if err != nil || len(rows) != 2 || rows[0].GroupID != "replaced" || rows[1].GroupID != "inserted" {
		t.Fatalf("UPSERT SELECT updates and inserts: %+v, %v", rows, err)
	}
	if err := q.InsertRecords(ctx, native.InsertRecordsParams{OwnerID: ownerID, CreatedAt: createdAt}); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
	label := "Москва"
	reversed, err := q.ReverseGroupLabel(ctx, &label)
	if err != nil || reversed.ReversedLabel == nil || *reversed.ReversedLabel != "авксоМ" {
		t.Fatalf("configured function: %+v, %v", reversed, err)
	}
	reversed, err = q.ReverseGroupLabel(ctx, nil)
	if err != nil || reversed.ReversedLabel != nil {
		t.Fatalf("configured AutoMap: %+v, %v", reversed, err)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	rows, err := q.ListRecords(ctx, ownerID)
	if err != nil || len(rows) != 2 {
		t.Fatalf("insert/list: %+v, %v", rows, err)
	}
	key := sq.GetRecordKey{OwnerHash: rows[0].OwnerHash, RecordID: "a"}
	row, err := q.GetRecord(ctx, key)
	if err != nil || row.OwnerID != ownerID || !bytes.Equal(row.Payload, payload) || row.Attributes != `["red"]` || !row.CreatedAt.Equal(createdAt) {
		t.Fatalf("Struct key and scalar values: %+v, %v", row, err)
	}
	err = q.UpdateRecords(ctx, sq.UpdateRecordsParams{
		OwnerID: ownerID, UpdatedAt: updatedAt,
		Rows: []sq.UpdateRecordsRowsItem{
			{RecordID: "a", GroupID: "changed", Payload: []byte("updated"), Attributes: `["green"]`},
			{RecordID: "missing", GroupID: "changed", Payload: payload, Attributes: `[]`},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err = q.GetRecord(ctx, key)
	if err != nil || row.GroupID != "changed" || string(row.Payload) != "updated" || !row.CreatedAt.Equal(createdAt) || !row.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("partial UPDATE preserves created_at: %+v, %v", row, err)
	}
	rows, err = q.ListRecords(ctx, ownerID)
	if err != nil || len(rows) != 2 || rows[1].GroupID != "new" {
		t.Fatalf("UPDATE must not insert missing keys or change other rows: %+v, %v", rows, err)
	}
	filtered, err := q.FilterRecords(ctx, sq.FilterRecordsParams{OwnerID: ownerID, RecordIds: []string{"a", "b"}, GroupID: "changed"})
	if err != nil || len(filtered) != 1 || filtered[0].RecordID != "a" {
		t.Fatalf("typed IN list: %+v, %v", filtered, err)
	}
	tagged, err := q.FindRecordsByTags(ctx, [][]byte{[]byte("green")})
	if err != nil || len(tagged) != 1 || tagged[0].RecordID != "a" {
		t.Fatalf("JSON tag intersection: %+v, %v", tagged, err)
	}
	tagged, err = q.FindRecordsByTags(ctx, nil)
	if err != nil || len(tagged) != 0 {
		t.Fatalf("empty tag list: %+v, %v", tagged, err)
	}
	err = q.DeleteRecords(ctx, sq.DeleteRecordsParams{OwnerID: ownerID, RecordIds: []string{"a", "b"}, GroupID: "changed"})
	if err != nil {
		t.Fatal(err)
	}
	rows, err = q.ListRecords(ctx, ownerID)
	if err != nil || len(rows) != 1 || rows[0].RecordID != "b" {
		t.Fatalf("filtered DELETE ON: %+v, %v", rows, err)
	}
	err = q.UpsertRecords(ctx, sq.UpsertRecordsParams{
		OwnerID: ownerID, CreatedAt: updatedAt,
		Rows: []sq.UpsertRecordsRowsItem{
			{RecordID: "b", GroupID: "replaced", Payload: payload, Attributes: `[]`},
			{RecordID: "c", GroupID: "inserted", Payload: payload, Attributes: `[]`},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, err = q.ListRecords(ctx, ownerID)
	if err != nil || len(rows) != 2 || rows[0].GroupID != "replaced" || rows[1].GroupID != "inserted" {
		t.Fatalf("UPSERT SELECT updates and inserts: %+v, %v", rows, err)
	}
	if err := q.InsertRecords(ctx, sq.InsertRecordsParams{OwnerID: ownerID, CreatedAt: createdAt}); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
	label := "Москва"
	reversed, err := q.ReverseGroupLabel(ctx, &label)
	if err != nil || reversed.ReversedLabel == nil || *reversed.ReversedLabel != "авксоМ" {
		t.Fatalf("configured function: %+v, %v", reversed, err)
	}
	reversed, err = q.ReverseGroupLabel(ctx, nil)
	if err != nil || reversed.ReversedLabel != nil {
		t.Fatalf("configured AutoMap: %+v, %v", reversed, err)
	}
}
