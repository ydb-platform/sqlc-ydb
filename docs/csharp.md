# C# generation

The built-in C# generator has `adonet` and `dapper` runtime
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
Dapper `CommandDefinition`, `QueryFirstAsync<Row>`, `QueryAsync<Row>`, and
`ExecuteAsync`. Dapper constructs the result records; no generated reader loop
or ordinal row mapper is used. `:many` is buffered and returned as an
`IReadOnlyList<Row>`; bound large reads explicitly in SQL.

Rows whose column names differ from their C# members receive a type-specific
Dapper `ITypeMap` (for example, `book_id` to `BookID`). Registration is scoped to
those generated record types; `DefaultTypeMap.MatchNamesWithUnderscores` and the
mapping of application types are not changed. SQL text and aliases are unchanged.

`SqlMapper.IDynamicParameters` adds concrete `YdbParameter` instances, preserving
YDB-specific types and typed optional nulls instead of relying on CLR inference.

## Types and parameters

The supported scalar types are `Bool`, signed and unsigned integer types,
`Float`, `Double`, `Utf8`, `String`, `Json`, `Timestamp`, and `Uuid`, plus one
level of `Optional<T>`. They map to `bool`, the corresponding C# numeric type,
`float`, `double`, `string`, `byte[]`, `string`, `DateTime`, and `Guid`.
Unsupported YQL types fail generation; there is no `object` fallback.

All profiles bind explicit YDB types. Standard primitives use their exact
`DbType`. `Json` and `Timestamp` use the SDK's
`YdbValue.MakeJson` and `YdbValue.MakeTimestamp` builders. Optional parameters
use the corresponding typed `YdbValue.MakeOptional*` builder for both present
and null values. A bare present CLR value would otherwise bind as `T`, while a
declared YQL parameter requires `Optional<T>`.

## Dependencies

The shared example project targets `net8.0` and pins `Ydb.Sdk` 0.35.0,
Dapper 2.1.79. Runtime packages belong to the generated
application. See the [shared C# examples](../examples/csharp/README.md) for
build and usage entry points.

Timestamp parameters normalize Local values to UTC. Unspecified values are
interpreted as UTC. Optional nulls remain null.
The normalization occurs before constructing a typed YdbValue in every profile.

ADO.NET and Dapper constructors reject transactions belonging to another
connection. The caller must also keep the transaction active; the SDK checks
active-transaction state during execution. Wrappers never commit or dispose a
caller-owned transaction. Single SQL arguments use camelCase; record properties
remain PascalCase.

Dapper methods accept an optional `commandTimeout` in seconds, passed to
`CommandDefinition`. Null retains the Dapper/connection default. It follows the
existing cancellationToken argument so positional cancellation calls remain valid.
SQL uses C# multiline raw strings; SQL with control characters or line endings
that C# would normalize retains escaped literals. Parameters are built separately
with explicit YDB types.
