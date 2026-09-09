# Development

Release artifact builds and the manual workflow's dry-run mode are described in
[releasing](releasing.md); product readiness and responsibilities are in
[the release plan](release-plan.md).

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

[Git attributes](../.gitattributes) keep text files in LF form on every host.
Release scripts and generated fixtures must survive a Windows checkout without
byte changes; `make test-release` checks this with `core.autocrlf=true`.

CI separates this fast offline suite from Linux acceptance jobs. Each acceptance
host runs a single pinned YDB service and waits for the image's health check;
there is no multi-database startup framework, engine matrix, plugin subprocess
runner or optional Postgres/MySQL fallback. See
[the fixture runner](../internal/endtoend/README.md) for the upstream references
and baseline update command.

Generator tests compile generated Go against the selected SDK in a temporary
module and execute generated code using mock adapters. Python 3.9 or newer must
be available as `python3` for the Python generator's execution tests. Java 17+
(`javac` and `java`), a C++20 compiler, Node.js 22, Rust 1.95, and PHP 8.2
are used for exact SQL literal round-trip checks. A normal generator build needs
only Go.

Published SDK checks are opt-in locally and enabled in CI:

```sh
SQLC_YDB_CSHARP_DOTNET=dotnet go test ./internal/codegen/csharp
SQLC_YDB_TEST_MAVEN=mvn go test ./internal/codegen/java
SQLC_YDB_RUST_SDK_CHECK=1 go test ./internal/codegen/rust
```

These compile generated scalar and nullable bindings using the real .NET,
Java and Rust dependencies. C# also compiles and runs its SQL byte checks.
No generated runtime imports are added to the generator's Go module.

## Generated runtime checks

The [examples](../examples/README.md) share one Go module in `examples/`; generated
code and tests live under each example's `go/` directory. C# framework,
JavaScript, Rust and PHP examples share dependencies and test harnesses in
`examples/csharp`, `examples/javascript`, `examples/rust` and `examples/php`.
JavaScript dependencies resolve from `examples/package.json`. The authors
example retains the Python, Java, C++ and ADO.NET application builds. Schema,
queries and generator configuration are shared in each example root. `make
generate` and `make check-examples` cover every example configuration; the
release smoke test also compiles and diffs all of them.

Check all generated example families against their pinned runtime dependencies:

```sh
make check-examples
dotnet build examples/csharp/GeneratedProfiles.csproj
npm ci --prefix examples --ignore-scripts
npm run check --prefix examples
CARGO_BUILD_JOBS=1 cargo test --manifest-path examples/rust/Cargo.toml --locked
composer install --working-dir=examples/php
composer --working-dir=examples/php check
```

Optional live generator tests use `SQLC_YDB_TEST_DSN` to select an isolated YDB
database. Use a disposable development database: tests create and drop uniquely
named tables. No live tests run when the variable is absent.

**Run local-ydb checks sequentially.** Do not run different image versions,
runtime suites or Docker builds concurrently on one host. Use `go test -p 1`
for live suites and do not add `t.Parallel` to them. CI intentionally runs Go,
Python, C#, Java, JavaScript, Rust and PHP examples in successive steps. If
image-version coverage is added later, preserve sequential execution rather than
introducing concurrent containers; memory limits are a known constraint.
The `make test` and `make check` targets also serialize Go packages, so setting
the live-test environment variables does not accidentally run language suites
in parallel.

C# and the four Java profiles run in successive acceptance steps too. C++ uses
the pinned userver/SDK development image to compile both executables before
starting local-ydb; native and userver runtime probes then execute sequentially.
The test image and its CMake packaging workaround are in
`examples/authors/cpp/Dockerfile`; see [C++](cpp.md) for commands.

```sh
SQLC_YDB_TEST_DSN=grpc://localhost:2136/local go test -p 1 -count=1 -timeout=180s ./internal/codegen/golang -run TestLiveYDB -v
```

For Python live generator tests, install the pinned example requirements in a
virtual environment, then set `SQLC_YDB_TEST_PYTHON` to its interpreter and run
`go test -p 1 ./internal/codegen/python -run TestLiveYDBGeneratedRuntimes -v` with the
same DSN.

Each Go example executes the actual CLI-generated queries against a disposable
database, including JSON and timestamp bindings, joins, aggregates and migrations.
The database must not already contain any example tables. Cleanup is registered
after successful schema files; a failed file can leave partially created tables,
but must never drop a pre-existing table. Run all example packages sequentially:

```sh
cd examples
SQLC_YDB_TEST_DSN=grpc://localhost:2136/local go test -p 1 -count=1 -timeout=180s -v ./...
cd authors
SQLC_YDB_TEST_DSN=grpc://localhost:2136/local python -m python.smoke
```

From `examples/authors`, the Python interpreter needs
`pip install -r python/requirements.txt`. Run the smoke test as a module from
this directory so the generated `python/sqlalchemy` package does not shadow
the installed SQLAlchemy library.
Integration checks include the maximum Uint64 value, UTF-8 text, optional values,
single-column projections, list queries, writes and missing rows.

From `examples/authors`, the additional live checks are:

```sh
SQLC_YDB_TEST_DSN='Host=localhost;Port=2136;Database=/local' \
  dotnet run --project csharp/adonet/Authors.AdoNet.csproj
SQLC_YDB_TEST_DSN=grpc://localhost:2136/local sh java/run-smoke.sh
```

Each creates and drops its own `authors` table only after a successful create.
Use an otherwise empty disposable database. Java/.NET runtime builds and tests
are separate from offline SQL generation.

Run the shared Dapper, linq2db, JavaScript, Rust and PHP harnesses from the
repository root. Each command covers all five example families:

```sh
SQLC_YDB_TEST_DSN='Host=localhost;Port=2136;Database=/local' \
  dotnet run --project examples/csharp/GeneratedProfiles.csproj --no-build -- dapper
SQLC_YDB_TEST_DSN='Host=localhost;Port=2136;Database=/local' \
  dotnet run --project examples/csharp/GeneratedProfiles.csproj --no-build -- linq2db
SQLC_YDB_TEST_DSN=grpc://localhost:2136/local npm run smoke --prefix examples
SQLC_YDB_TEST_DSN=grpc://localhost:2136/local CARGO_BUILD_JOBS=1 \
  cargo test --manifest-path examples/rust/Cargo.toml --locked --test live_smoke -- --nocapture
SQLC_YDB_TEST_DSN=grpc://localhost:2136/local composer --working-dir=examples/php smoke
```

The [JavaScript](javascript.md), [Rust](rust.md) and [PHP](php.md) pages define
their value representations, dependencies and runtime ownership.

Build a container with `docker build -t sqlc-ydb:dev .`. The ANTLR-generated Go
parser is large; the Docker build limits compile concurrency and uses more
frequent garbage collection to reduce peak memory use.

The old plugin implementation remains in the archive branch, including its
historical module path and sibling `replace` dependency. The standalone `main`
must not regain either dependency. Runtime SDK versions belong to tests and
examples, not to the generator's imports.
