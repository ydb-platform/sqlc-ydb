#!/usr/bin/env bash
# Install the published sqlc-ydb binary without Go or administrator privileges.

install_sqlc_ydb() (
    set -euo pipefail

    fail() { printf 'sqlc-ydb: %s\n' "$*" >&2; exit 1; }
    download() {
        curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' \
            --connect-timeout 15 --max-time 300 --retry 2 "$@"
    }

    local version='' bin_dir="${HOME:?HOME must be set}/.local/bin"
    while (($#)); do
        case "$1" in
            --version|--bin-dir)
                (($# >= 2)) && [[ -n $2 ]] || fail "$1 requires a value"
                case "$1" in
                    --version) version=$2 ;;
                    --bin-dir) bin_dir=$2 ;;
                esac
                shift 2 ;;
            -h|--help)
                printf 'Usage: bash install.sh [--version vX.Y.Z[-rcN]] [--bin-dir DIRECTORY]\n'
                return ;;
            *) fail "unknown argument: $1" ;;
        esac
    done

    local os arch
    case "$(uname -s)" in
        Linux) os=linux ;;
        Darwin) os=darwin ;;
        *) fail 'supported platforms: Linux and macOS; use release ZIP archives on Windows' ;;
    esac
    case "$(uname -m)" in
        x86_64|amd64) arch=amd64 ;;
        arm64|aarch64) arch=arm64 ;;
        *) fail 'supported architectures: amd64 and arm64' ;;
    esac
    command -v curl >/dev/null || fail 'curl is required'
    command -v tar >/dev/null || fail 'tar is required'
    local hash_command
    if command -v sha256sum >/dev/null; then
        hash_command=sha256sum
    elif command -v shasum >/dev/null; then
        hash_command=shasum
    else
        fail 'sha256sum or shasum is required'
    fi

    local releases='https://github.com/ydb-platform/sqlc-ydb/releases' url
    if [[ -z $version ]]; then
        url=$(download --output /dev/null --write-out '%{url_effective}' "$releases/latest") ||
            fail 'cannot resolve the latest stable release; use --version vX.Y.Z-rcN for a published RC'
        [[ $url == "$releases/tag/"* ]] || fail 'no stable release found; specify a published version with --version'
        version=${url##*/}
    fi
    [[ $version =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rc(0|[1-9][0-9]*))?$ ]] ||
        fail 'version must have the form vX.Y.Z or vX.Y.Z-rcN'

    local work='' staged=''
    trap '[[ -z $work ]] || rm -rf "$work"; [[ -z $staged ]] || rm -f "$staged"' EXIT
    work=$(mktemp -d)
    local base="sqlc-ydb_${version#v}_${os}_${arch}" archive
    archive="$base.tar.gz"
    printf 'Installing sqlc-ydb %s (%s/%s)...\n' "${version#v}" "$os" "$arch"
    download --output "$work/$archive" "$releases/download/$version/$archive"
    download --output "$work/SHA256SUMS" "$releases/download/$version/SHA256SUMS"
    local expected actual
    expected=$(awk -v name="$archive" '$2 == name {print $1}' "$work/SHA256SUMS")
    [[ $expected =~ ^[0-9a-fA-F]{64}$ ]] || fail "missing or ambiguous checksum for $archive"
    if [[ $hash_command == sha256sum ]]; then
        actual=$(sha256sum "$work/$archive")
    else
        actual=$(shasum -a 256 "$work/$archive")
    fi
    [[ ${actual%% *} == "$expected" ]] || fail 'checksum mismatch; existing installation was not changed'
    # Extract only the executable as bytes, without trusting archive paths or modes.
    tar -xzOf "$work/$archive" "$base/sqlc-ydb" > "$work/sqlc-ydb"
    chmod 755 "$work/sqlc-ydb"
    [[ $("$work/sqlc-ydb" version) == "${version#v}" ]] || fail 'downloaded binary reports an unexpected version'

    mkdir -p -- "$bin_dir"
    bin_dir=$(cd -- "$bin_dir" && pwd)
    local target="$bin_dir/sqlc-ydb"
    [[ ! -d $target ]] || fail "$target is a directory"
    if [[ -f $target ]] && cmp -s "$work/sqlc-ydb" "$target"; then
        printf 'sqlc-ydb %s is already installed.\n' "${version#v}"
    else
        staged=$(mktemp "$bin_dir/.sqlc-ydb.XXXXXX")
        cp "$work/sqlc-ydb" "$staged"
        chmod 755 "$staged"
        mv -f -- "$staged" "$target"
        staged=''
        printf 'Installed sqlc-ydb %s\n' "${version#v}"
    fi
    printf 'Location: %s\n' "$target"
    case ":${PATH}:" in
        *":$bin_dir:"*) ;;
        *)
            printf '\nAdd this directory to PATH in your terminal and shell startup file:\n'
            if [[ ${SHELL:-} == */fish ]]; then
                printf '  fish_add_path %q\n' "$bin_dir"
            else
                printf '  export PATH=%q:"$PATH"\n' "$bin_dir"
            fi
            return ;;
    esac
    local active
    active=$(command -v sqlc-ydb || true)
    if [[ $active != "$target" ]]; then
        printf '\nAnother sqlc-ydb takes precedence: %s\nPut %s first in PATH.\n' "$active" "$bin_dir"
    else
        printf '\nNext: sqlc-ydb init\n'
    fi
)

# Keep this call last: a truncated download must not start installation.
install_sqlc_ydb "$@"
