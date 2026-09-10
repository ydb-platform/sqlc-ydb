package analyzer

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	parser "github.com/ydb-platform/yql-parsers/go"
)

// UNION reconciles columns by wire name and wildcard projections may expand to
// multiple columns. Keep their SQL intact and let the consumer map result keys.
func resultAliasSpans(cores []*parser.Select_coreContext, tokens []antlr.Token, declarations []*parser.Declare_stmtContext) []model.ResultAlias {
	if len(cores) != 1 {
		return nil
	}
	removed := declarationTokens(tokens, declarations)
	offset := func(index int) int {
		result := index
		for i, token := range tokens {
			if token.GetStart() >= index {
				break
			}
			if removed[i] {
				result -= token.GetStop() - token.GetStart() + 1
			}
		}
		return result
	}
	var spans []model.ResultAlias
	for _, result := range cores[0].AllResult_column() {
		if result.Expr() == nil {
			return nil
		}
		var alias antlr.ParserRuleContext
		if result.An_id_or_type() != nil {
			alias = result.An_id_or_type()
		} else if result.An_id_as_compat() != nil {
			alias = result.An_id_as_compat()
		}
		if alias == nil {
			end := offset(result.Expr().GetStop().GetStop() + 1)
			spans = append(spans, model.ResultAlias{Start: end, End: end})
			continue
		}
		// An explicit alias may be referenced by ORDER BY or another clause.
		// Without rewriting those references, changing it would alter semantics.
		for _, token := range tokens {
			if token.GetStart() > result.GetStop().GetStop() && token.GetChannel() == antlr.TokenDefaultChannel && identifier(token.GetText()) == identifier(alias.GetText()) {
				return nil
			}
		}
		spans = append(spans, model.ResultAlias{Start: offset(alias.GetStart().GetStart()), End: offset(alias.GetStop().GetStop() + 1)})
	}
	return spans
}
