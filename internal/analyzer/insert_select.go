package analyzer

import (
	"fmt"
	"slices"
	"strings"

	"github.com/antlr4-go/antlr/v4"
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

func updateSelect(statement *parser.Update_stmtContext) parser.ISelect_stmtContext {
	if statement.ON() == nil || statement.Into_values_source() == nil || statement.Into_values_source().Values_source() == nil {
		return nil
	}
	return statement.Into_values_source().Values_source().Select_stmt()
}

func deleteSelect(statement *parser.Delete_stmtContext) parser.ISelect_stmtContext {
	if statement.ON() == nil || statement.Into_values_source() == nil || statement.Into_values_source().Values_source() == nil {
		return nil
	}
	return statement.Into_values_source().Values_source().Select_stmt()
}

func analyzeSelectRows(catalog model.Catalog, block queryBlock, stmt parser.ISelect_stmtContext, bindings, inferred map[string]model.Type, syntax *model.QuerySyntax, projection func(queryBlock, *parser.Select_coreContext, []relation, map[string]model.Type) ([]model.Column, []model.Diagnostic)) ([]model.Column, []model.Diagnostic) {
	cores, partials, diagnostics := selectArms(block, stmt)
	if len(diagnostics) != 0 {
		return nil, diagnostics
	}
	if len(cores) != 1 {
		return nil, []model.Diagnostic{diagnosticAt(block.file, block.line-1, stmt, "DML SELECT supports one SELECT input; UNION is not yet supported")}
	}
	return analyzeSelectCore(catalog, block, cores[0], partials[0], bindings, inferred, syntax, projection)
}

func analyzeSelectCore(catalog model.Catalog, block queryBlock, core *parser.Select_coreContext, partial parser.ISelect_kind_partialContext, bindings, inferred map[string]model.Type, syntax *model.QuerySyntax, projection func(queryBlock, *parser.Select_coreContext, []relation, map[string]model.Type) ([]model.Column, []model.Diagnostic)) ([]model.Column, []model.Diagnostic) {
	var diagnostics []model.Diagnostic
	relations, ds := selectRelations(catalog, block, core, bindings)
	diagnostics = append(diagnostics, ds...)
	if len(ds) != 0 {
		return nil, diagnostics
	}
	if len(relations) == 0 && containsAggregate(core) {
		return nil, []model.Diagnostic{diagnosticAt(block.file, block.line-1, core, "aggregate functions require a FROM source")}
	}
	recordColumnBindings(syntax, core, relations)
	inferFromComparisons(core, relations, inferred)
	inferFromInLists(core, relations, inferred)
	for name, typ := range inferred {
		if _, ok := bindings[name]; !ok && typ.Kind != "" {
			bindings[name] = typ
		}
	}
	subqueries, ds := analyzeINSubqueries(catalog, block, core, relations, bindings, inferred, syntax)
	diagnostics = append(diagnostics, ds...)
	if len(ds) != 0 {
		return nil, diagnostics
	}
	inferFromExpressionContexts(core, bindings, inferred)
	if partial != nil {
		inferLimitOffset(partial, bindings, inferred)
	}
	for name, typ := range inferred {
		if _, ok := bindings[name]; !ok && typ.Kind != "" {
			bindings[name] = typ
		}
	}
	diagnostics = append(diagnostics, validateLimitOffset(block, partial, bindings)...)
	columns, ds := projection(block, core, relations, bindings)
	diagnostics = append(diagnostics, ds...)
	if len(ds) == 0 {
		diagnostics = append(diagnostics, resolveOrderByProjections(block, core, relations, columns, syntax)...)
	}
	diagnostics = append(diagnostics, validateColumnReferences(block, core, relations, columns)...)
	diagnostics = append(diagnostics, validatePredicateContexts(block, core, relations, bindings, subqueries)...)
	diagnostics = append(diagnostics, validateGrouping(block, core, relations, bindings)...)
	resolved := syntax.Selects[core.GetStart().GetTokenIndex()]
	resolved.Columns = columns
	syntax.Selects[core.GetStart().GetTokenIndex()] = resolved
	return columns, diagnostics
}

func analyzeInsertSelect(catalog model.Catalog, block queryBlock, statement *parser.Into_table_stmtContext, target *model.Table, bindings, inferred map[string]model.Type, syntax *model.QuerySyntax) []model.Diagnostic {
	source := statement.Into_values_source()
	stmt := insertSelect(statement)
	cores, partials, diagnostics := selectArms(block, stmt)
	if len(diagnostics) != 0 {
		return diagnostics
	}
	if len(cores) != 1 {
		return []model.Diagnostic{diagnosticAt(block.file, block.line-1, statement, "INSERT/UPSERT SELECT supports one SELECT input; UNION is not yet supported")}
	}
	core := cores[0]
	if source.Pure_column_list() == nil {
		columns, ds := analyzeSelectCore(catalog, block, core, partials[0], bindings, inferred, syntax, selectProjection)
		if len(ds) != 0 {
			return ds
		}
		return validateNamedDMLColumns(block, statement, target, columns, true)
	}
	for _, result := range core.AllResult_column() {
		if result.ASTERISK() != nil {
			return []model.Diagnostic{diagnosticAt(block.file, block.line-1, result, "INSERT/UPSERT SELECT requires explicit source columns; wildcard column order is not guaranteed")}
		}
	}
	columns, ds := analyzeSelectCore(catalog, block, core, partials[0], bindings, inferred, syntax, selectDMLProjection)
	diagnostics = append(diagnostics, ds...)
	if len(ds) != 0 {
		return diagnostics
	}
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
		if !compatibleDMLAssignmentTypes(from, to) {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, id, fmt.Sprintf("SELECT column %q has type %s but target column %q requires %s", columns[i].Name, from.String(), name, to.String())))
		}
	}
	return append(diagnostics, validateRequiredDMLColumns(block, statement, target, seen, true)...)
}

