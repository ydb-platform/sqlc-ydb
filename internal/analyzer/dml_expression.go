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
	if len(scope.relations) == 0 && len(columnRefs(expr)) != 0 {
		return fmt.Errorf("column references are not allowed in VALUES expressions; use a parameter, literal, or INSERT/UPSERT SELECT")
	}
	if containsAggregate(expr) {
		return fmt.Errorf("aggregate functions are not allowed in DML values")
	}
	scope.predicate = true
	typ, err := resolveExpression(expr, scope)
	if err != nil {
		return err
	}
	if !compatibleDMLAssignmentTypes(typ, column.Type) {
		return fmt.Errorf("cannot assign %s to column %q of type %s; use a compatible value or an explicit CAST", typ.String(), column.Name, column.Type.String())
	}
	return nil
}

func compatibleDMLAssignmentTypes(source, target model.Type) bool {
	if source.Kind == "Null" {
		return target.IsOptional()
	}
	return source.Equal(target) || target.IsOptional() && source.Equal(target.UnwrapOptional()) || builtins.CanWidenInteger(source, target)
}
