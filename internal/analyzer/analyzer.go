// Package analyzer parses YQL and resolves schema, query parameters, and results.
package analyzer

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-engine-ydb/internal/model"
	parser "github.com/ydb-platform/yql-parsers/go"
)

// Analyze builds the schema catalog first and then semantically analyzes every
// named query against that catalog.
func Analyze(schema, queries []model.Source) (*model.AnalysisResult, error) {
	result := &model.AnalysisResult{}
	catalog, diagnostics := buildCatalog(schema)
	result.Catalog = catalog
	result.Diagnostics = append(result.Diagnostics, diagnostics...)
	if len(diagnostics) == 0 {
		queryNames := map[string]model.Position{}
		for _, source := range queries {
			blocks, blockDiagnostics := queryBlocks(source)
			result.Diagnostics = append(result.Diagnostics, blockDiagnostics...)
			for _, block := range blocks {
				key := strings.ToLower(block.name)
				if first, exists := queryNames[key]; exists {
					result.Diagnostics = append(result.Diagnostics, model.Diagnostic{
						Position: model.Position{File: block.file, Line: block.line, Column: 1},
						Message:  fmt.Sprintf("query %q is declared more than once (first declaration at %s:%d:%d)", block.name, first.File, first.Line, first.Column),
					})
					continue
				}
				queryNames[key] = model.Position{File: block.file, Line: block.line, Column: 1}
				query, queryDiagnostics := analyzeQuery(catalog, block)
				result.Diagnostics = append(result.Diagnostics, queryDiagnostics...)
				if len(queryDiagnostics) == 0 {
					result.Queries = append(result.Queries, query)
				}
			}
		}
	}
	result.Diagnostics = uniqueDiagnostics(result.Diagnostics)
	if len(result.Diagnostics) != 0 {
		errList := make([]error, len(result.Diagnostics))
		for i := range result.Diagnostics {
			errList[i] = result.Diagnostics[i]
		}
		return result, errors.Join(errList...)
	}
	return result, nil
}

func uniqueDiagnostics(diagnostics []model.Diagnostic) []model.Diagnostic {
	seen := map[string]bool{}
	out := make([]model.Diagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		key := fmt.Sprintf("%s\x00%d\x00%d\x00%s", diagnostic.Position.File, diagnostic.Position.Line, diagnostic.Position.Column, diagnostic.Message)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, diagnostic)
	}
	return out
}

type parsedYQL struct {
	tree parser.ISql_queryContext
}

type syntaxErrorListener struct {
	*antlr.DefaultErrorListener
	file        string
	lineOffset  int
	diagnostics []model.Diagnostic
}

func (l *syntaxErrorListener) SyntaxError(_ antlr.Recognizer, _ interface{}, line, column int, message string, _ antlr.RecognitionException) {
	l.diagnostics = append(l.diagnostics, model.Diagnostic{
		Position: model.Position{File: l.file, Line: l.lineOffset + line, Column: column + 1},
		Message:  message,
	})
}

