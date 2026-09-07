# Code generation using Docker

You can generate code with sqlc-ydb without installing sqlc or any plugins on your system. Everything runs inside a minimal Docker image.

## Prerequisites

- Docker
- A project with `sqlc.yaml`, `schema.sql`, and `queries.sql` (see [examples](../examples/) for v2 config and YDB schema/query syntax)

## Using the published image

If the image is published to GitHub Container Registry (ghcr.io):

```bash
# Replace OWNER with the GitHub org/user (e.g. sqlc-dev)
docker run --rm -v "$(pwd):/src" -w /src ghcr.io/OWNER/sqlc-ydb:latest generate
```

Your current directory must contain `sqlc.yaml` and the schema/query files referenced in it. Generated code will appear in the output directories specified in `sqlc.yaml` (e.g. `ydb-go-sdk/`, `ydb-database-sql/`).

## Building the image yourself

From the sqlc-ydb repository root:

```bash
make docker-build
```

This builds an image named `sqlc-ydb` by default. Override the name:

```bash
make docker-build DOCKER_IMAGE=my-sqlc-ydb
```

Optional build-args (branch/tag to clone from the sqlc fork):

```bash
docker build --build-arg ENGINE_PLUGIN_REF=engine-plugin -t sqlc-ydb .
```

The image builds sqlc from the fork https://github.com/ydb-platform/sqlc (branch `engine-plugin` by default).

## Running code generation

From your project directory (where `sqlc.yaml` lives):

```bash
docker run --rm -v "$(pwd):/src" -w /src sqlc-ydb generate
```

- `--rm` removes the container after it exits.
- `-v "$(pwd):/src"` mounts your project into the container at `/src`.
- `-w /src` sets the working directory so sqlc reads your config and writes output there.

Generated files are created on your host in the same paths as when running sqlc locally.

## What’s in the image

- **sqlc** (built from [engine-plugin](https://github.com/sqlc-dev/engine-plugin))
- **sqlc-engine-ydb** (YDB engine plugin)
- **sqlc-gen-ydb-go-sdk** (codegen for ydb-go-sdk)
- **sqlc-gen-ydb-database-sql** (codegen for database/sql)

The runtime image is based on Alpine for small size; no Go or other build tools are installed in the final image.
