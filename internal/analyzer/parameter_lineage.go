package analyzer

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	parser "github.com/ydb-platform/yql-parsers/go"
)

func parameterColumns(catalog model.Catalog, block queryBlock, tree queryTree, syntax *model.QuerySyntax) map[string][]model.Column {
	columns := map[string][]model.Column{}
	add := func(name string, column model.Column) {
		for _, previous := range columns[name] {
			if previous.Table == column.Table && previous.Name == column.Name {
				return
			}
		}
		columns[name] = append(columns[name], column)
	}
	for _, statement := range tree.statements {
		descendants(statement, func(root antlr.Tree) {
			switch node := root.(type) {
			case *parser.Xor_subexprContext:
				if condition := node.Cond_expr(); condition != nil && condition.BETWEEN() != nil {
					refs := columnRefs(node.Eq_subexpr())
					if len(refs) != 1 || !sameOrWrappedExpression(node.Eq_subexpr(), refs[0].ctx) {
						return
					}
					binding, ok := syntax.Columns[refs[0].ctx.GetStart().GetTokenIndex()]
					if !ok {
						return
					}
					for _, bound := range condition.AllEq_subexpr() {
						scopeDescendants(bound, func(child antlr.Tree) {
							if bind, ok := child.(parser.IBind_parameterContext); ok && sameOrWrappedExpression(bound, bind) {
								add(bindName(bind), binding.Column)
							}
						})
					}
					return
				}
			case *parser.Eq_subexprContext:
			default:
				return
			}
			refs := columnRefs(root)
			if len(refs) != 1 {
				return
			}
			binding, ok := syntax.Columns[refs[0].ctx.GetStart().GetTokenIndex()]
			if !ok {
				return
			}
			var binds []parser.IBind_parameterContext
			scopeDescendants(root, func(child antlr.Tree) {
				if bind, ok := child.(parser.IBind_parameterContext); ok {
					binds = append(binds, bind)
				}
			})
			if len(binds) == 1 && isDirectComparison(root, refs[0], binds[0]) {
				add(bindName(binds[0]), binding.Column)
			}
		})
	}
	for _, statement := range tree.insert {
		table := findTable(catalog, resolveTablePath(block.tablePathPrefix, intoTableName(statement)))
		if table == nil || statement.Into_values_source() == nil || statement.Into_values_source().Pure_column_list() == nil || statement.Into_values_source().Values_source() == nil || statement.Into_values_source().Values_source().Values_stmt() == nil || statement.Into_values_source().Values_source().Values_stmt().Values_source_row_list() == nil {
			continue
		}
		ids := statement.Into_values_source().Pure_column_list().AllAn_id()
		for _, row := range statement.Into_values_source().Values_source().Values_stmt().Values_source_row_list().AllValues_source_row() {
			if row.Expr_list() == nil {
				continue
			}
			expressions := directExprs(row.Expr_list())
			if len(expressions) != len(ids) {
				continue
			}
			for i, expression := range expressions {
				if bind := directBind(expression); bind != nil {
					if column := tableColumn(table, identifier(ids[i].GetText())); column != nil {
						add(bindName(bind), *column)
					}
				}
			}
		}
	}
	for _, statement := range tree.updates {
		table := findTable(catalog, resolveTablePath(block.tablePathPrefix, simpleTableName(statement.Simple_table_ref())))
		if table == nil || statement.Set_clause_choice() == nil || statement.Set_clause_choice().Set_clause_list() == nil {
			continue
		}
		descendants(statement.Set_clause_choice().Set_clause_list(), func(node antlr.Tree) {
			clause, ok := node.(*parser.Set_clauseContext)
			if !ok || clause.Set_target() == nil || clause.Set_target().Column_name() == nil || clause.Expr() == nil {
				return
			}
			if bind := directBind(clause.Expr()); bind != nil {
				if column := tableColumn(table, identifier(clause.Set_target().Column_name().An_id().GetText())); column != nil {
					add(bindName(bind), *column)
				}
			}
		})
	}
	if len(columns) == 0 {
		return nil
	}
	return columns
}
