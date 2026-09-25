// Package analyzer parses YQL and resolves schema, query parameters, and results.
package analyzer

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	"github.com/ydb-platform/sqlc-ydb/internal/yql/builtins"
	parser "github.com/ydb-platform/yql-parsers/go"
)

// Analyze builds the schema catalog first and then semantically analyzes every
// named query against that catalog.
func Analyze(schema, queries []model.Source) (*model.AnalysisResult, error) {
	return AnalyzeWithOptions(schema, queries, Options{})
}

// AnalyzeWithOptions analyzes queries using one explicit compilation contract.
// The zero value of Options selects the shipped function catalog.
func AnalyzeWithOptions(schema, queries []model.Source, options Options) (*model.AnalysisResult, error) {
	return analyze(context.Background(), schema, queries, options, nil)
}

func analyze(ctx context.Context, schema, queries []model.Source, options Options, database Database) (*model.AnalysisResult, error) {
	result := &model.AnalysisResult{}
	functions, err := builtins.NewRegistry(options.Functions)
	if err != nil {
		return result, err
	}
	catalog, diagnostics := buildCatalog(schema)
	result.Catalog = catalog
	result.Diagnostics = append(result.Diagnostics, diagnostics...)
	if len(diagnostics) == 0 {
		var allBlocks []queryBlock
		queryNames := map[string]model.Position{}
		for _, source := range queries {
			blocks, blockDiagnostics := queryBlocks(source)
			result.Diagnostics = append(result.Diagnostics, blockDiagnostics...)
			for _, block := range blocks {
				block.functions = functions
				key := strings.ToLower(block.name)
				if first, exists := queryNames[key]; exists {
					result.Diagnostics = append(result.Diagnostics, model.Diagnostic{
						Position: model.Position{File: block.file, Line: block.line, Column: 1},
						Message:  fmt.Sprintf("query %q is declared more than once (first declaration at %s:%d:%d)", block.name, first.File, first.Line, first.Column),
					})
					continue
				}
				queryNames[key] = model.Position{File: block.file, Line: block.line, Column: 1}
				allBlocks = append(allBlocks, block)
			}
		}
		for _, name := range slices.Sorted(maps.Keys(options.Parameters)) {
			if !slices.ContainsFunc(allBlocks, func(block queryBlock) bool { return block.name == name }) {
				errList := make([]error, 0, len(result.Diagnostics)+1)
				for _, diagnostic := range result.Diagnostics {
					errList = append(errList, diagnostic)
				}
				errList = append(errList, fmt.Errorf("analyzer.parameters references unknown query %q", name))
				return result, errors.Join(errList...)
			}
		}
		for i := range allBlocks {
			allBlocks[i].parameters = options.Parameters[allBlocks[i].name]
		}
		if database != nil && len(result.Diagnostics) == 0 {
			for _, block := range allBlocks {
				if containsEmbedMacro(block) {
					continue
				}
				position := model.Position{File: block.file, Line: block.line, Column: 1}
				if err := ctx.Err(); err != nil {
					result.Diagnostics = append(result.Diagnostics, model.Diagnostic{Position: position, Message: fmt.Sprintf("database analysis canceled: %v", err)})
					break
				}
				validationSQL, validationDiagnostics := queryValidationSQL(block)
				result.Diagnostics = append(result.Diagnostics, validationDiagnostics...)
				if len(validationDiagnostics) != 0 {
					continue
				}
				if err := database.ValidateQuery(ctx, validationSQL); err != nil {
					result.Diagnostics = append(result.Diagnostics, model.Diagnostic{Position: position, Message: fmt.Sprintf("database query validation failed: %v", err)})
				}
			}
		}
		if database != nil && len(result.Diagnostics) == 0 {
			catalog, diagnostics = databaseCatalog(ctx, database, schema, catalog, allBlocks)
			result.Catalog = catalog
			result.Diagnostics = append(result.Diagnostics, diagnostics...)
		}
		if database == nil || len(result.Diagnostics) == 0 {
			for _, block := range allBlocks {
				query, queryDiagnostics := analyzeExecutableQuery(catalog, block)
				result.Diagnostics = append(result.Diagnostics, queryDiagnostics...)
				if len(queryDiagnostics) == 0 {
					if database != nil && containsEmbedMacro(block) {
						block.text = query.SQL
						validationSQL, validationDiagnostics := queryValidationSQL(block)
						result.Diagnostics = append(result.Diagnostics, validationDiagnostics...)
						if len(validationDiagnostics) != 0 {
							continue
						}
						if err := database.ValidateQuery(ctx, validationSQL); err != nil {
							result.Diagnostics = append(result.Diagnostics, model.Diagnostic{Position: query.Source, Message: fmt.Sprintf("database query validation failed: %v", err)})
							continue
						}
					}
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
	seen := map[model.Diagnostic]bool{}
	out := make([]model.Diagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		if seen[diagnostic] {
			continue
		}
		seen[diagnostic] = true
		out = append(out, diagnostic)
	}
	return out
}

type parsedYQL struct {
	tree   parser.ISql_queryContext
	tokens []antlr.Token
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
	return parsedYQL{tree: tree, tokens: tokens.GetAllTokens()}, listener.diagnostics
}

type queryBlock struct {
	name            string
	command         model.Command
	file            string
	line            int
	text            string
	functions       *builtins.Registry
	parameters      map[string]model.Type
	parsed          *parsedYQL
	wildcards       *wildcardRewrites
	tablePathPrefix string
	tabular         map[string]*model.Table
	lambdas         map[string]lambdaBinding
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
	hasPreambleSQL := false
	for _, token := range tokens.GetAllTokens() {
		if len(annotations) == 0 && token.GetChannel() == antlr.TokenDefaultChannel && token.GetTokenType() != antlr.TokenEOF && token.GetTokenType() != parser.YQLLexerCOMMENT {
			hasPreambleSQL = true
		}
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
	if len(annotations) == 0 && hasPreambleSQL {
		diagnostics = append(diagnostics, model.Diagnostic{Position: model.Position{File: source.Name, Line: 1, Column: 1}, Message: "query file contains SQL before any -- name: annotation"})
	}
	if len(annotations) != 0 && hasPreambleSQL {
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
		return "", "", false, "invalid query annotation; expected -- name: QueryName :one|:many|:each|:exec|:execrows"
	}
	command := model.Command(fields[1])
	switch command {
	case model.One, model.Many, model.Each, model.Exec, model.ExecRows:
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
