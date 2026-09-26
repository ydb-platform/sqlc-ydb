package analyzer

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	parser "github.com/ydb-platform/yql-parsers/go"
)

const unsupportedSQLCMacroMessage = "sqlc.slice is unsupported; use a typed List parameter instead"

type sqlcArgument struct {
	nullable bool
	position model.Position
	start    int
}

type sqlcArgumentSourceMap struct {
	original, rewritten string
	replacements        []wildcardReplacement
}

func lowerSQLCArguments(block *queryBlock) []model.Diagnostic {
	input := antlr.NewInputStream(block.text)
	lexer := parser.NewYQLLexer(input)
	tokens := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	tokens.Fill()
	var significant []antlr.Token
	for _, token := range tokens.GetAllTokens() {
		if token.GetChannel() == antlr.TokenDefaultChannel && token.GetTokenType() != antlr.TokenEOF && token.GetTokenType() != parser.YQLLexerCOMMENT {
			significant = append(significant, token)
		}
	}
	var replacements []wildcardReplacement
	var diagnostics []model.Diagnostic
	arguments := map[string]sqlcArgument{}
	for i := 0; i+3 < len(significant); i++ {
		start, dot, function, open := significant[i], significant[i+1], significant[i+2], significant[i+3]
		if start.GetTokenType() != parser.YQLLexerID_PLAIN || !strings.EqualFold(start.GetText(), "sqlc") || dot.GetTokenType() != parser.YQLLexerDOT || function.GetTokenType() != parser.YQLLexerID_PLAIN || open.GetTokenType() != parser.YQLLexerLPAREN {
			continue
		}
		nullable := strings.EqualFold(function.GetText(), "narg")
		if !nullable && !strings.EqualFold(function.GetText(), "arg") {
			continue
		}
		position := model.Position{File: block.file, Line: block.line - 1 + start.GetLine(), Column: start.GetColumn() + 1}
		if i+5 >= len(significant) || significant[i+5].GetTokenType() != parser.YQLLexerRPAREN {
			diagnostics = append(diagnostics, model.Diagnostic{Position: position, Message: fmt.Sprintf("sqlc.%s expects exactly one parameter name", function.GetText())})
			continue
		}
		argument := significant[i+4]
		name, ok := sqlcArgumentName(argument)
		if !ok || name == "" {
			diagnostics = append(diagnostics, model.Diagnostic{Position: position, Message: fmt.Sprintf("sqlc.%s expects a nonempty identifier or quoted string parameter name", function.GetText())})
			continue
		}
		if previous, exists := arguments[name]; exists && previous.nullable != nullable {
			diagnostics = append(diagnostics, model.Diagnostic{Position: position, Message: fmt.Sprintf("parameter %q cannot use both sqlc.arg and sqlc.narg", name)})
			continue
		}
		startByte := runeByteOffset(block.text, start.GetStart())
		arguments[name] = sqlcArgument{nullable: nullable, position: position, start: startByte}
		parameterName := quotedYQLIdentifier(name)
		if argument.GetTokenType() == parser.YQLLexerID_PLAIN {
			parameterName = argument.GetText()
		}
		replacements = append(replacements, wildcardReplacement{start: startByte, end: runeByteOffset(block.text, significant[i+5].GetStop()+1), text: "$" + parameterName})
		i += 5
	}
	if len(diagnostics) != 0 || len(replacements) == 0 {
		return diagnostics
	}
	rewrites := wildcardRewrites{source: block.text, replacements: replacements}
	original := block.text
	block.text, _ = rewrites.apply()
	block.sourceMap = &sqlcArgumentSourceMap{original: original, rewritten: block.text, replacements: replacements}
	for name, argument := range arguments {
		shift := 0
		for _, replacement := range replacements {
			if replacement.start >= argument.start {
				break
			}
			shift += len(replacement.text) - (replacement.end - replacement.start)
		}
		argument.position = positionInSQL(block.file, block.line, block.text, argument.start+shift)
		arguments[name] = argument
	}
	block.arguments = arguments
	return nil
}

