# Kotlin

`gen.kotlin` generates synchronous Kotlin query methods and data classes from
the resolved SQL types. Select `runtime: ydb` (default), `jdbc`, or `exposed`;
`native` aliases `ydb`. Options are `package`, `out`, and `runtime`.

```yaml
gen:
  kotlin:
    package: authors.exposed
    out: kotlin/exposed
    runtime: exposed
```

The [authors example](../examples/authors/kotlin) builds all three profiles from
the same [SQL](../examples/authors/queries.sql). The `batch`, `booktest`,
`jets`, and `ondeck` examples generate native Query SDK APIs. Run `make generate`
to update the checked-in examples.

## Generated API

`Queries` borrows the runtime object supplied by the caller:

| Runtime | Constructor argument | Transaction ownership |
| --- | --- | --- |
| `ydb` | `SessionRetryContext` or `QueryTransaction` from the Java Query SDK | A retry context runs one transaction per method; a transaction groups several methods and remains caller-owned. |
| `jdbc` | `java.sql.Connection` | The caller owns the connection, autocommit setting and transaction. |
| `exposed` | Exposed `JdbcTransaction` | The caller owns the Exposed transaction; methods use its underlying JDBC connection. |

`:one` returns a nullable row, `:many` returns a list, and `:exec` returns `Unit`.
Rows are data classes. Optional columns and parameters use nullable Kotlin
types; an absent row differs from a row whose optional fields are null.
Methods created from a native `QueryTransaction`, JDBC and Exposed methods do not
commit, roll back, close borrowed resources or create nested transactions.
Exposed output is SQL-first: it does not infer `Table` objects or translate SQL
into the Exposed DSL. SQL is embedded at each execution site without generated declarations. JDBC and Exposed use positional `?` parameters and standard
`PreparedStatement` setters. Unsigned integers, `Json`, and `Timestamp` use
typed SDK values so the driver receives their YQL types without generated
`DECLARE` statements or `unwrap`.

## Type coverage

Supported types are `Bool`, signed and unsigned 8/16/32/64-bit integers,
`Float`, `Double`, `Utf8`, `String`, `Json`, `Timestamp`, and their optional
forms. `String` maps to `ByteArray`; `Utf8` and `Json` map to Kotlin `String`;
`Timestamp` maps to `java.time.Instant`. `Uint8` and `Uint16` use `Int`,
`Uint32` uses `Long`, and `Uint64` uses the full `Long` bit pattern, matching
the Java SDK. Use `java.lang.Long.toUnsignedString` to format a `Uint64` value.
Values are bound through the SDK's typed values, including typed empty optionals.
Other YQL types fail generation explicitly.

SQL literals preserve Unicode and control characters without Kotlin
interpolation. Unsupported identifiers and generated-name collisions
produce errors instead of invalid Kotlin source.

## Dependencies and verification

The example pins Kotlin 2.2.20, YDB Query SDK 2.4.11, JDBC driver 2.4.1,
Exposed 1.3.0 and YDB Exposed dialect 0.9.0, targeting JVM 17. These are
application dependencies; the generator remains a standalone Go binary.

The [published YDB Exposed dialect POM](https://repo.maven.apache.org/maven2/tech/ydb/dialects/kotlin-exposed-ydb-dialect/0.9.0/kotlin-exposed-ydb-dialect-0.9.0.pom)
defines the Kotlin/Exposed pairing. Exposed's
[JDBC interoperability](https://www.jetbrains.com/help/exposed/working-with-database.html)
provides access to the transaction's underlying JDBC connection.

Compile the handwritten harness and generated profiles with:

```sh
make generate
mvn -f examples/authors/kotlin/pom.xml test-compile
```

The harness uses a disposable YDB database and runs profiles sequentially.
It checks typed values, absent and nullable results, CRUD, and caller-owned
transaction commit/rollback. Run it with `YDB_CONNECTION_STRING=grpc://localhost:2136/local sh examples/authors/kotlin/run-smoke.sh`.
