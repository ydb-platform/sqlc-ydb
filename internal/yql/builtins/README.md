# YQL built-in type resolution

This package is a strict, offline type resolver for the YQL expressions that `sqlc-ydb` can analyze without a database. It returns a concrete `model.Type` or an error. `Any`, an empty type, an unknown function, an unsupported overload, and an invalid argument never produce a successful result.

The public package API is:

- `Resolve(name string, args []model.Type) (model.Type, error)` for functions;
- `NewRegistry(signatures []Signature)` and `Registry.ResolveCall` for validated concrete custom signatures and named arguments;
- `CommonType(types ...model.Type) (model.Type, error)` for branches and homogeneous values;
- `Arithmetic(operator string, left, right model.Type) (model.Type, error)` for primitive numeric `+`, `-`, and `*`, preserving operand optionality;
- `CanWidenInteger(source, target model.Type) bool` for lossless integer assignment conversions;
- `Cast(source, target model.Type) (model.Type, error)` for the supported primitive `CAST` matrix.

`model.Type{Kind: "Null"}` is accepted only as a contextual input. A successful result is always concrete, optionally wrapped once in `Optional`. Nested `Optional` values are rejected because the current generator model does not have enough context to preserve their YQL semantics.

## Supported functions

SQL built-in names are case-insensitive:

- `COALESCE`/`NVL`, `IF`;
- `NANVL` for Float/Double inputs;
- `CurrentUtcDate`, `CurrentUtcDatetime`, `CurrentUtcTimestamp`, `CurrentTzDate`, `CurrentTzDatetime`, `CurrentTzTimestamp`;
- `Random`, `RandomNumber`, `RandomUuid`, `Version`;
- `LENGTH`/`LEN`, `SUBSTRING`, `FIND`, `RFIND`, `StartsWith`, `EndsWith`;
- `ABS`;
- `ToSet` and `SetIsDisjoint` for concrete List/Dict key types;
- `COUNT`, `COUNT_IF`, `MIN`, `MAX`, `SUM`, `AVG`.

C++ library names use the documented, case-sensitive `Module::Function` spelling:

- `String::Base64Encode`, `Base64Decode`, `Base64StrictDecode`, `EscapeC`, `UnescapeC`, `HexEncode`, `HexDecode`, `EncodeHtml`, `DecodeHtml`, `CgiEscape`, `CgiUnescape`, `Strip`, `Collapse`, `Find`, `ReverseFind`, `Substring`, `AsciiToLower`, `AsciiToUpper`, `AsciiToTitle`, `ReplaceAll`, `ReplaceFirst`, and `ReplaceLast`;
- `Unicode::IsUtf`, `GetLength`, `Find`, `RFind`, `Substring`, `ToLower`, `ToUpper`, `ToTitle`, `Normalize`, `NormalizeNFC`, `NormalizeNFD`, `NormalizeNFKC`, and `NormalizeNFKD`;
- `DateTime::GetYear`, `GetDayOfYear`, `GetMonth`, `GetMonthName`, `GetWeekOfYear`, `GetWeekOfYearIso8601`, `GetDayOfMonth`, `GetDayOfWeek`, `GetDayOfWeekName`, `GetHour`, `GetMinute`, `GetSecond`, `GetMillisecondOfSecond`, `GetMicrosecondOfSecond`, `GetTimezoneId`, and `GetTimezoneName`, when called with primitive date/time values that YQL can implicitly split into the library resource.
- `Yson::ConvertToStringList` for `Json` and `Yson` inputs, including optional inputs; verified YDB behavior keeps its `List<String>` result non-optional.
- the scalar `Digest` subset documented in [function signatures](../../../docs/functions.md), including `Digest::CityHash` with its named, omittable `Init` argument.

The aggregate resolver models empty-input behavior: `COUNT` and `COUNT_IF` are non-optional `Uint64`; the other supported aggregates are optional when an empty input is possible. `COUNT_IF` requires Bool or Optional<Bool>, also accepts contextual NULL, and counts only true values; NULL and empty input do not make its result optional. A grouped aggregate over a non-optional argument is non-optional and the analyzer removes that wrapper using its group context. `SUM` widens signed and unsigned integers to `Int64` and `Uint64`, respectively, and widens Decimal precision to 35 while preserving its scale. `AVG` converts integer, `Float`, and interval input to `Double`, while preserving Decimal precision and scale. The strict `MIN`/`MAX` subset accepts primitive numeric values plus `String` and `Utf8`.

`CommonType` implements the documented primitive numeric result matrix and preserves optionality. Non-numeric types must match exactly. Different Decimal precision or scale is rejected rather than inventing Decimal arithmetic rules.

`COALESCE` and `NVL` reconcile arguments pairwise from left to right. Numeric bases use the existing numeric common-type matrix, and mixed String/Utf8 becomes String. A direct right-hand integer literal that fits the accumulated left-hand integer type is resolved as that type. The analyzer retains its exact value in `CallArgument.IntegerLiteral`; parameter values, local bindings and arithmetic expressions are not guessed to be fitting literals. Thus `COALESCE($optional_uint32, 0l)` has type Uint32, while reversing these arguments or using an Int64 parameter/arithmetic fallback has type Int64. A result remains optional only when all available non-NULL alternatives are optional. Other incompatible base types require CAST. This is type analysis only: generated SQL and bound values are unchanged, including YDB's signed/unsigned conversion behavior.

