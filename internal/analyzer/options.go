package analyzer

import (
	"fmt"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
	"github.com/ydb-platform/sqlc-ydb/internal/yql/builtins"
)

// Options supplies additional offline type contracts to one compilation unit.
type Options struct {
	Functions []builtins.Signature
}

// ParseType reads a YQL type using the same grammar and normalization as DECLARE.
// Only a single type is accepted; query text cannot be injected through options.
func ParseType(text string) (model.Type, error) {
	parsed, diagnostics := parseYQL("function signature", "DECLARE $signature_type AS "+text+";", 0)
	if len(diagnostics) != 0 {
		return model.Type{}, fmt.Errorf("invalid YQL type %q: %s", text, diagnostics[0].Message)
	}
	tree := collectQueryTree(parsed.tree)
	if len(tree.statements) != 1 || len(tree.declares) != 1 {
		return model.Type{}, fmt.Errorf("expected one YQL type, got %q", text)
	}
	return parseType(tree.declares[0].Type_name().GetText())
}