func parseYQL(file, text string, lineOffset int) (parsedYQL, []model.Diagnostic) {
	input := antlr.NewInputStream(text)
	lexer := parser.NewYQLLexer(input)
	lexer.RemoveErrorListeners()
	listener := &syntaxErrorListener{DefaultErrorListener: antlr.NewDefaultErrorListener(), file: file, lineOffset: lineOffset}
	lexer.AddErrorListener(listener)
	tokens := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	p := parser.NewYQLParser(tokens)
	p.RemoveErrorListeners()
	p.AddErrorListener(listener)
	tree := p.Sql_query()
	tokens.Fill()
	for _, token := range tokens.GetAllTokens() {
		if token.GetTokenType() == parser.YQLLexerID_QUOTED && strings.Contains(token.GetText(), `\`) {
			listener.diagnostics = append(listener.diagnostics, model.Diagnostic{
				Position: model.Position{File: file, Line: lineOffset + token.GetLine(), Column: token.GetColumn() + 1},
				Message:  "backslash escapes in quoted identifiers are unsupported",
			})
		}
	}
	return parsedYQL{tree: tree}, listener.diagnostics
}

type queryBlock struct {
	name    string
	command model.Command
	file    string
	line    int
	text    string
}

func queryBlocks(source model.Source) ([]queryBlock, []model.Diagnostic) {
	input := antlr.NewInputStream(source.Text)
	lexer := parser.NewYQLLexer(input)
	lexer.RemoveErrorListeners()
	listener := &syntaxErrorListener{DefaultErrorListener: antlr.NewDefaultErrorListener(), file: source.Name}
	lexer.AddErrorListener(listener)
	tokens := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	tokens.Fill()
	type annotation struct {
		name    string
		command model.Command
		start   int
		line    int
	}
	var annotations []annotation
	var diagnostics []model.Diagnostic
	for _, token := range tokens.GetAllTokens() {
		if token.GetTokenType() != parser.YQLLexerCOMMENT || !strings.HasPrefix(strings.TrimSpace(token.GetText()), "--") {
			continue
		}
		name, command, ok, message := parseAnnotation(token.GetText())
		if message != "" {
			diagnostics = append(diagnostics, model.Diagnostic{Position: model.Position{File: source.Name, Line: token.GetLine(), Column: token.GetColumn() + 1}, Message: message})
		}
		if ok {
			annotations = append(annotations, annotation{name: name, command: command, start: runeByteOffset(source.Text, token.GetStart()), line: token.GetLine()})
		}
	}
	diagnostics = append(diagnostics, listener.diagnostics...)
	if len(annotations) == 0 && strings.TrimSpace(source.Text) != "" {
		diagnostics = append(diagnostics, model.Diagnostic{Position: model.Position{File: source.Name, Line: 1, Column: 1}, Message: "query file contains SQL before any -- name: annotation"})
	}
	if len(annotations) != 0 && strings.TrimSpace(source.Text[:annotations[0].start]) != "" {
		diagnostics = append(diagnostics, model.Diagnostic{Position: model.Position{File: source.Name, Line: 1, Column: 1}, Message: "query file preamble before the first -- name: annotation is unsupported; move declarations into each named query"})
	}
	blocks := make([]queryBlock, 0, len(annotations))
	for i, item := range annotations {
		end := len(source.Text)
		if i+1 < len(annotations) {
			end = annotations[i+1].start
		}
		blocks = append(blocks, queryBlock{name: item.name, command: item.command, file: source.Name, line: item.line, text: strings.TrimSpace(source.Text[item.start:end])})
	}
	return blocks, diagnostics
}

func runeByteOffset(text string, runeIndex int) int {
	if runeIndex <= 0 {
		return 0
	}
	count := 0
	for index := range text {
		if count == runeIndex {
			return index
		}
		count++
	}
	return len(text)
}

func parseAnnotation(line string) (string, model.Command, bool, string) {
	text := strings.TrimSpace(line)
	if !strings.HasPrefix(text, "--") {
		return "", "", false, ""
	}
	text = strings.TrimSpace(strings.TrimPrefix(text, "--"))
	if strings.HasPrefix(strings.ToLower(text), "sqlc") {
		text = strings.TrimSpace(text[len("sqlc"):])
		text = strings.TrimSpace(strings.TrimPrefix(text, "--"))
	}
	if !strings.HasPrefix(strings.ToLower(text), "name:") {
		return "", "", false, ""
	}
	fields := strings.Fields(strings.TrimSpace(text[len("name:"):]))
	if len(fields) != 2 || fields[0] == "" {
		return "", "", false, "invalid query annotation; expected -- name: QueryName :one|:many|:exec|:execrows"
	}
	command := model.Command(fields[1])
	switch command {
	case model.One, model.Many, model.Exec, model.ExecRows:
		return fields[0], command, true, ""
	default:
		return "", "", false, fmt.Sprintf("unsupported query command %q", fields[1])
	}
}

func identifier(text string) string {
	if len(text) >= 2 {
		switch text[0] {
		case '`':
			if text[len(text)-1] == '`' {
				return strings.ReplaceAll(text[1:len(text)-1], "``", "`")
			}
		case '"':
			if text[len(text)-1] == '"' {
				if value, err := strconv.Unquote(text); err == nil {
					return value
				}
			}
		}
	}
	return text
}

func bindName(ctx parser.IBind_parameterContext) string {
	if ctx == nil {
		return ""
	}
	return identifier(strings.TrimPrefix(ctx.GetText(), "$"))
}

func diagnosticAt(file string, lineOffset int, ctx antlr.ParserRuleContext, message string) model.Diagnostic {
	line, column := 1, 1
	if ctx != nil && ctx.GetStart() != nil {
		line = lineOffset + ctx.GetStart().GetLine()
		column = ctx.GetStart().GetColumn() + 1
	}
	return model.Diagnostic{Position: model.Position{File: file, Line: line, Column: column}, Message: message}
}

func descendants(root antlr.Tree, visit func(antlr.Tree)) {
	if root == nil {
		return
	}
	visit(root)
	for i := 0; i < root.GetChildCount(); i++ {
		descendants(root.GetChild(i), visit)
	}
}
