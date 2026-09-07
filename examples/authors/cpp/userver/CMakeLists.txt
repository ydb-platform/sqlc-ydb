add_executable(authors_userver main.cpp smoke_handler.cpp models.hpp queries.hpp queries.cpp)
target_link_libraries(authors_userver PRIVATE userver::ydb)
