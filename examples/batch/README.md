# Batch

This adapts the upstream sqlc `batch` example at commit `3c2546a4b47fabbcec3e07df420effb1a464728f` to YDB. The upstream example queues separate PostgreSQL statements with `pgx.Batch`. The YDB Go query API has no equivalent queue, so the handwritten Go integration uses the YDB table client's `BulkUpsert` operation to send all book rows in one request. Bulk upsert is non-transactional and can apply only some rows when an error is returned.

The SQL file retains ordinary generated equivalents of the original create, read, update, and delete operations. Upstream `:batchone`, `:batchmany`, and `:batchexec` annotations become their ordinary `:one`, `:many`, and `:exec` forms; those methods do not claim batch behavior. `:execresult` becomes `:exec` because the generated YDB APIs do not expose a portable command-result type. PostgreSQL `sqlc.arg(book_id)` and `@book_id` use the explicitly declared YQL parameter `$book_id`.

PostgreSQL enum values (`FICTION`, `NONFICTION`) are stored as `Utf8` and validated by the application. Foreign keys, unique ISBN constraints and defaults from the upstream schema are omitted. Biography and tag arrays use YDB `Json`; the Go batch helper accepts their encoded JSON strings. IDs and timestamps are supplied explicitly because the YDB schema does not use the PostgreSQL serial and timestamp defaults from the source example.

`BulkUpsertBooks` accepts the generated `CreateBookParams` type; an empty slice does not send a request. The [live test](../../tests/examples/go/batch/batch_test.go) shows bulk writes followed by generated reads and updates.

## SQL batch insert

`CreateBooks` inserts a list of books with one `INSERT INTO books (...) SELECT ... FROM AS_TABLE($books)` statement. The declared `List<Struct<...>>` retains each field's YQL type, including `Json` tags and the `Timestamp` availability date. Generated methods accept a collection of named item values and build the SDK parameter internally. The native Go method is `CreateBooks(ctx context.Context, books []CreateBooksBooksItem, opts ...query.ExecuteOption) error`; the `database/sql` method accepts the same item type without execute options. Go item fields use the same types as `CreateBookParams`, including `string` for JSON.

This SQL insert participates in the query's transaction and retains `INSERT` duplicate-key semantics. An empty collection is sent as a typed empty list. The existing `BulkUpsertBooks` helper remains a separate non-transactional bulk-upsert example. The shared runtime tests cover empty and populated SQL batches and read the inserted books through generated methods.

## SQL batch writes by column name

`CreateAuthors` and `UpsertAuthors` omit the target column list, so SELECT result names determine the destination columns. Their Struct declares `name` before `author_id`, unlike the table schema, and includes a trailing comma. `CreateAuthors` combines `a.*` with `NULL AS biography`; `UpsertAuthors` uses a bare `*` and omits the optional biography column. Updating an existing author therefore changes the name while preserving the biography; inserting a new author leaves the biography NULL. Both required fields, `author_id` and `name`, must be supplied.

The analyzer expands these wildcards into explicit named projections before generation. The [shared configuration](sqlc.yaml) generates both methods for every supported runtime profile: Go native/database/sql, Python native/DB-API/SQLAlchemy, C++ native/userver, C# ADO.NET/Dapper, Java native/JDBC/jOOQ, Kotlin native/JDBC/Exposed, TypeScript, Rust and PHP. Each accepts a collection of typed author items, including an empty collection. The [Go acceptance test](../../tests/examples/go/batch/batch_test.go) exercises both methods through the native SDK and database/sql and checks nullable-column preservation.

Unlike the explicit-target `CreateBooks` example, these queries match fields by name rather than SELECT position. They remain ordinary transactional INSERT/UPSERT statements, with transaction boundaries controlled by the caller through the selected runtime's generated API.
