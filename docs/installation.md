# Installation

## Build from source

With Go 1.26 or newer, run from a checkout of this repository:

```sh
go build -o bin/sqlc-ydb ./cmd/sqlc-ydb
./bin/sqlc-ydb version
./bin/sqlc-ydb version --verbose
```

The executable includes the YQL parser, analyzer and generators. Generation
does not require a running YDB or language SDKs. Applications using the
generated code need the dependencies described in the [target reference](targets.md).
See the [quick start](../README.md#quick-start) for the first generation.

## Release archives

Check [GitHub Releases](https://github.com/ydb-platform/sqlc-ydb/releases) for
published versions and assets. Build from source if a release is not yet
available.

The packaging format covers these targets:

| Platform | Architectures | Archive |
| --- | --- | --- |
| Linux | `amd64`, `arm64` | `.tar.gz` |
| macOS (`darwin` in filenames) | `amd64`, `arm64` | `.tar.gz` |
| Windows | `amd64`, `arm64` | `.zip` |

Download the archive for your operating system and architecture together with
`SHA256SUMS` from the same release. Archive names follow
`sqlc-ydb_VERSION_OS_ARCH.tar.gz` (or `.zip` for Windows), with no leading `v`
in `VERSION`. Each archive contains a matching directory with the executable
and `LICENSE`; the Windows executable is `sqlc-ydb.exe`.

Compare the archive's SHA256 digest with its entry in `SHA256SUMS` before
extracting. For example, replacing `ARCHIVE` with the downloaded filename:

```sh
# Linux
sha256sum ARCHIVE
# macOS
shasum -a 256 ARCHIVE
```

On Windows PowerShell, use `Get-FileHash .\ARCHIVE -Algorithm SHA256`.
After extraction, place the executable in a directory on your `PATH` or invoke
it by its full path.

## Identify the installed build

`sqlc-ydb version` prints the product version, including an RC suffix if any.
`sqlc-ydb version --verbose` also prints the embedded source commit for release
builds. Ordinary source builds report `unknown` for that field unless linker
flags supply it. Include both values when reporting an issue.

Release candidates are for evaluation before a stable release.
