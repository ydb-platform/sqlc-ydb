package analyzer

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
	parser "github.com/ydb-platform/yql-parsers/go"
)

func multiResultNames(block queryBlock, parsed parsedYQL, tree queryTree) ([]string, []model.Diagnostic) {
	var selects []*parser.Sql_stmtContext
	for _, statement := range tree.statements {
		if statement.Sql_stmt_core().Select_stmt() != nil {
			selects = append(selects, statement)
		}
	}
	names := make([]string, len(selects))
	var diagnostics []model.Diagnostic
	for _, token := range parsed.tokens {
		if token.GetTokenType() != parser.YQLLexerCOMMENT {
			continue
		}
		comment := strings.TrimSpace(token.GetText())
		if !strings.HasPrefix(comment, "--") {
			continue
		}
		marker := strings.TrimSpace(strings.TrimPrefix(comment, "--"))
		if !strings.HasPrefix(strings.ToLower(marker), "result:") {
			continue
		}
		position := model.Position{File: block.file, Line: block.line - 1 + token.GetLine(), Column: token.GetColumn() + 1}
		name := strings.TrimSpace(marker[len("result:"):])
		if !validMultiResultName(name) {
			diagnostics = append(diagnostics, model.Diagnostic{Position: position, Message: fmt.Sprintf("invalid result name %q; use an exported identifier after -- result:", name)})
			continue
		}
		start := runeByteOffset(block.text, token.GetStart())
		lineStart := strings.LastIndex(block.text[:start], "\n") + 1
		for i, statement := range selects {
			if statement.GetStart().GetStart() <= token.GetStop() {
				continue
			}
			between := block.text[runeByteOffset(block.text, token.GetStop()+1):runeByteOffset(block.text, statement.GetStart().GetStart())]
			if strings.TrimSpace(block.text[lineStart:start]) == "" && strings.TrimSpace(between) == "" && names[i] == "" {
				names[i] = name
			} else {
				diagnostics = append(diagnostics, model.Diagnostic{Position: position, Message: "result annotation must immediately precede a top-level SELECT"})
			}
			break
		}
		if len(selects) == 0 || selects[len(selects)-1].GetStart().GetStart() <= token.GetStop() {
			diagnostics = append(diagnostics, model.Diagnostic{Position: position, Message: "result annotation must immediately precede a top-level SELECT"})
		}
	}
	seen := map[string]bool{}
	for i := range names {
		if names[i] == "" {
			names[i] = fmt.Sprintf("Result%d", i+1)
		}
		if seen[names[i]] {
			diagnostics = append(diagnostics, model.Diagnostic{Position: model.Position{File: block.file, Line: block.line, Column: 1}, Message: fmt.Sprintf("result name %q is used more than once", names[i])})
		}
		seen[names[i]] = true
	}
	return names, diagnostics
}

func validMultiResultName(name string) bool {
	return token.IsIdentifier(name) && ast.IsExported(name)
}
