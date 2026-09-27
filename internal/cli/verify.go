package cli

import (
	"fmt"

	"github.com/ydb-platform/sqlc-ydb/internal/config"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	"github.com/ydb-platform/sqlc-ydb/internal/source"
)

func verify(proposed *config.Config, against string, noDatabase bool) error {
	released, err := config.Load(against)
	if err != nil {
		return fmt.Errorf("released configuration: %w", err)
	}
	matches := make([]int, len(released.SQL))
	seen := map[string]bool{}
	for i, old := range released.SQL {
		if old.Name != "" {
			if seen[old.Name] {
				return fmt.Errorf("released query set name %q is repeated", old.Name)
			}
			seen[old.Name] = true
		}
		j, err := proposedSet(proposed.SQL, old.Name, i)
		if err != nil {
			return fmt.Errorf("released sql[%d]: %w", i, err)
		}
		if len(old.Schema) == 0 || len(proposed.SQL[j].Schema) == 0 {
			return fmt.Errorf("released sql[%d] and proposed sql[%d] require local schema inputs for verify", i, j)
		}
		matches[i] = j
	}
	if _, err := prepare(proposed, false, noDatabase); err != nil {
		return fmt.Errorf("proposed configuration: %w", err)
	}
	for i, old := range released.SQL {
		j := matches[i]
		next := proposed.SQL[j]
		oldSchema, err := source.Read(released.Dir, old.Schema, true)
		if err != nil {
			return fmt.Errorf("released sql[%d] schema: %w", i, err)
		}
		oldQueries, err := source.Read(released.Dir, old.Queries, false)
		if err != nil {
			return fmt.Errorf("released sql[%d] queries: %w", i, err)
		}
		original, err := analyzeSources(released.Dir, old, oldSchema, oldQueries, true)
		if err != nil {
			return fmt.Errorf("released sql[%d]: %w", i, err)
		}
		newSchema, err := source.Read(proposed.Dir, next.Schema, true)
		if err != nil {
			return fmt.Errorf("proposed sql[%d] schema: %w", j, err)
		}
		executable := make([]model.Source, len(original.Queries))
		for k, q := range original.Queries {
			executable[k] = model.Source{Name: q.Source.File, Text: q.SQL}
		}
		next.Analyzer = old.Analyzer
		checked, err := analyzeSources(proposed.Dir, next, newSchema, executable, noDatabase)
		if err != nil {
			return fmt.Errorf("released sql[%d] against proposed sql[%d]: %w", i, j, err)
		}
		if len(checked.Queries) != len(original.Queries) {
			return fmt.Errorf("released sql[%d]: expected %d checked queries, got %d", i, len(original.Queries), len(checked.Queries))
		}
		for k, q := range original.Queries {
			if err := verifyQuery(q, checked.Queries[k]); err != nil {
				return fmt.Errorf("released sql[%d] query %s: %w", i, q.Name, err)
			}
		}
	}
	return nil
}

func proposedSet(sets []config.SQL, name string, index int) (int, error) {
	if name == "" {
		if index < len(sets) && sets[index].Name == "" {
			return index, nil
		}
		return 0, fmt.Errorf("unnamed query set has no unnamed proposed sql[%d]", index)
	}
	match := -1
	for i, set := range sets {
		if set.Name != name {
			continue
		}
		if match >= 0 {
			return 0, fmt.Errorf("multiple proposed query sets named %q", name)
		}
		match = i
	}
	if match < 0 {
		return 0, fmt.Errorf("no proposed query set named %q", name)
	}
	return match, nil
}

func verifyQuery(old, next model.AnalyzedQuery) error {
	if old.Name != next.Name || old.Command != next.Command {
		return fmt.Errorf("query identity changed from %s %s to %s %s", old.Name, old.Command, next.Name, next.Command)
	}
	if len(old.Parameters) != len(next.Parameters) {
		return fmt.Errorf("parameter count changed from %d to %d", len(old.Parameters), len(next.Parameters))
	}
	params := make(map[string]model.Type, len(next.Parameters))
	for _, p := range next.Parameters {
		params[p.Name] = p.Type
	}
	for _, p := range old.Parameters {
		typ, ok := params[p.Name]
		if !ok {
			return fmt.Errorf("parameter $%s is missing", p.Name)
		}
		if !p.Type.Equal(typ) {
			return fmt.Errorf("parameter $%s changed type from %s to %s", p.Name, p.Type, typ)
		}
	}
	if len(old.ResultSets) != len(next.ResultSets) {
		return fmt.Errorf("result-set count changed from %d to %d", len(old.ResultSets), len(next.ResultSets))
	}
	for i, result := range old.ResultSets {
		updated := next.ResultSets[i]
		if result.Name != updated.Name || len(result.Columns) != len(updated.Columns) {
			return fmt.Errorf("result set %d changed shape", i+1)
		}
		for j, col := range result.Columns {
			other := updated.Columns[j]
			if col.ResultName() != other.ResultName() || !col.Type.Equal(other.Type) {
				return fmt.Errorf("result set %d column %d changed from %s %s to %s %s", i+1, j+1, col.ResultName(), col.Type, other.ResultName(), other.Type)
			}
		}
	}
	return nil
}
