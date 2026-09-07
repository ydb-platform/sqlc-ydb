# C# generation

The built-in C# generator targets the current `Ydb.Sdk` ADO.NET provider. It
generates `Models.cs` and `Queries.cs`; it does not generate a data source,
open a connection, begin a transaction, or dispose caller-owned resources.
Construct `Queries` with an already-open `YdbConnection`. Pass a
caller-owned `YdbTransaction` to the constructor, or use `WithTransaction`.
Every generated operation is async and accepts a `CancellationToken`.

```yaml
version: "2"
sql:
  - engine: ydb
    schema: schema.sql
    queries: queries.sql
    gen:
      csharp:
        namespace: Authors.AdoNet
        out: csharp/adonet
```

The generated methods cover `:one`, `:many`, and `:exec`. `:execrows` is
rejected because YDB does not return affected-row counts. SQL is emitted as
portable, escaped C# string fragments so quotes, backslashes, CRLF, and C0
control characters preserve their exact values without depending on a raw
literal delimiter.

`:one` returns the first row and throws `InvalidOperationException` if no row
exists. `:many` returns `IReadOnlyList<Row>` and `:exec` returns a `Task`.
There is one C# profile: the modern SDK is already an ADO.NET provider.

## Types and parameters

The supported scalar types are `Bool`, signed and unsigned integer types,
`Float`, `Double`, `Utf8`, `String`, and `Uuid`, plus one level of
`Optional<T>`. They map to `bool`, the corresponding C# numeric type,
`float`, `double`, `string`, `byte[]`, and `Guid`. Unsupported YQL types fail
generation; there is no `object` or inferred-type fallback.

Parameters are constructed as `YdbParameter` with an explicit standard
`DbType`, which the YDB provider maps to its concrete YDB type. In particular,
`Uint64` always binds as `DbType.UInt64`, `Utf8` as `DbType.String`, and
`String` as `DbType.Binary`. An optional parameter uses the same explicit type
for a value and for `DBNull.Value`, which lets the provider create a correctly
typed YDB null.

## SDK evidence and build target

The API choice was checked against `ydb-platform/ydb` main at
`204baf30e62446f850fc0271979aa309e2932d63` in
`ydb/docs/en/core/reference/languages-and-apis/ado-net/basic-usage.md` and
`type-mapping.md`, and `ydb-platform/ydb-dotnet-sdk` main at
`e35785a671b88f0f05ab6f4f9e15a260c44600f8`. The SDK README calls
`Ydb.Sdk` the ADO.NET provider and demonstrates `YdbDataSource`,
`YdbConnection`, and `YdbCommand`; the provider source exposes
`YdbParameter(string, DbType, object?)` and
`YdbCommand.ExecuteReaderAsync(CancellationToken)`. `YdbParameter` emits a
typed null when its `DbType` is explicit, so `IsNullable` is not the
mechanism used for YQL null typing.

The authors smoke project targets `net8.0` and pins `Ydb.Sdk` `0.33.3`.
`Ydb.Sdk` belongs to generated-project dependencies, never to sqlc-ydb's Go
module. Use .NET SDK 8.x to build the smoke project.
