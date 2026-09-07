cmake_minimum_required(VERSION 3.22)
project(sqlc_ydb_authors_cpp LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

# The pinned image's corrected userver YDB package loads these SDK components
# once and exposes the SDK aliases used by the native example as well.
find_package(userver REQUIRED COMPONENTS core ydb)

add_subdirectory(native)
add_subdirectory(userver)
