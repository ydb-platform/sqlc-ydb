#include "smoke_handler.hpp"

#include <userver/components/minimal_server_component_list.hpp>
#include <userver/storages/secdist/component.hpp>
#include <userver/storages/secdist/provider_component.hpp>
#include <userver/utils/daemon_run.hpp>
#include <userver/ydb/component.hpp>

int main(int argc, char* argv[]) {
    auto components =
        components::MinimalServerComponentList()
            .Append<components::DefaultSecdistProvider>()
            .Append<components::Secdist>()
            .Append<authors::userver_example::SmokeHandler>()
            .Append<ydb::YdbComponent>();
    return utils::DaemonMain(argc, argv, components);
}
