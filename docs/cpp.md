# C++ generation

The built-in C++ generator emits C++20 for two runtimes:

- `runtime: ydb` uses the native YDB C++ Query SDK.
- `runtime: userver` uses userver's asynchronous YDB driver.

Each output contains `models.hpp`, `queries.hpp`, and `queries.cpp`. The configured namespace may contain nested components such as `authors::native`. Generated query methods implement `:one`, `:many`, and `:exec`. A `:one` method returns `std::optional<Row>` and reads the first row, so an empty result becomes `std::nullopt`. A `:many` method returns `std::vector<Row>`. An `:exec` method checks execution success and intentionally discards any result sets. `:execrows` and unsupported types are rejected during generation.

## Configuration

```yaml
gen:
  cpp:
    namespace: authors::native
    out: cpp/native
    runtime: ydb
```

Use `runtime: userver` for the userver adapter. The configuration layer also accepts `runtime: native` as an alias for `ydb`; the generator's canonical runtime names are `ydb` and `userver`.

## Ownership and transactions

`Queries` stores a non-owning reference to the runtime client. The caller must keep `NYdb::NQuery::TQueryClient` or `userver::ydb::TableClient` alive longer than the generated `Queries` object. The native driver and userver component remain caller-owned as well.

Every native generated method calls `TQueryClient::RetryQuerySync`, obtains a retry-managed `TSession`, and executes one query with `BeginTx(SerializableRW()).CommitTx()`. Every retry attempt rebuilds the parameter object. This is a self-contained transaction per generated method; generated methods do not join a caller-owned transaction.

Every userver generated method calls `TableClient::ExecuteQuery`. userver performs retries internally and its default operation settings select a committed serializable read-write transaction for that call. Multi-statement caller transactions belong in handwritten code using `TableClient::RetryTx`; generated methods do not join them.

## Types

| YQL | Native SDK C++ | userver C++ |
| --- | --- | --- |
| `Bool` | `bool` | `bool` |
| `Int8` / `Uint8` | `std::int8_t` / `std::uint8_t` | same |
| `Int16` / `Uint16` | `std::int16_t` / `std::uint16_t` | same |
| `Int32` / `Uint32` | `std::int32_t` / `std::uint32_t` | same |
| `Int64` / `Uint64` | `std::int64_t` / `std::uint64_t` | same |
| `Float` | `float` | unsupported by userver |
| `Double` | `double` | `double` |
| `String` | `std::string` through SDK `String` accessors | `std::string` |
| `Utf8` | `std::string` through SDK `Utf8` accessors | `userver::ydb::Utf8` |
| `Optional<T>` | `std::optional<T>` | `std::optional<T>` |

The native mapping retains the `String` versus `Utf8` distinction in its parameter builders and result parsers even though both values use `std::string`. userver uses its strong `Utf8` typedef, so the distinction is also visible in the public C++ type. Nested optionals and non-scalar containers are rejected explicitly.

Identifiers must be ASCII C++ identifiers, must not be C++20 keywords, and must not start with `_` or the generator-reserved `sqlc_` prefix. Duplicate query, parameter, or result-column names are rejected, as are names that collide with generated row types, the `Queries` class, its client member, or per-query SQL constants. Generated SQL normally remains readable as a multiline raw string. The generator selects a raw-string delimiter absent from the SQL and switches to length-preserving escaped fragments for control bytes, carriage returns, byte-order marks, and invalid UTF-8.

## Upstream API evidence

The API was checked on 2026-09-07 against these exact revisions:

