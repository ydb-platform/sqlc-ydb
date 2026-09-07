#pragma once

#include <userver/components/component_context.hpp>
#include <userver/server/handlers/http_handler_base.hpp>
#include <userver/utest/using_namespace_userver.hpp>
#include <userver/ydb/component.hpp>

#include <memory>
#include <string>

namespace authors::userver_example {

class SmokeHandler final : public server::handlers::HttpHandlerBase {
public:
    static constexpr std::string_view kName = "handler-authors-smoke";

    SmokeHandler(const components::ComponentConfig& config, const components::ComponentContext& context);

    std::string HandleRequest(
        server::http::HttpRequest& request,
        server::request::RequestContext& context
    ) const override;

private:
    std::shared_ptr<ydb::TableClient> client_;
};

}  // namespace authors::userver_example
