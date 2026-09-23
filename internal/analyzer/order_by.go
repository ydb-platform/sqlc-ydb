package analyzer

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	parser "github.com/ydb-platform/yql-parsers/go"
)

func resolveOrderByProjections(block queryBlock, core *parser.Select_coreContext, relations []relation, columns []model.Column, syntax *model.QuerySyntax) []model.Diagnostic {
	// Output names shadow source columns in ORDER BY. An empty binding marks
	// a computed output, which must not be rendered as a physical table field.
	outputs := make(map[string]model.ColumnBinding, len(columns))
	for _, column := range columns {
		outputs[column.ResultName()] = model.ColumnBinding{}
	}
	for _, result := range core.AllResult_column() {
		if result.ASTERISK() != nil {
			prefix := identifier(strings.TrimSuffix(result.Opt_id_prefix().GetText(), "."))
			for _, relation := range relations {
				if prefix != "" && prefix != relation.alias {
					continue
				}
				for _, column := range relation.table.Columns {
					outputs[column.Name] = model.ColumnBinding{TableBinding: model.TableBinding{Table: relation.table.Name, Alias: relation.alias}, Column: column}
				}
			}
			continue
		}
		expr := unwrapOrderByColumn(result.Expr())
		if !isPureColumnExpression(expr) {
			continue
		}
		ref := columnRefs(expr)[0]
		name := ref.name
		if len(relations) > 1 && ref.qualifier != "" {
			name = qualifiedName(ref)
		}
		if result.An_id_or_type() != nil {
			name = identifier(result.An_id_or_type().GetText())
		}
		if result.An_id_as_compat() != nil {
			name = identifier(result.An_id_as_compat().GetText())
		}
		outputs[name] = syntax.Columns[ref.ctx.GetStart().GetTokenIndex()]
	}
	var diagnostics []model.Diagnostic
	descendants(core, func(node antlr.Tree) {
		order, ok := node.(*parser.Sort_specificationContext)
		if !ok {
			return
		}
		direct := isPureColumnExpression(unwrapOrderByColumn(order.Expr()))
		for _, ref := range columnRefs(order.Expr()) {
			output, exists := outputs[ref.name]
			if ref.qualifier != "" || !exists {
				continue
			}
			token := ref.ctx.GetStart().GetTokenIndex()
			original := syntax.Columns[token]
			if output.Column.Name != "" && output.TableBinding == original.TableBinding && output.Column.Name == original.Column.Name {
				continue
			}
			if !direct {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, order, "ORDER BY expressions referencing projection aliases are not yet supported; project the complete sorting expression with AS and order by that alias directly"))
				continue
			}
			if output.Column.Name == "" {
				delete(syntax.Columns, token)
			} else {
				syntax.Columns[token] = output
			}
		}
	})
	return diagnostics
}

func unwrapOrderByColumn(expr parser.IExprContext) parser.IExprContext {
	for inner := parenthesizedExpression(expr); inner != nil; inner = parenthesizedExpression(expr) {
		expr = inner
	}
	return expr
}
