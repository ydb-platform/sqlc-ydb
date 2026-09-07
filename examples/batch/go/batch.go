// Package batch contains the handwritten YDB bulk operation for this example.
package batch

import (
	"context"

	native "example.com/sqlc-ydb-examples/batch/go/native"
	"github.com/ydb-platform/ydb-go-sdk/v3/table"
	"github.com/ydb-platform/ydb-go-sdk/v3/types"
)

// BulkUpsertBooks sends all rows in one YDB BulkUpsert request. YDB bulk upsert
// is non-transactional: an error may be returned after some rows were applied.
func BulkUpsertBooks(ctx context.Context, client table.Client, tablePath string, books []native.CreateBookParams) error {
	if len(books) == 0 {
		return nil
	}
	rows := make([]types.Value, 0, len(books))
	for _, book := range books {
		rows = append(rows, types.StructValue(
			types.StructFieldValue("book_id", types.Uint64Value(book.BookID)),
			types.StructFieldValue("author_id", types.Uint64Value(book.AuthorID)),
			types.StructFieldValue("isbn", types.UTF8Value(book.Isbn)),
			types.StructFieldValue("book_type", types.UTF8Value(book.BookType)),
			types.StructFieldValue("title", types.UTF8Value(book.Title)),
			types.StructFieldValue("year", types.Int32Value(book.Year)),
			types.StructFieldValue("available", types.TimestampValueFromTime(book.Available)),
			types.StructFieldValue("tags", types.JSONValue(book.Tags)),
		))
	}
	return client.BulkUpsert(ctx, tablePath, table.BulkUpsertDataRows(types.ListValue(rows...)))
}
