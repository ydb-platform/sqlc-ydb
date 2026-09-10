# C++ SDK evidence and example builds

The public API contract is in [C++ generation](../docs/cpp.md). These notes
describe the pinned example environment and its maintainer workarounds, not
a claim about the latest SDK releases.

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

The SDK release examined for this build setup was `v3.22.0`, which publishes Ubuntu 24.04 `libydb-cpp-dev` and `yandex-googleapis-api-common-protos` packages. Its CMake package installs below `/usr/share/yandex`. Native links the real `YDB-CPP-SDK::Driver`, `YDB-CPP-SDK::Params`, and `YDB-CPP-SDK::Query` targets; userver links `userver::ydb`. The pinned userver target also links `YDB-CPP-SDK::ydb-cpp-iam`.

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

Both live smokes expect to run with `examples/authors` as the working directory and use `YDB_CONNECTION_STRING`, defaulting in the wrappers to `grpc://localhost:2136/local`. The smoke launchers split that value into the SDK endpoint `localhost:2136` and database `/local`. SDK 3.21.1 stores `TDriverConfig::SetEndpoint` input verbatim, and userver passes its configured endpoint directly to that method, so the protocol prefix belongs only to the external smoke DSN. They create the `authors` table from `schema.sql` without `IF NOT EXISTS`, test maximum `Uint64`, present and null optionals, missing `:one`, the named single-column query, `:many`, and `:exec`, then drop the table. Cleanup is armed only after table creation succeeds.

```bash
# Run from the repository root after a disposable YDB is ready on port 2136.
docker run --rm --network host \
  -v "$PWD:/workspace" -w /workspace \
  -e YDB_CONNECTION_STRING=grpc://localhost:2136/local \
  sqlc-ydb-authors-cpp bash examples/authors/cpp/run-smoke.sh
```

The runner sets the working directory, executes native first, starts userver,
waits for its listener, invokes `/smoke` once, and stops the userver process.
The compiled binaries stay in the mounted `cpp/build` directory and execute
inside the same SDK image used to build them.
The smoke config disables userver's optional coroutine stack usage monitor,
whose `userfaultfd` call is blocked by Docker's default seccomp profile.
The example runs with ordinary container permissions.
