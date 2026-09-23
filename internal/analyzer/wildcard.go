package analyzer

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

type wildcardReplacement struct {
	start, end int
	text       string
}

type wildcardRewrites struct {
	source       string
	replacements []wildcardReplacement
}

// Normalize once before any generator so executable SQL fixes the same column
// order used by row models and positional decoders. The final parse supplies
// jOOQ with token positions and resolved bindings for the rewritten SQL.
func analyzeExecutableQuery(catalog model.Catalog, block queryBlock) (model.AnalyzedQuery, []model.Diagnostic) {
	rewrites := wildcardRewrites{source: block.text}
	block.wildcards = &rewrites
	query, diagnostics := analyzeQuery(catalog, block)
	if len(diagnostics) != 0 || len(rewrites.replacements) == 0 {
		return query, diagnostics
	}
	sql, err := rewrites.apply()
	if err != nil {
		return query, []model.Diagnostic{{Position: query.Source, Message: fmt.Sprintf("cannot expand query wildcards: %v", err)}}
	}
	block.text = sql
	block.parsed = nil
	block.wildcards = nil
	return analyzeQuery(catalog, block)
}

func (r *wildcardRewrites) add(token antlr.Token, expressions []string) {
	r.replacements = append(r.replacements, wildcardReplacement{
		start: runeByteOffset(r.source, token.GetStart()),
		end:   runeByteOffset(r.source, token.GetStop()+1),
		text:  strings.Join(expressions, ", "),
	})
}

func (r *wildcardRewrites) apply() (string, error) {
	slices.SortFunc(r.replacements, func(a, b wildcardReplacement) int { return cmp.Compare(a.start, b.start) })
	var sql strings.Builder
	cursor := 0
	for _, replacement := range r.replacements {
		if replacement.start < cursor || replacement.end > len(r.source) || replacement.start > replacement.end {
			return "", fmt.Errorf("invalid or overlapping wildcard source span")
		}
		sql.WriteString(r.source[cursor:replacement.start])
		sql.WriteString(replacement.text)
		cursor = replacement.end
	}
	sql.WriteString(r.source[cursor:])
	return sql.String(), nil
}

func quotedYQLIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}
