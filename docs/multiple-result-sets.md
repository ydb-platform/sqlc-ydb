# Multiple result sets

Use `:multi` when two or more read-only top-level SELECT statements should execute in one YDB request and return different row shapes. It is supported by `gen.go.sql_package: ydb` and `database/sql`, `gen.python.runtime: ydb`, and Java `runtime: jdbc`; every other target rejects it during generation. A UNION inside one SELECT remains one result set. DML, including RETURNING, is not supported by `:multi`.

```sql
-- name: FetchSummary :multi
DECLARE $id AS Uint64;
-- result: Item
SELECT $id AS id;
-- result: Flags
SELECT true AS enabled LIMIT 1;
SELECT "ready"u AS status;
```

The annotation owns SQL up to the next `-- name:` line or end of file. Put an optional `-- result: Name` comment on its own line immediately before a top-level SELECT to name its result field. Names must be distinct exported Go identifiers; missing names become `Result1`, `Result2`, and so on by statement position. Explicit names cannot collide with default names. Python converts result field names to snake_case and rejects collisions after conversion. These comments stay in the executable YQL unchanged.

The generated method returns one struct whose fields are typed slices, even for an empty SELECT or one with `LIMIT 1`:

```go
type FetchSummaryResult struct {
    Item    []FetchSummaryItemRow
    Flags   []FetchSummaryFlagsRow
    Result3 []FetchSummaryResult3Row
}
```

Both Go adapters submit the complete SQL once, consume all three result sets, check each set's column names, order and YQL types against the analyzed schema, and check for missing or extra sets and late stream errors. They close the result before returning. An error returns a zero result, without partial slices. `emit_empty_slices` makes successful empty fields non-nil; by default they are nil. `gen.go.rename` changes row field names, not SQL result names or `-- result:` names.

Python native returns a dataclass with one typed `list` field per SELECT, including empty and `LIMIT 1` results. For the example above, the fields are `item`, `flags` and `result3`. It checks each result set's column names, order and YQL types before building the dataclass and raises on missing, extra, truncated or mismatched results. The native SDK completes the result stream before returning it, so a late server error does not produce a partial result. Python DB-API 0.1.23 merges rows from separate result sets into one iterator and its `nextset()` always returns `False`; SQLAlchemy uses that driver. Both profiles reject `:multi` during generation.

The supplied executor owns transaction boundaries and retry policy. The method does not start a transaction, replay a partly consumed stream or split the script into separate calls. Native client, session and transaction executors, plus `database/sql` clients and transactions, are exercised in the [live example](../examples/multi_results/README.md). Python native accepts a `QuerySessionPool` or a caller-owned `QueryTxContext`.

Java JDBC generates one record per result set and a result record with `List<Row>` fields named from the annotations (`item`, `flags`, `result3` in the example). Successful empty fields are non-null empty lists. The method executes the complete SQL once through a borrowed `Connection`, checks each result set's column count, names, order, YQL types and nullability before reading rows, then checks for missing or extra sets and late errors. It closes its statement and result sets, leaves the connection and transaction with the caller, and throws `SQLException` without returning partial results on error. Java result names must also be representable as Java identifiers. The [Java example](../examples/multi_results/README.md) exercises both buffered and streaming JDBC modes.
