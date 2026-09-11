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

# Compile every generated C++ example in the same pinned SDK environment.
foreach(example batch booktest jets ondeck)
  foreach(runtime native userver)
    set(source_dir "${CMAKE_CURRENT_SOURCE_DIR}/../../${example}/cpp/${runtime}")
    add_library(${example}_${runtime} STATIC "${source_dir}/queries.cpp")
    target_include_directories(${example}_${runtime} PUBLIC "${source_dir}")
    if(runtime STREQUAL "native")
      target_link_libraries(${example}_${runtime} PUBLIC YDB-CPP-SDK::Driver YDB-CPP-SDK::Params YDB-CPP-SDK::Query)
    else()
      target_link_libraries(${example}_${runtime} PUBLIC userver::ydb)
    endif()
    add_dependencies(authors_${runtime} ${example}_${runtime})
  endforeach()
endforeach()
target_link_libraries(authors_native PRIVATE batch_native)
target_link_libraries(authors_userver PRIVATE batch_userver)
