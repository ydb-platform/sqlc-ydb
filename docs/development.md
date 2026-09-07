# Development

```sh
make test
make build
make generate
make check
```

The fast suite includes a YDB-only golden fixture runner under
`internal/endtoend`: real CLI generation must match committed output filenames,
contents and expected diagnostics. Fixture updates are explicit, never an
automatic part of tests. Semantic unit tests independently assert resolved
parameters, result columns and errors.

CI separates this fast offline suite from one Linux acceptance job backed by a
single pinned YDB service. GitHub Actions waits for the image's health check;
there is no multi-database startup framework, engine matrix, plugin subprocess
runner or optional Postgres/MySQL fallback. See
[the fixture runner](../internal/endtoend/README.md) for the upstream references
and baseline update command.

Generator tests compile generated Go against the selected SDK in a temporary
module and execute generated code using mock adapters. Python 3.9 or newer must
be available as `python3` for the Python generator's execution tests. A normal
generator build has no dependency on Python.

The authors example keeps its Go module and tests in `go/`, and Python packages,
requirements and tests in `python/`. Schema, queries and generator configuration
are shared in the example root. Check generated packages with:

```sh
cd examples/authors/go
go test ./...
cd ..
python3 -m compileall -q python
```

Optional live generator tests use `SQLC_YDB_TEST_DSN` to select an isolated YDB
database. Use a disposable development database: tests create and drop uniquely
named tables. No live tests run when the variable is absent.

**Run local-ydb checks sequentially.** Do not run different image versions,
runtime suites or Docker builds concurrently on one host. Use `go test -p 1`
for live suites and do not add `t.Parallel` to them. CI intentionally runs Go,
Python and the complete example in successive steps. If image-version coverage
is added later, preserve sequential execution rather than introducing concurrent
containers; memory limits are a known constraint for these tests.
The `make test` and `make check` targets also serialize Go packages, so setting
the live-test environment variables does not accidentally run language suites
in parallel.

```sh
SQLC_YDB_TEST_DSN=grpc://localhost:2136/local go test -p 1 -count=1 -timeout=180s ./internal/codegen/golang -run TestLiveYDB -v
```

For Python live generator tests, install the pinned example requirements in a
virtual environment, then set `SQLC_YDB_TEST_PYTHON` to its interpreter and run
`go test -p 1 ./internal/codegen/python -run TestLiveYDBGeneratedRuntimes -v` with the
same DSN.

The actual CLI-generated authors example has its own end-to-end checks. These
require a disposable database with no existing `authors` table. Setup fails
without dropping an existing table; successful tests remove the table they made.
Run these sequentially:

```sh
cd examples/authors/go
SQLC_YDB_TEST_DSN=grpc://localhost:2136/local go test -p 1 -count=1 -timeout=90s -v ./...
cd ..
SQLC_YDB_TEST_DSN=grpc://localhost:2136/local python -m python.smoke
```

From `examples/authors`, the Python interpreter needs
`pip install -r python/requirements.txt`. Run the smoke test as a module from
this directory so the generated `python/sqlalchemy` package does not shadow
the installed SQLAlchemy library.
Integration checks include the maximum Uint64 value, UTF-8 text, optional values,
single-column projections, list queries, writes and missing rows.

Build a container with `docker build -t sqlc-ydb:dev .`. The ANTLR-generated Go
parser is large; the Docker build limits compile concurrency and uses more
frequent garbage collection to reduce peak memory use.

The old plugin implementation remains in the archive branch, including its
historical module path and sibling `replace` dependency. The standalone `main`
must not regain either dependency. The runtime SDK versions used to validate
generated code belong to tests/examples, not to the generator's imports.
