FROM ghcr.io/userver-framework/ubuntu-24.04-userver@sha256:8b71ba0bdc5f79038d2e639cc7d8f669405db7377b851f7581b09c67183151e4

# This image's userver 3.2-rc YDB package asks CMake for the obsolete
# "googleapis" package name. The installed YDB SDK 3.21.1 package exports the
# real protobuf target through "yandex-googleapis-api-common-protos" instead.
# Fail if either package layout changes so this metadata-only correction cannot
# silently turn into a partial or stubbed SDK setup.
RUN set -eux; \
    config=/usr/lib/cmake/userver/userver-ydb-config.cmake; \
    package=/usr/share/yandex/lib/cmake/yandex-googleapis-api-common-protos/yandex-googleapis-api-common-protosConfig.cmake; \
    test -f "${config}"; \
    test -f "${package}"; \
    grep -Fq 'find_dependency(googleapis CONFIG)' "${config}"; \
    sed -i 's/find_dependency(googleapis CONFIG)/find_dependency(yandex-googleapis-api-common-protos CONFIG)/' "${config}"; \
    ! grep -Fq 'find_dependency(googleapis CONFIG)' "${config}"; \
    grep -Fq 'find_dependency(yandex-googleapis-api-common-protos CONFIG)' "${config}"

WORKDIR /workspace
