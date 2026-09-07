FROM ghcr.io/userver-framework/ubuntu-24.04-userver@sha256:8b71ba0bdc5f79038d2e639cc7d8f669405db7377b851f7581b09c67183151e4

# This image's userver 3.2-rc YDB package asks CMake for the obsolete
# "googleapis" package name and loads the non-idempotent YDB SDK package without
# the components required by its exported target. Correct both dependency lines
# and fail if the pinned package layout changes. This only fixes installed CMake
# metadata; all headers, targets, and libraries remain the packaged artifacts.
RUN set -eux; \
    config=/usr/lib/cmake/userver/userver-ydb-config.cmake; \
    package=/usr/share/yandex/lib/cmake/yandex-googleapis-api-common-protos/yandex-googleapis-api-common-protosConfig.cmake; \
    test -f "${config}"; \
    test -f "${package}"; \
    grep -Fq 'find_dependency(googleapis CONFIG)' "${config}"; \
    grep -Fq 'find_dependency(ydb-cpp-sdk CONFIG)' "${config}"; \
    sed -i 's/find_dependency(googleapis CONFIG)/find_dependency(yandex-googleapis-api-common-protos CONFIG)/' "${config}"; \
    sed -i 's/find_dependency(ydb-cpp-sdk CONFIG)/find_dependency(ydb-cpp-sdk CONFIG COMPONENTS Driver Params Query Iam)/' "${config}"; \
    ! grep -Fq 'find_dependency(googleapis CONFIG)' "${config}"; \
    ! grep -Fxq '    find_dependency(ydb-cpp-sdk CONFIG)' "${config}"; \
    grep -Fq 'find_dependency(yandex-googleapis-api-common-protos CONFIG)' "${config}"; \
    grep -Fq 'find_dependency(ydb-cpp-sdk CONFIG COMPONENTS Driver Params Query Iam)' "${config}"

WORKDIR /workspace
