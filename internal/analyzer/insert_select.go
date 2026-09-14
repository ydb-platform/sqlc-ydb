package analyzer

import (
	"fmt"
	"strings"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
	parser "github.com/ydb-platform/yql-parsers/go"
)

func asTableRelation(ref parser.ITable_refContext, bindings map[string]model.Type) (*model.Table, error) {
	if ref.An_id_expr() == nil || !strings.EqualFold(ref.An_id_expr().GetText(), "AS_TABLE") || ref.Cluster_expr() != nil || ref.VIEW() != nil || len(ref.AllTable_arg()) != 1 {
		return nil, fmt.Errorf("dynamic table references are unsupported; use AS_TABLE($parameter) with DECLARE List<Struct<...>>")
	}
	arg := ref.Table_arg(0)
	if arg.Named_expr() == nil || arg.Named_expr().Expr() == nil || arg.Named_expr().AS() != nil || arg.COMMAT() != nil || arg.VIEW() != nil {
		return nil, fmt.Errorf("AS_TABLE requires one direct List<Struct> parameter")
	}
	expr := arg.Named_expr().Expr()
	if directBind(expr) == nil {
		return nil, fmt.Errorf("AS_TABLE requires one direct List<Struct> parameter")
	}
	name := bindName(directBind(expr))
	typ, ok := bindings[name]
	if !ok || typ.Kind != "List" || typ.Elem == nil || typ.Elem.Kind != "Struct" {
		return nil, fmt.Errorf("AS_TABLE($%s) requires DECLARE $%s AS List<Struct<...>>", name, name)
	}
	table := &model.Table{Name: "AS_TABLE"}
	for _, field := range typ.Elem.Fields {
		table.Columns = append(table.Columns, model.Column{Name: field.Name, Type: field.Type})
	}
	return table, nil
}

func insertSelect(statement *parser.Into_table_stmtContext) parser.ISelect_stmtContext {
	source := statement.Into_values_source()
	if source == nil || source.Values_source() == nil {
		return nil
	}
	return source.Values_source().Select_stmt()
}

func analyzeInsertSelect(catalog model.Catalog, block queryBlock, statement *parser.Into_table_stmtContext, target *model.Table, bindings, inferred map[string]model.Type, syntax *model.QuerySyntax) []model.Diagnostic {
	source := statement.Into_values_source()
	if source.Pure_column_list() == nil {
		return []model.Diagnostic{diagnosticAt(block.file, block.line-1, statement, "INSERT/UPSERT SELECT requires an explicit target column list")}
	}
	stmt := insertSelect(statement)
	cores, partials, diagnostics := selectArms(block, stmt)
	if len(diagnostics) != 0 {
		return diagnostics
	}
	// UNION inputs use YQL's name-based alignment and need a separate DML contract.
	if len(cores) != 1 {
		return []model.Diagnostic{diagnosticAt(block.file, block.line-1, statement, "INSERT/UPSERT SELECT supports one SELECT input; UNION is not yet supported")}
	}
	core := cores[0]
	for _, result := range core.AllResult_column() {
		if result.ASTERISK() != nil {
			return []model.Diagnostic{diagnosticAt(block.file, block.line-1, result, "INSERT/UPSERT SELECT requires explicit source columns; wildcard column order is not guaranteed")}
		}
	}
	relations, ds := selectRelations(catalog, block, core, bindings)
	diagnostics = append(diagnostics, ds...)
	if len(ds) != 0 {
		return diagnostics
	}
	recordColumnBindings(syntax, core, relations)
	tree := collectQueryTree(core)
	inferFromComparisons(tree, relations, inferred)
	inferFromInLists(tree.conds, relations, inferred)
	inferFromExpressionContexts(core, bindings, inferred)
	if len(partials) > 0 {
		inferLimitOffset(partials[0], inferred)
	}
	for name, typ := range inferred {
		if _, ok := bindings[name]; !ok {
			bindings[name] = typ
		}
	}
	columns, ds := selectProjection(block, core, relations, bindings)
	diagnostics = append(diagnostics, ds...)
	diagnostics = append(diagnostics, validateColumnReferences(block, core, relations)...)
	diagnostics = append(diagnostics, validateGrouping(block, core, relations, bindings)...)
	ids := source.Pure_column_list().AllAn_id()
	if len(columns) != len(ids) {
		return append(diagnostics, diagnosticAt(block.file, block.line-1, statement, fmt.Sprintf("SELECT has %d columns for %d target columns", len(columns), len(ids))))
	}
	seen := map[string]bool{}
	for i, id := range ids {
		name := identifier(id.GetText())
		column := tableColumn(target, name)
		if seen[name] {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, id, fmt.Sprintf("duplicate target column %q", name)))
		}
		seen[name] = true
		if column == nil {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, id, fmt.Sprintf("unknown column %q", name)))
			continue
		}
		from, to := columns[i].Type, column.Type
		if !from.Equal(to) && !(to.IsOptional() && from.Equal(to.UnwrapOptional())) {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, id, fmt.Sprintf("SELECT column %q has type %s but target column %q requires %s", columns[i].Name, from.String(), name, to.String())))
		}
	}
	return diagnostics
}
