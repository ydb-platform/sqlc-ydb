# C# generation

The built-in C# generator has `adonet`, `dapper`, and `linq2db` runtime
profiles. The default is `adonet`, which preserves the original configuration
and generated API. Every profile generates `Models.cs` and `Queries.cs`, keeps
the source YQL in readable exact string fragments, and exposes async `:one`,
`:many`, and `:exec` methods with a `CancellationToken`. `:execrows` is rejected
because YDB does not return affected-row counts.

```yaml
version: "2"
sql:
  - engine: ydb
    schema: schema.sql
    queries: queries.sql
    gen:
      csharp:
        namespace: Authors.Dapper
        out: csharp/dapper
        runtime: dapper
```

SQL is emitted as portable escaped C# string fragments so quotes, backslashes,
CRLF, Unicode, and C0 control characters preserve their exact text without a
raw-literal delimiter dependency. Result records and parameter records come
from resolved SQL shapes. The generator does not infer ORM entities.

`:one` returns the first row and throws `InvalidOperationException` if no row
exists. `:many` returns `IReadOnlyList<Row>` and `:exec` returns `Task`.

## Runtime ownership

The `adonet` profile accepts an already-open `YdbConnection` and an optional
caller-owned `YdbTransaction`. `WithTransaction` returns another lightweight
`Queries` wrapper. Generated code creates and disposes each `YdbCommand`; it
does not create a data source, open a connection, begin a transaction, or
dispose caller-owned resources.

The `dapper` profile has the same connection and transaction contract. It uses
Dapper `CommandDefinition`, `ExecuteReaderAsync`, and `ExecuteAsync`. Generated
`SqlMapper.IDynamicParameters` code adds concrete `YdbParameter` instances, so
YDB-specific and optional wire types do not depend on Dapper's CLR inference.
Rows are decoded by generated ordinal mappers; this also makes `snake_case`
columns and nullable `LEFT JOIN` results independent of Dapper naming settings.

The `linq2db` profile accepts a caller-owned `DataConnection`. Configure it
with the official YDB provider, for example:

```csharp
using var db = YdbTools.CreateDataConnection(ydbConnection);
var queries = new Queries(db);
```

For a transaction, the caller creates the `DataConnection` with
`YdbTools.CreateDataConnection(ydbTransaction)`. Generated code uses linq2db's
raw-SQL `QueryToListAsync` and `ExecuteAsync` APIs plus explicit
`DataParameter` types. It neither creates nor disposes the data connection or
transaction.

## Types and parameters

The supported scalar types are `Bool`, signed and unsigned integer types,
`Float`, `Double`, `Utf8`, `String`, `Json`, `Timestamp`, and `Uuid`, plus one
level of `Optional<T>`. They map to `bool`, the corresponding C# numeric type,
`float`, `double`, `string`, `byte[]`, `string`, `DateTime`, and `Guid`.
Unsupported YQL types fail generation; there is no `object` fallback.

All profiles bind explicit YDB types. Standard primitives use their exact
`DbType` or linq2db `DataType`. `Json` and `Timestamp` use the SDK's
`YdbValue.MakeJson` and `YdbValue.MakeTimestamp` builders. Optional parameters
use the corresponding typed `YdbValue.MakeOptional*` builder for both present
and null values. A bare present CLR value would otherwise bind as `T`, while a
declared YQL parameter requires `Optional<T>`.

## SDK evidence and build target

The ADO.NET type contract was checked in the current official YDB documentation
and `ydb-platform/ydb-dotnet-sdk` main at
`236bfa176940feafbf61f11c1cb9fc000572b237`. The SDK exposes
`YdbParameter`, `YdbCommand.ExecuteReaderAsync`, `GetFieldValue<T>`, and typed
`YdbValue` builders for `Json`, `Timestamp`, and their optional forms.

Dapper integration was checked against Dapper main at
`6d48ef664acc7298c649e2d449d903b3360d5a90`; its public
`SqlMapper.IDynamicParameters` contract accepts provider-specific parameters,
and `CommandDefinition` carries the transaction and cancellation token.

linq2db integration targets `6.4.0`, commit
`82fbf0f91399cc8c9cea22d09dcae20e4d7568c6`. This release promotes its YDB
provider to supported status and exposes `YdbTools.CreateDataConnection` for a
connection or transaction. Its own build pins `Ydb.Sdk` `0.35.0`.

The shared example compile project targets `net8.0` and pins `Ydb.Sdk`
`0.35.0`, Dapper `2.1.79`, and linq2db `6.4.0`. Runtime packages belong to
generated projects, never to sqlc-ydb's Go module.
