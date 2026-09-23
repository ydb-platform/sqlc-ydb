# Typed record operations

A record store with a hashed owner key, batch writes and filtered reads/deletes. This example demonstrates the typed DML, Struct/list bindings and function signatures introduced in [PR #20](https://github.com/ydb-platform/sqlc-ydb/pull/20), together with wildcard expansion from [PR #23](https://github.com/ydb-platform/sqlc-ydb/pull/23). It generates Go clients for the native YDB Query SDK and `database/sql`.

## Queries to try

| Query in [queries.sql](queries.sql) | Capability |
| --- | --- |
| `InsertRecords`, `UpsertRecords` | `INSERT/UPSERT ... SELECT` maps explicit target columns by position; `Digest::CityHash` computes the owner hash, while `AS_TABLE($rows)` supplies typed `List<Struct<...>>` rows. Computed SELECT expressions do not need aliases. |
| `UpdateRecords` | `UPDATE ... ON SELECT` identifies rows by the complete primary key and updates only the supplied columns. `r.*` expands the batch fields; `created_at` and `owner_id` are preserved, and missing keys are not inserted. |
| `GetRecord` | A single generated key struct binds `Struct<owner_hash: Uint64, record_id: Utf8>`; SQL accesses its fields as `$key.owner_hash` and `$key.record_id`. |
| `FilterRecords`, `DeleteRecords` | A typed `List<Utf8>` binds `IN $record_ids`; `DELETE ... ON SELECT` combines that list with owner and group predicates. |
| `ListRecords` | `SELECT *` is expanded to fixed columns before generation. |
| `FindRecordsByTags` | JSON arrays of tags are converted with `Yson::ConvertToStringList` and compared with a scalar list using `ToSet` and `SetIsDisjoint`. Store JSON arrays in `attributes` for this query. |
| `ReverseGroupLabel` | The built-in `Unicode::Reverse` signature accepts a nullable label through AutoMap and returns the reversed Unicode text. |

The built-in [`Unicode::Reverse`](https://ydb.tech/docs/en/yql/reference/udf/list/unicode) contract is `Utf8{Flags:AutoMap} -> Utf8`. AutoMap makes a null input return null, represented as `*string` in Go. See [function signatures](../../docs/functions.md).

## Generated calls

With an initialized native Query SDK client, insertion looks like this:

```go
q := records.New(driver.Query())
err := q.InsertRecords(ctx, records.InsertRecordsParams{
    OwnerID: 42,
    CreatedAt: time.Now().UTC(),
    Rows: []records.InsertRecordsRowsItem{
        {
            RecordID: "first",
            GroupID: "inbox",
            Payload: []byte("message"),
            Attributes: `["new","important"]`,
        },
    },
})
```

Import `example.com/sqlc-ydb-examples/records/go/native` as `records` within the example module. For `database/sql`, import `records/go/database/sql` from the same module and pass `*sql.DB` to `New`. The [executable tests](../../tests/examples/go/records/smoke_test.go) show complete calls for both adapters, including a Struct lookup, partial updates, list predicates, replacement through UPSERT and nullable Unicode function calls. The generated code preserves `Uint64`, binary `Bytes`, `Json` and `Timestamp` values.

From the repository root, `make generate` regenerates the committed clients and `make check-examples` checks generation and Go compilation. For live execution against a disposable database without a `records` table:

```sh
cd tests/examples/go
YDB_CONNECTION_STRING=grpc://localhost:2136/local \
  go test -p 1 -count=1 -timeout=180s ./records -v
```

Run live suites sequentially. The tests create and drop their table; without the environment variable they compile and skip execution. These focused examples select the two Go adapters; SDK-specific limits, including jOOQ's SELECT-backed DML boundary, remain in [compatibility](../../docs/compatibility.md) and [Java](../../docs/java.md).
