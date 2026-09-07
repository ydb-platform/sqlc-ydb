# Batch

This adapts the upstream sqlc `batch` example at commit
`3c2546a4b47fabbcec3e07df420effb1a464728f` to YDB. The upstream example queues
separate PostgreSQL statements with `pgx.Batch`. The YDB Go query API has no
equivalent queue, so the handwritten Go integration uses the YDB table client's
`BulkUpsert` operation to send all book rows in one request. Bulk upsert is
non-transactional and can apply only some rows when an error is returned.

The SQL file retains ordinary generated equivalents of the original create,
read, update, and delete operations. Upstream `:batchone`, `:batchmany`, and
`:batchexec` annotations become their ordinary `:one`, `:many`, and `:exec`
forms; those methods do not claim batch behavior. `:execresult` becomes `:exec`
because the generated YDB APIs do not expose a portable command-result type.
PostgreSQL `sqlc.arg(book_id)` and `@book_id` use the explicitly declared YQL
parameter `$book_id`.

PostgreSQL enum values (`FICTION`, `NONFICTION`) are stored as `Utf8` and validated
by the application. Foreign keys, unique ISBN constraints and defaults from the
upstream schema are omitted. Biography and tag arrays use YDB
`Json`; the Go batch helper accepts their encoded JSON strings. IDs and
timestamps are supplied explicitly because the YDB schema does not use the
PostgreSQL serial and timestamp defaults from the source example.

`BulkUpsertBooks` accepts the generated `CreateBookParams` type; an empty slice
does not send a request. The [live test](go/batch_test.go) shows bulk writes
followed by generated reads and updates.
