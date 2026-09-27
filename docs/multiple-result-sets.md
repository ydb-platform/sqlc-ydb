# Multiple result sets in Go

Use `:multi` when two or more read-only top-level SELECT statements should execute in one YDB request and return different row shapes. It is supported by `gen.go.sql_package: ydb` and `database/sql`; every other target rejects it during generation. A UNION inside one SELECT remains one result set. DML, including RETURNING, is not supported by `:multi`.

```sql
-- name: FetchSummary :multi
DECLARE $id AS Uint64;
-- result: Item
SELECT $id AS id;
-- result: Flags
SELECT true AS enabled LIMIT 1;
SELECT "ready"u AS status;
```

The annotation owns SQL up to the next `-- name:` line or end of file. Put an optional `-- result: Name` comment on its own line immediately before a top-level SELECT to name its result field. Names must be distinct exported Go identifiers; missing names become `Result1`, `Result2`, and so on by statement position. Explicit names cannot collide with default names. These comments stay in the executable YQL unchanged.

The generated method returns one struct whose fields are typed slices, even for an empty SELECT or one with `LIMIT 1`:

```go
type FetchSummaryResult struct {
    Item    []FetchSummaryItemRow
    Flags   []FetchSummaryFlagsRow
    Result3 []FetchSummaryResult3Row
}
```

Both Go adapters submit the complete SQL once, consume all three result sets, check each set's column names, order and YQL types against the analyzed schema, and check for missing or extra sets and late stream errors. They close the result before returning. An error returns a zero result, without partial slices. `emit_empty_slices` makes successful empty fields non-nil; by default they are nil. `gen.go.rename` changes row field names, not SQL result names or `-- result:` names.

The supplied executor owns transaction boundaries and retry policy. The method does not start a transaction, replay a partly consumed stream or split the script into separate calls. Native client, session and transaction executors, plus `database/sql` clients and transactions, are exercised in the [live example](../examples/multi_results/README.md).
