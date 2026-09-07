#!/usr/bin/env bash
set -euo pipefail

: "${AUTHORS_USERVER_URL:=http://127.0.0.1:8080/smoke}"
curl --fail --silent --show-error "${AUTHORS_USERVER_URL}"
