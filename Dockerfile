# sqlc-ydb: minimal image with sqlc (from ydb-platform/sqlc fork), sqlc-engine-ydb, and codegen plugins.
# User runs: docker run -v $(pwd):/src -w /src <image> generate
#
# Build: from sqlc-ydb repo root. sqlc is built from git clone of the fork.
# Fork with engine support: https://github.com/ydb-platform/sqlc, branch engine-plugin.
# Optional build-arg: ENGINE_PLUGIN_REF=engine-plugin (branch/tag to clone).

# Must satisfy sqlc-ydb/go.mod (e.g. go 1.26.0) and the cloned engine-plugin go version.
ARG GO_VERSION=1.26
ARG ENGINE_PLUGIN_REF=engine-plugin

# --- Build sqlc and sqlc-ydb plugins ---
FROM golang:${GO_VERSION}-alpine AS builder
ARG ENGINE_PLUGIN_REF
RUN apk add --no-cache git make protobuf-dev
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
ENV PATH="${PATH}:/root/go/bin"

# Avoid git credential prompt in Docker (public repo, no auth needed).
ENV GIT_TERMINAL_PROMPT=0
RUN git config --global credential.helper ""

WORKDIR /work
# Clone ydb-platform/sqlc fork (branch engine-plugin) so go.mod replace works.
RUN git -c credential.helper= clone --depth 1 -b "${ENGINE_PLUGIN_REF}" \
    https://github.com/ydb-platform/sqlc.git engine-plugin
COPY . sqlc-ydb/

WORKDIR /work/sqlc-ydb
RUN go mod download
RUN make proto && make build
RUN cd /work/engine-plugin && go build -o /work/sqlc ./cmd/sqlc

# --- Minimal runtime ---
# We use Alpine (not scratch) because sqlc runs engine and codegen plugins as child processes
# (separate executables). scratch has no dynamic linker/libc and cannot exec(3) other binaries.
FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /work/sqlc /usr/local/bin/sqlc
COPY --from=builder /work/sqlc-ydb/bin/sqlc-engine-ydb /usr/local/bin/sqlc-engine-ydb
COPY --from=builder /work/sqlc-ydb/bin/sqlc-gen-ydb-go-sdk /usr/local/bin/sqlc-gen-ydb-go-sdk
COPY --from=builder /work/sqlc-ydb/bin/sqlc-gen-ydb-database-sql /usr/local/bin/sqlc-gen-ydb-database-sql
COPY --from=builder /work/sqlc-ydb/bin/sqlc-gen-ydb-python-sdk /usr/local/bin/sqlc-gen-ydb-python-sdk
ENV PATH="/usr/local/bin:${PATH}"

WORKDIR /src
ENTRYPOINT ["sqlc"]
CMD ["generate"]
