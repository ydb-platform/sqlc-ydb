# Examples

YDB adaptations of all five families in
[sqlc's examples](https://github.com/sqlc-dev/sqlc/tree/3c2546a4b47fabbcec3e07df420effb1a464728f/examples).
The PostgreSQL, MySQL and SQLite directories there are dialect variants of these
families. Here each family has a shared YDB schema and queries, with generated
code grouped by language and runtime.

| Example | Scenario |
| --- | --- |
| [authors](authors) | Create, fetch, list and delete; all built-in languages |
| [batch](batch) | YDB bulk writes and generated book queries; differences from pgx batching |
| [booktest](booktest) | Book catalog, joins, JSON tags, updates and a scalar greeting |
| [jets](jets) | Related tables, aggregate counts and limited result sets |
| [ondeck](ondeck) | Ordered schema migrations, query directories and venue aggregates |

The four additional families generate Go native SDK and `database/sql` APIs,
matching upstream's Go focus. They share [one Go module](go.mod) and SDK pin;
`authors` also retains the other language runtimes.

From the repository root:

```sh
make generate       # regenerate every example
make check-examples # compile SQL, compare generated output, compile/test Go
```

Go tests compile without a database and skip live cases unless
`SQLC_YDB_TEST_DSN` is set. Run the live cases sequentially against a disposable
database with none of the example tables already present:

```sh
cd examples
SQLC_YDB_TEST_DSN=grpc://localhost:2136/local go test -p 1 -count=1 -timeout=180s ./... -v
```

Example READMEs document SQL changes and unsupported upstream contracts. These
are working YDB recipes, not a claim that PostgreSQL-specific features, cloud
configuration or every upstream generator option is implemented. See the
[compatibility contract](../docs/compatibility.md).

SQL/schema scenarios are adapted from upstream commit
`3c2546a4b47fabbcec3e07df420effb1a464728f`. Its MIT notice is preserved in
[UPSTREAM_LICENSE](UPSTREAM_LICENSE); generated code comes from sqlc-ydb.
