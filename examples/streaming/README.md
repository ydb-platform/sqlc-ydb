# Callback exports

This example exports an ordered range of devices as newline-delimited JSON using `:each`. The same [queries](queries.sql) generate typed callbacks for both supported Go profiles: [native YDB](go/native) and [database/sql](go/database/sql). Nullable names remain pointers, callbacks run sequentially, and writer errors stop the export. `VisitAllDevices` demonstrates the no-parameter API.

```sql
-- name: VisitDevices :each
SELECT id, name
FROM streaming_devices
WHERE id BETWEEN $min_id AND $max_id
ORDER BY id;
```

The analyzer infers the `Uint64` bounds from `streaming_devices.id`, so this query needs no `DECLARE` for them.

With the native package imported as `devices`, a caller-owned session or transaction can stream into an encoder:

```go
q := devices.New(session)
encoder := json.NewEncoder(output)
err := q.VisitDevices(ctx, devices.VisitDevicesParams{MinID: 1, MaxID: 1000},
    func(row devices.VisitDevicesRow) error {
        return encoder.Encode(row)
    },
)
```

The `database/sql` profile has the same callback shape and accepts `*sql.DB`, `*sql.Conn` or `*sql.Tx`. Native `New(client)` is also supported and retains the SDK's materialized-result behavior; the generated method uses the selected executor directly. The [runnable acceptance test](../../tests/examples/go/streaming/streaming_test.go) compiles and executes native client, session and transaction calls plus SQL client and transaction calls. Its SDK-owned operations explicitly use a zero retry budget because replaying an export could duplicate output; generation adds no retry policy.

Only the two Go profiles support `:each`. Other language/runtime profiles reject it; no additional SQL expressions were enabled by this feature. The shared CLI tests verify this rejection for every other configured runtime. See [streaming callbacks](../../docs/streaming.md) for cancellation, error and resource ownership contracts.

From the repository root:

```sh
make generate
make check-examples
```

The live example needs a disposable YDB with no pre-existing `streaming_devices` table. It creates and removes that table and runs each executor sequentially:

```sh
cd tests/examples/go
YDB_CONNECTION_STRING=grpc://localhost:2136/local go test -p 1 -count=1 ./streaming -v
```
