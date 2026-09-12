# C# generation

The built-in C# generator has `adonet` (default) and `dapper` runtime profiles. Both generate `Models.cs` and `Queries.cs`, with records and async `:one`, `:many`, and `:exec` methods accepting a `CancellationToken`. `:execrows` is rejected because the selected YDB APIs do not expose affected-row counts.

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

Result and parameter records come from resolved SQL shapes. The generator does not infer ORM entities. SQL appears at the execution site; query annotations become C# comments before methods. ADO.NET uses escaped string fragments. Dapper uses indented multiline raw strings, falling back to escaped literals for control characters or line endings a raw string would normalize.

`:one` returns the first row and throws `InvalidOperationException` if no row exists. `:many` returns `IReadOnlyList<Row>` and `:exec` returns `Task`.

## Runtime ownership

The `adonet` profile accepts an already-open `YdbConnection` and an optional caller-owned `YdbTransaction`. `WithTransaction` returns another lightweight `Queries` wrapper. Generated code creates and disposes each `YdbCommand`; it does not create a data source, open a connection, begin a transaction, or dispose caller-owned resources.

Both constructors reject a transaction belonging to another connection. The caller must keep the transaction active; the SDK checks its state during execution. Generated wrappers do not commit or roll back caller transactions.

The `dapper` profile has the same connection and transaction contract. It uses Dapper `CommandDefinition`, `QueryFirstAsync<Row>`, `QueryAsync<Row>`, and `ExecuteAsync`. Dapper constructs the result records; no generated reader loop or ordinal row mapper is used. `:many` is buffered and returned as an `IReadOnlyList<Row>`; bound large reads explicitly in SQL.

Rows whose column names differ from their C# members receive a type-specific Dapper `ITypeMap` (for example, `book_id` to `BookID`). Registration is scoped to those generated record types; `DefaultTypeMap.MatchNamesWithUnderscores` and the mapping of application types are not changed. SQL text and aliases are unchanged.

`SqlMapper.IDynamicParameters` adds concrete `YdbParameter` instances, preserving YDB-specific types and typed optional nulls instead of relying on CLR inference. Parameters are built separately, one per line. Dapper methods accept optional `commandTimeout` seconds after `cancellationToken`; null retains the Dapper/connection default. Both controls are passed through `CommandDefinition`.

## Types and parameters

The supported scalar types are `Bool`, signed and unsigned integer types, `Float`, `Double`, `Utf8`, `String`, `Json`, `Timestamp`, and `Uuid`, plus one level of `Optional<T>`. They map to `bool`, the corresponding C# numeric type, `float`, `double`, `string`, `byte[]`, `string`, `DateTime`, and `Guid`. Unsupported YQL types fail generation; there is no `object` fallback.

All profiles bind explicit YDB types. Standard primitives use their exact `DbType`. `Json` and `Timestamp` use the SDK's `YdbValue.MakeJson` and `YdbValue.MakeTimestamp` builders. Optional parameters use the corresponding typed `YdbValue.MakeOptional*` builder for both present and null values. A bare present CLR value would otherwise bind as `T`, while a declared YQL parameter requires `Optional<T>`.

Timestamp parameters normalize Local values to UTC; Unspecified values are interpreted as UTC. Normalization precedes typed `YdbValue` construction; optional nulls remain null. Scalar method parameters use camelCase; record properties use PascalCase.

## Dependencies

The shared example project targets `net8.0` and pins `Ydb.Sdk` 0.35.0 and Dapper 2.1.79. Runtime packages belong to the generated application. See the [shared C# examples](../tests/examples/csharp/README.md) for build and usage entry points.
