package analyzer

import (
	"fmt"
	"strings"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
	parser "github.com/ydb-platform/yql-parsers/go"
)

func flattenListRelation(block queryBlock, source parser.IFlatten_sourceContext, rel relation) (relation, []model.Diagnostic) {
	if source.LIST() == nil || source.Flatten_by_arg() == nil {
		return rel, []model.Diagnostic{diagnosticAt(block.file, block.line-1, source, "only FLATTEN LIST BY is supported")}
	}
	if named := source.Flatten_by_arg().Named_column(); named != nil {
		column := named.Column_name()
		qualifier := ""
		if column.Opt_id_prefix() != nil {
			qualifier = identifier(strings.TrimSuffix(column.Opt_id_prefix().GetText(), "."))
		}
		alias := ""
		if named.An_id() != nil {
			alias = identifier(named.An_id().GetText())
		}
		return flattenListColumn(block, source, rel, qualifier, identifier(column.An_id().GetText()), alias)
	}
	for _, expr := range source.Flatten_by_arg().Named_expr_list().AllNamed_expr() {
		if !isPureColumnExpression(expr.Expr()) {
			return rel, []model.Diagnostic{diagnosticAt(block.file, block.line-1, expr, "FLATTEN LIST BY expressions require a source column; compute the list in a derived SELECT")}
		}
		refs := columnRefs(expr.Expr())
		name := refs[0].name
		alias := ""
		if expr.An_id_or_type() != nil {
			alias = identifier(expr.An_id_or_type().GetText())
		}
		var ds []model.Diagnostic
		rel, ds = flattenListColumn(block, source, rel, refs[0].qualifier, name, alias)
		if len(ds) != 0 {
			return rel, ds
		}
	}
	return rel, nil
}

func flattenListColumn(block queryBlock, source parser.IFlatten_sourceContext, rel relation, qualifier, name, alias string) (relation, []model.Diagnostic) {
	if qualifier != "" && qualifier != rel.alias {
		return rel, []model.Diagnostic{diagnosticAt(block.file, block.line-1, source, fmt.Sprintf("unknown FLATTEN source alias %q", qualifier))}
	}
	table := *rel.table
	table.Columns = append([]model.Column(nil), table.Columns...)
	position := -1
	for i, column := range table.Columns {
		if column.Name == name {
			position = i
		}
		if alias != "" && column.Name == alias {
			return rel, []model.Diagnostic{diagnosticAt(block.file, block.line-1, source, fmt.Sprintf("FLATTEN LIST BY result column %q already exists", alias))}
		}
	}
	if position < 0 {
		return rel, []model.Diagnostic{diagnosticAt(block.file, block.line-1, source, fmt.Sprintf("unknown FLATTEN column %q", name))}
	}
	typ := table.Columns[position].Type.UnwrapOptional()
	if typ.Kind != "List" || typ.Elem == nil {
		return rel, []model.Diagnostic{diagnosticAt(block.file, block.line-1, source, fmt.Sprintf("FLATTEN LIST BY column %q requires List<T>; got %s", name, table.Columns[position].Type.String()))}
	}
	item := table.Columns[position]
	item.Type = *typ.Elem
	if alias == "" {
		table.Columns[position] = item
	} else {
		item.Name = alias
		table.Columns = append(table.Columns, item)
	}
	rel.table = &table
	return rel, nil
}
