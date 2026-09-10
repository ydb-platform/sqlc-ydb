#include "queries.hpp"

#include <ydb-cpp-sdk/client/driver/driver.h>
#include <ydb-cpp-sdk/client/query/client.h>
#include <ydb-cpp-sdk/client/types/status/status.h>

#include <cstdint>
#include <cstdlib>
#include <exception>
#include <fstream>
#include <iostream>
#include <limits>
#include <optional>
#include <sstream>
#include <stdexcept>
#include <string>
#include <string_view>

namespace {

std::string ReadSchema() {
    std::ifstream input{"schema.sql"};
    if (!input) {
        throw std::runtime_error("open schema.sql from the examples/authors working directory");
    }
    std::ostringstream contents;
    contents << input.rdbuf();
    return contents.str();
}

struct TestDatabase final {
    std::string_view endpoint;
    std::string_view database;
};

constexpr TestDatabase ParseTestDsn(std::string_view value) {
    constexpr std::string_view kPrefix{"grpc://"};
    const auto database_pos = value.find('/', kPrefix.size());
    if (!value.starts_with(kPrefix) || database_pos == std::string::npos || database_pos == kPrefix.size()) {
        throw std::runtime_error("YDB_CONNECTION_STRING must look like grpc://host:port/database");
    }
    return {value.substr(kPrefix.size(), database_pos - kPrefix.size()), value.substr(database_pos)};
}

static_assert(ParseTestDsn("grpc://localhost:2136/local").endpoint == "localhost:2136");
static_assert(ParseTestDsn("grpc://localhost:2136/local").database == "/local");

void ExecuteStatement(NYdb::NQuery::TQueryClient& client, const std::string& statement) {
    const auto status = client.RetryQuerySync([&](NYdb::NQuery::TSession session) -> NYdb::TStatus {
        return session.ExecuteQuery(
            statement,
            NYdb::NQuery::TTxControl::NoTx()
        ).GetValueSync();
    });
    NYdb::NStatusHelpers::ThrowOnError(status);
}

class CreatedAuthorsTable final {
public:
    explicit CreatedAuthorsTable(NYdb::NQuery::TQueryClient& client) : client_(client) {}
    ~CreatedAuthorsTable() {
        if (active_) {
            try {
                ExecuteStatement(client_, "DROP TABLE authors;");
            } catch (...) {
            }
        }
    }

    void Drop() {
        ExecuteStatement(client_, "DROP TABLE authors;");
        active_ = false;
    }

private:
    NYdb::NQuery::TQueryClient& client_;
    bool active_{true};
};

}  // namespace

int main() {
    const char* dsn = std::getenv("YDB_CONNECTION_STRING");
    if (!dsn || !*dsn) {
        std::cerr << "YDB_CONNECTION_STRING is required\n";
        return 2;
    }

    try {
        const auto test_database = ParseTestDsn(dsn);
        NYdb::TDriverConfig driver_config;
        driver_config.SetEndpoint(std::string{test_database.endpoint}).SetDatabase(std::string{test_database.database});
        NYdb::TDriver driver{driver_config};
        NYdb::NQuery::TQueryClient client{driver};
        ExecuteStatement(client, ReadSchema());
        CreatedAuthorsTable created_table{client};
        authors::native::Queries queries{client};

        constexpr std::uint64_t kMaxId = std::numeric_limits<std::uint64_t>::max();
        queries.UpsertAuthor(kMaxId, "C++ SDK", std::optional<std::string>{"present"});
        queries.UpsertAuthor(kMaxId - 1, "optional null", std::nullopt);

        const auto max_author = queries.GetAuthor(kMaxId);
        const auto null_author = queries.GetAuthor(kMaxId - 1);
        const auto missing_author = queries.GetAuthor(kMaxId - 2);
        const auto name = queries.GetAuthorName(kMaxId);
        if (!max_author || max_author->id != kMaxId || max_author->bio != std::optional<std::string>{"present"} ||
            !null_author || null_author->bio || missing_author || !name || name->name != "C++ SDK") {
            throw std::runtime_error("native C++ generated adapter returned unexpected boundary values");
        }
        const auto authors = queries.ListAuthors();
        if (authors.size() != 2) {
            throw std::runtime_error("native C++ generated adapter returned an unexpected row count");
        }
        queries.DeleteAuthor(kMaxId);
        queries.DeleteAuthor(kMaxId - 1);
        created_table.Drop();
        driver.Stop(true);
        std::cout << "native C++ generated adapter ok; rows=" << authors.size() << '\n';
        return 0;
    } catch (const std::exception& error) {
        std::cerr << error.what() << '\n';
        return 1;
    }
}
