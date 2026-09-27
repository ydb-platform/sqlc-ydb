package analyzer

import (
	"fmt"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	parser "github.com/ydb-platform/yql-parsers/go"
)

func rowNumberWindows(block queryBlock, core *parser.Select_coreContext, relations []relation) (map[string]parser.IWindow_specificationContext, []model.Diagnostic) {
	clause := core.Window_clause()
	if clause == nil {
		return nil, nil
	}
	windows := make(map[string]parser.IWindow_specificationContext)
	var diagnostics []model.Diagnostic
	for _, definition := range clause.Window_definition_list().AllWindow_definition() {
		name := identifier(definition.New_window_name().GetText())
		if _, exists := windows[name]; exists {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, definition, fmt.Sprintf("duplicate window %q", name)))
			continue
		}
		windows[name] = definition.Window_specification()
		if err := validateRowNumberWindow(definition.Window_specification(), relations); err != nil {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, definition, fmt.Sprintf("window %q: %v", name, err)))
		}
	}
	return windows, diagnostics
}

func validateRowNumberWindow(spec parser.IWindow_specificationContext, relations []relation) error {
	if spec == nil || spec.Window_specification_details() == nil {
		return fmt.Errorf("invalid window specification")
	}
	details := spec.Window_specification_details()
	if details.Existing_window_name() != nil {
		return fmt.Errorf("inherited window specifications are not yet supported")
	}
	if details.Window_frame_clause() != nil {
		return fmt.Errorf("explicit window frames are not yet supported")
	}
	if partition := details.Window_partition_clause(); partition != nil {
		if partition.COMPACT() != nil {
			return fmt.Errorf("PARTITION COMPACT is not yet supported")
		}
		for _, named := range partition.Named_expr_list().AllNamed_expr() {
			if named.AS() != nil || !isPureColumnExpression(named.Expr()) {
				return fmt.Errorf("PARTITION BY requires direct columns without aliases")
			}
			if _, err := resolveColumn(relations, columnRefs(named.Expr())[0]); err != nil {
				return err
			}
		}
	}
	if order := details.Window_order_clause(); order != nil {
		for _, sort := range order.Order_by_clause().Sort_specification_list().AllSort_specification() {
			if !isPureColumnExpression(sort.Expr()) {
				return fmt.Errorf("window ORDER BY requires direct columns")
			}
			if _, err := resolveColumn(relations, columnRefs(sort.Expr())[0]); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateWindowPlacement(block queryBlock, core *parser.Select_coreContext) []model.Diagnostic {
	var diagnostics []model.Diagnostic
	scopeDescendants(core, func(node antlr.Tree) {
		invoke, ok := node.(*parser.Invoke_exprContext)
		if !ok || invoke.Invoke_expr_tail() == nil || invoke.Invoke_expr_tail().OVER() == nil {
			return
		}
		if !directWindowProjection(invoke) {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, invoke, "window functions are supported only as direct SELECT projections"))
		}
	})
	return diagnostics
}

func hasWindowCall(root antlr.Tree) bool {
	found := false
	scopeDescendants(root, func(node antlr.Tree) {
		invoke, ok := node.(*parser.Invoke_exprContext)
		found = found || ok && invoke.Invoke_expr_tail() != nil && invoke.Invoke_expr_tail().OVER() != nil
	})
	return found
}

func directWindowProjection(invoke *parser.Invoke_exprContext) bool {
	direct := false
	for parent := invoke.GetParent(); parent != nil; parent = parent.GetParent() {
		if result, ok := parent.(*parser.Result_columnContext); ok {
			expr := unwrapOrderByColumn(result.Expr())
			_, call, matched := directFunctionCall(expr)
			direct = matched && call == invoke
			for _, item := range tupleExpressions(expr) {
				_, call, matched := directFunctionCall(unwrapOrderByColumn(item))
				direct = direct || matched && call == invoke
			}
		}
		if _, ok := parent.(*parser.Select_coreContext); ok {
			return direct
		}
	}
	return false
}
