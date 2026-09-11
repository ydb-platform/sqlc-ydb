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
session or an interactive transaction. Calls default to non-idempotent because
`:one` can be an `INSERT ... RETURNING` query and the generator cannot infer a
safe retry policy from the result shape.

Configure a helper instance explicitly for read-only operations:

```php
$reads = new Authors\Native\Queries(
    $ydb->table(),
    idempotent: true,
    configure: static function (YdbPlatform\Ydb\YdbQuery $query): void {
        $query->beginTx('snapshot');
        $query->operationParams(new Ydb\Operations\OperationParams([
            'operation_timeout' => new Google\Protobuf\Duration(['seconds' => 5]),
        ]));
    },
    retryParams: new YdbPlatform\Ydb\Retry\RetryParams(),
);
$author = $reads->getAuthor('1');
```

These settings apply to every method on this helper instance. Use the default
instance for writes; do not call mutations through a read-only instance. The
configuration callback runs for every retry attempt and must not have external
side effects. Retry parameters and idempotency are forwarded to the SDK unchanged.

**Transaction boundary:** each call owns a separate transaction. Calling a helper
inside `Table::retryTransaction()` does not enlist it in that transaction and
cannot make several helper calls atomic. The configuration callback is for
one-shot query settings, not for attaching an existing transaction identifier.

The pinned SDK's interactive `Session` API keeps its transaction identifier
private. Its public `Session::query` path also converts results through JSON,
which loses exact JSON text and timestamp microseconds. Consequently generated
helpers cannot join an interactive transaction while retaining the target's
lossless result contract. This requires a PHP SDK API that executes a query in
the current transaction and exposes the raw `ExecuteQueryResult` protobuf.

`:one` returns a typed row object or `null` when the result is empty. `:many`
returns a list of typed row objects, and `:exec` returns `void`. A method with a
single parameter accepts that scalar directly; a method with several parameters
accepts a generated immutable `*Params` object. `:execrows` is rejected because
the SDK does not expose a portable affected-row count.

SQL is embedded at the query call site in an adaptive nowdoc. The sqlc query
annotation is emitted as a PHP comment before the method. Parameters are bound
separately, and parameterized queries explicitly request execution-plan caching.

Table API can truncate result sets. Generated row helpers reject a truncated
result with an exception instead of returning an incomplete list as complete.
Use a bounded query or explicit pagination for large results.

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
case before constructing a row. Only codec methods reachable from the generated
queries and their dependencies are emitted.

## Dependency and checks

The shared examples pin `ydb-platform/ydb-php-sdk` 1.16.1. Inspected SDK source
versions and links are recorded in the [SDK investigation notes](../.agents/sdk-evidence.md).

SDK 1.16.1 fixes `google/protobuf` at 3.15.8. On PHP 8.2 that protobuf runtime
emits deprecation notices for legacy interface return types and dynamic
properties; a deprecation-clean PHP 8.2 run requires the SDK to update its
protobuf dependency.

Use the repository's [development commands](../.agents/development.md) for generated-code
and live checks. The [shared PHP example harness](../examples/php/README.md)
documents its generated-class loading layout.

Primary references: the YDB documentation for [installing an
SDK](https://ydb.tech/docs/en/reference/ydb-sdk/install) and the official
[`ydb-platform/ydb-php-sdk`](https://github.com/ydb-platform/ydb-php-sdk).