func (block queryBlock) originalDiagnostics(diagnostics []model.Diagnostic) []model.Diagnostic {
	if block.sourceMap == nil {
		return diagnostics
	}
	for i := range diagnostics {
		position := diagnostics[i].Position
		if position.File != block.file || position.Line < block.line {
			continue
		}
		line, column, index := block.line, 1, 0
		for index < len(block.sourceMap.rewritten) && (line < position.Line || column < position.Column) {
			r, width := utf8.DecodeRuneInString(block.sourceMap.rewritten[index:])
			index += width
			if r == '\n' {
				line++
				column = 1
			} else {
				column++
			}
		}
		if line != position.Line || column != position.Column {
			continue
		}
		shift := 0
		originalIndex := index
		for _, replacement := range block.sourceMap.replacements {
			start := replacement.start + shift
			end := start + len(replacement.text)
			if index < start {
				break
			}
			if index < end {
				originalIndex = replacement.start
				break
			}
			shift += len(replacement.text) - (replacement.end - replacement.start)
			originalIndex = index - shift
		}
		diagnostics[i].Position = positionInSQL(block.file, block.line, block.sourceMap.original, originalIndex)
	}
	return diagnostics
}

func positionInSQL(file string, startLine int, sql string, index int) model.Position {
	line, column := startLine, 1
	for _, r := range sql[:index] {
		if r == '\n' {
			line++
			column = 1
		} else {
			column++
		}
	}
	return model.Position{File: file, Line: line, Column: column}
}

func originalBlockDiagnostics(blocks []queryBlock, diagnostics []model.Diagnostic) []model.Diagnostic {
	for i := range diagnostics {
		var block *queryBlock
		for j := range blocks {
			if blocks[j].file == diagnostics[i].Position.File && blocks[j].line <= diagnostics[i].Position.Line && (block == nil || block.line < blocks[j].line) {
				block = &blocks[j]
			}
		}
		if block != nil {
			block.originalDiagnostics(diagnostics[i : i+1])
		}
	}
	return diagnostics
}

func sqlcArgumentName(token antlr.Token) (string, bool) {
	switch token.GetTokenType() {
	case parser.YQLLexerID_PLAIN, parser.YQLLexerID_QUOTED:
		return identifier(token.GetText()), true
	case parser.YQLLexerSTRING_VALUE:
		text := token.GetText()
		if len(text) < 2 || text[0] != '\'' || text[len(text)-1] != '\'' || strings.Contains(text[1:len(text)-1], "\\") {
			return "", false
		}
		return strings.ReplaceAll(text[1:len(text)-1], "''", "'"), true
	default:
		return "", false
	}
}

func containsEmbedMacro(block *queryBlock) (bool, []model.Diagnostic) {
	parsed, diagnostics := parseYQL(block.file, block.text, block.line-1)
	if len(diagnostics) != 0 {
		return false, diagnostics
	}
	block.parsed = &parsed
	for _, call := range sqlcMacroCalls(parsed.tokens) {
		if strings.EqualFold(call.name, "embed") {
			return true, nil
		}
	}
	return false, nil
}

func unsupportedSQLCMacroDiagnostics(block queryBlock, tokens []antlr.Token) []model.Diagnostic {
	var diagnostics []model.Diagnostic
	for _, call := range sqlcMacroCalls(tokens) {
		if strings.EqualFold(call.name, "embed") {
			continue
		}
		diagnostics = append(diagnostics, model.Diagnostic{
			Position: model.Position{File: block.file, Line: block.line - 1 + call.token.GetLine(), Column: call.token.GetColumn() + 1},
			Message:  unsupportedSQLCMacroMessage,
		})
	}
	return diagnostics
}

type sqlcMacroCall struct {
	name  string
	token antlr.Token
}

func sqlcMacroCalls(tokens []antlr.Token) []sqlcMacroCall {
	significant := make([]antlr.Token, 0, len(tokens))
	for _, token := range tokens {
		if token.GetChannel() != antlr.TokenDefaultChannel || token.GetTokenType() == antlr.TokenEOF || token.GetTokenType() == parser.YQLLexerCOMMENT {
			continue
		}
		significant = append(significant, token)
	}

	var calls []sqlcMacroCall
	for i := 0; i+3 < len(significant); i++ {
		namespace, dot, function, leftParen := significant[i], significant[i+1], significant[i+2], significant[i+3]
		if namespace.GetTokenType() != parser.YQLLexerID_PLAIN || !strings.EqualFold(namespace.GetText(), "sqlc") ||
			dot.GetTokenType() != parser.YQLLexerDOT ||
			function.GetTokenType() != parser.YQLLexerID_PLAIN || !isSQLCMacroName(function.GetText()) ||
			leftParen.GetTokenType() != parser.YQLLexerLPAREN {
			continue
		}
		calls = append(calls, sqlcMacroCall{name: function.GetText(), token: namespace})
	}
	return calls
}

func isSQLCMacroName(name string) bool {
	return strings.EqualFold(name, "arg") || strings.EqualFold(name, "narg") || strings.EqualFold(name, "slice") || strings.EqualFold(name, "embed")
}
