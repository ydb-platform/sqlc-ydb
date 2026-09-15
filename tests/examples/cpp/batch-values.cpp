#include "../../../examples/batch/cpp/native/queries.cpp"
#include "../../../examples/batch/cpp/userver/models.hpp"

#include <userver/formats/json/serialize.hpp>
#include <userver/ydb/io/supported_types.hpp>

#include <limits>
#include <stdexcept>

namespace {
void Check(const NYdb::TValue& value, std::size_t expected_rows) {
    NYdb::TTypeParser type(value.GetType());
    type.OpenList();
    type.OpenStruct();
    std::size_t fields = 0;
    while (type.TryNextMember()) ++fields;
    if (fields != 8) throw std::runtime_error("List<Struct> lost member types");
    type.CloseStruct();
    type.CloseList();

    NYdb::TValueParser parser(value);
    parser.OpenList();
    std::size_t rows = 0;
    while (parser.TryNextListItem()) {
        parser.OpenStruct();
        while (parser.TryNextMember()) {
            const auto& name = parser.GetMemberName();
            if (name == "book_id" && parser.GetUint64() != std::numeric_limits<std::uint64_t>::max()) {
                throw std::runtime_error("Uint64 binding lost precision");
            }
            if (name == "available" && parser.GetTimestamp() != TInstant::MicroSeconds(1700000000123456)) {
                throw std::runtime_error("Timestamp binding lost precision");
            }
            if (name == "tags" && parser.GetJson() != "{\"batch\":true}") {
                throw std::runtime_error("JSON binding changed content");
            }
        }
        parser.CloseStruct();
        ++rows;
    }
    parser.CloseList();
    if (rows != expected_rows) throw std::runtime_error("List<Struct> row count mismatch");
}
}

int main() {
    const auto id = std::numeric_limits<std::uint64_t>::max();
    const auto timestamp = TInstant::MicroSeconds(1700000000123456);
    const batch::native::CreateBooksBooksItem native{id, 1, "isbn", "type", "title", 2026, timestamp, "{\"batch\":true}"};
    Check(batch::native::sqlc_bind_CreateBooksBooksItem({}), 0);
    Check(batch::native::sqlc_bind_CreateBooksBooksItem({native, native}), 2);
    const batch::userver::CreateBooksBooksItem userver{
        id, 1, ::userver::ydb::Utf8{"isbn"}, ::userver::ydb::Utf8{"type"}, ::userver::ydb::Utf8{"title"}, 2026,
        std::chrono::system_clock::time_point{std::chrono::microseconds{1700000000123456}},
        ::userver::formats::json::FromString("{\"batch\":true}")};
    for (const auto& rows : {std::vector<batch::userver::CreateBooksBooksItem>{}, std::vector{userver, userver}}) {
        NYdb::TValueBuilder builder;
        ::userver::ydb::Write(builder, rows);
        Check(builder.Build(), rows.size());
    }
}
