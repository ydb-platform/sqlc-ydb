package cli

import (
	"fmt"
	"sort"

	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/config"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
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
	if len(raw.Parameters) != 0 {
		options.Parameters = make(map[string]map[string]model.Type, len(raw.Parameters))
	}
	queries := make([]string, 0, len(raw.Parameters))
	for query := range raw.Parameters {
		queries = append(queries, query)
	}
	sort.Strings(queries)
	for _, query := range queries {
		types := make(map[string]model.Type, len(raw.Parameters[query]))
		parameters := make([]string, 0, len(raw.Parameters[query]))
		for name := range raw.Parameters[query] {
			parameters = append(parameters, name)
		}
		sort.Strings(parameters)
		for _, name := range parameters {
			typ, err := analyzer.ParseType(raw.Parameters[query][name])
			if err != nil {
				return options, fmt.Errorf("query %q parameter $%s: %w", query, name, err)
			}
			types[name] = typ
		}
		options.Parameters[query] = types
	}
	return options, nil
}
