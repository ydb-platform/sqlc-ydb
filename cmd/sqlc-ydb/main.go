package main

import (
	"github.com/ydb-platform/sqlc-ydb/internal/cli"
	"os"
)

func main() { os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr)) }
