package analyzer

import (
	"fmt"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
	"github.com/ydb-platform/sqlc-ydb/internal/yql/builtins"
	parser "github.com/ydb-platform/yql-parsers/go"
)

func validateDMLValue(expr parser.IExprContext, column model.Column, scope expressionScope, inferred map[string]model.Type) error {
	for inner := parenthesizedExpression(expr); inner != nil; inner = parenthesizedExpression(expr) {
		expr = inner
	}
	if inferDirectDMLBind(expr, column.Type, inferred) {
		return nil
	}
	if containsAggregate(expr) {
		return fmt.Errorf("aggregate functions are not allowed in DML values")
	}
	typ, err := resolveExpression(expr, scope)
	if err != nil {
		return err
	}
	if !compatibleDMLSelectTypes(typ, column.Type) && !builtins.CanWidenInteger(typ, column.Type) {
		return fmt.Errorf("cannot assign %s to column %q of type %s; use a compatible value or an explicit CAST", typ.String(), column.Name, column.Type.String())
	}
	return nil
}
