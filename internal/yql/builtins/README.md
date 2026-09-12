# YQL built-in type resolution

This package is a strict, offline type resolver for the YQL expressions that `sqlc-ydb` can analyze without a database. It returns a concrete `model.Type` or an error. `Any`, an empty type, an unknown function, an unsupported overload, and an invalid argument never produce a successful result.

The public package API is:

- `Resolve(name string, args []model.Type) (model.Type, error)` for functions;
- `CommonType(types ...model.Type) (model.Type, error)` for branches and homogeneous values;
- `Cast(source, target model.Type) (model.Type, error)` for the supported primitive `CAST` matrix.

`model.Type{Kind: "Null"}` is accepted only as a contextual input. A successful result is always concrete, optionally wrapped once in `Optional`. Nested `Optional` values are rejected because the current generator model does not have enough context to preserve their YQL semantics.

## Supported functions

SQL built-in names are case-insensitive:

- `COALESCE`/`NVL`, `IF`;
- `LENGTH`/`LEN`, `SUBSTRING`, `FIND`, `RFIND`, `StartsWith`, `EndsWith`;
- `ABS`;
- `COUNT`, `MIN`, `MAX`, `SUM`, `AVG`.

C++ library names use the documented, case-sensitive `Module::Function` spelling:

- `String::Base64Encode`, `Base64Decode`, `Base64StrictDecode`, `EscapeC`, `UnescapeC`, `HexEncode`, `HexDecode`, `EncodeHtml`, `DecodeHtml`, `CgiEscape`, `CgiUnescape`, `Strip`, `Collapse`, `Find`, `ReverseFind`, `Substring`, `AsciiToLower`, `AsciiToUpper`, `AsciiToTitle`, `ReplaceAll`, `ReplaceFirst`, and `ReplaceLast`;
- `Unicode::IsUtf`, `GetLength`, `Find`, `RFind`, `Substring`, `ToLower`, `ToUpper`, `ToTitle`, `Normalize`, `NormalizeNFC`, `NormalizeNFD`, `NormalizeNFKC`, and `NormalizeNFKD`;
- `DateTime::GetYear`, `GetDayOfYear`, `GetMonth`, `GetMonthName`, `GetWeekOfYear`, `GetWeekOfYearIso8601`, `GetDayOfMonth`, `GetDayOfWeek`, `GetDayOfWeekName`, `GetHour`, `GetMinute`, `GetSecond`, `GetMillisecondOfSecond`, `GetMicrosecondOfSecond`, `GetTimezoneId`, and `GetTimezoneName`, when called with primitive date/time values that YQL can implicitly split into the library resource.

The aggregate resolver models empty-input behavior: `COUNT` is non-optional `Uint64`; the other supported aggregates are optional when an empty input is possible. A grouped aggregate over a non-optional argument is non-optional and the analyzer removes that wrapper using its group context. `SUM` widens signed and unsigned integers to `Int64` and `Uint64`, respectively, and widens Decimal precision to 35 while preserving its scale. `AVG` converts integer, `Float`, and interval input to `Double`, while preserving Decimal precision and scale. The strict `MIN`/`MAX` subset accepts primitive numeric values plus `String` and `Utf8`.

`CommonType` implements the documented primitive numeric result matrix and preserves optionality. Non-numeric types must match exactly. Different Decimal precision or scale is rejected rather than inventing Decimal arithmetic rules.

`COALESCE` and `NVL` require all non-`Null` arguments to have the same base type. YQL also permits some value-dependent implicit conversions, such as narrowing an integer literal when its value fits the other argument's type, but `model.Type` does not retain the literal value needed to resolve those calls correctly. Mixed base types therefore require an explicit `CAST` to the same YQL type. `Null` and `Optional` inputs keep their normal result-nullability behavior.

Core `SUBSTRING` accepts `String` or `Optional<String>`; use `Unicode::Substring` for `Utf8`. Core `SUBSTRING` offsets and lengths and the optional third argument of core `FIND`/`RFIND` accept `Null`, `Uint8`, `Uint16`, or `Uint32`, including optional forms. Other integer types require an explicit `CAST(... AS Uint32)`. This is deliberately stricter than YQL's handling of fitting literals because the resolver sees their types but not their values. The C++ `String::` and `Unicode::` library position rules are separate and continue to use `Uint64` where their signatures require it.

`Cast` supports identity, primitive non-Decimal numeric conversions, `String`/`Utf8` parsing to primitive numeric types, primitive numeric conversion to `String`, and conversion between `String` and `Utf8`. A conversion that is not valid for every source value adds one `Optional` level, matching YQL's failure-to-`NULL` rule. Container casts and cross-precision Decimal casts are left unsupported until the analyzer can preserve their full semantics.

## Sources

Rules and signatures were checked against the primary YDB documentation:

- [basic built-in functions](https://ydb.tech/docs/en/yql/reference/builtins/basic)
- [aggregate functions](https://github.com/ydb-platform/ydb/blob/main/ydb/docs/en/core/yql/reference/builtins/aggregation.md)
- [primitive types and numeric result matrix](https://ydb.tech/docs/en/yql/reference/types/primitive)
- [`CAST` nullability and container rules](https://ydb.tech/docs/en/yql/reference/types/cast)
- [`String` library](https://ydb.tech/docs/en/yql/reference/udf/list/string)
- [`Unicode` library](https://ydb.tech/docs/en/yql/reference/udf/list/unicode)
- [`DateTime` library](https://ydb.tech/docs/en/yql/reference/udf/list/datetime)

On 2026-09-09, the package's supported scalar and library signatures were also checked against result-set metadata from the pinned `ydbplatform/local-ydb:26.3.1.8` image through `internal/endtoend/semantic_live_test.go` and `internal/endtoend/builtin_live_cases_test.go`. This caught rules not stated fully in the prose reference: `AVG(Float)` returns `Double`, Decimal `SUM` widens precision to 35, grouped aggregates over non-optional inputs are non-optional, `String::Substring` requires its position argument, core `SUBSTRING` is byte-string-only, and core string positions use the bounded unsigned types through `Uint32`.

The function inventory was also compared with `ydb-platform/sqlc@8eed5d890396eb03953248a3ec4ab7e28dfaed45`, especially `internal/engine/ydb/lib/{basic,aggregate,cpp}.go`. Those archived descriptors used broad `any` arguments and incomplete nullability, so they are provenance and coverage input rather than executable type authority.

This resolver intentionally omits unverified archived names, window functions, resource-valued `DateTime` transformations, collection functions, and functions whose overload selection depends on information absent from `model.Type`.
