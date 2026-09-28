# Vet YQL queries

`sqlc-ydb vet -f sqlc.yaml` analyzes every named query without generating files, then evaluates the selected rules. A rule is a CEL expression that reports a failure when it returns `true`. This is a local YDB implementation of [upstream sqlc vet](https://docs.sqlc.dev/en/latest/howto/vet.html); it does not use a cloud service or apply schema migrations.

```yaml
version: "2"
sql:
  - engine: ydb
    schema: schema.sql
    queries: queries.sql
    database:
      uri: ${YDB_CONNECTION_STRING}
    rules:
      - no-full-scan
      - sqlc/db-prepare
rules:
  - name: no-full-scan
    message: query scans an entire table
    rule: ydb.plan.operations.exists(op, op == 'TableFullScan')
```

With `YDB_CONNECTION_STRING=grpc://localhost:2136/local`, the example checks that each query compiles against that database and reports a rule failure for any plan containing a `TableFullScan` operation. Use a disposable database with the current schema already applied; `vet` never executes the named SELECT or DML query. `sqlc/db-prepare` is the supported built-in rule and requires a connected database. It uses the same non-executing YDB compilation as database-assisted analysis. Selected custom rules are evaluated for every query in their `sql` entry, in the listed order. A custom rule needs a unique `name`, a nonempty `rule`, and optionally a `message`; an omitted message becomes `rule matched`.

CEL exposes `config.version`, `config.engine`, `config.schema`, and `config.queries`; the latter two are lists of configured paths. `query.sql` is the analyzed executable YQL, `query.name` is the name from `-- name:`, `query.cmd` is the annotation without its colon (such as `one`, `many`, or `exec`), and `query.params` is an ordered list with `number` (one-based), `name`, and resolved YQL `type` fields. These fields allow offline rules such as `query.cmd == 'exec'` or `query.params.size() > 1`.

When a selected rule references `ydb`, connected `vet` requests a non-executing QueryService `EXPLAIN` plan with full statistics after semantic analysis. `ydb.plan.operations` lists distinct names from the `Plan` tree's `Node Type` and `Operators[].Name` fields in encounter order; `ydb.plan.json` is the unchanged plan JSON string, and `ydb.explain` is the decoded JSON object for advanced rules. The operation names and plan structure come from the connected YDB version. A missing or malformed plan is an error, not an empty plan that could pass a rule accidentally. QueryService errors, including failures in later response parts, also fail `vet`.

`sqlc-ydb vet --no-database` forces offline analysis even if `database.uri` is configured. It evaluates rules using `config` and `query` without resolving database credentials; a rule that needs `ydb.plan` or `ydb.explain`, or selects `sqlc/db-prepare`, fails with a database requirement diagnostic. Offline analysis requires local `schema` inputs. The upstream `@sqlc-vet-disable` annotation, PostgreSQL/MySQL plan variables, and upstream cloud/managed-database rules are not implemented.