func analyzeNamedDMLSelect(catalog model.Catalog, block queryBlock, stmt parser.ISelect_stmtContext, context antlr.ParserRuleContext, target *model.Table, bindings, inferred map[string]model.Type, syntax *model.QuerySyntax) []model.Diagnostic {
	columns, diagnostics := analyzeSelectRows(catalog, block, stmt, bindings, inferred, syntax, selectProjection)
	if len(diagnostics) != 0 {
		return diagnostics
	}
	return validateNamedDMLColumns(block, context, target, columns, false)
}

func validateNamedDMLColumns(block queryBlock, context antlr.ParserRuleContext, target *model.Table, columns []model.Column, insert bool) []model.Diagnostic {
	var diagnostics []model.Diagnostic
	seen := map[string]bool{}
	for _, source := range columns {
		name := source.ResultName()
		if seen[name] {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, context, fmt.Sprintf("duplicate source column %q", name)))
			continue
		}
		seen[name] = true
		destination := tableColumn(target, name)
		if destination == nil {
			message := fmt.Sprintf("unknown target column %q", name)
			if dot := strings.LastIndex(name, "."); dot >= 0 && dot+1 < len(name) {
				message += fmt.Sprintf("; use AS %s to map the qualified result", name[dot+1:])
			}
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, context, message))
			continue
		}
		if !compatibleDMLAssignmentTypes(source.Type, destination.Type) {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, context, fmt.Sprintf("source column %q has type %s but target column requires %s", name, source.Type.String(), destination.Type.String())))
		}
	}
	return append(diagnostics, validateRequiredDMLColumns(block, context, target, seen, insert)...)
}

func validateRequiredDMLColumns(block queryBlock, context antlr.ParserRuleContext, target *model.Table, seen map[string]bool, insert bool) []model.Diagnostic {
	var diagnostics []model.Diagnostic
	for _, column := range target.Columns {
		if seen[column.Name] || (insert && column.SequenceGenerated) {
			continue
		}
		if slices.Contains(target.PrimaryKey, column.Name) {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, context, fmt.Sprintf("missing primary key column %q", column.Name)))
		} else if insert && !column.Type.IsOptional() {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, context, fmt.Sprintf("missing required column %q; INSERT/UPSERT must provide all NOT NULL columns", column.Name)))
		}
	}
	return diagnostics
}
