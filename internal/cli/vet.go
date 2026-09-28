package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/cel-go/cel"
	celast "github.com/google/cel-go/common/ast"
	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/config"
	"github.com/ydb-platform/sqlc-ydb/internal/database"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	"github.com/ydb-platform/sqlc-ydb/internal/source"
)

type vetRule struct {
	config.Rule
	program   cel.Program
	needsPlan bool
}

func vet(c *config.Config, noDatabase bool) error {
	env, err := cel.NewEnv(
		cel.Variable("config", cel.DynType),
		cel.Variable("query", cel.DynType),
		cel.Variable("ydb", cel.DynType),
	)
	if err != nil {
		return err
	}
	rules := make(map[string]vetRule, len(c.Rules))
	for _, rule := range c.Rules {
		ast, issues := env.Compile(rule.Rule)
		if err := issues.Err(); err != nil {
			return fmt.Errorf("rule %q: invalid CEL expression: %w", rule.Name, err)
		}
		if !ast.OutputType().IsExactType(cel.BoolType) {
			return fmt.Errorf("rule %q: CEL expression must return bool, got %s", rule.Name, ast.OutputType())
		}
		program, err := env.Program(ast, cel.CostLimit(100000))
		if err != nil {
			return fmt.Errorf("rule %q: %w", rule.Name, err)
		}
		needsPlan := false
		celast.PreOrderVisit(ast.NativeRep().Expr(), celast.NewExprVisitor(func(expr celast.Expr) {
			if expr.Kind() == celast.IdentKind && expr.AsIdent() == "ydb" {
				needsPlan = true
			}
		}))
		rules[rule.Name] = vetRule{Rule: rule, program: program, needsPlan: needsPlan}
	}
	var failures []error
	withFailures := func(err error) error { return errors.Join(append(failures, err)...) }
	for i, set := range c.SQL {
		connected := !noDatabase && set.DatabaseEnabled()
		for _, name := range set.Rules {
			if name == "sqlc/db-prepare" && !connected {
				return withFailures(fmt.Errorf("sql[%d].rules: sqlc/db-prepare requires database.uri and connected analysis", i))
			}
			if rules[name].needsPlan && !connected {
				return withFailures(fmt.Errorf("sql[%d].rules: rule %q requires database.uri and connected analysis for YDB plan checks", i, name))
			}
		}
		if len(set.Schema) == 0 && !connected {
			return withFailures(fmt.Errorf("sql[%d]: schema is required when database-assisted analysis is disabled", i))
		}
		var schemas []model.Source
		if len(set.Schema) != 0 {
			schemas, err = source.Read(c.Dir, set.Schema, true)
			if err != nil {
				return withFailures(fmt.Errorf("sql[%d] schema: %w", i, err))
			}
		}
		queries, err := source.Read(c.Dir, set.Queries, false)
		if err != nil {
			return withFailures(fmt.Errorf("sql[%d] queries: %w", i, err))
		}
		result, err := analyzeSources(c.Dir, set, schemas, queries, noDatabase)
		if err != nil {
			return withFailures(fmt.Errorf("sql[%d]: %w", i, err))
		}
		var client *database.Client
		if connected && hasPlanVetRules(set.Rules, rules) {
			settings, err := set.Database.Resolve(c.Dir)
			if err != nil {
				return withFailures(fmt.Errorf("sql[%d]: %w", i, err))
			}
			client, err = database.New(settings)
			if err != nil {
				return withFailures(fmt.Errorf("sql[%d]: connect for vet: %w", i, err))
			}
		}
		failuresForSet, checkErr := vetQueries(c, set, result.Queries, rules, client)
		if client != nil {
			checkErr = errors.Join(checkErr, client.Close())
		}
		failures = append(failures, failuresForSet...)
		if checkErr != nil {
			return withFailures(fmt.Errorf("sql[%d]: %w", i, checkErr))
		}
	}
	return errors.Join(failures...)
}

func hasPlanVetRules(names []string, rules map[string]vetRule) bool {
	for _, name := range names {
		if rules[name].needsPlan {
			return true
		}
	}
	return false
}

func vetQueries(c *config.Config, set config.SQL, queries []model.AnalyzedQuery, rules map[string]vetRule, client *database.Client) ([]error, error) {
	var failures []error
	for _, query := range queries {
		activation := map[string]any{
			"config": map[string]any{"version": c.Version, "engine": set.Engine, "schema": []string(set.Schema), "queries": []string(set.Queries)},
			"query":  vetQuery(query),
			"ydb":    map[string]any{},
		}
		if client != nil {
			plan, err := client.ExplainQuery(context.Background(), analyzer.ValidationSQL(query))
			if err != nil {
				return failures, fmt.Errorf("query %s: %w", query.Name, err)
			}
			operations, document, err := vetPlan(plan)
			if err != nil {
				return failures, fmt.Errorf("query %s: %w", query.Name, err)
			}
			activation["ydb"] = map[string]any{"plan": map[string]any{"operations": operations, "json": plan}, "explain": document}
		}
		for _, name := range set.Rules {
			if name == "sqlc/db-prepare" {
				continue
			}
			rule := rules[name]
			value, _, err := rule.program.Eval(activation)
			if err != nil {
				return failures, fmt.Errorf("rule %q on query %s: %w", name, query.Name, err)
			}
			matched, ok := value.Value().(bool)
			if !ok {
				return failures, fmt.Errorf("rule %q on query %s returned %T instead of bool", name, query.Name, value.Value())
			}
			if matched {
				message := rule.Message
				if message == "" {
					message = "rule matched"
				}
				failures = append(failures, model.Diagnostic{Position: query.Source, Message: fmt.Sprintf("query %s: vet rule %s: %s", query.Name, name, message)})
			}
		}
	}
	return failures, nil
}

func vetQuery(query model.AnalyzedQuery) map[string]any {
	params := make([]map[string]any, len(query.Parameters))
	for i, param := range query.Parameters {
		params[i] = map[string]any{"number": int64(i + 1), "name": param.Name, "type": param.Type.String()}
	}
	return map[string]any{"sql": query.SQL, "name": query.Name, "cmd": strings.TrimPrefix(string(query.Command), ":"), "params": params}
}

func vetPlan(raw string) ([]string, map[string]any, error) {
	var document map[string]any
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		return nil, nil, fmt.Errorf("decode YDB query plan: %w", err)
	}
	plan, ok := document["Plan"].(map[string]any)
	if !ok {
		return nil, nil, errors.New("YDB query plan has no Plan object")
	}
	operations := make([]string, 0)
	seen := map[string]bool{}
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			operations = append(operations, name)
		}
	}
	var visit func(map[string]any) error
	visit = func(node map[string]any) error {
		if name, ok := node["Node Type"].(string); ok {
			add(name)
		}
		if values, exists := node["Operators"]; exists {
			operators, ok := values.([]any)
			if !ok {
				return errors.New("YDB query plan Operators must be a list")
			}
			for _, value := range operators {
				operator, ok := value.(map[string]any)
				if !ok {
					return errors.New("YDB query plan operator must be an object")
				}
				if name, ok := operator["Name"].(string); ok {
					add(name)
				}
			}
		}
		if values, exists := node["Plans"]; exists {
			children, ok := values.([]any)
			if !ok {
				return errors.New("YDB query plan Plans must be a list")
			}
			for _, value := range children {
				child, ok := value.(map[string]any)
				if !ok {
					return errors.New("YDB query plan child must be an object")
				}
				if err := visit(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(plan); err != nil {
		return nil, nil, err
	}
	return operations, document, nil
}
