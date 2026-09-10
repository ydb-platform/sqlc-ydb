package analyzer

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	parser "github.com/ydb-platform/yql-parsers/go"
)

const unsupportedSQLCMacroMessage = "sqlc macros are unsupported; use DECLARE parameters and explicit result columns instead"

func unsupportedSQLCMacroDiagnostics(block queryBlock, tokens []antlr.Token) []model.Diagnostic {
	significant := make([]antlr.Token, 0, len(tokens))
	for _, token := range tokens {
		if token.GetChannel() != antlr.TokenDefaultChannel || token.GetTokenType() == antlr.TokenEOF || token.GetTokenType() == parser.YQLLexerCOMMENT {
			continue
		}
		significant = append(significant, token)
	}

	var diagnostics []model.Diagnostic
	for i := 0; i+3 < len(significant); i++ {
		namespace, dot, function, leftParen := significant[i], significant[i+1], significant[i+2], significant[i+3]
		if namespace.GetTokenType() != parser.YQLLexerID_PLAIN || !strings.EqualFold(namespace.GetText(), "sqlc") ||
			dot.GetTokenType() != parser.YQLLexerDOT ||
			function.GetTokenType() != parser.YQLLexerID_PLAIN || !isSQLCMacroName(function.GetText()) ||
			leftParen.GetTokenType() != parser.YQLLexerLPAREN {
			continue
		}
		diagnostics = append(diagnostics, model.Diagnostic{
			Position: model.Position{File: block.file, Line: block.line - 1 + namespace.GetLine(), Column: namespace.GetColumn() + 1},
			Message:  unsupportedSQLCMacroMessage,
		})
	}
	return diagnostics
}

func isSQLCMacroName(name string) bool {
	return strings.EqualFold(name, "arg") || strings.EqualFold(name, "narg") || strings.EqualFold(name, "slice") || strings.EqualFold(name, "embed")
}
