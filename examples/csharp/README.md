# C# generated-profile smoke

`GeneratedProfiles.csproj` compiles the Dapper and linq2db generated sources
for `authors`, `batch`, `booktest`, `jets`, and `ondeck` together. The project
keeps their runtime dependencies and exact package versions in one place; the
generator itself has no .NET dependency.

The same project has an opt-in live smoke for the runtime-specific execution
paths. It runs `authors`, `batch`, `booktest`, `jets`, and `ondeck` sequentially
and applies each example's real schema. The smoke covers CRUD and missing-row
behavior, DML `RETURNING`, aggregates, nullable `LEFT JOIN` results,
`Optional<Json>`, `Json`, microsecond `Timestamp` values, high `Uint64` values,
typed result decoding, and parameter binding. Run each profile sequentially
against a disposable database without tables from these examples. Build and
execution commands are in [development](../../docs/development.md#generated-runtime-checks).

The linq2db profile accepts a caller-owned `DataConnection`. Configure one with
the official YDB provider, for example with
`YdbTools.CreateDataConnection(ydbConnection)` or
`YdbTools.CreateDataConnection(ydbTransaction)`. The Dapper profile accepts a
caller-owned `YdbConnection` and optional `YdbTransaction` directly.

The harness never removes a table whose creation did not succeed. It tracks
each table it creates, including the `venues` to `venue` rename in the ordered
`ondeck` migrations, and drops only those tables during cleanup.
