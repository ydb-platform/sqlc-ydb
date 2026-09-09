# PHP generation

The built-in PHP generator targets the official YDB PHP SDK. `namespace`
defaults to `Db` and `runtime` defaults to `ydb`; no PDO compatibility profile
is generated.

```yaml
version: "2"
sql:
  - engine: ydb
    schema: schema.sql
    queries: queries.sql
    gen:
      php:
        namespace: Authors\Native
        out: php/native
        runtime: ydb
```

The application constructs and owns `YdbPlatform\Ydb\Ydb` and its `Table`
service. Pass the `Table` to the generated `Queries` class:

```php
$ydb = new YdbPlatform\Ydb\Ydb($config);
$queries = new Authors\Native\Queries($ydb->table());

$author = $queries->getAuthor('18446744073709551615');
```

Each method uses the SDK session retry helper with a one-shot
`serializable_read_write` transaction that commits in the same
`ExecuteDataQuery` request. The generated class does not expose or retain a
session or an interactive transaction. Calls are marked non-idempotent because
`:one` can be an `INSERT ... RETURNING` query and the generator cannot infer a
safe retry policy from the result shape.

`:one` returns a typed row object or `null` when the result is empty. `:many`
returns a list of typed row objects, and `:exec` returns `void`. A method with a
single parameter accepts that scalar directly; a method with several parameters
accepts a generated immutable `*Params` object. `:execrows` is rejected because
the SDK does not expose a portable affected-row count.

Every source query remains available as a public `Queries::*_SQL` constant. SQL
is rendered as an adaptive nowdoc so the original multiline text stays readable
and exact. A quoted-fragment representation is used only when source control
bytes cannot be represented safely in a nowdoc. The PHP SDK sends explicit
parameter values but does not reconstruct YQL `DECLARE` statements, so generated
methods execute the complete original SQL.

## Types and exact values

Generated PHP requires PHP 8.2 or newer on a 64-bit runtime. It supports the
scalar YQL types used by the bundled examples plus one level of `Optional<T>`:

| YQL type | PHP type |
| --- | --- |
| `Bool` | `bool` |
| `Int8`, `Int16`, `Int32`, `Uint8`, `Uint16`, `Uint32` | range-checked `int` |
| `Int64` | `int` |
| `Uint64` | canonical decimal `string` |
| `Float` | finite `float` within the Float32 range |
| `Double` | finite `float` |
| `Utf8` | UTF-8 `string` |
| `String` | binary `string` |
| `Json`, `JsonDocument` | validated, unchanged JSON text `string` |
| `Timestamp` | integer microseconds since the Unix epoch, `0` through `4291747199999999` |
| `Optional<T>` | nullable form of the mapped PHP type |

`Uint64` is decimal text so values through `18446744073709551615` remain
representable. `Timestamp` never passes through floating point. JSON is validated
but is not decoded and re-encoded, preserving its exact text and large numeric
tokens.

The SDK's public `QueryResult` converts the protobuf result set through JSON,
decodes YDB JSON values into PHP objects, and converts timestamps through a
floating-point division. Generated code therefore uses a small runtime bridge
over the SDK's public `RequestTrait` and returns `ExecuteQueryResult` protobufs.
The bridge receives the SDK client's credentials, metadata, discovery settings,
logger and gRPC status handling from the caller-owned `Table`; it does not use
reflection or private SDK state. Generated decoders verify the result-set count,
column order and names, analyzed YQL types, row width and every protobuf value
case before constructing a row.

## Dependency and checks

The shared examples pin `ydb-platform/ydb-php-sdk` 1.16.1. Inspected SDK source
versions and links are recorded in [provenance](provenance.md).

SDK 1.16.1 fixes `google/protobuf` at 3.15.8. On PHP 8.2 that protobuf runtime
emits deprecation notices for legacy interface return types and dynamic
properties; a deprecation-clean PHP 8.2 run requires the SDK to update its
protobuf dependency.

Use the repository's [development commands](development.md) for generated-code
and live checks. The [shared PHP example harness](../examples/php/README.md)
documents its generated-class loading layout.

Primary references: the YDB documentation for [installing an
SDK](https://ydb.tech/docs/en/reference/ydb-sdk/install) and the official
[`ydb-platform/ydb-php-sdk`](https://github.com/ydb-platform/ydb-php-sdk).
