package analyzer

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	parser "github.com/ydb-platform/yql-parsers/go"
)

// Remove only syntax tokens: comments, whitespace, local bindings and literals
// remain untouched. ANTLR source indices address runes, not UTF-8 bytes.
func withoutDeclarations(sql string, tokens []antlr.Token, declarations []*parser.Declare_stmtContext) string {
	if len(declarations) == 0 {
		return sql
	}
	removed := declarationTokens(tokens, declarations)
	source := []rune(sql)
	var out strings.Builder
	last := 0
	for i, token := range tokens {
		if removed[i] {
			out.WriteString(string(source[last:token.GetStart()]))
			last = token.GetStop() + 1
		}
	}
	out.WriteString(string(source[last:]))
	return out.String()
}

func declarationTokens(tokens []antlr.Token, declarations []*parser.Declare_stmtContext) map[int]bool {
	removed := make(map[int]bool)
	for _, declaration := range declarations {
		start, stop := declaration.GetStart().GetTokenIndex(), declaration.GetStop().GetTokenIndex()
		for i := start; i <= stop; i++ {
			if tokens[i].GetChannel() == antlr.TokenDefaultChannel {
				removed[i] = true
			}
		}
		for i := stop + 1; i < len(tokens); i++ {
			if tokens[i].GetChannel() != antlr.TokenDefaultChannel {
				continue
			}
			if tokens[i].GetText() == ";" {
				removed[i] = true
			}
			break
		}
	}
	return removed
}
