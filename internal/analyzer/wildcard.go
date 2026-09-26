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
func analyzeExecutableQuery(catalog model.Catalog, block *queryBlock) (model.AnalyzedQuery, []model.Diagnostic) {
	rewrites := wildcardRewrites{source: block.text, usedEmbeds: map[int]bool{}, usedEmbedArgs: map[int]bool{}}
	block.wildcards = &rewrites
	query, diagnostics := analyzeQuery(catalog, *block)
	if len(diagnostics) == 0 {
		for _, parameter := range query.Parameters {
			if argument, exists := block.arguments[parameter.Name]; exists && argument.nullable && block.parameters[parameter.Name].Kind == "" {
				if block.assumed == nil {
					block.assumed = map[string]model.Type{}
				}
				block.assumed[parameter.Name] = parameter.Type
			}
		}
		if len(block.assumed) != 0 {
			rewrites = wildcardRewrites{source: block.text, usedEmbeds: map[int]bool{}, usedEmbedArgs: map[int]bool{}}
			block.wildcards = &rewrites
			query, diagnostics = analyzeQuery(catalog, *block)
		}
	}
	if len(diagnostics) != 0 || len(rewrites.replacements) == 0 {
		return query, diagnostics
	}
	sql, err := rewrites.apply()
	if err != nil {
		return query, []model.Diagnostic{{Position: query.Source, Message: fmt.Sprintf("cannot expand query wildcards: %v", err)}}
	}
	replacements := rewrites.replacements
	if len(rewrites.embeds) != 0 && !hasOrderedColumns(query.Syntax.Root.(parser.ISql_queryContext)) {
		first := strings.IndexByte(sql, '\n')
		if first < 0 {
			return query, []model.Diagnostic{{Position: query.Source, Message: "sqlc.embed query requires a newline after its -- name: annotation"}}
		}
		newline := "\n"
		if first > 0 && sql[first-1] == '\r' {
			newline = "\r\n"
		}
		pragma := "PRAGMA OrderedColumns;" + newline
		sql = sql[:first+1] + pragma + sql[first+1:]
		replacements = append([]wildcardReplacement{{start: first + 1, end: first + 1, text: pragma}}, replacements...)
	}
	if block.sourceMap != nil {
		block.sourceMap = &querySourceMap{original: block.text, rewritten: sql, replacements: replacements, parent: block.sourceMap}
	}
	block.text = sql
	block.parsed = nil
	block.wildcards = nil
	expanded, diagnostics := analyzeQuery(catalog, *block)
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

func hasOrderedColumns(root parser.ISql_queryContext) bool {
	for _, statement := range root.Sql_stmt_list().AllSql_stmt() {
		if pragma := statement.Sql_stmt_core().Pragma_stmt(); pragma != nil && strings.EqualFold(identifier(pragma.An_id().GetText()), "OrderedColumns") {
			return true
		}
	}
	return false
}

func (r *wildcardRewrites) add(token antlr.Token, expressions []string) {
	r.replacements = append(r.replacements, wildcardReplacement{
		start: runeByteOffset(r.source, token.GetStart()),
		end:   runeByteOffset(r.source, token.GetStop()+1),
		text:  strings.Join(expressions, ", "),
	})
}

func (r *wildcardRewrites) removeResultColumn(core *parser.Select_coreContext, ordinal int) {
	results := core.AllResult_column()
	result := results[ordinal]
	start := runeByteOffset(r.source, result.GetStart().GetStart())
	end := runeByteOffset(r.source, result.GetStop().GetStop()+1)
	if ordinal+1 < len(results) {
		end = runeByteOffset(r.source, results[ordinal+1].GetStart().GetStart())
	} else if ordinal > 0 {
		for _, comma := range core.AllCOMMA() {
			if comma.GetSymbol().GetStart() > results[ordinal-1].GetStop().GetStop() && comma.GetSymbol().GetStart() < result.GetStart().GetStart() {
				start = runeByteOffset(r.source, comma.GetSymbol().GetStart())
				break
			}
		}
	}
	r.replacements = append(r.replacements, wildcardReplacement{start: start, end: end})
}

func (r *wildcardRewrites) addExpression(expr parser.IExprContext, expressions []string) {
	r.replacements = append(r.replacements, wildcardReplacement{
		start: runeByteOffset(r.source, expr.GetStart().GetStart()),
		end:   runeByteOffset(r.source, expr.GetStop().GetStop()+1),
		text:  strings.Join(expressions, ", "),
	})
}

func (r *wildcardRewrites) addWithout(core *parser.Select_coreContext) {
	token := core.WITHOUT().GetSymbol()
	list := core.Without_column_list()
	start := runeByteOffset(r.source, token.GetStart())
	results := core.AllResult_column()
	for _, comma := range core.AllCOMMA() {
		if comma.GetSymbol().GetStart() > results[len(results)-1].GetStop().GetStop() && comma.GetSymbol().GetStop() < token.GetStart() {
			start = runeByteOffset(r.source, comma.GetSymbol().GetStart())
			break
		}
	}
	if start == runeByteOffset(r.source, token.GetStart()) && start > 0 && (r.source[start-1] == ' ' || r.source[start-1] == '\t') {
		start--
	}
	r.replacements = append(r.replacements, wildcardReplacement{
		start: start,
		end:   runeByteOffset(r.source, list.GetStop().GetStop()+1),
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
