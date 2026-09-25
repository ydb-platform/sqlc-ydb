package analyzer

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	parser "github.com/ydb-platform/yql-parsers/go"
)

type wildcardReplacement struct {
	start, end int
	text       string
}

type wildcardRewrites struct {
	source        string
	replacements  []wildcardReplacement
	embeds        []model.Embedding
	embedCore     *parser.Select_coreContext
	usedEmbeds    map[int]bool
	usedEmbedArgs map[int]bool
}

// Normalize once before any generator so executable SQL fixes the same column
// order used by row models and positional decoders. The final parse supplies
// jOOQ with token positions and resolved bindings for the rewritten SQL.
func analyzeExecutableQuery(catalog model.Catalog, block queryBlock) (model.AnalyzedQuery, []model.Diagnostic) {
	rewrites := wildcardRewrites{source: block.text, usedEmbeds: map[int]bool{}, usedEmbedArgs: map[int]bool{}}
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
	expanded, diagnostics := analyzeQuery(catalog, block)
	if len(diagnostics) == 0 && len(rewrites.embeds) != 0 {
		if len(query.ResultSets) != 1 || len(expanded.ResultSets) != 1 || len(query.ResultSets[0].Columns) != len(expanded.ResultSets[0].Columns) {
			return query, []model.Diagnostic{{Position: query.Source, Message: "sqlc.embed expansion changed the result shape"}}
		}
		for i, column := range query.ResultSets[0].Columns {
			resolved := expanded.ResultSets[0].Columns[i]
			if column.ResultName() != resolved.ResultName() || !column.Type.Equal(resolved.Type) {
				return query, []model.Diagnostic{{Position: query.Source, Message: fmt.Sprintf("sqlc.embed expansion changed result column %d", i+1)}}
			}
		}
		expanded.ResultSets[0] = query.ResultSets[0]
	}
	return expanded, diagnostics
}

func (r *wildcardRewrites) add(token antlr.Token, expressions []string) {
	r.replacements = append(r.replacements, wildcardReplacement{
		start: runeByteOffset(r.source, token.GetStart()),
		end:   runeByteOffset(r.source, token.GetStop()+1),
		text:  strings.Join(expressions, ", "),
	})
}

func (r *wildcardRewrites) addExpression(expr parser.IExprContext, expressions []string) {
	r.replacements = append(r.replacements, wildcardReplacement{
		start: runeByteOffset(r.source, expr.GetStart().GetStart()),
		end:   runeByteOffset(r.source, expr.GetStop().GetStop()+1),
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
