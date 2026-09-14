// Package jdbc renders positional SQL for the Java and Kotlin JDBC generators.
package jdbc

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	yql "github.com/ydb-platform/yql-parsers/go"
)

// SQL replaces only parameter tokens, preserving quoted text and local variables.
// Bindings follow occurrences, including repeated uses of the same parameter.
func SQL(q model.AnalyzedQuery) (string, []int) {
	text := q.SQL
	text = model.WithoutQueryAnnotation(text)
	if HasDeclarations(q) {
		var declarations strings.Builder
		for _, p := range q.Parameters {
			if !q.IsDeclaredParameter(p.Name) {
				declarations.WriteString("DECLARE $" + declarationName(p.Name) + " AS " + p.Type.String() + ";\n")
			}
		}
		return declarations.String() + text, nil
	}
	parameters := map[string]int{}
	for i, p := range q.Parameters {
		parameters[p.Name] = i
	}
	lexer := yql.NewYQLLexer(antlr.NewInputStream(text))
	lexer.RemoveErrorListeners()
	var tokens []antlr.Token
	for t := lexer.NextToken(); t.GetTokenType() != antlr.TokenEOF; t = lexer.NextToken() {
		if t.GetChannel() == antlr.TokenDefaultChannel {
			tokens = append(tokens, t)
		}
	}
	runes := []rune(text)
	var b strings.Builder
	var bindings []int
	cursor := 0
	for i, t := range tokens {
		if t.GetTokenType() != yql.YQLLexerDOLLAR || i+1 == len(tokens) {
			continue
		}
		next := tokens[i+1]
		parameter, ok := parameters[strings.Trim(next.GetText(), "`")]
		if !ok {
			continue
		}
		b.WriteString(string(runes[cursor:t.GetStart()]))
		b.WriteByte('?')
		cursor = next.GetStop() + 1
		bindings = append(bindings, parameter)
	}
	b.WriteString(string(runes[cursor:]))
	return b.String(), bindings
}

// HasDeclarations selects named driver binding without rewriting source declarations.
func HasDeclarations(q model.AnalyzedQuery) bool { return len(q.DeclaredParameters) != 0 }

func declarationName(name string) string {
	for i, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (i > 0 && r >= '0' && r <= '9')) {
			return "`" + strings.ReplaceAll(name, "`", "``") + "`"
		}
	}
	return name
}
