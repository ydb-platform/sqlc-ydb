package analyzer

import (
	"fmt"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-engine-ydb/internal/model"
	"github.com/ydb-platform/sqlc-engine-ydb/internal/yql/builtins"
	parser "github.com/ydb-platform/yql-parsers/go"
)

func topLevelSelect(statements []*parser.Sql_stmtContext) parser.ISelect_stmtContext {
	for _, statement := range statements {
		if statement.Sql_stmt_core() != nil && statement.Sql_stmt_core().Select_stmt() != nil {
			return statement.Sql_stmt_core().Select_stmt()
		}
	}
	return nil
}

func selectArms(block queryBlock, statement parser.ISelect_stmtContext) ([]*parser.Select_coreContext, []parser.ISelect_kind_partialContext, []model.Diagnostic) {
	if statement == nil || statement.Select_stmt_core() == nil {
		return nil, nil, []model.Diagnostic{diagnosticAt(block.file, block.line-1, statement, "invalid SELECT statement")}
	}
	var diagnostics []model.Diagnostic
	if statement.Cte_with_clause() != nil {
		diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, statement.Cte_with_clause(), "CTEs are not yet supported"))
	}
	stmtCore := statement.Select_stmt_core()
	for _, op := range stmtCore.AllUnion_op() {
		if op.EXCEPT() != nil {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, op, "EXCEPT is not yet supported"))
		}
	}
	var cores []*parser.Select_coreContext
	var partials []parser.ISelect_kind_partialContext
	for _, intersect := range stmtCore.AllSelect_stmt_intersect() {
		if len(intersect.AllIntersect_op()) != 0 {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, intersect, "INTERSECT is not yet supported"))
			continue
		}
		kinds := intersect.AllSelect_kind_parenthesis()
		if len(kinds) != 1 || kinds[0].Select_kind_partial() == nil || kinds[0].Select_kind_partial().Select_kind() == nil {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, intersect, "unsupported SELECT input"))
			continue
		}
		partial := kinds[0].Select_kind_partial()
		kind := partial.Select_kind()
		if kind.DISCARD() != nil || kind.INTO() != nil {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, kind, "DISCARD and INTO RESULT are unsupported in named queries"))
			continue
		}
		core, ok := kind.Select_core().(*parser.Select_coreContext)
		if !ok || core == nil {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, kind, "only SELECT inputs are supported in UNION"))
			continue
		}
		cores = append(cores, core)
		partials = append(partials, partial)
	}
	return cores, partials, diagnostics
}

func reconcileUnionColumns(block queryBlock, statement parser.ISelect_stmtContext, arms [][]model.Column) ([]model.Column, []model.Diagnostic) {
	if len(arms) == 1 {
		return arms[0], nil
	}
	byArm := make([]map[string]model.Column, len(arms))
	var diagnostics []model.Diagnostic
	var names []string
	seenNames := map[string]bool{}
	for i, columns := range arms {
		byArm[i] = make(map[string]model.Column, len(columns))
		for _, column := range columns {
			if _, exists := byArm[i][column.Name]; exists {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, statement, fmt.Sprintf("UNION input has duplicate result column %q", column.Name)))
				continue
			}
			byArm[i][column.Name] = column
			if !seenNames[column.Name] {
				names = append(names, column.Name)
				seenNames[column.Name] = true
			}
		}
	}
	if len(diagnostics) != 0 {
		return nil, diagnostics
	}

	columns := make([]model.Column, 0, len(names))
	for _, name := range names {
		var types []model.Type
		missing := false
		for _, input := range byArm {
			column, ok := input[name]
			if !ok {
				missing = true
				continue
			}
			types = append(types, column.Type)
		}
		typeValue, err := builtins.CommonType(types...)
		if err != nil {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, statement, fmt.Sprintf("UNION column %q has incompatible types: %v", name, err)))
			continue
		}
		if missing && !typeValue.IsOptional() {
			typeValue = model.Optional(typeValue)
		}
		columns = append(columns, model.Column{Name: name, Type: typeValue})
	}
	return columns, diagnostics
}

