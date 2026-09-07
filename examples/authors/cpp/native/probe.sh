#!/usr/bin/env bash
set -euo pipefail

: "${SQLC_YDB_TEST_DSN:=grpc://localhost:2136/local}"
export SQLC_YDB_TEST_DSN
exec "${1:-./authors_native}"
