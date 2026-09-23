package analyzer

import (
	"fmt"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	"github.com/ydb-platform/sqlc-ydb/internal/yql/builtins"
	parser "github.com/ydb-platform/yql-parsers/go"
)

// Each SELECT resolves its own columns, parameters and aggregates. The outer
// pass must not interpret a nested SELECT using the outer relation set.
func scopeDescendants(root antlr.Tree, visit func(antlr.Tree)) {
	if root == nil {
		return
	}
	visit(root)
	for _, child := range root.GetChildren() {
		if _, nested := child.(*parser.Select_kind_partialContext); nested {
			continue
		}
		scopeDescendants(child, visit)
	}
}

func inSubquery(expr parser.IIn_exprContext) parser.ISelect_subexprContext {
	if expr.In_unary_subexpr().In_unary_casual_subexpr() == nil {
		return nil
	}
	casual := expr.In_unary_subexpr().In_unary_casual_subexpr()
	if casual.Unary_subexpr_suffix().GetText() != "" || casual.In_atom_expr() == nil || casual.In_atom_expr().Lambda() == nil {
		return nil
	}
	lambda := casual.In_atom_expr().Lambda()
	if lambda.ARROW() != nil {
		return nil
	}
	sub := lambda.Smart_parenthesis().Select_subexpr()
	if sub == nil {
		return nil
	}
	for _, intersect := range sub.Select_subexpr_core().AllSelect_subexpr_intersect() {
		for _, source := range intersect.AllSelect_or_expr() {
			if source.Select_kind_partial() != nil {
				return sub
			}
		}
	}
	return nil
}

func containsINSubquery(root antlr.Tree) bool {
	found := false
	descendants(root, func(node antlr.Tree) {
		if in, ok := node.(*parser.In_exprContext); ok && inSubquery(in) != nil {
			found = true
		}
	})
	return found
}

func validateINSubqueryContexts(block queryBlock, root antlr.Tree) []model.Diagnostic {
	var diagnostics []model.Diagnostic
	descendants(root, func(node antlr.Tree) {
		in, ok := node.(*parser.In_exprContext)
		if !ok || inSubquery(in) == nil {
			return
		}
		allowed := false
		message := "IN subqueries are supported only in WHERE predicates; they are not yet supported in projections, CASE, IF, or HAVING"
	context:
		for child, parent := antlr.Tree(in), in.GetParent(); parent != nil; child, parent = parent, parent.GetParent() {
			switch ctx := parent.(type) {
			case *parser.Case_exprContext, *parser.Invoke_exprContext, *parser.Cast_exprContext, *parser.Bitcast_exprContext:
				break context
			case *parser.Join_constraintContext:
				message = "IN subqueries are supported only in WHERE predicates; JOIN ON membership is unsupported"
				break context
			case *parser.Select_coreContext:
				allowed = ctx.WHERE() != nil && child == ctx.Expr(0)
				break context
			case *parser.Update_stmtContext:
				allowed = ctx.WHERE() != nil && child == ctx.Expr()
				break context
			case *parser.Delete_stmtContext:
				allowed = ctx.WHERE() != nil && child == ctx.Expr()
				break context
			}
		}
		if !allowed {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, in, message))
		}
	})
	return diagnostics
}

func analyzeINSubqueries(catalog model.Catalog, block queryBlock, root antlr.Tree, outer []relation, bindings, inferred map[string]model.Type, syntax *model.QuerySyntax) (map[int]model.Type, []model.Diagnostic) {
	types := map[int]model.Type{}
	var diagnostics []model.Diagnostic
	scopeDescendants(root, func(node antlr.Tree) {
		in, ok := node.(*parser.In_exprContext)
		if !ok {
			return
		}
		sub := inSubquery(in)
		if sub == nil {
			return
		}
		intersects := sub.Select_subexpr_core().AllSelect_subexpr_intersect()
		if sub.Cte_with_clause() != nil || len(intersects) != 1 || len(intersects[0].AllSelect_or_expr()) != 1 {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, in, "IN subqueries currently require one SELECT; CTEs, UNION and INTERSECT are unsupported"))
			return
		}
		partial := intersects[0].Select_or_expr(0).Select_kind_partial()
		kind := partial.Select_kind()
		core, ok := kind.Select_core().(*parser.Select_coreContext)
		if !ok || kind.DISCARD() != nil || kind.INTO() != nil {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, in, "IN subqueries require a SELECT without DISCARD or INTO RESULT"))
			return
		}
		columns, ds := analyzeSelectCore(catalog, block, core, partial, bindings, inferred, syntax, selectINProjection)
		diagnostics = append(diagnostics, ds...)
		if _, resolved := syntax.Selects[core.GetStart().GetTokenIndex()]; resolved && len(ds) != 0 {
			for _, ref := range columnRefs(core) {
				if _, bound := syntax.Columns[ref.ctx.GetStart().GetTokenIndex()]; bound {
					continue
				}
				output := false
				if ref.qualifier == "" && isOrderByReference(ref.ctx) {
					for _, column := range columns {
						output = output || column.ResultName() == ref.name
					}
				}
				if !output {
					if _, err := resolveColumn(outer, ref); err == nil {
						diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, ref.ctx, fmt.Sprintf("correlated IN subqueries are unsupported: %q may refer to an outer column; use only the subquery's own sources", qualifiedName(ref))))
						return
					}
				}
			}
		}
		if len(ds) != 0 {
			return
		}
		if len(columns) != 1 {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, core, "IN subquery must return exactly one column; use SELECT (key1, key2) for a tuple key"))
			return
		}
		types[in.GetStart().GetTokenIndex()] = columns[0].Type
	})
	return types, diagnostics
}