func validateGrouping(block queryBlock, core *parser.Select_coreContext, relations []relation, bindings map[string]model.Type) []model.Diagnostic {
	grouped := map[string]bool{}
	var diagnostics []model.Diagnostic
	if groupBy := core.Group_by_clause(); groupBy != nil && groupBy.Grouping_element_list() != nil {
		for _, element := range groupBy.Grouping_element_list().AllGrouping_element() {
			ordinary := element.Ordinary_grouping_set()
			if ordinary == nil || ordinary.Named_expr() == nil || ordinary.Named_expr().Expr() == nil {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, element, "only direct column GROUP BY elements are currently supported"))
				continue
			}
			expr := ordinary.Named_expr().Expr()
			refs := columnRefs(expr)
			if len(refs) != 1 || !isPureColumnExpression(expr) {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, expr, "only direct column GROUP BY expressions are currently supported"))
				continue
			}
			key, err := resolvedColumnKey(relations, refs[0])
			if err == nil {
				grouped[key] = true
			}
		}
	}

	having := selectHavingExpression(core)
	aggregating := core.Group_by_clause() != nil
	for _, result := range core.AllResult_column() {
		if result.Expr() != nil && containsAggregate(result.Expr()) {
			aggregating = true
		}
	}
	if having != nil && containsAggregate(having) {
		aggregating = true
	}
	if !aggregating {
		return diagnostics
	}
	for _, result := range core.AllResult_column() {
		if result.ASTERISK() != nil {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, result, "star projections are unsupported in grouped or aggregate queries; list grouped columns explicitly"))
			continue
		}
		if result.Expr() == nil {
			continue
		}
		for _, ref := range unaggregatedColumnRefs(result.Expr()) {
			key, _ := resolvedColumnKey(relations, ref)
			if !grouped[key] {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, ref.ctx, fmt.Sprintf("projection column %q must appear in GROUP BY or an aggregate function", qualifiedName(ref))))
			}
		}
	}
	if having != nil {
		for _, ref := range unaggregatedColumnRefs(having) {
			key, _ := resolvedColumnKey(relations, ref)
			if !grouped[key] {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, ref.ctx, fmt.Sprintf("HAVING column %q must appear in GROUP BY or an aggregate function", qualifiedName(ref))))
			}
		}
		typeValue, err := resolveExpression(having, expressionScope{relations: relations, bindings: bindings, grouped: core.Group_by_clause() != nil, predicate: true})
		if err != nil {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, having, fmt.Sprintf("cannot resolve HAVING expression: %v", err)))
		} else if typeValue.UnwrapOptional().Kind != "Bool" {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, having, fmt.Sprintf("HAVING expression has type %s, want Bool", typeString(typeValue))))
		}
	}
	return diagnostics
}

func resolvedColumnKey(relations []relation, ref columnRef) (string, error) {
	var key string
	matches := 0
	for _, rel := range relations {
		if ref.qualifier != "" && ref.qualifier != rel.alias && ref.qualifier != rel.table.Name {
			continue
		}
		if tableColumn(rel.table, ref.name) != nil {
			matches++
			key = rel.alias + "\x00" + ref.name
		}
	}
	if matches == 0 {
		return "", fmt.Errorf("unknown column %q", qualifiedName(ref))
	}
	if matches > 1 {
		return "", fmt.Errorf("ambiguous column %q", ref.name)
	}
	return key, nil
}

func selectHavingExpression(core *parser.Select_coreContext) parser.IExprContext {
	if core.HAVING() == nil {
		return nil
	}
	index := 0
	if core.WHERE() != nil {
		index++
	}
	return core.Expr(index)
}

func containsAggregate(root antlr.Tree) bool {
	found := false
	descendants(root, func(node antlr.Tree) {
		unary, ok := node.(*parser.Unary_subexprContext)
		if !ok {
			return
		}
		name, _, call := functionCallFromUnary(unary)
		found = found || call && isAggregateFunction(name)
	})
	return found
}

func unaggregatedColumnRefs(root antlr.Tree) []columnRef {
	var aggregateSpans [][2]int
	descendants(root, func(node antlr.Tree) {
		unary, ok := node.(*parser.Unary_subexprContext)
		if !ok {
			return
		}
		name, _, call := functionCallFromUnary(unary)
		if call && isAggregateFunction(name) {
			aggregateSpans = append(aggregateSpans, [2]int{unary.GetStart().GetStart(), unary.GetStop().GetStop()})
		}
	})
	var refs []columnRef
	for _, ref := range columnRefs(root) {
		position := ref.ctx.GetStart().GetStart()
		inside := false
		for _, span := range aggregateSpans {
			if position >= span[0] && position <= span[1] {
				inside = true
				break
			}
		}
		if !inside {
			refs = append(refs, ref)
		}
	}
	return refs
}

func isAggregateFunction(name string) bool {
	switch strings.ToLower(name) {
	case "count", "sum", "avg", "min", "max", "some", "every":
		return true
	default:
		return false
	}
}

func inferLimitOffset(partial parser.ISelect_kind_partialContext, inferred map[string]model.Type) {
	if partial == nil || partial.LIMIT() == nil {
		return
	}
	for _, expr := range partial.AllExpr() {
		if bind := directBind(expr); bind != nil {
			inferParameter(inferred, bindName(bind), model.Type{Kind: "Uint64"})
		}
	}
}

func inferFromInLists(conditions []*parser.Cond_exprContext, relations []relation, inferred map[string]model.Type) {
	for _, condition := range conditions {
		if condition.IN() == nil || condition.In_expr() == nil {
			continue
		}
		text := condition.In_expr().GetText()
		if !strings.HasPrefix(text, "(") || !strings.HasSuffix(text, ")") {
			continue
		}
		parent, ok := condition.GetParent().(antlr.Tree)
		if !ok {
			continue
		}
		refs := columnRefs(parent)
		if len(refs) != 1 {
			continue
		}
		column, err := resolveColumn(relations, refs[0])
		if err != nil {
			continue
		}
		var binds []parser.IBind_parameterContext
		var directPositions = map[int]bool{}
		descendants(condition.In_expr(), func(node antlr.Tree) {
			switch ctx := node.(type) {
			case parser.IBind_parameterContext:
				binds = append(binds, ctx)
			case parser.IExprContext:
				if bind := directBind(ctx); bind != nil && bind.GetStart() != nil {
					directPositions[bind.GetStart().GetStart()] = true
				}
			}
		})
		if len(binds) == 0 || len(binds) != len(directPositions) {
			continue
		}
		for _, bind := range binds {
			if directPositions[bind.GetStart().GetStart()] {
				inferParameter(inferred, bindName(bind), column.Type)
			}
		}
	}
}
