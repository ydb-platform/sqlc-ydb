# Computed counters and database analysis

A small counter table demonstrates constants and computed DML from [PR #24](https://github.com/ydb-platform/sqlc-ydb/pull/24), plus fixed wildcard projections and database-assisted analysis from [PR #23](https://github.com/ydb-platform/sqlc-ydb/pull/23). The default [configuration](sqlc.yaml) generates Go native Query SDK and `database/sql` clients entirely offline.

## Queries to try

| Query in [queries.sql](queries.sql) | Capability |
| --- | --- |
| `CreateCounter` | `INSERT ... VALUES` mixes a parameter with numeric, `NULL`, Utf8 and Boolean constants; `RETURNING *` returns the created row. |
| `IncrementCounter` | `UPDATE ... SET value = value + $delta` performs an increment in YDB and returns the resulting row. |
| `TransformCounter` | Parenthesized `+`, `*` and `-`, `COALESCE` for an optional counter, a constant label and a Boolean comparison assignment. All SET expressions read the original row. |
| `ClearOptional` | `SET optional_value = NULL, label = NULL` clears optional columns. |
| `UpsertCounter` | `UPSERT ... VALUES` computes a value from the declared `$seed` parameter. |
| `ReadCounter`, `ListCounters` | `SELECT c.*` and `SELECT *` become explicit projections in the generated SQL. |

For example, creating a counter, incrementing by 5 and then transforming it yields values 0, 5 and 17. The transformed `enabled` value is false because `value > 10l` reads the original value 5. The [executable Go tests](../../tests/examples/go/counters/smoke_test.go) demonstrate both adapters, transaction rollback and SELECT/RETURNING after an unrelated column is added. A native SDK call is:

```go
q := counters.New(driver.Query())
row, err := q.IncrementCounter(ctx, counters.IncrementCounterParams{
    ID: "requests",
    Delta: 1,
})
```

Import `example.com/sqlc-ydb-examples/counters/go/native` as `counters` within the example module; the `database/sql` package has the same parameters and takes `*sql.DB` or a transaction. Transaction boundaries and retries remain owned by the SDK/caller. Compound expressions need typed operands, so this example declares its external parameters explicitly. Division, remainder, unary numeric operators and Decimal/temporal arithmetic remain outside the [supported arithmetic contract](../../docs/compatibility.md).

## Three generation modes

Run these commands from the repository root after `make generate` builds `bin/sqlc-ydb`. The connected modes require an existing `counters` table matching [schema.sql](schema.sql) in a disposable database. Apply that schema separately, for example through the [local-ydb initialization recipe](../../docs/database-analysis.md#prepare-a-disposable-local-database); the generator never applies DDL or executes application writes.

| Configuration | Schema source | Generated output |
| --- | --- | --- |
| [sqlc.yaml](sqlc.yaml) | Local `schema.sql`, no connection | Committed `go/native` and `go/database/sql` |
| [sqlc.database.yaml](sqlc.database.yaml) | Local schema checked against live metadata; server EXPLAIN validates queries | The same `go/native` output |
| [sqlc.discovery.yaml](sqlc.discovery.yaml) | Live DescribeTable metadata, with no local `schema` option; server EXPLAIN validates queries | Ignored `build/discovered` scratch output |

```sh
# Offline generation is the default and is included in make generate.
./bin/sqlc-ydb generate -f examples/counters/sqlc.yaml

export YDB_CONNECTION_STRING=grpc://localhost:2136/local
# Validate schema/query compatibility and confirm identical committed native output.
./bin/sqlc-ydb compile -f examples/counters/sqlc.database.yaml
./bin/sqlc-ydb diff -f examples/counters/sqlc.database.yaml

# Discover the schema and generate a separate client.
./bin/sqlc-ydb generate -f examples/counters/sqlc.discovery.yaml
./bin/sqlc-ydb diff -f examples/counters/sqlc.discovery.yaml

# Force offline analysis using the local schema, without resolving credentials.
./bin/sqlc-ydb diff --no-database -f examples/counters/sqlc.database.yaml
```

Discovery uses descriptor column order, so its generated declarations can differ from the local-schema client while each client's SQL and decoding order still agree. Both modes expand wildcards before generation; adding an unrelated column later does not widen the already generated result. Connected schema checking reports that same addition as drift on the next compile. Discovery cannot use `--no-database` because it has no local schema. Full connection/authentication contracts are in [database-assisted analysis](../../docs/database-analysis.md).

Alternate connected configurations are deliberately opt-in; `make generate`, `make check-examples` and release smoke checks use the offline `sqlc.yaml`. The [database analysis test](../../tests/examples/go/counters/database_test.go) exercises both connected configurations in a temporary output directory, compiles and executes the discovered client, verifies that analysis does not write rows, and checks schema drift and the offline override.

## Run the examples

After `make generate`, run against a disposable database without a `counters` table:

```sh
cd tests/examples/go
YDB_CONNECTION_STRING=grpc://localhost:2136/local \
  go test -p 1 -count=1 -timeout=180s ./counters -v
```

The tests create and drop the table, so do not pre-create it for this command. Run live suites sequentially. Without `YDB_CONNECTION_STRING`, the tests compile and skip live execution.
