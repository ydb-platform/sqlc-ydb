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

`make test`, `make coverage`, and `make check` generate example outputs locally
before running tests. Sources, configurations, dependency manifests and handwritten
harnesses remain in the repository; missing generated example files are recreated
by `make generate`.

The fast suite includes a YDB-only golden fixture runner under
`internal/endtoend`: real CLI generation must match committed output filenames,
contents and expected diagnostics. Fixture updates are explicit, never an
automatic part of tests. Semantic unit tests independently assert resolved
parameters, result columns and errors in their owning analyzer and builtins suites.
Adapted CLI fixtures use the same runner; source references are recorded in
[provenance](../docs/provenance.md).

Review `git diff --cached --check` after staging new files: an unstaged diff
does not include untracked generated outputs. If significant whitespace inside
SQL triggers a warning, escape it in the source literal without trimming or
otherwise changing the runtime SQL text.

CI separates offline checks from Linux acceptance jobs. Each acceptance host
uses one pinned YDB service.

## Code coverage

```sh
make coverage
go tool cover -html=coverage.out -o coverage.html
```

`make coverage` runs the root Go module's tests without caching and writes
`coverage.out` in atomic mode. `-coverpkg=./...` includes calls across package
boundaries, so CLI and golden tests contribute to analyzer and generator coverage.
The final `total` from `go tool cover` is the combined statement coverage; the
per-test-package percentages are not independent package coverage figures.
The denominator contains only sqlc-ydb runtime code, including the CLI entry
point even when untested. Unit and end-to-end tests run and contribute coverage
of that code, but their own source is not measured. Examples, the
`internal/endtoend` harness, `*_test.go` files, golden outputs, `.github` tooling
and the external ANTLR parser dependency are outside this scope. Go already
excludes test source and the separate examples module from the profile;
`codecov.yml` also explicitly excludes the repository's non-runtime paths.
This local profile excludes live YDB
tests unless their environment variables are set, and excludes optional SDK
checks unless explicitly enabled.

CI saves the profile as the `generator-coverage` artifact and uploads it to
Codecov with the `unit` flag on pushes to `main` and pull requests. The
`ydb-acceptance` job also instruments the existing semantic, Go generator and
Python generator live tests and uploads their three profiles with the
`integration` flag, saving them as the `generator-integration-coverage` artifact.
Codecov merges these profiles; generated application runtime coverage and the
separate optional SDK compilation checks are not measured. The PR comment
updates as reports arrive, so the first report can show only offline coverage.
[codecov.yml](../codecov.yml)
compares project coverage with the base commit (allowing a one percentage point
drop) and requires 80% patch coverage. It enables one updated PR comment with
the coverage difference and impacted files, including on the first PR without
a base report. A successful `main` upload establishes the comparison baseline
and populates the README badge.

Repository administrators must enable `ydb-platform/sqlc-ydb` in Codecov and
grant the [Codecov GitHub App](https://github.com/apps/codecov) access so it can
post PR comments. Set the repository Actions secret `CODECOV_TOKEN` to the
Codecov upload token, as in ydb-go-sdk. Alternatively, the organization can
allow tokenless public uploads with **Global Upload Token → Not required** in
Codecov; the action accepts an empty secret in that mode. Public fork PR uploads
do not need access to the secret. See [Codecov token authentication](https://docs.codecov.com/docs/codecov-tokens)
and [PR comments](https://docs.codecov.com/docs/pull-request-comments).
An upload failure fails the CI job rather than silently leaving stale coverage.

## Test prerequisites

PHP CI installs the pinned gRPC 1.83.1 binary package with
[`.github/scripts/install-php-grpc`](../.github/scripts/install-php-grpc).
It downloads the PHP 8.2 package from the Ondřej Surý Ubuntu PPA, verifies its
pinned SHA-256 and extracts `grpc.so` without requiring PHP packages in APT.
It verifies the binary loads in PHP before installing it into PHP's extension
directory, then enables it and checks the loaded version. Both PHP jobs
use Ubuntu 24.04 amd64. Installation has a five-minute timeout and never falls
back to compilation. Package updates require updating the revision and checksum
in the script together and running the PHP checks and live smoke tests.

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

## Golden fixtures

Each directory in `internal/endtoend/testdata` is a standalone current YDB input.
Positive fixtures contain `expected/` generated files; negative fixtures contain
`stderr.txt`. The runner copies inputs into a temporary directory and invokes
the real CLI, checking output filenames, contents and diagnostics together.

```sh
go test ./internal/analyzer ./internal/yql/builtins ./internal/endtoend
# Only after reviewing an intentional output change:
go test ./internal/endtoend -update
```

Review updated files alongside the SQL, configuration and implementation changes.
Do not hand-edit generated files or accept a new baseline just to make a test
pass. Tests never update goldens by default; update mode fails if a positive
fixture fails generation. New negative fixtures must explicitly include
`stderr.txt`. Assert distinct semantic behavior in the owning unit suite; add a
CLI fixture when configuration, source loading or generated output needs joint
coverage. Adapt historical scenarios to the current contracts rather than
restoring bulk snapshots or obsolete options.

## Generated runtime checks

The [examples](../examples/README.md) share one Go module in `examples/`; generated
code and tests live under each example's `go/` directory. C# framework,
JavaScript, Rust and PHP examples share dependencies and test harnesses in
`examples/csharp`, `examples/javascript`, `examples/rust` and `examples/php`.
JavaScript dependencies resolve from `examples/package.json`. The authors
example retains the Python, Java, C++ and ADO.NET application builds. Schema,
queries and generator configuration are shared in each example root. `make
generate` and `make check-examples` cover every example configuration; the
release smoke test also compiles, generates and diffs all of them. Both checks
fail when generation changes tracked files under `examples`, so committed
generated outputs are still checked for drift. Missing untracked outputs are
created locally before runtime compilation.

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

The semantic metadata suite compares analyzer result types, nullability and column
order directly with YDB, independently of generated code. Run it sequentially
with the runtime suites after installing the pinned Python dependencies:

```sh
SQLC_YDB_TEST_DSN=grpc://localhost:2136/local SQLC_YDB_TEST_PYTHON=python3 \
  go test -p 1 -count=1 -timeout=180s ./internal/endtoend -run TestLiveYDBSemanticTypes -v
```

It creates and drops one uniquely named table. Its source and table-driven
function probes document the executable coverage; offline diagnostic tests do
not establish that the server accepts the SQL.

**Run local-ydb checks sequentially per host**, including image versions, runtime
suites and Docker builds. Use `go test -p 1` and no `t.Parallel` in live tests;
concurrent runs can exhaust host memory. `make test` and `make check` also
serialize packages.

Recreate disposable local-ydb containers after stopping them when using
`YDB_USE_IN_MEMORY_PDISKS=true`. Restarting the same container can retain storage
metadata without the in-memory disk contents and fail schema operations.

C# and the four Java profiles run in successive acceptance steps too. C++ uses
the pinned userver/SDK development image to compile both executables before
starting local-ydb; native and userver runtime probes then execute sequentially.
The test image and its CMake packaging workaround are in
`examples/authors/cpp/Dockerfile`; see
[C++ development](cpp-development.md) for commands.

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

The [JavaScript](../docs/javascript.md), [Rust](../docs/rust.md) and
[PHP](../docs/php.md) pages define their value representations, dependencies
and runtime ownership.
