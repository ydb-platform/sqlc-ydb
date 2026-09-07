add_executable(authors_native main.cpp models.hpp queries.hpp queries.cpp)
target_link_libraries(
  authors_native
  PRIVATE
    YDB-CPP-SDK::Driver
    YDB-CPP-SDK::Params
    YDB-CPP-SDK::Query
)
