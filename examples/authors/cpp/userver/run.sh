#!/usr/bin/env bash
set -euo pipefail

: "${SQLC_YDB_TEST_DSN:=grpc://localhost:2136/local}"

if [[ ! "${SQLC_YDB_TEST_DSN}" =~ ^(grpc://[^/]+)(/.*)$ ]]; then
    echo "SQLC_YDB_TEST_DSN must look like grpc://host:port/database" >&2
    exit 2
fi

endpoint="${BASH_REMATCH[1]}"
database="${BASH_REMATCH[2]}"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

exec "${1:-./authors_userver}" \
    --config "${2:-${script_dir}/static_config.yaml}" \
    --config_vars <(printf '{"ydb-endpoint":"%s","ydb-database":"%s"}\n' "${endpoint}" "${database}")
