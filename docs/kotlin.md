# Kotlin

`gen.kotlin` generates synchronous Kotlin query methods and data classes from the resolved SQL types. Select `runtime: ydb` (default), `jdbc`, or `exposed`; `native` aliases `ydb`. Options are `package`, `out`, and `runtime`.

```yaml
gen:
  kotlin:
    package: authors.exposed
    out: kotlin/exposed
    runtime: exposed
```

The [authors example](../examples/authors/kotlin) builds all three profiles from the same [SQL](../examples/authors/queries.sql). The `batch` example builds all three profiles; `booktest`, `jets`, and `ondeck` generate native Query SDK APIs. Run `make generate` to update the checked-in examples.

## Generated API

`Queries` borrows the runtime object supplied by the caller:

| Runtime | Constructor argument | Transaction ownership |
| --- | --- | --- |
| `ydb` | `SessionRetryContext` or `QueryTransaction` from the Java Query SDK | A retry context runs one transaction per method; a transaction groups several methods and remains caller-owned. |
| `jdbc` | `java.sql.Connection` | The caller owns the connection, autocommit setting and transaction. |
| `exposed` | Exposed `JdbcTransaction` | The caller owns the Exposed transaction; methods use its underlying JDBC connection. |

`:one` returns a nullable row, `:many` returns a list, and `:exec` returns `Unit`. Rows are data classes. Optional columns and parameters use nullable Kotlin types; an absent row differs from a row whose optional fields are null. Methods created from a native `QueryTransaction`, JDBC and Exposed methods do not commit, roll back, close borrowed resources or create nested transactions. Exposed output is SQL-first: it does not infer `Table` objects or translate SQL into the Exposed DSL. SQL is embedded at each execution site. Without explicit declarations, JDBC and Exposed use positional `?` parameters and standard `PreparedStatement` setters. Unsigned integers, `Json`, `Timestamp`, and structured lists use typed SDK values so the driver receives their YQL types.

## Explicit declarations

Source `DECLARE` statements and their named references remain in generated SQL for every runtime. JDBC and Exposed unwrap the borrowed `YdbConnection` and prepare the named query with `YdbPrepareMode.DATA_QUERY`, which avoids automatic batch flattening. Typed values bind by their original names; the driver sends the prepared text unchanged. If a query mixes explicit and inferred parameters, only the missing inferred declarations are prefixed to the preserved source. Connection settings must allow data-query preparation. Native execution passes the original SQL and typed parameters to the Query SDK.

## Type coverage

Supported types are `Bool`, signed and unsigned 8/16/32/64-bit integers, `Float`, `Double`, `Utf8`, `String`, `Json`, `Timestamp`, and their optional forms. `String` maps to `ByteArray`; `Utf8` and `Json` map to Kotlin `String`; `Timestamp` maps to `java.time.Instant`. `Uint8` and `Uint16` use `Int`, `Uint32` uses `Long`, and `Uint64` uses the full `Long` bit pattern, matching the Java SDK. Use `java.lang.Long.toUnsignedString` to format a `Uint64` value. Values are bound through the SDK's typed values, including typed empty optionals. Other YQL types fail generation explicitly, except for the structured batch parameters below.

SQL literals preserve Unicode and control characters without Kotlin interpolation. Unsupported identifiers and generated-name collisions produce errors instead of invalid Kotlin source.

## Batch parameters

`List<Struct<...>>` parameters generate a named data class and a `List<Item>` argument in every runtime. `CreateBooks` with `$books` produces `CreateBooksBooksItem`; its `bookId` is `Long` and its JSON fields are `String`. Fields support the scalar types and optional forms listed above. Other nested containers remain unsupported.

The generator binds one SDK list value with an explicit struct element type. Empty lists retain their declared type and optional fields retain theirs when null. Empty batches are sent to the server through the same path as populated batches; the method returns the server result or propagates its error. JDBC and Exposed pass the value through `PreparedStatement.setObject`. The native SDK passes it through `Params`. Each invocation executes the batch query once, and the runtime ownership rules above still apply.

## Dependencies and verification

The example pins Kotlin 2.2.20, YDB Query SDK 2.4.11, JDBC driver 2.4.1, Exposed 1.3.0 and YDB Exposed dialect 0.9.0, targeting JVM 17. These are application dependencies; the generator remains a standalone Go binary.

The [published YDB Exposed dialect POM](https://repo.maven.apache.org/maven2/tech/ydb/dialects/kotlin-exposed-ydb-dialect/0.9.0/kotlin-exposed-ydb-dialect-0.9.0.pom) defines the Kotlin/Exposed pairing. Exposed's [JDBC interoperability](https://www.jetbrains.com/help/exposed/working-with-database.html) provides access to the transaction's underlying JDBC connection.

Compile the handwritten harness and generated profiles with:

```sh
make generate
mvn -f examples/authors/kotlin/pom.xml test-compile
```

The harness uses a disposable YDB database and runs profiles sequentially. It checks typed values, absent and nullable results, CRUD, and caller-owned transaction commit/rollback. Run it with `YDB_CONNECTION_STRING=grpc://localhost:2136/local sh examples/authors/kotlin/run-smoke.sh`.

The [batch harness](../tests/examples/kotlin/pom.xml) compiles the native, JDBC and Exposed batch APIs. Run `mvn -f tests/examples/kotlin/pom.xml test-compile` to compile it, or `YDB_CONNECTION_STRING=grpc://localhost:2136/local sh tests/examples/kotlin/run-smoke.sh` against a disposable database without a `books` table to verify empty batches, multiple rows, JSON, unsigned IDs and caller rollback sequentially.
