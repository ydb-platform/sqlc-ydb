# Java generation

The authors example has two direct SQL execution profiles plus a separate jOOQ
prototype. The generated API is SQL first: each direct profile keeps the query text and produces a final `Queries`
class with top-level row records. `:one` methods return `Optional<Row>`,
`:many` methods return `List<Row>`, and `:exec` methods return `void`.

Configure the Java package and its output directory relative to `sqlc.yaml`:

```yaml
gen:
  java:
    package: authors.nativeapi
    out: java/native/src/main/java/authors/nativeapi
    runtime: ydb
```

`runtime` accepts `ydb` (also `native`), `jdbc`, or
`jooq`. The [jOOQ prototype](#jooq-prototype) has its own DSL and runtime contract.
Files are emitted directly into `out`; match it to your Java package directory.
Each schema table and query projection gets a record, without ORM annotations.

Build with Java 17 or newer and Maven, then run the smoke programs:

```sh
cd examples/authors
mvn -f java/pom.xml test-compile
export YDB_CONNECTION_STRING=grpc://localhost:2136/local
# Each smoke creates and drops `authors`; use a disposable database.
sh java/run-smoke.sh
```

The smoke sources are compile-time examples and require a running target only
when their `main` methods are invoked. Each checks both nullable biography
states, a `Uint64` whose bit pattern is `2^64-1` (`-1L` in Java), result
mapping, and deletion. The runner invokes profiles sequentially. Each creates
and drops its own `authors` table and fails if a table already exists.

Published dependencies are pinned to YDB SDK BOM `2.4.11` and JDBC `2.4.1`.

The native constructor receives a borrowed `tech.ydb.query.QueryTransaction`.
The application owns retry, commit, rollback and the transaction lifecycle.
Native `:exec` methods call `createQuery(...).execute()` and check the returned
status; only methods returning rows use `QueryReader`.
The JDBC constructor receives a borrowed `java.sql.Connection`; statements and
result sets are method-owned and the connection remains application-owned.

JDBC uses standard positional `?` parameters and `PreparedStatement` setters.
Each occurrence is bound in SQL order, including repeated parameters. Text uses
`setString`, binary values use `setBytes`, and signed primitives use their typed
setters. Unsigned integers use `setObject` with an SDK value to retain their YQL
type; `Uint64` preserves all bits of a Java `long`, including `-1L`.
The driver handles parameter types without generated `DECLARE` statements or
`unwrap`. Nullable primitive results use `getObject(index, BoxedType.class)`;
`getString` and `getBytes` already return null for absent values.
Native nullable text and bytes use SDK getters directly; nullable primitives
still require a presence check because their getters return Java primitives.
Non-null optional parameters use `PrimitiveValue.new…(value).makeOptional()`.
`Uint8`, `Uint16`, and `Uint32` inputs are checked before execution, so a wider
Java integer cannot be silently truncated by an SDK constructor.

Supported scalar types are `Bool`, signed and unsigned integers, `Float`,
`Double`, `Utf8`, and `String`, plus one level of `Optional<T>`. Optional
primitives use boxed Java types; unsupported types fail generation. Native and
JDBC methods use the caller's transaction; they never commit, roll back, or
close caller-owned transactions or connections.

SQL uses Java 17 text blocks with escaped delimiters, control characters, and
trailing whitespace. Literal tests compile and execute the emitted Java and
compare exact UTF-8 bytes with the original SQL.

The inspected SDK sources and framework API references are recorded in
[source provenance](../.agents/sdk-evidence.md#java-sdk-and-framework-references).
Dependency versions used by the example are pinned in [its Maven build](../examples/authors/java/pom.xml).

## jOOQ prototype

`runtime: jooq` translates named YQL queries into the jOOQ DSL. SQL files remain
its input; generated methods contain no embedded SQL statements. Unsupported
constructs fail generation with the query name and offending syntax, without a
plain-SQL fallback. The prototype covers all 40 queries in the five example
families: SELECT, projections, aliases, LEFT JOIN, comparisons and logical
conditions, ordering, LIMIT, GROUP BY with COUNT(*), INSERT/UPSERT, UPDATE,
DELETE and RETURNING. Its YQL function subset includes SetIsDisjoint, ToSet and
Yson::ConvertToStringList; other functions require an explicit implementation.

```yaml
gen:
  java:
    package: authors.jooq
    out: java/jooq
    runtime: jooq
```

Each output contains `Tables.java` (typed fields derived from the local schema),
`Queries.java` and projection records. Generation is offline and does not need
a second jOOQ schema-generation step or a running database. Table aliases retain
typed fields. Parameters are bound with the YDB field types, never interpolated
into SQL. The constructor borrows a `YdbDSLContext`: callers own connection,
transaction, retry and lifecycle. `:one` returns `Optional<Row>` and rejects
multiple rows; `:many` returns `List<Row>`; `:exec` returns `void`.

The shared [Maven project](../examples/java/jooq/pom.xml) pins Java 21,
jOOQ 3.21.0, YDB jOOQ dialect 2.0.0 and JDBC 2.4.1. Value carriers follow that
dialect: Uint64 uses ULong, Json uses JSON, Timestamp uses Instant, Utf8 uses
String. DTO members use reference types, including nullable values. There is
no automatic conversion to the other Java profiles' long-based Uint64 API.

Two details are specific to the pinned dialect:

- Built-in function names use `systemName`, so the dialect does not quote
  `Yson::ConvertToStringList` as one identifier.
- RETURNING lists use unqualified fields. Execution uses
  `dsl.resultQuery("{0}", stmt).coerce(...)` because the dialect selects jOOQ's
  DEFAULT DML path, which otherwise calls JDBC executeUpdate/getGeneratedKeys.
  YDB returns an ordinary result set. `{0}` embeds the already constructed jOOQ
  query part, preserving its typed bindings; it is not a SQL statement or a
  conversion of the query back into a string. This compatibility path is covered
  by live INSERT and UPDATE RETURNING tests.

```sh
make generate
mvn -f examples/java/jooq/pom.xml test
YDB_CONNECTION_STRING=grpc://localhost:2136/local \
  mvn -f examples/java/jooq/pom.xml test
```

The offline test invokes every generated example method through the real dialect
and a JDBC mock. Live tests use unique mapped table names, verify the mapping
before executing queries, and drop only tables they created. They cover nullable
values, JSON filters, timestamp microseconds, maximum Uint64, joins, aggregates,
LIMIT, RETURNING and caller-owned rollback. SDK compilation and execution must
pass before adding new DSL constructs.
