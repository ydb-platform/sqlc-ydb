# PHP generation

The built-in PHP generator targets the official YDB PHP SDK. `namespace` defaults to `Db` and `runtime` defaults to `ydb`; no PDO compatibility profile is generated.

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

The application constructs and owns `YdbPlatform\Ydb\Ydb` and its `Table` service. Pass the `Table` to the generated `Queries` class:

```php
$ydb = new YdbPlatform\Ydb\Ydb($config);
$queries = new Authors\Native\Queries($ydb->table());

$author = $queries->getAuthor('18446744073709551615');
```

Each method uses the SDK session retry helper with a one-shot `serializable_read_write` transaction that commits in the same `ExecuteDataQuery` request. By default, the generated class does not retain a session or an interactive transaction. Calls default to non-idempotent because `:one` can be an `INSERT ... RETURNING` query and the generator cannot infer a safe retry policy from the result shape.

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

These settings apply to every method on this helper instance. Use the default instance for writes; do not call mutations through a read-only instance. The configuration callback runs for every retry attempt and must not have external side effects. Retry parameters and idempotency are forwarded to the SDK unchanged.

## Caller-owned transactions

`withTx(Session $session, string $txId)` returns a new helper bound to an existing transaction. The original helper is unchanged. Calling `withTx()` on an already-bound helper throws `LogicException` before changing any session reservation; start from the original unbound helper for each transaction. Binding reserves the session with the SDK's `Session::take()`; bound requests keep it reserved until the caller invokes SDK commit or rollback, which releases it. Save the non-empty transaction ID returned by the SDK's `Session::beginTransaction()` and pass it together with that same session and its owning `Table`:

```php
$table = $ydb->table();
$queries = new Authors\Native\Queries($table);
$session = $table->session();
$txId = $session->beginTransaction();
try {
    $txQueries = $queries->withTx($session, $txId);
    $txQueries->upsertAuthor(new Authors\Native\UpsertAuthorParams('1', 'Ada', null));
    $author = $txQueries->getAuthor('1');
    $session->commitTransaction();
} catch (\Throwable $error) {
    try {
        $session->rollbackTransaction();
    } catch (\Throwable) {
        // Preserve the original failure if rollback also fails.
    }
    throw $error;
}
```

After commit or rollback, the session stays in the SDK pool as an idle session available for reuse; the application does not need to delete it. For the next transaction, acquire a session through `$table->session()` again and bind a fresh helper from the original unbound `Queries`. Do not keep using a released session directly, because the pool may have handed it to another operation.

Bound methods execute on the supplied session with the supplied transaction ID, preserve raw protobuf decoding, and never begin, commit, roll back or retry a transaction. The caller owns the session and transaction lifetime: do not share the session with concurrent operations or mix bound calls with other SDK session operations that release it, and discard the bound helper after commit, rollback or a transaction failure. The helper cannot inspect the SDK's protected transaction state; a stale or mismatched ID produces an SDK/server error and never falls back to a new transaction. The caller must ensure that the Table, session and ID belong together.

The configuration callback still runs, so query timeouts and statistics can be configured. Changing transaction control on a bound helper throws `LogicException` before execution. Idempotency and retry parameters apply only to unbound calls. If retries are needed, retry the entire operation, beginning a fresh transaction and creating a new bound helper on every attempt.

Calling an unbound helper inside `Table::retryTransaction()` does not enlist it in that transaction. The SDK callback provides a session but does not expose its current transaction ID; this API requires an ID saved from an explicit `beginTransaction()` call.


`:one` returns a typed row object or `null` when the result is empty. `:many` returns a list of typed row objects, and `:exec` returns `void`. A method with a single parameter accepts that scalar directly; a method with several parameters accepts a generated immutable `*Params` object. `:execrows` is rejected because the SDK does not expose a portable affected-row count.

SQL is embedded at the query call site in an adaptive nowdoc. The sqlc query annotation is emitted as a PHP comment before the method. Parameters are bound separately, and parameterized queries explicitly request execution-plan caching.

Table API can truncate result sets. Generated row helpers reject a truncated result with an exception instead of returning an incomplete list as complete. Use a bounded query or explicit pagination for large results.

## Types and exact values

Generated PHP requires PHP 8.2 or newer on a 64-bit runtime. It supports the scalar YQL types used by the bundled examples plus one level of `Optional<T>`:

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

`Uint64` is decimal text so values through `18446744073709551615` remain representable. `Timestamp` never passes through floating point. JSON is validated but is not decoded and re-encoded, preserving its exact text and large numeric tokens.

The SDK's public `QueryResult` converts the protobuf result set through JSON, decodes YDB JSON values into PHP objects, and converts timestamps through a floating-point division. Generated code therefore uses a small runtime bridge over the SDK's public `RequestTrait` and returns `ExecuteQueryResult` protobufs. The bridge receives the SDK client's credentials, metadata, discovery settings, logger and gRPC status handling from the caller-owned `Table`; it does not use reflection or private SDK state. Generated decoders verify the result-set count, column order and names, analyzed YQL types, row width and every protobuf value case before constructing a row. Only codec methods reachable from the generated queries and their dependencies are emitted.

## Dependency and checks

The shared examples pin `ydb-platform/ydb-php-sdk` 1.16.1. Inspected SDK source versions and links are recorded in the [SDK investigation notes](../.agents/sdk-evidence.md).

SDK 1.16.1 fixes `google/protobuf` at 3.15.8. On PHP 8.2 that protobuf runtime emits deprecation notices for legacy interface return types and dynamic properties; a deprecation-clean PHP 8.2 run requires the SDK to update its protobuf dependency.

Use the repository's [development commands](../.agents/development.md) for generated-code and live checks. The [shared PHP example harness](../tests/examples/php/README.md) documents its generated-class loading layout.

Primary references: the YDB documentation for [installing an SDK](https://ydb.tech/docs/en/reference/ydb-sdk/install) and the official [`ydb-platform/ydb-php-sdk`](https://github.com/ydb-platform/ydb-php-sdk).

## Structured batch parameters

`List<Struct<...>>` parameters accept a list of generated immutable item objects. The binder serializes each item with the usual scalar codecs and sends one typed protobuf list, preserving the declared struct schema even for an empty PHP array. See the [batch example](../examples/batch/README.md).