UTC clock functions return the required date/time type named by the function. Random, RandomNumber and RandomUuid return required Double, Uint64 and Uuid. Their typed positional arguments control evaluation dependencies and do not propagate optionality. UTC clock arguments are optional, while each Random function requires at least one dependency argument (NULL is accepted). CurrentTz functions require a String, Optional<String> or contextual NULL zone followed by optional dependency arguments; their TzDate/TzDatetime/TzTimestamp result is optional because YDB resolves and validates the zone. Utf8 zone arguments are rejected. Version takes no arguments and returns String. NANVL reconciles only Float/Double arguments and preserves optionality.

Core `SUBSTRING` accepts `String` or `Optional<String>`; use `Unicode::Substring` for `Utf8`. Core `SUBSTRING` offsets and lengths and the optional third argument of core `FIND`/`RFIND` accept `Null`, `Uint8`, `Uint16`, or `Uint32`, including optional forms. Other integer types require an explicit `CAST(... AS Uint32)`. This is deliberately stricter than YQL's handling of fitting literals because the resolver sees their types but not their values. The C++ `String::` and `Unicode::` library position rules are separate and continue to use `Uint64` where their signatures require it.

`Cast` supports identity, primitive non-Decimal numeric conversions, all eight integer widths to/from Bool, `String`/`Utf8` parsing to primitive numeric types, primitive numeric conversion to `String`, and conversion between `String` and `Utf8`. Date, Datetime and Timestamp to String is total; Timestamp to Uint64 is also total, while Uint64 to Timestamp may fail. String/Utf8 to Json validates the input and may fail even though a particular literal is valid. A conversion that is not valid for every source value adds one `Optional` level, matching YQL's failure-to-`NULL` rule. Container casts and cross-precision Decimal casts are left unsupported until the analyzer can preserve their full semantics.

## Sources

Rules and signatures were checked against the primary YDB documentation:

- [basic built-in functions](https://ydb.tech/docs/en/yql/reference/builtins/basic)
- [aggregate functions](https://github.com/ydb-platform/ydb/blob/main/ydb/docs/en/core/yql/reference/builtins/aggregation.md)
- [primitive types and numeric result matrix](https://ydb.tech/docs/en/yql/reference/types/primitive)
- [`CAST` nullability and container rules](https://ydb.tech/docs/en/yql/reference/types/cast)
- [`String` library](https://ydb.tech/docs/en/yql/reference/udf/list/string)
- [dictionary and set functions](https://ydb.tech/docs/en/yql/reference/builtins/dict)
- [`Yson` library](https://ydb.tech/docs/en/yql/reference/udf/list/yson)
- [`Unicode` library](https://ydb.tech/docs/en/yql/reference/udf/list/unicode)
- [`DateTime` library](https://ydb.tech/docs/en/yql/reference/udf/list/datetime)

The additional shared-expression rules were checked on 2026-09-23 against YDB commit `1415fed8104201c5e973dd8bbf12c71c6b1ed8b9`: [basic functions](https://github.com/ydb-platform/ydb/blob/1415fed8104201c5e973dd8bbf12c71c6b1ed8b9/ydb/docs/en/core/yql/reference/builtins/basic.md), [aggregation](https://github.com/ydb-platform/ydb/blob/1415fed8104201c5e973dd8bbf12c71c6b1ed8b9/ydb/docs/en/core/yql/reference/builtins/aggregation.md), [CAST rules](https://github.com/ydb-platform/ydb/blob/1415fed8104201c5e973dd8bbf12c71c6b1ed8b9/ydb/docs/en/core/yql/reference/types/cast.md), and `CoalesceWrapper`, `DataGeneratorWrapper` and `CurrentTzWrapper` in [type_ann_core.cpp](https://github.com/ydb-platform/ydb/blob/1415fed8104201c5e973dd8bbf12c71c6b1ed8b9/yql/essentials/core/type_ann/type_ann_core.cpp). The SQL-level arities are recorded separately in [translation/builtin.cpp](https://github.com/ydb-platform/ydb/blob/1415fed8104201c5e973dd8bbf12c71c6b1ed8b9/yql/essentials/sql/v1/translation/builtin.cpp): Random functions require at least one argument even though their lower-level type wrapper accepts zero. Local YDB 26.3.1.16 metadata and execution probes confirmed COUNT_IF empty/NULL behavior, clock argument/result types, fitting versus nonliteral COALESCE fallbacks, mixed String/Utf8 COALESCE, all integer/Bool cast widths, Date/Datetime/Timestamp casts and String/Utf8 JSON validation. The [complete reference inventory](../../../docs/yql-builtins.md) distinguishes these implemented rules from remaining analyzer/type-model prerequisites.

On 2026-09-09, the package's supported scalar and library signatures were also checked against result-set metadata from the pinned `ydbplatform/local-ydb:26.3.1.8` image through `internal/endtoend/semantic_live_test.go` and `internal/endtoend/builtin_live_cases_test.go`. This caught rules not stated fully in the prose reference: `AVG(Float)` returns `Double`, Decimal `SUM` widens precision to 35, grouped aggregates over non-optional inputs are non-optional, `String::Substring` requires its position argument, core `SUBSTRING` is byte-string-only, and core string positions use the bounded unsigned types through `Uint32`.

The function inventory was also compared with `ydb-platform/sqlc@8eed5d890396eb03953248a3ec4ab7e28dfaed45`, especially `internal/engine/ydb/lib/{basic,aggregate,cpp}.go`. Those archived descriptors used broad `any` arguments and incomplete nullability, so they are provenance and coverage input rather than executable type authority.

This resolver intentionally omits unverified archived names, window functions, resource-valued `DateTime` transformations, collection functions, and functions whose overload selection depends on information absent from `model.Type`.
