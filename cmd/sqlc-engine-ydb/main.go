// Command sqlc-engine-ydb is a database **engine plugin** for sqlc: it implements
// engine.EngineService / Parse from sqlc’s engine.proto (see engine-plugin
// protos/engine/engine.proto — service EngineService { rpc Parse ... }).
//
// sqlc invokes this binary with the same subprocess contract as codegen plugins:
// argv ends with the full gRPC method name /engine.EngineService/Parse, request
// protobuf on stdin, response on stdout. The Go SDK engine.Run dispatches Parse
// for that method (and still accepts the legacy argv "parse" for manual runs).
//
// YDB-specific parsing lives in internal/handler.
package main

import (
	"github.com/sqlc-dev/sqlc/pkg/engine"

	"github.com/sqlc-dev/sqlc-engine-ydb/internal/handler"
)

func main() {
	engine.Run(engine.Handler{
		PluginName:    "ydb",
		PluginVersion: "1.0.0",
		Parse:         handler.Parse,
	})
}
