package cli

import (
	"fmt"

	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/config"
	"github.com/ydb-platform/sqlc-ydb/internal/yql/builtins"
)

func functionOptions(raw config.Analyzer) (analyzer.Options, error) {
	var options analyzer.Options
	for _, function := range raw.Functions {
		result, err := analyzer.ParseType(function.Returns)
		if err != nil {
			return options, fmt.Errorf("function %q return type: %w", function.Name, err)
		}
		signature := builtins.Signature{Name: function.Name, Returns: result}
		for i, argument := range function.Args {
			typ, err := analyzer.ParseType(argument.Type)
			if err != nil {
				return options, fmt.Errorf("function %q argument %d: %w", function.Name, i+1, err)
			}
			signature.Arguments = append(signature.Arguments, builtins.Parameter{Name: argument.Name, Type: typ, Optional: argument.Optional, AutoMap: argument.AutoMap})
		}
		options.Functions = append(options.Functions, signature)
	}
	return options, nil
}
