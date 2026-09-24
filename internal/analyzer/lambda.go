package analyzer

import (
	"fmt"
	"maps"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	parser "github.com/ydb-platform/yql-parsers/go"
)

type lambdaBinding struct {
	lambda   parser.ILambdaContext
	bindings map[string]model.Type
	lambdas  map[string]lambdaBinding
}

func directLambda(expr parser.IExprContext) parser.ILambdaContext {
	var result parser.ILambdaContext
	descendants(expr, func(node antlr.Tree) {
		if lambda, ok := node.(parser.ILambdaContext); ok && lambda.ARROW() != nil && sameSpan(expr, lambda) {
			result = lambda
		}
	})
	return result
}

func lambdaParameters(lambda parser.ILambdaContext) []parser.IBind_parameterContext {
	var parameters []parser.IBind_parameterContext
	descendants(lambda.Smart_parenthesis(), func(node antlr.Tree) {
		if bind, ok := node.(parser.IBind_parameterContext); ok {
			parameters = append(parameters, bind)
		}
	})
	return parameters
}

func resolveLambda(lambda parser.ILambdaContext, parameterTypes []model.Type, scope expressionScope) (model.Type, error) {
	parameters := lambdaParameters(lambda)
	names := make([]string, len(parameters))
	for i, parameter := range parameters {
		names[i] = parameter.GetText()
	}
	parameterText := lambda.Smart_parenthesis().GetText()
	if parameterText != "("+strings.Join(names, ",")+")" {
		return model.Type{}, fmt.Errorf("lambda parameters must be plain $name bindings")
	}
	if len(parameters) != len(parameterTypes) {
		return model.Type{}, fmt.Errorf("lambda expects %d parameter(s), got %d", len(parameterTypes), len(parameters))
	}
	scope.bindings = maps.Clone(scope.bindings)
	scope.lambdas = maps.Clone(scope.lambdas)
	scope.relations = nil
	seen := map[string]bool{}
	for i, parameter := range parameters {
		name := bindName(parameter)
		if seen[name] {
			return model.Type{}, fmt.Errorf("lambda parameter $%s is repeated", name)
		}
		if parameterTypes[i].Kind == "Lambda" {
			return model.Type{}, fmt.Errorf("lambda-valued parameter $%s is unsupported without a callable type signature", name)
		}
		seen[name] = true
		scope.bindings[name] = parameterTypes[i]
		delete(scope.lambdas, name)
	}
	var result model.Type
	var err error
	if body := lambda.Lambda_body(); body != nil {
		for _, statement := range body.AllLambda_stmt() {
			assignment := statement.Named_nodes_stmt()
			if assignment == nil || assignment.Expr() == nil || assignment.Bind_parameter_list() == nil {
				return model.Type{}, fmt.Errorf("lambda body supports scalar local assignments and RETURN")
			}
			lhs := assignment.Bind_parameter_list().AllBind_parameter()
			if len(lhs) != 1 {
				return model.Type{}, fmt.Errorf("lambda local assignments require one name")
			}
			name := bindName(lhs[0])
			if seen[name] {
				return model.Type{}, fmt.Errorf("lambda local $%s is assigned more than once", name)
			}
			if containsAggregate(assignment.Expr()) {
				return model.Type{}, fmt.Errorf("aggregate functions are not allowed in lambda local assignments")
			}
			if refs := columnRefs(assignment.Expr()); len(refs) != 0 {
				return model.Type{}, fmt.Errorf("column reference %q is not allowed in lambda function", qualifiedName(refs[0]))
			}
			result, err = resolveExpression(assignment.Expr(), scope)
			if err != nil {
				return model.Type{}, fmt.Errorf("cannot resolve lambda local $%s: %w", name, err)
			}
			scope.bindings[name] = result
			delete(scope.lambdas, name)
			seen[name] = true
		}
		if body.Expr() == nil {
			return model.Type{}, fmt.Errorf("lambda body requires RETURN expression")
		}
		if containsAggregate(body.Expr()) {
			return model.Type{}, fmt.Errorf("aggregate functions are not allowed in lambda RETURN")
		}
		if refs := columnRefs(body.Expr()); len(refs) != 0 {
			return model.Type{}, fmt.Errorf("column reference %q is not allowed in lambda function", qualifiedName(refs[0]))
		}
		result, err = resolveExpression(body.Expr(), scope)
	} else if lambda.Expr() != nil {
		if containsAggregate(lambda.Expr()) {
			return model.Type{}, fmt.Errorf("aggregate functions are not allowed in lambda expressions")
		}
		if refs := columnRefs(lambda.Expr()); len(refs) != 0 {
			return model.Type{}, fmt.Errorf("column reference %q is not allowed in lambda function", qualifiedName(refs[0]))
		}
		result, err = resolveExpression(lambda.Expr(), scope)
	} else {
		return model.Type{}, fmt.Errorf("lambda requires an expression or RETURN body")
	}
	if err != nil {
		return model.Type{}, fmt.Errorf("cannot resolve lambda result: %w", err)
	}
	return model.Type{Kind: "Callable", Items: parameterTypes, Elem: &result}, nil
}

func lambdaLocalBindPositions(root antlr.Tree) map[int]bool {
	positions := map[int]bool{}
	var walk func(antlr.Tree, map[string]bool)
	walk = func(node antlr.Tree, locals map[string]bool) {
		if lambda, ok := node.(parser.ILambdaContext); ok && lambda.ARROW() != nil {
			locals = maps.Clone(locals)
			for _, bind := range lambdaParameters(lambda) {
				locals[bindName(bind)] = true
				positions[bind.GetStart().GetStart()] = true
			}
			if lambda.Lambda_body() != nil {
				walk(lambda.Lambda_body(), locals)
			} else if lambda.Expr() != nil {
				walk(lambda.Expr(), locals)
			}
			return
		}
		if statement, ok := node.(parser.ILambda_stmtContext); ok && statement.Named_nodes_stmt() != nil {
			assignment := statement.Named_nodes_stmt()
			if assignment.Expr() != nil {
				walk(assignment.Expr(), locals)
			}
			if assignment.Bind_parameter_list() != nil {
				for _, bind := range assignment.Bind_parameter_list().AllBind_parameter() {
					locals[bindName(bind)] = true
					positions[bind.GetStart().GetStart()] = true
				}
			}
			return
		}
		if bind, ok := node.(parser.IBind_parameterContext); ok && locals[bindName(bind)] {
			positions[bind.GetStart().GetStart()] = true
		}
		for _, child := range node.GetChildren() {
			walk(child, locals)
		}
	}
	walk(root, map[string]bool{})
	return positions
}
