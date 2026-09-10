#!/usr/bin/env bash
set -euo pipefail

: "${YDB_CONNECTION_STRING:?set YDB_CONNECTION_STRING to a disposable YDB database}"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
build_dir="${1:-${script_dir}/build}"
cd "${script_dir}/.."

"${build_dir}/native/authors_native"

cpp/userver/run.sh "${build_dir}/userver/authors_userver" &
userver_pid=$!
cleanup() {
    kill "${userver_pid}" 2>/dev/null || true
    wait "${userver_pid}" 2>/dev/null || true
}
trap cleanup EXIT

# Wait for the listener without retrying a failed database exercise.
ready=false
for attempt in {1..60}; do
    if ! kill -0 "${userver_pid}" 2>/dev/null; then
        echo 'userver exited before opening its listener' >&2
        exit 1
    fi
    if (echo > /dev/tcp/127.0.0.1/8080) 2>/dev/null; then
        ready=true
        break
    fi
    sleep 1
done
if [[ "${ready}" != true ]]; then
    echo 'userver did not open its listener within 60 seconds' >&2
    exit 1
fi
cpp/userver/probe.sh