func selectINProjection(block queryBlock, core *parser.Select_coreContext, relations []relation, bindings map[string]model.Type) ([]model.Column, []model.Diagnostic) {
	results := core.AllResult_column()
	if len(results) == 1 && results[0].Expr() != nil && len(tupleExpressions(results[0].Expr())) > 1 && core.Without_column_list() == nil {
		result := results[0]
		typ, err := resolveINOperand(result.Expr(), expressionScope{relations: relations, bindings: bindings, grouped: core.Group_by_clause() != nil, functions: block.functions})
		if err != nil {
			return nil, []model.Diagnostic{diagnosticAt(block.file, block.line-1, result, err.Error())}
		}
		name := "column0"
		if result.An_id_or_type() != nil {
			name = identifier(result.An_id_or_type().GetText())
		}
		if result.An_id_as_compat() != nil {
			name = identifier(result.An_id_as_compat().GetText())
		}
		return []model.Column{{Name: name, Type: typ}}, nil
	}
	return selectProjection(block, core, relations, bindings)
}

func tupleExpressions(root antlr.ParserRuleContext) []parser.IExprContext {
	var expressions []parser.IExprContext
	scopeDescendants(root, func(node antlr.Tree) {
		lambda, ok := node.(*parser.LambdaContext)
		if !ok || !sameSpan(root, lambda) || lambda.ARROW() != nil {
			return
		}
		sub := lambda.Smart_parenthesis().Select_subexpr()
		if sub == nil || sub.Cte_with_clause() != nil {
			return
		}
		intersects := sub.Select_subexpr_core().AllSelect_subexpr_intersect()
		if len(intersects) != 1 || len(intersects[0].AllSelect_or_expr()) != 1 {
			return
		}
		tuple := intersects[0].Select_or_expr(0).Tuple_or_expr()
		if tuple == nil || tuple.An_id_or_type() != nil || len(tuple.AllNamed_expr()) == 0 {
			return
		}
		expressions = append(expressions, tuple.Expr())
		for _, item := range tuple.AllNamed_expr() {
			if item.AS() != nil {
				expressions = nil
				return
			}
			expressions = append(expressions, item.Expr())
		}
	})
	return expressions
}

func resolveINOperand(root antlr.ParserRuleContext, scope expressionScope) (model.Type, error) {
	items := tupleExpressions(root)
	if len(items) == 0 {
		return resolveScalarNode(root, scope)
	}
	typ := model.Type{Kind: "Tuple"}
	for _, item := range items {
		value, err := resolveExpression(item, scope)
		if err != nil {
			return model.Type{}, fmt.Errorf("cannot resolve IN tuple item: %w", err)
		}
		typ.Items = append(typ.Items, value)
	}
	return typ, nil
}

func validateINSubqueryTypes(left, right model.Type) error {
	if left.Kind == "Null" && right.Kind == "Null" {
		return nil
	}
	if left.Kind == "Tuple" && right.Kind == "Tuple" {
		if len(left.Items) != len(right.Items) {
			return fmt.Errorf("tuple arity differs: %d and %d", len(left.Items), len(right.Items))
		}
		for i, item := range left.Items {
			if err := validateINSubqueryTypes(item, right.Items[i]); err != nil {
				return fmt.Errorf("tuple component %d: %w", i+1, err)
			}
		}
		return nil
	}
	_, err := builtins.CommonType(left, right)
	return err
}
