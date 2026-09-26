# Examples

YDB adaptations of all five families in [sqlc's examples](https://github.com/sqlc-dev/sqlc/tree/3c2546a4b47fabbcec3e07df420effb1a464728f/examples). The PostgreSQL, MySQL and SQLite directories there are dialect variants of these families. Here each family has a shared YDB schema and queries, with generated code grouped by language and runtime. Additional YDB-specific recipes demonstrate typed DML, computed values, database-assisted analysis and callback exports.

| Example | Scenario |
| --- | --- |
| [authors](authors) | Create, fetch, list and delete; all built-in languages |
| [batch](batch) | YDB bulk writes and generated book queries; differences from pgx batching |
| [booktest](booktest) | Book catalog, joins, JSON tags, updates and a scalar greeting |
| [jets](jets) | Related tables, aggregate counts and limited result sets |
| [ondeck](ondeck) | Ordered schema migrations, query directories and venue aggregates |
| [records](records) | Typed INSERT/UPSERT/UPDATE/DELETE SELECT, Struct/list parameters, Digest, JSON tag predicates and configured function signatures; Go native and `database/sql` |
| [counters](counters) | Computed DML, constants, fixed wildcard projections and opt-in live schema checking/discovery; Go native and `database/sql` |
| [streaming](streaming) | Typed callback exports; Go native SDK and database/sql |
| [namespaces](namespaces) | Static TablePathPrefix, two catalogs with identically named tables, indexed reads and cross-catalog joins; all built-in languages |
| [renaming](renaming) | Upstream-style `gen.go.rename` for Go struct fields; native SDK and `database/sql` |

The five upstream-derived families generate Go native SDK and `database/sql`, C++ native SDK and userver, C# Dapper, Java jOOQ, Kotlin Query SDK, TypeScript, Rust and PHP APIs. The `authors` family also includes Python (native, DB-API, SQLAlchemy), C# ADO.NET, Java native/JDBC, and Kotlin JDBC/Exposed profiles. Shared cross-example builds and test harnesses live in `tests/examples/`, grouped by language. The streaming recipe covers both Go profiles; other profiles reject `:each`. The example directories contain their SQL, generator configuration and code grouped by language and runtime. Runtime commands are in [development](../.agents/development.md).

From the repository root:

```sh
make generate       # regenerate every example
make check-examples # analyze SQL, compare outputs, check generated Go and Python
```

Go tests compile without a database and skip live cases unless `YDB_CONNECTION_STRING` is set. Run the live cases sequentially against a disposable database with none of the example tables already present:

```sh
cd tests/examples/go
YDB_CONNECTION_STRING=grpc://localhost:2136/local go test -p 1 -count=1 -timeout=180s ./... -v
```

Example READMEs document SQL changes and unsupported upstream contracts. These are working YDB recipes, not a claim that PostgreSQL-specific features, cloud configuration or every upstream generator option is implemented. See the [compatibility contract](../docs/compatibility.md).

The first five SQL/schema scenarios are adapted from upstream commit `3c2546a4b47fabbcec3e07df420effb1a464728f`; generated code comes from sqlc-ydb.

The `records`, `counters`, `streaming`, `namespaces` and `renaming` recipes are native sqlc-ydb examples. Their default configurations are offline and included in the same generation/Go checks; the counters README documents the optional connected configurations.
