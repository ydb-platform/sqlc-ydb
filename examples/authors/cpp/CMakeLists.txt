cmake_minimum_required(VERSION 3.22)
project(sqlc_ydb_authors_cpp LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

# userver::ydb links the SDK IAM library in the pinned distribution, so load
# that component before userver imports its installed targets.
find_package(ydb-cpp-sdk REQUIRED COMPONENTS Driver Params Query Iam)
find_package(userver REQUIRED COMPONENTS core ydb)

add_subdirectory(native)
add_subdirectory(userver)
