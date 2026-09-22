# Database-assisted analysis

`compile`, `generate`, and `diff` can read table metadata from YDB and ask the server to compile each query. The commands never execute application queries or apply schema migrations. The default remains offline analysis when no database is configured.

## Generate from an existing database

Omit `schema` to discover the ordinary tables referenced by your queries:

```yaml
version: "2"
sql:
  - engine: ydb
    queries: queries.sql
    database:
      uri: ${YDB_CONNECTION_STRING}
    gen:
      go:
        out: db
        sql_package: ydb
```

Set `YDB_CONNECTION_STRING` to a URI such as `grpc://localhost:2136/local`, then run `sqlc-ydb generate`. Table columns, nullability, primary keys and sequence-generated columns come from `TableService.DescribeTable`. Names in queries may be relative to the configured database or absolute YDB paths. Unrelated tables are not enumerated. Discovery supports ordinary tables, not topics, views or external data sources. Unsupported column types and literal column defaults produce errors; they are not replaced with guessed values.

The existing semantic analyzer resolves parameters and query projections against this catalog. Database discovery does not add support for expressions, functions, CTEs or other syntax outside the [current analyzer coverage](compatibility.md#current-analyzer-coverage). In particular, connecting to YDB does not make a computed projection supported automatically.

## Check a local schema against YDB

Keep the `schema` option to analyze the local migration Up sections and check their final catalog against the database before generating code:

```yaml
version: "2"
sql:
  - engine: ydb
    schema: migrations
    queries: queries.sql
    database:
      uri: ${YDB_CONNECTION_STRING}
      timeout: 10s
    gen:
      go:
        out: db
        sql_package: database/sql
```

Every table in the local catalog must already exist with the same columns, types, nullability, primary-key order and sequence-generation contract. Extra columns are also reported as schema drift. Unrelated server tables are ignored. Apply migrations separately to a disposable development or CI database; the analyzer never applies them itself.

In connected mode, catalog columns use YDB's lexicographic wildcard order, even when the CREATE TABLE source or DescribeTable response lists them differently. Explicit SELECT projections retain their query order. Switching a query using `SELECT *` from offline mode can therefore change generated field order; review the generated diff or list the columns explicitly.

## Server query validation

After local analysis, the compiler sends each query once to QueryService with execution mode `EXPLAIN`. This compiles SELECT and DML without executing either. The compiler temporarily supplies DECLARE statements for parameters inferred locally, so validation requires no parameter values. Explicit declarations and the original SQL bytes in generated code are preserved.

A server error fails the command; it never silently falls back to offline analysis. All selected generators consume the same analysis. Connection, metadata and validation errors occur before generated files are written. No persistent metadata cache is used: every invocation checks the current schema again.

Server compilation does not prove query performance, authorization at execution time or the absence of future schema changes. It also does not supply a public typed result-column contract for arbitrary expressions. This version does not execute `SELECT ... LIMIT 0` to discover result types.

## Prepare a disposable local database

local-ydb can create a test schema using [initialization scripts](https://ydb.tech/docs/en/reference/docker/init-scripts). Put only the intended forward schema statements in a directory such as `init.d`, using names such as `01-schema.sql` and `02-seed.sql` to establish alphabetical execution order. Do not mount a migration directory containing rollback scripts directly.

```sh
docker run --pull=always --rm --name sqlc-ydb-local -d -h localhost \
  --platform linux/amd64 -p 2136:2136 -p 8765:8765 \
  -e GRPC_PORT=2136 -e MON_PORT=8765 \
  --mount type=bind,src="$PWD/init.d",dst=/init.d,readonly \
  ydbplatform/local-ydb:latest
```

Scripts in `/init.d` run once after the server starts. Check the container logs for successful initialization before invoking the generator, then use `grpc://localhost:2136/local`. The container prepares the database; sqlc-ydb only reads its metadata and compiles queries. Use a fresh disposable container when changing initialization scripts. For CI, pin the image tag or digest rather than relying on `latest`.

## Connection settings

| Option | Meaning |
| --- | --- |
| `database.uri` | Required `grpc://host:port/database` or `grpcs://host:port/database`. `${NAME}` substitutions read nonempty environment variables when connected analysis runs. |
| `database.auth_token_env` | Optional name of an environment variable containing the authentication token. Missing or empty configured tokens are errors. Omit for anonymous authentication. |
| `database.ca_file` | Optional PEM CA file for `grpcs`, relative to the configuration file. TLS uses certificate verification; this option is not accepted for `grpc`. |
| `database.timeout` | Positive duration for each metadata or compilation request, including connection establishment. Defaults to `10s`. |
| `analyzer.database` | Optional boolean. Defaults to enabled when `database` is present. `true` requires a database configuration; `false` selects offline analysis. |

URIs must not contain user information, query parameters or fragments. Use `auth_token_env` for credentials. The transport connects to the specified endpoint directly; it does not discover endpoints or refresh tokens. Use an endpoint reachable from the generator and provide a current token before invoking the command.

For TLS and token authentication:

```yaml
database:
  uri: ${YDB_CONNECTION_STRING}
  auth_token_env: YDB_TOKEN
  ca_file: certificates/ca.pem
  timeout: 20s
```

## Force offline analysis

```sh
sqlc-ydb compile --no-database
sqlc-ydb generate --no-database
sqlc-ydb diff --no-database
```

`--no-database` overrides all configured database analysis for that command. It does not resolve database URI environment variables, read token variables or CA files, or open database connections. Offline analysis requires local `schema` inputs. `analyzer.database: false` makes the same choice for one query set.

`--no-remote` only disables the version command's release check; it does not disable database analysis. Use `--no-database` for compilation and generation.
