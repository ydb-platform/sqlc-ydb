# Streaming callbacks

Use `:each` for exports and background scans that consume typed rows through a callback. Whether rows arrive incrementally depends on the selected SDK executor or driver mode. It is a sqlc-ydb annotation, supported by the Go native YDB and `database/sql` profiles and Java JDBC. Other targets reject it. Existing `:many` queries continue to return a complete collection.

## Go query and generated API

For a table with `id Uint64 NOT NULL` and nullable `name Utf8`, `queries.sql` can contain:

```sql
-- name: VisitDevices :each
SELECT id, name
FROM devices
WHERE id BETWEEN $min_id AND $max_id
ORDER BY id;
```

The analyzer infers the bounds' `Uint64` types from `id`, so this query does not need `DECLARE` statements for them.

The generated Go types are:

```go
type VisitDevicesParams struct {
    MinID uint64
    MaxID uint64
}

type VisitDevicesRow struct {
    ID   uint64
    Name *string
}
```

The native YDB method accepts the usual execution options after its callback:

```go
func (q *Queries) VisitDevices(
    ctx context.Context,
    arg VisitDevicesParams,
    consume func(VisitDevicesRow) error,
    opts ...query.ExecuteOption,
) error
```

The `database/sql` signature omits `opts`. Queries with one parameter take that parameter directly; queries with no parameters take only the context, callback and, for native YDB, options. `emit_interface` includes these methods in `Querier`. Row types, tags and parameter binding use the same rules as `:many`. The [streaming example](../examples/streaming) includes generated code and runnable exports for both Go profiles. The [executable fixture](../internal/endtoend/testdata/each) includes zero, one and multiple parameters and both profiles.

```go
q := db.New(session)
err := q.VisitDevices(ctx, db.VisitDevicesParams{MinID: 0, MaxID: 1000},
    func(row db.VisitDevicesRow) error {
        return encoder.Encode(row)
    },
)
```

Here `db` is the generated package, `session` is a caller-owned YDB query session and `encoder` is an application-owned encoder. A nil callback is rejected before executing SQL. `:each` accepts exactly one SELECT result set; DML, including DML RETURNING, is rejected. The annotation does not select ScanQuery, change transaction isolation, or add SQL limits.

## Go consumption, cancellation and errors

Callbacks run synchronously, one at a time, in the order received from YDB. Use `ORDER BY` when the SQL must guarantee order. The generated method does not retain previous rows or create a producer queue. The SDK may buffer response parts; an application can still consume unbounded memory by retaining rows inside its callback. An empty successful result returns nil without invoking the callback.

Returning a callback error stops consumption immediately. The method cancels its unfinished query stream and closes its result. Use an application-owned error for deliberate early termination and check it with `errors.Is`; there is no special success sentinel. In the native profile, closing the canceled stream can return either nil or `context.Canceled`, depending on whether SDK cancellation has finished. Consequently, an early-stop error can also satisfy `errors.Is(err, context.Canceled)` while the original caller context remains active. Check that original context's `Err()` to distinguish caller cancellation from cancellation initiated by cleanup. Context cancellation stops further delivery, but cannot interrupt an already-running callback: a long-running callback must observe the context itself.

Execution, decoding, iteration and cleanup failures are returned. Cleanup errors returned by the SDK/driver are joined with the primary error, preserving `errors.Is` and `errors.As`. During cancellation, `database/sql` can close rows asynchronously and expose the context error instead of a driver close error. Native methods also add the SDK stack trace. Callback panics propagate after deferred cancellation and cleanup; the generated method does not recover them.

The generated method owns the query result and closes it on every exit. The caller retains ownership of the client, connection, session and transaction. It must keep these resources alive until the method returns. After early cancellation, immediate reuse of the same native Go session/transaction is not guaranteed: YDB may still report `SESSION_BUSY` while finishing the canceled query. Return failures to the SDK operation or transaction owner so it can handle that resource correctly. Avoid issuing another query on the same session/transaction or a single-connection SQL pool from inside a callback while its result is open.

## Go executor choice and retries

Generated native methods call `Query` on exactly the object passed to `New`. The caller chooses its execution and buffering behavior:

| Constructor | Pinned SDK behavior |
| --- | --- |
| `New(driver.Query())` | `query.Client.Query` materializes the full result before callbacks begin. |
| `New(session)` | `Session.Query` streams rows from the session. |
| `New(tx)` | `TxActor.Query` streams rows inside the caller's transaction. |

The generator does not inspect the executor type, acquire sessions, change retry settings, commit or roll back transactions. `:each` controls how the generated method consumes the result; it adds no full-result collection of its own. SDK buffering and retries remain those of the supplied executor, including custom `DBTX` wrappers. A native client may retry and materialize before returning its result, before any callback runs. Callback errors cannot cause the generated method to replay previously delivered rows because it adds no retry loop.

With `database/sql`, use the SDK's Query Service connector (`sql.OpenDB(ydb.MustConnector(driver))`). Its rows stream through `Session.Query` or the active transaction. The generated method invokes `QueryContext` once and never retries after row delivery. The standard library can retry a bad connection before returning rows. A custom driver or a legacy table-service mode may buffer results; `:each` cannot change the driver's execution model.

Callbacks can produce external effects before a later read, cleanup or transaction commit fails. Receiving a row is not a commit acknowledgment. A caller-owned `Do`, `DoTx` or application retry loop can still rerun the entire operation: callers must decide whether those external effects are safe to repeat. The generated method provides no exactly-once delivery across such retries.

## Java JDBC

Java JDBC `:each` methods accept a `java.util.function.Consumer<Row>` after their ordinary parameters and return `void`. The generated method requires a YDB JDBC connection configured with `useStreamResultSets=true`; it checks the pinned driver's connection setting before executing SQL and raises `SQLException` if buffering is enabled. For example, pass a `Properties` object containing `useStreamResultSets=true` to `DriverManager.getConnection("jdbc:ydb:" + endpoint, properties)`. The [streaming example](../examples/streaming) includes generated Java code and a runnable smoke test. Native Java SDK and jOOQ continue to reject `:each`.

The driver delivers rows as result parts arrive. The callback runs synchronously for each decoded row; a `null` callback is rejected before SQL execution. An unchecked callback exception stops iteration and propagates unchanged. The generated method closes its result set and prepared statement on success, callback failure, decoding failure, or SQL error. In the pinned JDBC driver, closing the statement cancels an unfinished result stream. Reading through EOF and advancing the statement reports a later error or an unexpected second result set. The caller owns the connection and transaction; a callback must not issue another query on that connection while its result is open. A caller-owned transaction is neither committed nor rolled back by the generated method.
