#include "smoke_handler.hpp"

#include "queries.hpp"

#include <cstdint>
#include <fstream>
#include <limits>
#include <optional>
#include <sstream>
#include <stdexcept>
#include <string>

namespace authors::userver_example {

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

class CreatedAuthorsTable final {
public:
    explicit CreatedAuthorsTable(ydb::TableClient& client) : client_(client) {}
    ~CreatedAuthorsTable() {
        if (active_) {
            try {
                client_.ExecuteSchemeQuery("DROP TABLE authors;");
            } catch (...) {
            }
        }
    }

    void Drop() {
        client_.ExecuteSchemeQuery("DROP TABLE authors;");
        active_ = false;
    }

private:
    ydb::TableClient& client_;
    bool active_{true};
};

}  // namespace

SmokeHandler::SmokeHandler(const components::ComponentConfig& config, const components::ComponentContext& context)
    : HttpHandlerBase(config, context),
      client_(context.FindComponent<ydb::YdbComponent>().GetTableClient("authors")) {}

std::string SmokeHandler::HandleRequest(server::http::HttpRequest&, server::request::RequestContext&) const {
    client_->ExecuteSchemeQuery(ReadSchema());
    CreatedAuthorsTable created_table{*client_};
    ::authors::userver::Queries queries{*client_};
    constexpr std::uint64_t kMaxId = std::numeric_limits<std::uint64_t>::max();
    queries.UpsertAuthor(
        kMaxId,
        ydb::Utf8{"userver"},
        std::optional<ydb::Utf8>{ydb::Utf8{"present"}}
    );
    queries.UpsertAuthor(kMaxId - 1, ydb::Utf8{"optional null"}, std::nullopt);
    const auto max_author = queries.GetAuthor(kMaxId);
    const auto null_author = queries.GetAuthor(kMaxId - 1);
    const auto missing_author = queries.GetAuthor(kMaxId - 2);
    const auto name = queries.GetAuthorName(kMaxId);
    if (!max_author || max_author->id != kMaxId || !max_author->bio ||
        max_author->bio->GetUnderlying() != "present" || !null_author || null_author->bio || missing_author || !name ||
        name->name.GetUnderlying() != "userver") {
        throw std::runtime_error("userver generated adapter returned unexpected boundary values");
    }
    const auto row_count = queries.ListAuthors().size();
    if (row_count != 2) {
        throw std::runtime_error("userver generated adapter returned an unexpected row count");
    }

    client_->RetryTx("sqlc-generated-helpers", {}, [&](ydb::TxActor& transaction) {
        ::authors::userver::Queries transactional_queries{transaction};
        transactional_queries.UpsertAuthor(kMaxId - 2, ydb::Utf8{"rolled back"}, std::nullopt);
        if (!transactional_queries.GetAuthor(kMaxId - 2)) {
            throw std::runtime_error("userver generated adapter did not share the transaction");
        }
        return ydb::TxAction::kRollback;
    });
    if (queries.GetAuthor(kMaxId - 2)) {
        throw std::runtime_error("userver generated adapter committed a caller-owned transaction");
    }

    queries.DeleteAuthor(kMaxId);
    queries.DeleteAuthor(kMaxId - 1);
    created_table.Drop();
    return "userver generated adapter ok; rows=" + std::to_string(row_count) + "\n";
}

}  // namespace authors::userver_example
