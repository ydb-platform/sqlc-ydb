#!/usr/bin/env bash
set -euo pipefail

: "${YDB_CONNECTION_STRING:=grpc://localhost:2136/local}"
export YDB_CONNECTION_STRING
exec "${1:-./authors_native}"
