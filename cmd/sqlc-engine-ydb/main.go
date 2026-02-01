package main

import (
	"github.com/sqlc-dev/sqlc/pkg/engine"

	"github.com/sqlc-dev/sqlc-engine-ydb/internal/handler"
)

func main() {
	engine.Run(engine.Handler{
		PluginName:        "ydb",
		PluginVersion:     "1.0.0",
		Parse:             handler.Parse,
	})
}