- YDB C++ SDK `main`: [`6ea7a0f93bd97bcb92dca3a4ac948bb743077e50`](https://github.com/ydb-platform/ydb-cpp-sdk/tree/6ea7a0f93bd97bcb92dca3a4ac948bb743077e50). `TQueryClient` declares the `ExecuteQuery` and `RetryQuerySync` overloads in [`client.h`](https://github.com/ydb-platform/ydb-cpp-sdk/blob/6ea7a0f93bd97bcb92dca3a4ac948bb743077e50/include/ydb-cpp-sdk/client/query/client.h#L74-L120). The SDK value API provides width-specific, `String`, `Utf8`, and optional builders/parsers in [`value.h`](https://github.com/ydb-platform/ydb-cpp-sdk/blob/6ea7a0f93bd97bcb92dca3a4ac948bb743077e50/include/ydb-cpp-sdk/client/value/value.h#L328-L510). Status failures are raised by `NYdb::NStatusHelpers::ThrowOnError`, declared in [`status.h`](https://github.com/ydb-platform/ydb-cpp-sdk/blob/6ea7a0f93bd97bcb92dca3a4ac948bb743077e50/include/ydb-cpp-sdk/client/types/status/status.h#L51-L100). The maintained basic example demonstrates `RetryQuerySync`, transaction control, parameter construction, and `TResultSetParser` in [`basic_example.cpp`](https://github.com/ydb-platform/ydb-cpp-sdk/blob/6ea7a0f93bd97bcb92dca3a4ac948bb743077e50/examples/basic_example/basic_example.cpp#L163-L247).
- userver `develop`: [`86759637d175baa64f0b3b01f1a027bfedbf795a`](https://github.com/userver-framework/userver/tree/86759637d175baa64f0b3b01f1a027bfedbf795a). `TableClient::ExecuteQuery` and its retry contract are declared in [`table.hpp`](https://github.com/userver-framework/userver/blob/86759637d175baa64f0b3b01f1a027bfedbf795a/ydb/include/userver/ydb/table.hpp#L90-L235). Cursor and typed row extraction are defined in [`response.hpp`](https://github.com/userver-framework/userver/blob/86759637d175baa64f0b3b01f1a027bfedbf795a/ydb/include/userver/ydb/response.hpp#L35-L180). The public primitive mapping, including the absence of `Float` and the distinct `Utf8` strong type, is documented in [`types.hpp`](https://github.com/userver-framework/userver/blob/86759637d175baa64f0b3b01f1a027bfedbf795a/ydb/include/userver/ydb/types.hpp#L15-L75). The implementation shows retry-managed Query SDK execution and per-call transaction selection in [`table.cpp`](https://github.com/userver-framework/userver/blob/86759637d175baa64f0b3b01f1a027bfedbf795a/ydb/src/ydb/table.cpp#L420-L445). The official Ubuntu image enables YDB in [`ubuntu-24.04-userver.dockerfile`](https://github.com/userver-framework/userver/blob/86759637d175baa64f0b3b01f1a027bfedbf795a/scripts/docker/ubuntu-24.04-userver.dockerfile), and [`SetupYdbCppSDK.cmake`](https://github.com/userver-framework/userver/blob/86759637d175baa64f0b3b01f1a027bfedbf795a/cmake/SetupYdbCppSDK.cmake#L4-L55) pins SDK 3.21.1 and requests its `Iam` component.
- YDB documentation `main`: [`2a1fce8e188f51004950d1e8684b395304a229aa`](https://github.com/ydb-platform/ydb/tree/2a1fce8e188f51004950d1e8684b395304a229aa). The [retry guide](https://github.com/ydb-platform/ydb/blob/2a1fce8e188f51004950d1e8684b395304a229aa/ydb/docs/en/core/recipes/ydb-sdk/retry.md) recommends native `RetryQuerySync` with a session and states that all userver `TableClient` methods include retry handling.

## Build and smoke

Generate all authors adapters from the repository root:

```bash
go run ./cmd/sqlc-ydb generate -f examples/authors/sqlc.yaml
```

The current YDB C++ SDK release is `v3.22.0`, which publishes Ubuntu 24.04 `libydb-cpp-dev` and `yandex-googleapis-api-common-protos` packages. Its CMake package installs below `/usr/share/yandex`. Native links the real `YDB-CPP-SDK::Driver`, `YDB-CPP-SDK::Params`, and `YDB-CPP-SDK::Query` targets; userver links `userver::ydb`. The pinned userver target also links `YDB-CPP-SDK::ydb-cpp-iam`.

The official `ghcr.io/userver-framework/ubuntu-24.04-userver` image is built with `USERVER_FEATURE_YDB=1` and includes the YDB SDK packages. The compile environment is pinned in `examples/authors/cpp/Dockerfile` to `ghcr.io/userver-framework/ubuntu-24.04-userver@sha256:8b71ba0bdc5f79038d2e639cc7d8f669405db7377b851f7581b09c67183151e4`, which contains userver 3.2-rc and YDB C++ SDK 3.21.1.

That image's installed `userver-ydb-config.cmake` has two packaging defects. It asks for the obsolete CMake package name `googleapis`, while its real installed SDK package exports `yandex-googleapis-api-common-protos::api-common-protos` from `yandex-googleapis-api-common-protosConfig.cmake`. It also loads the SDK without components even though `userver::ydb` requires the IAM library. The SDK 3.21.1 package is not safe to load twice with different component lists because it recreates component aliases.

The Dockerfile verifies and corrects both dependency lines. Its single SDK load requests `Driver`, `Params`, and `Query` for the native example plus `Iam` for `userver::ydb`. The top-level project therefore loads userver once and uses the real SDK targets that dependency exports. The workaround does not add replacement headers, targets, or libraries. These commands use Linux with an amd64 Docker engine, as in CI. Build and compile before starting local-ydb, and keep compilation sequential:

```bash
docker build \
  -t sqlc-ydb-authors-cpp \
  -f examples/authors/cpp/Dockerfile examples/authors/cpp

docker run --rm \
  -v "$PWD:/workspace" -w /workspace \
  sqlc-ydb-authors-cpp \
  bash -lc 'cmake -S examples/authors/cpp -B examples/authors/cpp/build -GNinja -DCMAKE_PREFIX_PATH=/usr/share/yandex && cmake --build examples/authors/cpp/build --target authors_native authors_userver -j1'
```

Both live smokes expect to run with `examples/authors` as the working directory and use `SQLC_YDB_TEST_DSN`, defaulting in the wrappers to `grpc://localhost:2136/local`. The smoke launchers split that value into the SDK endpoint `grpc://localhost:2136` and database `/local`; this matches `TDriverConfig::SetEndpoint` plus `SetDatabase` and userver's YDB component schema. They create the `authors` table from `schema.sql` without `IF NOT EXISTS`, test maximum `Uint64`, present and null optionals, missing `:one`, the named single-column query, `:many`, and `:exec`, then drop the table. Cleanup is armed only after table creation succeeds.

```bash
# Run from the repository root after a disposable YDB is ready on port 2136.
docker run --rm --network host \
  -v "$PWD:/workspace" -w /workspace \
  -e SQLC_YDB_TEST_DSN=grpc://localhost:2136/local \
  sqlc-ydb-authors-cpp bash examples/authors/cpp/run-smoke.sh
```

The runner sets the working directory, executes native first, starts userver,
waits for its listener, invokes `/smoke` once, and stops the userver process.
The compiled binaries stay in the mounted `cpp/build` directory and execute
inside the same SDK image used to build them.
