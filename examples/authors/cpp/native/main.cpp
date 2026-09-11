#include "queries.hpp"
#include "../../../batch/cpp/native/queries.hpp"

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

std::string ReadSchema(const char* path = "schema.sql") {
    std::ifstream input{path};
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
    explicit CreatedAuthorsTable(NYdb::NQuery::TQueryClient& client, std::string table = "authors") : client_(client), table_(table) {}
    ~CreatedAuthorsTable() {
        if (active_) {
            try {
                ExecuteStatement(client_, "DROP TABLE " + table_ + ";");
            } catch (...) {
            }
        }
    }

    void Drop() {
        ExecuteStatement(client_, "DROP TABLE " + table_ + ";");
        active_ = false;
    }

private:
    NYdb::NQuery::TQueryClient& client_;
    std::string table_;
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

        const auto created = queries.CreateAuthor(kMaxId - 3, "created", std::optional<std::string>{"returning"});
        const auto created_null = queries.CreateAuthor(kMaxId - 4, "created null", std::nullopt);
        if (!created || created->id != kMaxId - 3 || created->name != "created" || created->bio != "returning" ||
            !created_null || created_null->id != kMaxId - 4 || created_null->name != "created null" || created_null->bio) {
            throw std::runtime_error("native INSERT RETURNING mapping failed");
        }
        queries.DeleteAuthor(kMaxId - 3);
        queries.DeleteAuthor(kMaxId - 4);

        authors::native::Queries reads{client,
            NYdb::NRetry::TRetryOperationSettings().Idempotent(true).MaxRetries(2),
            NYdb::NQuery::TTxSettings::SnapshotRO(),
            NYdb::NQuery::TExecuteQuerySettings().ClientTimeout(TDuration::Seconds(10))};
        const auto max_author = reads.GetAuthor(kMaxId);
        const auto null_author = queries.GetAuthor(kMaxId - 1);
        const auto missing_author = queries.GetAuthor(kMaxId - 2);
        const auto name = queries.GetAuthorName(kMaxId);
        if (!max_author || max_author->id != kMaxId || max_author->bio != std::optional<std::string>{"present"} ||
            !null_author || null_author->bio || missing_author || !name || name->name != "C++ SDK") {
            throw std::runtime_error("native C++ generated adapter returned unexpected boundary values");
        }
        const auto authors = reads.ListAuthors();
        if (authors.size() != 2) {
            throw std::runtime_error("native C++ generated adapter returned an unexpected row count");
        }

        auto session_result = client.GetSession().GetValueSync();
        NYdb::NStatusHelpers::ThrowOnError(session_result);
        auto session = session_result.GetSession();
        auto begin_result = session.BeginTransaction(NYdb::NQuery::TTxSettings::SerializableRW()).GetValueSync();
        NYdb::NStatusHelpers::ThrowOnError(begin_result);
        auto transaction = begin_result.GetTransaction();
        authors::native::Queries transactional_queries{transaction,
            NYdb::NQuery::TExecuteQuerySettings().ClientTimeout(TDuration::Seconds(10))};
        transactional_queries.UpsertAuthor(kMaxId - 2, "rolled back", std::nullopt);
        if (!transactional_queries.GetAuthor(kMaxId - 2)) {
            throw std::runtime_error("native C++ generated adapter did not share the transaction");
        }
        NYdb::NStatusHelpers::ThrowOnError(transaction.Rollback().GetValueSync());
        if (queries.GetAuthor(kMaxId - 2)) {
            throw std::runtime_error("native C++ generated adapter committed a caller-owned transaction");
        }

        queries.DeleteAuthor(kMaxId);
        queries.DeleteAuthor(kMaxId - 1);
        created_table.Drop();

        const auto batch_schema = ReadSchema("../batch/schema.sql");
        const auto books_start = batch_schema.find("CREATE TABLE books");
        if (books_start == std::string::npos) throw std::runtime_error("batch books schema not found");
        ExecuteStatement(client, batch_schema.substr(0, books_start));
        CreatedAuthorsTable batch_authors{client};
        ExecuteStatement(client, batch_schema.substr(books_start));
        CreatedAuthorsTable batch_books{client, "books"};
        batch::native::Queries batch_queries{client};
        const std::string json = R"({"id":18446744073709551615})";
        const auto json_author = batch_queries.CreateAuthor(1, "json", json);
        const auto null_json_author = batch_queries.CreateAuthor(2, "null", std::nullopt);
        const auto timestamp = TInstant::MicroSeconds(1700000000123456);
        const auto book = batch_queries.CreateBook(kMaxId, 1, "isbn", "type", "title", 2026, timestamp, json);
        const auto books = batch_queries.BooksByYear(2026);
        if (!json_author || json_author->biography != json || !null_json_author || null_json_author->biography ||
            !book || book->available != timestamp || book->tags != json || books.size() != 1 || books[0].available != timestamp) {
            throw std::runtime_error("native Json/Timestamp round-trip failed");
        }
        batch_books.Drop();
        batch_authors.Drop();

        driver.Stop(true);
        std::cout << "native C++ generated adapter ok; rows=" << authors.size() << '\n';
        return 0;
    } catch (const std::exception& error) {
        std::cerr << error.what() << '\n';
        return 1;
    }
}
