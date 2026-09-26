package analyzer

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	"github.com/ydb-platform/sqlc-ydb/internal/yql/builtins"
	parser "github.com/ydb-platform/yql-parsers/go"
)

type queryTree struct {
	statements []*parser.Sql_stmtContext
	declares   []*parser.Declare_stmtContext
	named      []*parser.Named_nodes_stmtContext
	insert     []*parser.Into_table_stmtContext
	updates    []*parser.Update_stmtContext
	deletes    []*parser.Delete_stmtContext
	binds      []parser.IBind_parameterContext
}

func collectQueryTree(tree antlr.Tree) queryTree {
	var out queryTree
	descendants(tree, func(node antlr.Tree) {
		switch ctx := node.(type) {
		case *parser.Sql_stmtContext:
			if !insideLambda(ctx) {
				out.statements = append(out.statements, ctx)
			}
		case *parser.Declare_stmtContext:
			if !insideLambda(ctx) {
				out.declares = append(out.declares, ctx)
			}
		case *parser.Named_nodes_stmtContext:
			if !insideLambda(ctx) {
				out.named = append(out.named, ctx)
			}
		case *parser.Into_table_stmtContext:
			out.insert = append(out.insert, ctx)
		case *parser.Update_stmtContext:
			out.updates = append(out.updates, ctx)
		case *parser.Delete_stmtContext:
			out.deletes = append(out.deletes, ctx)
		case *parser.Bind_parameterContext:
			out.binds = append(out.binds, ctx)
		}
	})
	return out
}

func insideLambda(ctx antlr.ParserRuleContext) bool {
	for parent := ctx.GetParent(); parent != nil; parent = parent.GetParent() {
		if lambda, ok := parent.(*parser.LambdaContext); ok && lambda.ARROW() != nil {
			return true
		}
	}
	return false
}

func analyzeQuery(catalog model.Catalog, block queryBlock) (model.AnalyzedQuery, []model.Diagnostic) {
	var parsed parsedYQL
	var diagnostics []model.Diagnostic
	if block.parsed == nil {
		parsed, diagnostics = parseYQL(block.file, block.text, block.line-1)
	} else {
		parsed = *block.parsed
	}
	query := model.AnalyzedQuery{
		Name: block.name, Command: block.command, SQL: block.text,
		Source: model.Position{File: block.file, Line: block.line, Column: 1},
	}
	if len(diagnostics) != 0 {
		return query, diagnostics
	}
	block.tablePathPrefix, diagnostics = tablePathPrefix(block.file, block.line-1, parsed.tree)
	if len(diagnostics) != 0 {
		return query, diagnostics
	}
	query.Syntax = &model.QuerySyntax{Root: parsed.tree, Columns: map[int]model.ColumnBinding{}, Selects: map[int]model.SelectBinding{}, Tables: resolvedTableReferences(parsed.tree, block.tablePathPrefix), TablePathPrefix: block.tablePathPrefix}
	tree := collectQueryTree(parsed.tree)
	lambdaPositions := lambdaLocalBindPositions(parsed.tree)
	if diagnostics = unsupportedSQLCMacroDiagnostics(block, parsed.tokens); len(diagnostics) != 0 {
		return query, diagnostics
	}
	if diagnostics = validateQueryStatements(block, tree); len(diagnostics) != 0 {
		return query, diagnostics
	}
	if contextDiagnostics := validateINSubqueryContexts(block, parsed.tree); len(contextDiagnostics) != 0 {
		return query, append(diagnostics, contextDiagnostics...)
	}
	selectStatement := topLevelSelect(tree.statements)

	if block.command == model.Each && selectStatement == nil {
		return query, []model.Diagnostic{{Position: query.Source, Message: ":each requires a SELECT; use :one or :many for DML RETURNING"}}
	}

	declared, declarationPositions, declarationDiagnostics := declarations(block, tree)
	for _, declaration := range tree.declares {
		name := bindName(declaration.Bind_parameter())
		if !query.IsDeclaredParameter(name) {
			query.DeclaredParameters = append(query.DeclaredParameters, name)
		}
	}
	diagnostics = append(diagnostics, declarationDiagnostics...)
	declared, configuredDiagnostics := configuredDeclarations(block, declared)
	diagnostics = append(diagnostics, configuredDiagnostics...)
	inferred := map[string]model.Type{}
	localPositions, localNames, localTypes, tabular, lambdas, localDiagnostics := localBindings(catalog, block, tree, declared, inferred, query.Syntax)
	diagnostics = append(diagnostics, localDiagnostics...)
	block.tabular = tabular
	block.lambdas = lambdas

	bindings := make(map[string]model.Type, len(declared)+len(localTypes))
	for name, typeValue := range declared {
		bindings[name] = typeValue
	}
	for name, typeValue := range localTypes {
		bindings[name] = typeValue
	}
	var resultColumns []model.Column
	resultParameters := map[string]model.Type{}
	dataStatements := 0
	for _, statement := range tree.statements {
		core := statement.Sql_stmt_core()
		if core.Select_stmt() == nil && core.Into_table_stmt() == nil && core.Update_stmt() == nil && core.Delete_stmt() == nil {
			continue
		}
		dataStatements++
		columns, ds := analyzeDataStatement(catalog, block, statement, bindings, inferred, query.Syntax)
		diagnostics = append(diagnostics, ds...)
		if len(ds) != 0 && columns == nil && containsINSubquery(statement) {
			return query, diagnostics
		}
		if len(columns) != 0 {
			resultColumns = columns
			if core.Select_stmt() != nil {
				for _, bind := range collectQueryTree(statement).binds {
					if lambdaPositions[bind.GetStart().GetStart()] {
						continue
					}
					name := bindName(bind)
					if _, explicit := declared[name]; !explicit && !localNames[name] {
						resultParameters[name] = inferred[name]
					}
				}
			}
		}
	}

	query.MultipleStatements = dataStatements > 1
	if query.MultipleStatements && block.wildcards != nil && len(block.wildcards.embeds) != 0 {
		diagnostics = append(diagnostics, model.Diagnostic{Position: query.Source, Message: "sqlc.embed requires a single row-returning SELECT statement"})
	}
	for _, call := range sqlcMacroCalls(parsed.tokens) {
		if strings.EqualFold(call.name, "embed") && (block.wildcards == nil || !block.wildcards.usedEmbeds[call.token.GetStart()]) {
			diagnostics = append(diagnostics, model.Diagnostic{
				Position: model.Position{File: block.file, Line: block.line - 1 + call.token.GetLine(), Column: call.token.GetColumn() + 1},
				Message:  "sqlc.embed must be a direct result expression in a single top-level SELECT",
			})
		}
	}

	for name, localType := range localTypes {
		if usedType, ok := inferred[name]; ok {
			message := ""
			if usedType.Kind == "" {
				message = fmt.Sprintf("local $%s is constrained by incompatible column types", name)
			} else if !compatibleTypes(localType, usedType) {
				message = fmt.Sprintf("local $%s has type %s but is used with %s", name, localType.String(), usedType.String())
			}
			if message != "" {
				diagnostics = append(diagnostics, model.Diagnostic{Position: model.Position{File: block.file, Line: block.line, Column: 1}, Message: message})
			}
		}
	}

	for position := range lambdaPositions {
		localPositions[position] = true
	}
	parameters, parameterDiagnostics := externalParameters(block, tree.binds, declared, inferred, declarationPositions, localPositions, localNames)
	diagnostics = append(diagnostics, parameterDiagnostics...)
	for _, name := range slices.Sorted(maps.Keys(block.parameters)) {
		used := false
		for _, bind := range tree.binds {
			if bind.GetStart() == nil {
				continue
			}
			position := bind.GetStart().GetStart()
			if bindName(bind) == name && !localPositions[position] && (!localNames[name] || declarationPositions[position]) {
				used = true
				break
			}
		}
		if !used {
			diagnostics = append(diagnostics, model.Diagnostic{Position: query.Source, Message: fmt.Sprintf("unused parameter $%s in analyzer.parameters for query %q", name, block.name)})
		}
	}
	query.Parameters = parameters
	for _, parameter := range parameters {
		if previous, ok := resultParameters[parameter.Name]; ok && !previous.Equal(parameter.Type) {
			diagnostics = append(diagnostics, model.Diagnostic{Position: query.Source, Message: fmt.Sprintf("parameter $%s changes inferred type from %s to %s after the result statement; add DECLARE before the script to keep its result type stable", parameter.Name, previous.String(), parameter.Type.String())})
		}
	}

	for _, column := range resultColumns {
		if column.Type.Kind == "Null" {
			diagnostics = append(diagnostics, model.Diagnostic{Position: query.Source, Message: fmt.Sprintf("result column %q has unresolved Null type; cast it or combine it with a concrete compatible type", column.Name)})
		}
		if nonpersistableType(column.Type) {
			diagnostics = append(diagnostics, model.Diagnostic{Position: query.Source, Message: fmt.Sprintf("result column %q has nonpersistable type %s; serialize it before returning it", column.Name, column.Type.String())})
		}
	}
	returnsRows := len(resultColumns) != 0
	if len(diagnostics) == 0 {
		if (block.command == model.One || block.command == model.Many || block.command == model.Each) && !returnsRows {
			diagnostics = append(diagnostics, model.Diagnostic{Position: query.Source, Message: fmt.Sprintf("command %s requires a result set", block.command)})
		}
		if (block.command == model.Exec || block.command == model.ExecRows) && returnsRows {
			diagnostics = append(diagnostics, model.Diagnostic{Position: query.Source, Message: fmt.Sprintf("command %s cannot be used with a row-returning statement", block.command)})
		}
	}
	if returnsRows {
		result := model.ResultSet{Columns: resultColumns}
		if block.wildcards != nil {
			result.Embeds = block.wildcards.embeds
		}
		query.ResultSets = []model.ResultSet{result}
	}
	return query, diagnostics
}

func nonpersistableType(value model.Type) bool {
	if strings.HasPrefix(value.Kind, "Resource<") || value.Kind == "Callable" || value.Kind == "Lambda" || value.Kind == "Tagged" {
		return true
	}
	if value.Elem != nil && nonpersistableType(*value.Elem) {
		return true
	}
	if value.Key != nil && nonpersistableType(*value.Key) {
		return true
	}
	for _, item := range value.Items {
		if nonpersistableType(item) {
			return true
		}
	}
	for _, field := range value.Fields {
		if nonpersistableType(field.Type) {
			return true
		}
	}
	return false
}

func analyzeDataStatement(catalog model.Catalog, block queryBlock, statement *parser.Sql_stmtContext, bindings, inferred map[string]model.Type, syntax *model.QuerySyntax) ([]model.Column, []model.Diagnostic) {
	bindings = maps.Clone(bindings)
	tree := collectQueryTree(statement)
	selectStatement := topLevelSelect(tree.statements)
	var diagnostics []model.Diagnostic
	var relations []relation
	var resultColumns []model.Column
	var target *model.Table
	switch {
	case selectStatement != nil:
		var selectDiagnostics []model.Diagnostic
		var arms [][]model.Column
		var partials []parser.ISelect_kind_partialContext
		var cores []*parser.Select_coreContext
		cores, partials, selectDiagnostics = selectArms(block, selectStatement)
		diagnostics = append(diagnostics, selectDiagnostics...)
		if block.wildcards != nil && len(cores) == 1 {
			block.wildcards.embedCore = cores[0]
		}
		for i, core := range cores {
			columns, armDiagnostics := analyzeSelectCore(catalog, block, core, partials[i], bindings, inferred, syntax, selectProjection)
			diagnostics = append(diagnostics, armDiagnostics...)
			if len(armDiagnostics) != 0 {
				if columns == nil && containsINSubquery(core) {
					return nil, diagnostics
				}
				continue
			}
			arms = append(arms, columns)
		}
		if len(arms) == len(cores) && len(arms) != 0 {
			resultColumns, selectDiagnostics = reconcileUnionColumns(block, selectStatement, arms)
			diagnostics = append(diagnostics, selectDiagnostics...)
		}
	case len(tree.insert) == 1:
		target, diagnostics = targetTable(catalog, block, intoTableName(tree.insert[0]), tree.insert[0], diagnostics)
		if target != nil {
			relations = []relation{{table: target, alias: intoTableName(tree.insert[0])}}
		}
	case len(tree.updates) == 1:
		target, diagnostics = targetTable(catalog, block, simpleTableName(tree.updates[0].Simple_table_ref()), tree.updates[0], diagnostics)
		if target != nil {
			relations = []relation{{table: target, alias: simpleTableName(tree.updates[0].Simple_table_ref())}}
		}
	case len(tree.deletes) == 1:
		target, diagnostics = targetTable(catalog, block, simpleTableName(tree.deletes[0].Simple_table_ref()), tree.deletes[0], diagnostics)
		if target != nil {
			relations = []relation{{table: target, alias: simpleTableName(tree.deletes[0].Simple_table_ref())}}
		}
	}

	if selectStatement == nil && len(relations) != 0 && !(len(tree.insert) == 1 && insertSelect(tree.insert[0]) != nil) && !(len(tree.updates) == 1 && updateSelect(tree.updates[0]) != nil) && !(len(tree.deletes) == 1 && deleteSelect(tree.deletes[0]) != nil) {
		recordColumnBindings(syntax, statement, relations)
		diagnostics = append(diagnostics, validateColumnReferences(block, statement, relations, nil)...)
		inferFromComparisons(statement, relations, inferred)
		inferFromInLists(statement, relations, inferred)
		for name, typeValue := range inferred {
			if _, exists := bindings[name]; !exists && typeValue.Kind != "" {
				bindings[name] = typeValue
			}
		}
		subqueries, ds := analyzeINSubqueries(catalog, block, statement, relations, bindings, inferred, syntax)
		diagnostics = append(diagnostics, ds...)
		if len(ds) != 0 {
			return nil, diagnostics
		}
		diagnostics = append(diagnostics, validatePredicateContexts(block, statement, relations, bindings, subqueries)...)
	}
	if target != nil && len(tree.insert) == 1 {
		if stmt := insertSelect(tree.insert[0]); stmt != nil {
			ds := analyzeInsertSelect(catalog, block, tree.insert[0], target, bindings, inferred, syntax)
			diagnostics = append(diagnostics, ds...)
			if len(ds) != 0 && containsINSubquery(stmt) {
				return nil, diagnostics
			}
		} else {
			diagnostics = append(diagnostics, inferInsert(block, tree.insert[0], target, bindings, inferred)...)
		}
	}
	if target != nil && len(tree.updates) == 1 {
		if stmt := updateSelect(tree.updates[0]); stmt != nil {
			if tree.updates[0].Into_values_source().Pure_column_list() != nil {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, tree.updates[0].Into_values_source().Pure_column_list(), "UPDATE ON SELECT with an explicit source column list is unsupported"))
			} else {
				ds := analyzeNamedDMLSelect(catalog, block, stmt, tree.updates[0], target, bindings, inferred, syntax)
				diagnostics = append(diagnostics, ds...)
				if len(ds) != 0 && containsINSubquery(stmt) {
					return nil, diagnostics
				}
			}
		} else if tree.updates[0].ON() != nil {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, tree.updates[0], "UPDATE ON currently requires a SELECT source"))
		} else {
			diagnostics = append(diagnostics, inferUpdate(block, tree.updates[0], target, bindings, inferred)...)
		}
	}
	if target != nil && len(tree.deletes) == 1 {
		if stmt := deleteSelect(tree.deletes[0]); stmt != nil {
			if tree.deletes[0].Into_values_source().Pure_column_list() != nil {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, tree.deletes[0].Into_values_source().Pure_column_list(), "DELETE ON SELECT with an explicit source column list is unsupported"))
			} else {
				ds := analyzeNamedDMLSelect(catalog, block, stmt, tree.deletes[0], target, bindings, inferred, syntax)
				diagnostics = append(diagnostics, ds...)
				if len(ds) != 0 && containsINSubquery(stmt) {
					return nil, diagnostics
				}
			}
		} else if tree.deletes[0].ON() != nil {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, tree.deletes[0], "DELETE ON currently requires a SELECT source"))
		}
	}
	if target != nil {
		var returning parser.IReturning_columns_listContext
		switch {
		case len(tree.insert) == 1:
			returning = tree.insert[0].Returning_columns_list()
		case len(tree.updates) == 1:
			returning = tree.updates[0].Returning_columns_list()
		case len(tree.deletes) == 1:
			returning = tree.deletes[0].Returning_columns_list()
		}
		if returning != nil {
			var ds []model.Diagnostic
			resultColumns, ds = returningProjection(block, returning, target)
			diagnostics = append(diagnostics, ds...)
		}
	}

	return resultColumns, diagnostics
}

func inferFromExpressionContexts(root antlr.Tree, declared, inferred map[string]model.Type) {
	scopeDescendants(root, func(node antlr.Tree) {
		if ctx, ok := node.(*parser.Mul_subexprContext); ok {
			inferFromConcatenation(ctx, declared, inferred)
		}
	})
}

func inferFromConcatenation(concatenation *parser.Mul_subexprContext, declared, inferred map[string]model.Type) {
	if len(concatenation.AllDOUBLE_PIPE()) == 0 {
		return
	}
	var family model.Type
	var unknown []parser.IBind_parameterContext
	for _, operand := range concatenation.AllCon_subexpr() {
		var binds []parser.IBind_parameterContext
		var literals []parser.ILiteral_valueContext
		descendants(operand, func(node antlr.Tree) {
			switch ctx := node.(type) {
			case parser.IBind_parameterContext:
				binds = append(binds, ctx)
			case parser.ILiteral_valueContext:
				literals = append(literals, ctx)
			}
		})
		switch {
		case len(binds) == 1 && len(literals) == 0 && operand.GetText() == binds[0].GetText():
			name := bindName(binds[0])
			if typeValue, ok := declared[name]; ok {
				base := typeValue.UnwrapOptional()
				if base.Kind == "String" || base.Kind == "Utf8" {
					if family.Kind == "" {
						family = base
					} else if !family.Equal(base) {
						return
					}
				}
			} else {
				unknown = append(unknown, binds[0])
			}
		case len(literals) == 1 && len(binds) == 0 && operand.GetText() == literals[0].GetText() && literals[0].STRING_VALUE() != nil:
			typeValue, err := stringLiteralType(literals[0].GetText())
			if err != nil {
				return
			}
			if family.Kind == "" {
				family = typeValue
			} else if !family.Equal(typeValue) {
				return
			}
		}
	}
	if family.Kind == "" {
		return
	}
	for _, bind := range unknown {
		inferParameter(inferred, bindName(bind), family)
	}
}

func validateQueryStatements(block queryBlock, tree queryTree) []model.Diagnostic {
	var diagnostics []model.Diagnostic
	var dataStatements []*parser.Sql_stmtContext
	var lateBinding antlr.ParserRuleContext
	for _, statement := range tree.statements {
		core := statement.Sql_stmt_core()
		if statement.EXPLAIN() != nil {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, statement, "EXPLAIN is unsupported in named queries"))
			continue
		}
		if core == nil {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, statement, fmt.Sprintf("unsupported statement in named query: %q", statement.GetText())))
			continue
		}
		switch {
		case core.Pragma_stmt() != nil:
		case core.Declare_stmt() != nil, core.Named_nodes_stmt() != nil:
			if len(dataStatements) != 0 && lateBinding == nil {
				lateBinding = statement
			}
		case core.Select_stmt() != nil, core.Into_table_stmt() != nil, core.Update_stmt() != nil, core.Delete_stmt() != nil:
			dataStatements = append(dataStatements, statement)
		default:
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, statement, fmt.Sprintf("unsupported statement in named query: %q", statement.GetText())))
		}
	}
	if len(diagnostics) != 0 {
		return diagnostics
	}
	if len(dataStatements) == 0 {
		return []model.Diagnostic{{Position: model.Position{File: block.file, Line: block.line, Column: 1}, Message: "named query requires a SELECT, INSERT/UPSERT, UPDATE, or DELETE statement"}}
	}
	if lateBinding != nil {
		return []model.Diagnostic{diagnosticAt(block.file, block.line-1, lateBinding, "DECLARE and local assignments must precede all data statements in a script")}
	}
	if len(tree.named) != 0 {
		for _, declaration := range tree.declares {
			if declaration.GetStart().GetTokenIndex() > tree.named[0].GetStart().GetTokenIndex() {
				return []model.Diagnostic{diagnosticAt(block.file, block.line-1, declaration, "DECLARE statements must precede local assignments in a script")}
			}
		}
	}
	if len(dataStatements) == 1 {
		return nil
	}
	results := 0
	for _, statement := range dataStatements {
		core := statement.Sql_stmt_core()
		if core.Select_stmt() != nil ||
			core.Into_table_stmt() != nil && core.Into_table_stmt().Returning_columns_list() != nil ||
			core.Update_stmt() != nil && core.Update_stmt().Returning_columns_list() != nil ||
			core.Delete_stmt() != nil && core.Delete_stmt().Returning_columns_list() != nil {
			results++
		}
	}
	message := ""
	switch {
	case results > 1:
		message = fmt.Sprintf("multi-statement queries support at most one result-producing statement; found %d", results)
	case block.command == model.Each:
		message = "multi-statement :each is unsupported; use :one or :many to consume the result before returning"
	case block.command == model.ExecRows:
		message = "multi-statement :execrows is unsupported; use :exec, :one, or :many"
	case block.command == model.Exec && results != 0:
		message = "command :exec cannot be used with a row-returning script; use :one or :many"
	case (block.command == model.One || block.command == model.Many) && results == 0:
		message = fmt.Sprintf("command %s requires exactly one result-producing statement in a script", block.command)
	}
	if message != "" {
		return []model.Diagnostic{diagnosticAt(block.file, block.line-1, dataStatements[0], message)}
	}
	return nil
}

func declarations(block queryBlock, tree queryTree) (map[string]model.Type, map[int]bool, []model.Diagnostic) {
	declared := map[string]model.Type{}
	positions := map[int]bool{}
	var diagnostics []model.Diagnostic
	for _, declaration := range tree.declares {
		bind := declaration.Bind_parameter()
		name := bindName(bind)
		if bind != nil && bind.GetStart() != nil {
			positions[bind.GetStart().GetStart()] = true
		}
		typeValue, err := parseType(declaration.Type_name().GetText())
		if err != nil {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, declaration, err.Error()))
			continue
		}
		if previous, ok := declared[name]; ok && !previous.Equal(typeValue) {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, declaration, fmt.Sprintf("parameter $%s has conflicting DECLARE types %s and %s", name, previous.String(), typeValue.String())))
			continue
		}
		declared[name] = typeValue
	}
	return declared, positions, diagnostics
}

func configuredDeclarations(block queryBlock, declared map[string]model.Type) (map[string]model.Type, []model.Diagnostic) {
	if len(block.parameters) == 0 {
		return declared, nil
	}
	resolved := maps.Clone(declared)
	var diagnostics []model.Diagnostic
	for _, name := range slices.Sorted(maps.Keys(block.parameters)) {
		typ := block.parameters[name]
		parsed, err := parseType(typ.String())
		if err != nil || !parsed.Equal(typ) {
			diagnostics = append(diagnostics, model.Diagnostic{Position: model.Position{File: block.file, Line: block.line, Column: 1}, Message: fmt.Sprintf("configured parameter $%s has unsupported YQL type %s", name, typ.String())})
			continue
		}
		if source, ok := declared[name]; ok && !source.Equal(typ) {
			diagnostics = append(diagnostics, model.Diagnostic{Position: model.Position{File: block.file, Line: block.line, Column: 1}, Message: fmt.Sprintf("configured parameter $%s type %s conflicts with DECLARE type %s", name, typ.String(), source.String())})
			continue
		}
		resolved[name] = typ
	}
	return resolved, diagnostics
}

func localBindings(catalog model.Catalog, block queryBlock, tree queryTree, declared, inferred map[string]model.Type, syntax *model.QuerySyntax) (map[int]bool, map[string]bool, map[string]model.Type, map[string]*model.Table, map[string]lambdaBinding, []model.Diagnostic) {
	positions := map[int]bool{}
	names := map[string]bool{}
	types := map[string]model.Type{}
	tabular := map[string]*model.Table{}
	lambdas := map[string]lambdaBinding{}
	var diagnostics []model.Diagnostic
	block.tabular = tabular
	block.lambdas = lambdas
	for _, statement := range tree.named {
		if statement.Bind_parameter_list() == nil {
			continue
		}
		var lhs []parser.IBind_parameterContext
		descendants(statement.Bind_parameter_list(), func(node antlr.Tree) {
			if bind, ok := node.(*parser.Bind_parameterContext); ok && bind.GetStart() != nil {
				lhs = append(lhs, bind)
				positions[bind.GetStart().GetStart()] = true
				names[bindName(bind)] = true
			}
		})
		if len(lhs) != 1 {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, statement, "only single local assignments are supported"))
			continue
		}
		name := bindName(lhs[0])
		if _, exists := declared[name]; exists {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, statement, fmt.Sprintf("local $%s conflicts with a DECLARE parameter", name)))
			continue
		}
		if _, exists := types[name]; exists || tabular[name] != nil {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, statement, fmt.Sprintf("local $%s is assigned more than once", name)))
			continue
		}
		if core, partial, tabularSource, err := localSelectCore(statement); tabularSource {
			if err != nil {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, statement, err.Error()))
				continue
			}
			bindings := maps.Clone(declared)
			maps.Copy(bindings, types)
			for bindingName, typeValue := range inferred {
				if _, exists := bindings[bindingName]; !exists && typeValue.Kind != "" {
					bindings[bindingName] = typeValue
				}
			}
			columns, ds := analyzeSelectCore(catalog, block, core, partial, bindings, inferred, syntax, selectProjection)
			diagnostics = append(diagnostics, ds...)
			if len(ds) == 0 {
				var err error
				tabular[name], err = tabularTable(name, columns)
				if err != nil {
					diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, statement, err.Error()))
				}
			}
			continue
		}
		if statement.Expr() == nil {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, statement, "only scalar expressions or SELECT local assignments are supported"))
			continue
		}
		if lambda := directLambda(statement.Expr()); lambda != nil {
			bindings := maps.Clone(declared)
			maps.Copy(bindings, types)
			for bindingName, typeValue := range inferred {
				if _, exists := bindings[bindingName]; !exists && typeValue.Kind != "" {
					bindings[bindingName] = typeValue
				}
			}
			lambdas[name] = lambdaBinding{lambda: lambda, bindings: bindings, lambdas: maps.Clone(lambdas)}
			types[name] = model.Type{Kind: "Lambda"}
			continue
		}
		if containsAggregate(statement.Expr()) {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, statement.Expr(), "aggregate functions are not allowed in scalar local assignments"))
			continue
		}
		var rhsBinds []parser.IBind_parameterContext
		descendants(statement.Expr(), func(node antlr.Tree) {
			if bind, ok := node.(*parser.Bind_parameterContext); ok {
				rhsBinds = append(rhsBinds, bind)
			}
		})
		if len(rhsBinds) == 1 && sameOrWrappedExpression(statement.Expr(), rhsBinds[0]) {
			rhsName := bindName(rhsBinds[0])
			typeValue, ok := types[rhsName]
			if !ok {
				typeValue, ok = declared[rhsName]
			}
			if !ok {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, statement.Expr(), fmt.Sprintf("cannot resolve local $%s from $%s; declare the external parameter first", name, rhsName)))
				continue
			}
			types[name] = typeValue
			if typeValue.Kind == "Lambda" {
				lambdas[name] = lambdas[rhsName]
			}
			continue
		}
		bindings := make(map[string]model.Type, len(declared)+len(types))
		for bindingName, typeValue := range declared {
			bindings[bindingName] = typeValue
		}
		for bindingName, typeValue := range types {
			bindings[bindingName] = typeValue
		}
		typeValue, err := resolveExpression(statement.Expr(), expressionScope{bindings: bindings, lambdas: lambdas, functions: block.functions})
		if err == nil {
			types[name] = typeValue
			continue
		}
		diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, statement.Expr(), fmt.Sprintf("cannot resolve type of local $%s: %v", name, err)))
	}
	return positions, names, types, tabular, lambdas, diagnostics
}

func localSelectCore(statement *parser.Named_nodes_stmtContext) (*parser.Select_coreContext, parser.ISelect_kind_partialContext, bool, error) {
	var partial parser.ISelect_kind_partialContext
	if unparenthesized := statement.Select_unparenthesized_stmt(); unparenthesized != nil {
		if unparenthesized.Cte_with_clause() != nil {
			return nil, nil, true, fmt.Errorf("CTEs in tabular local assignments are not yet supported")
		}
		compound := unparenthesized.Select_unparenthesized_stmt_core()
		if compound == nil || len(compound.AllSelect_stmt_intersect()) != 0 || len(compound.AllUnion_op()) != 0 || compound.Select_unparenthesized_stmt_intersect() == nil || len(compound.Select_unparenthesized_stmt_intersect().AllIntersect_op()) != 0 {
			return nil, nil, true, fmt.Errorf("tabular local assignments support one SELECT input")
		}
		partial = compound.Select_unparenthesized_stmt_intersect().Select_kind_partial()
	} else if statement.Expr() != nil {
		var selected *parser.Select_subexprContext
		descendants(statement.Expr(), func(node antlr.Tree) {
			if sub, ok := node.(*parser.Select_subexprContext); ok && selected == nil {
				selected = sub
			}
		})
		if selected == nil {
			return nil, nil, false, nil
		}
		if !sameOrWrappedExpression(statement.Expr(), selected) {
			return nil, nil, false, nil
		}
		compound := selected.Select_subexpr_core()
		if selected.Cte_with_clause() != nil || compound == nil || len(compound.AllSelect_subexpr_intersect()) != 1 || len(compound.AllUnion_op()) != 0 || len(compound.Select_subexpr_intersect(0).AllIntersect_op()) != 0 || len(compound.Select_subexpr_intersect(0).AllSelect_or_expr()) != 1 {
			return nil, nil, true, fmt.Errorf("tabular local assignments support one SELECT input without CTE, UNION, or INTERSECT")
		}
		partial = compound.Select_subexpr_intersect(0).Select_or_expr(0).Select_kind_partial()
		if partial == nil {
			return nil, nil, false, nil
		}
	} else {
		return nil, nil, false, nil
	}
	if partial == nil || partial.Select_kind() == nil || partial.Select_kind().DISCARD() != nil || partial.Select_kind().INTO() != nil {
		return nil, nil, true, fmt.Errorf("tabular local assignments require SELECT without DISCARD or INTO RESULT")
	}
	core, ok := partial.Select_kind().Select_core().(*parser.Select_coreContext)
	if !ok || core == nil {
		return nil, nil, true, fmt.Errorf("tabular local assignments require SELECT")
	}
	return core, partial, true, nil
}

func tabularTable(name string, columns []model.Column) (*model.Table, error) {
	table := &model.Table{Name: "$" + name}
	seen := map[string]bool{}
	for _, column := range columns {
		resultName := column.ResultName()
		if resultName == "" || seen[resultName] {
			return nil, fmt.Errorf("tabular binding $%s has duplicate or unnamed result column %q; use unique AS aliases", name, resultName)
		}
		seen[resultName] = true
		table.Columns = append(table.Columns, model.Column{Name: resultName, Type: column.Type})
	}
	return table, nil
}

type relation struct {
	table    *model.Table
	alias    string
	optional bool
	physical bool
}

func selectRelations(catalog model.Catalog, block queryBlock, selectCore *parser.Select_coreContext, bindings, inferred map[string]model.Type, syntax *model.QuerySyntax) ([]relation, []model.Diagnostic) {
	var relations []relation
	var diagnostics []model.Diagnostic
	for _, join := range selectCore.AllJoin_source() {
		base := len(relations)
		for i, source := range join.AllFlatten_source() {
			if source.FLATTEN() != nil {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, source, "FLATTEN sources are not yet supported"))
				continue
			}
			named := source.Named_single_source()
			if named == nil || named.Hinted_single_source() == nil || named.Hinted_single_source().Single_source() == nil {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, source, "unsupported FROM or JOIN source"))
				continue
			}
			single := named.Hinted_single_source().Single_source()
			if named.Hinted_single_source().Table_hints() != nil || named.Sample_clause() != nil || named.Tablesample_clause() != nil {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, source, "table hints and sampling are not yet supported"))
				continue
			}
			var table *model.Table
			tableRef := single.Table_ref()
			if nested := single.Select_stmt(); nested != nil {
				columns, ds := analyzeSelectRows(catalog, block, nested, bindings, inferred, syntax, selectProjection)
				diagnostics = append(diagnostics, ds...)
				if len(ds) != 0 {
					continue
				}
				var err error
				table, err = tabularTable("derived", columns)
				if err != nil {
					diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, single, err.Error()))
					continue
				}
			} else if tableRef == nil {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, single, "unsupported FROM or JOIN source"))
				continue
			} else if tableRef.Cluster_expr() != nil || tableRef.COMMAT() != nil {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, tableRef, "cluster-qualified and temporary table references are unsupported"))
				continue
			} else if bind := tableRef.Bind_parameter(); bind != nil {
				name := bindName(bind)
				table = block.tabular[name]
				if table == nil {
					message := fmt.Sprintf("unknown tabular binding $%s; assign a SELECT before using it in FROM or JOIN", name)
					if _, scalar := bindings[name]; scalar {
						message = fmt.Sprintf("local $%s is scalar; FROM and JOIN require a SELECT binding", name)
					}
					diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, tableRef, message))
					continue
				}
			} else if tableRef.Table_key() == nil {
				var err error
				table, err = asTableRelation(tableRef, bindings)
				if err != nil {
					diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, source, err.Error()))
					continue
				}
			} else {
				key := tableRef.Table_key()
				name := resolveTablePath(block.tablePathPrefix, tableKeyName(key))
				table = findTable(catalog, name)
				if table == nil {
					diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, source, fmt.Sprintf("unknown table %q", name)))
					continue
				}
				if view := key.View_name(); view != nil {
					if view.An_id() == nil {
						diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, view, "VIEW PRIMARY KEY is not yet supported; select the table without VIEW"))
						continue
					}
					index := identifier(view.An_id().GetText())
					if _, exists := catalogIndexPosition(*table, index); !exists {
						diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, view, fmt.Sprintf("unknown index %q on table %q", index, name)))
						continue
					}
				}
			}
			alias := table.Name
			if tableRef != nil && tableRef.Table_key() != nil {
				alias = tableKeyName(tableRef.Table_key())
			}
			if named.An_id() != nil {
				alias = identifier(named.An_id().GetText())
			}
			if named.An_id_as_compat() != nil {
				alias = identifier(named.An_id_as_compat().GetText())
			}
			if single.Select_stmt() != nil && named.An_id() == nil && named.An_id_as_compat() == nil && (len(join.AllFlatten_source()) > 1 || len(selectCore.AllJoin_source()) > 1) {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, source, "derived SELECT requires an explicit alias"))
				continue
			}
			if tableRef != nil && tableRef.Table_key() == nil && tableRef.Bind_parameter() == nil && named.An_id() == nil && named.An_id_as_compat() == nil && (len(join.AllFlatten_source()) > 1 || len(selectCore.AllJoin_source()) > 1) {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, source, "AS_TABLE in a join requires an explicit alias; use AS_TABLE($parameter) AS rows"))
				continue
			}
			for _, previous := range relations {
				if previous.alias == alias && (strings.HasPrefix(table.Name, "$") || strings.HasPrefix(previous.table.Name, "$") || named.An_id() != nil || named.An_id_as_compat() != nil) {
					diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, source, fmt.Sprintf("duplicate source alias %q", alias)))
				}
			}
			relations = append(relations, relation{table: table, alias: alias, physical: tableRef != nil && tableRef.Table_key() != nil})
			if i > 0 {
				op := strings.ToUpper(join.Join_op(i - 1).GetText())
				switch {
				case strings.Contains(op, "LEFT"):
					relations[len(relations)-1].optional = true
				case strings.Contains(op, "RIGHT"):
					for j := base; j < len(relations)-1; j++ {
						relations[j].optional = true
					}
				case strings.Contains(op, "FULL"):
					for j := base; j < len(relations); j++ {
						relations[j].optional = true
					}
				}
			}
		}
		for _, constraint := range join.AllJoin_constraint() {
			if constraint.USING() != nil {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, constraint, "JOIN USING is not yet supported; use an explicit ON condition"))
			}
		}
	}
	if len(relations) == 0 && len(selectCore.AllJoin_source()) != 0 && len(diagnostics) == 0 {
		diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, selectCore, "SELECT without a catalog table is unsupported"))
	}
	return relations, diagnostics
}

func selectProjection(block queryBlock, selectCore *parser.Select_coreContext, relations []relation, declared map[string]model.Type) ([]model.Column, []model.Diagnostic) {
	return selectProjectionMode(block, selectCore, relations, declared, true)
}

func selectDMLProjection(block queryBlock, selectCore *parser.Select_coreContext, relations []relation, declared map[string]model.Type) ([]model.Column, []model.Diagnostic) {
	return selectProjectionMode(block, selectCore, relations, declared, false)
}

func selectProjectionMode(block queryBlock, selectCore *parser.Select_coreContext, relations []relation, declared map[string]model.Type, namedResult bool) ([]model.Column, []model.Diagnostic) {
	var columns []model.Column
	var diagnostics []model.Diagnostic
	var unnamed []implicitProjection
	hasWildcard := false
	emptyWildcards := 0
	excluded, withoutDiagnostics := selectWithoutColumns(block, selectCore, relations)
	if len(withoutDiagnostics) != 0 {
		return nil, withoutDiagnostics
	}
	for ordinal, result := range selectCore.AllResult_column() {
		if result.ASTERISK() != nil {
			hasWildcard = true
			prefix := identifier(strings.TrimSuffix(result.Opt_id_prefix().GetText(), "."))
			matched := false
			var expressions []string
			for _, rel := range relations {
				if prefix != "" && prefix != rel.alias {
					continue
				}
				matched = true
				for _, column := range rel.table.Columns {
					if excluded[withoutColumn{rel.alias, column.Name}] {
						continue
					}
					columns = append(columns, joinedColumn(column, rel.optional))
					expression := quotedYQLIdentifier(column.Name)
					if prefix != "" || len(relations) > 1 {
						// The original qualifier before the first '*' is retained,
						// including its whitespace and comments. Aliases preserve
						// the unqualified result keys produced by table wildcards.
						if prefix == "" || len(expressions) != 0 {
							expression = quotedYQLIdentifier(rel.alias) + "." + expression
						}
						expression += " AS " + quotedYQLIdentifier(column.Name)
					}
					expressions = append(expressions, expression)
				}
			}
			if !matched {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, result, fmt.Sprintf("unknown table or alias %q", prefix)))
			} else if block.wildcards != nil {
				if len(expressions) == 0 {
					emptyWildcards++
					block.wildcards.removeResultColumn(selectCore, ordinal)
				} else {
					block.wildcards.add(result.ASTERISK().GetSymbol(), expressions)
				}
			}
			continue
		}
		expr := result.Expr()
		if invoke, ok := directEmbedCall(expr); ok {
			hasWildcard = true
			embedColumns, embedding, expressions, err := embedProjection(block, selectCore, result, invoke, relations, ordinal, len(columns))
			if err != nil {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, expr, err.Error()))
				continue
			}
			columns = append(columns, embedColumns...)
			block.wildcards.embeds = append(block.wildcards.embeds, embedding)
			block.wildcards.usedEmbeds[expr.GetStart().GetStart()] = true
			for _, ref := range columnRefs(expr) {
				block.wildcards.usedEmbedArgs[ref.ctx.GetStart().GetStart()] = true
			}
			block.wildcards.addExpression(expr, expressions)
			continue
		}
		column, pure, err := expressionColumn(expr, relations, declared, selectCore.Group_by_clause() != nil, block.functions, block.lambdas)
		if err != nil {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, expr, err.Error()))
			continue
		}
		alias := ""
		if result.An_id_or_type() != nil {
			alias = identifier(result.An_id_or_type().GetText())
		}
		if result.An_id_as_compat() != nil {
			alias = identifier(result.An_id_as_compat().GetText())
		}
		if alias != "" {
			column.Name = alias
			column.WireName = ""
		}
		if alias == "" && !pure && namedResult {
			unnamed = append(unnamed, implicitProjection{column: len(columns), ordinal: ordinal, expression: expr})
		}
		if alias == "" && pure && len(relations) > 1 {
			refs := columnRefs(expr)
			if len(refs) == 1 && refs[0].qualifier != "" {
				column.WireName = qualifiedName(refs[0])
			}
		}
		columns = append(columns, column)
	}
	if selectCore.Without_column_list() != nil && len(columns) == 0 {
		diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, selectCore.Without_column_list(), "SELECT WITHOUT removes every result column; generated clients require at least one column"))
	}
	if emptyWildcards > 1 {
		diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, selectCore.Without_column_list(), "SELECT WITHOUT removes every column from multiple wildcard projections; select explicit columns"))
	}
	if block.wildcards != nil && len(block.wildcards.embeds) != 0 {
		if len(unnamed) != 0 {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, selectCore, "sqlc.embed with computed result expressions requires explicit AS aliases"))
		}
		seen := map[string]bool{}
		for _, column := range columns {
			name := column.ResultName()
			if seen[name] {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, selectCore, fmt.Sprintf("sqlc.embed result column %q collides with another projection; use a unique AS alias", name)))
			}
			seen[name] = true
		}
	}
	if namedResult {
		nameImplicitProjections(columns, unnamed, hasWildcard, block.wildcards)
	}
	return columns, diagnostics
}

type withoutColumn struct{ alias, column string }

func selectWithoutColumns(block queryBlock, core *parser.Select_coreContext, relations []relation) (map[withoutColumn]bool, []model.Diagnostic) {
	list := core.Without_column_list()
	if list == nil {
		return nil, nil
	}
	ifExists := core.IF() != nil
	candidates := map[withoutColumn]bool{}
	for _, result := range core.AllResult_column() {
		if result.ASTERISK() == nil {
			continue
		}
		prefix := identifier(strings.TrimSuffix(result.Opt_id_prefix().GetText(), "."))
		for _, rel := range relations {
			if prefix != "" && prefix != rel.alias {
				continue
			}
			for _, column := range rel.table.Columns {
				candidates[withoutColumn{rel.alias, column.Name}] = true
			}
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	excluded := map[withoutColumn]bool{}
	var diagnostics []model.Diagnostic
	for _, name := range list.AllWithout_column_name() {
		qualifier, column := "", ""
		if name.DOT() != nil {
			qualifier, column = identifier(name.An_id(0).GetText()), identifier(name.An_id(1).GetText())
		} else {
			column = identifier(name.An_id_without().GetText())
		}
		if qualifier == "" && len(relations) > 1 {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, name, fmt.Sprintf("SELECT WITHOUT column %q in a JOIN requires a table alias", column)))
			continue
		}
		var match withoutColumn
		for key := range candidates {
			if key.column != column || qualifier != "" && qualifier != key.alias {
				continue
			}
			match = key
			break
		}
		if match.column == "" {
			if !ifExists {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, name, fmt.Sprintf("unknown SELECT WITHOUT column %q", name.GetText())))
			}
		} else if excluded[match] {
			if !ifExists {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, name, fmt.Sprintf("duplicate SELECT WITHOUT column %q", name.GetText())))
			}
		} else {
			excluded[match] = true
		}
	}
	if len(diagnostics) == 0 && block.wildcards != nil {
		block.wildcards.addWithout(core)
	}
	return excluded, diagnostics
}

func expressionColumn(expr parser.IExprContext, relations []relation, declared map[string]model.Type, grouped bool, functions *builtins.Registry, lambdas map[string]lambdaBinding) (model.Column, bool, error) {
	for inner := parenthesizedExpression(expr); inner != nil; inner = parenthesizedExpression(expr) {
		expr = inner
	}
	scope := expressionScope{relations: relations, bindings: declared, lambdas: lambdas, grouped: grouped, functions: functions}
	if typeValue, ok, err := resolveMemberAccess(expr, scope); ok {
		return model.Column{Type: typeValue}, false, err
	}
	refs := columnRefs(expr)
	if len(refs) == 1 && isPureColumnExpression(expr) {
		column, err := resolveColumn(relations, refs[0])
		if err != nil {
			return model.Column{}, false, err
		}
		return column, true, nil
	}
	typeValue, err := resolveExpression(expr, scope)
	return model.Column{Type: typeValue}, false, err
}

func concatenationType(root antlr.ParserRuleContext, scope expressionScope) (model.Type, bool, error) {
	operation, ok := coveringExpressionContext(root).(*parser.Mul_subexprContext)
	if !ok || len(operation.AllDOUBLE_PIPE()) == 0 {
		return model.Type{}, false, nil
	}

	operands := operation.AllCon_subexpr()
	var result model.Type
	optional := false
	null := false
	for _, operand := range operands {
		typeValue, err := resolveScalarNode(operand, scope)
		if err != nil {
			return model.Type{}, true, err
		}
		if typeValue.Kind == "Null" {
			null = true
			continue
		}
		optional = optional || typeValue.IsOptional()
		base := typeValue.UnwrapOptional()
		if base.Kind != "String" && base.Kind != "Utf8" {
			return model.Type{}, true, fmt.Errorf("concatenation operand %s has unsupported type %s", operand.GetText(), typeValue.String())
		}
		if result.Kind == "" {
			result = base
		} else if !result.Equal(base) {
			return model.Type{}, true, fmt.Errorf("concatenation operands must both be String or both be Utf8")
		}
	}
	if null {
		return model.Type{Kind: "Null"}, true, nil
	}
	if optional {
		result = model.Optional(result)
	}
	return result, true, nil
}

type columnRef struct {
	qualifier, name string
	ctx             antlr.ParserRuleContext
}

func columnRefs(root antlr.Tree) []columnRef {
	var refs []columnRef
	scopeDescendants(root, func(node antlr.Tree) {
		ctx, ok := node.(*parser.Unary_subexprContext)
		if !ok || structFieldLabel(ctx) {
			return
		}
		casual := ctx.Unary_casual_subexpr()
		if casual == nil || casual.Id_expr() == nil || casual.Unary_subexpr_suffix() == nil {
			return
		}
		suffix := casual.Unary_subexpr_suffix()
		if len(suffix.AllInvoke_expr()) != 0 {
			return
		}
		base := identifier(casual.Id_expr().GetText())
		ids := suffix.AllAn_id_or_type()
		switch len(ids) {
		case 0:
			if functionTypeArgument(ctx) {
				return
			}
			refs = append(refs, columnRef{name: base, ctx: ctx})
		case 1:
			refs = append(refs, columnRef{qualifier: base, name: identifier(ids[0].GetText()), ctx: ctx})
		}
	})
	return refs
}

func structFieldLabel(root antlr.ParserRuleContext) bool {
	for parent := root.GetParent(); parent != nil; parent = parent.GetParent() {
		list, ok := parent.(*parser.Expr_struct_listContext)
		if !ok {
			continue
		}
		for i, expr := range list.AllExpr() {
			if i%2 == 0 && expr.GetStart().GetStart() <= root.GetStart().GetStart() && root.GetStop().GetStop() <= expr.GetStop().GetStop() {
				return true
			}
		}
		return false
	}
	return false
}

func functionTypeArgument(ref *parser.Unary_subexprContext) bool {
	for parent := ref.GetParent(); parent != nil; parent = parent.GetParent() {
		named, ok := parent.(*parser.Named_exprContext)
		if !ok {
			continue
		}
		if named.Expr() == nil || !sameSpan(named.Expr(), ref) {
			return false
		}
		if _, err := parseType(named.Expr().GetText()); err != nil {
			return false
		}
		for call := named.GetParent(); call != nil; call = call.GetParent() {
			unary, ok := call.(*parser.Unary_subexprContext)
			if !ok {
				continue
			}
			name, invoke, found := functionCallFromUnary(unary)
			if !found {
				return false
			}
			if strings.EqualFold(name, "ListCreate") {
				return true
			}
			if name == "Yson::ConvertTo" && invoke.Named_expr_list() != nil {
				arguments := invoke.Named_expr_list().AllNamed_expr()
				return len(arguments) >= 2 && arguments[1] == named
			}
			return false
		}
		return false
	}
	return false
}

func isPureColumnExpression(expr parser.IExprContext) bool {
	refs := columnRefs(expr)
	if len(refs) != 1 {
		return false
	}
	return expr.GetStart() == refs[0].ctx.GetStart() && expr.GetStop() == refs[0].ctx.GetStop()
}

func qualifiedName(ref columnRef) string {
	if ref.qualifier == "" {
		return ref.name
	}
	return ref.qualifier + "." + ref.name
}

func validateColumnReferences(block queryBlock, root antlr.Tree, relations []relation, projection []model.Column) []model.Diagnostic {
	var diagnostics []model.Diagnostic
	seen := map[int]bool{}
	for _, ref := range columnRefs(root) {
		position := ref.ctx.GetStart().GetStart()
		if block.wildcards != nil && block.wildcards.usedEmbedArgs[position] {
			continue
		}
		if seen[position] {
			continue
		}
		seen[position] = true
		if ref.qualifier == "" && isOrderByReference(ref.ctx) {
			found := false
			for _, column := range projection {
				found = found || column.ResultName() == ref.name
			}
			if found {
				continue
			}
		}
		if _, err := resolveColumn(relations, ref); err != nil {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, ref.ctx, err.Error()))
		}
	}
	return diagnostics
}

func isOrderByReference(root antlr.Tree) bool {
	for node := root.GetParent(); node != nil; node = node.GetParent() {
		switch node.(type) {
		case *parser.Order_by_clauseContext:
			return true
		case *parser.Select_coreContext:
			return false
		}
	}
	return false
}

func resolveColumn(relations []relation, ref columnRef) (model.Column, error) {
	var matches []model.Column
	for _, rel := range relations {
		if ref.qualifier != "" && ref.qualifier != rel.alias {
			continue
		}
		for _, column := range rel.table.Columns {
			if column.Name == ref.name {
				matches = append(matches, joinedColumn(column, rel.optional))
			}
		}
	}
	if len(matches) == 0 {
		return model.Column{}, fmt.Errorf("unknown column %q", qualifiedName(ref))
	}
	if len(matches) > 1 {
		return model.Column{}, fmt.Errorf("ambiguous column %q", ref.name)
	}
	return matches[0], nil
}

func joinedColumn(column model.Column, optional bool) model.Column {
	if optional && !column.Type.IsOptional() {
		column.Type = model.Optional(column.Type)
	}
	return column
}

func inferFromComparisons(root antlr.Tree, relations []relation, inferred map[string]model.Type) {
	scopeDescendants(root, func(root antlr.Tree) {
		switch node := root.(type) {
		case *parser.Xor_subexprContext:
			if condition := node.Cond_expr(); condition != nil && condition.BETWEEN() != nil {
				refs := columnRefs(node.Eq_subexpr())
				if len(refs) != 1 || !sameOrWrappedExpression(node.Eq_subexpr(), refs[0].ctx) {
					return
				}
				column, err := resolveColumn(relations, refs[0])
				if err != nil {
					return
				}
				for _, bound := range condition.AllEq_subexpr() {
					scopeDescendants(bound, func(node antlr.Tree) {
						if bind, ok := node.(parser.IBind_parameterContext); ok && sameOrWrappedExpression(bound, bind) {
							inferParameter(inferred, bindName(bind), column.Type)
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
		var binds []parser.IBind_parameterContext
		scopeDescendants(root, func(node antlr.Tree) {
			if bind, ok := node.(*parser.Bind_parameterContext); ok {
				binds = append(binds, bind)
			}
		})
		if len(binds) != 1 || !isDirectComparison(root, refs[0], binds[0]) {
			return
		}
		column, err := resolveColumn(relations, refs[0])
		if err != nil {
			return
		}
		inferParameter(inferred, bindName(binds[0]), column.Type)
	})
}

func isDirectComparison(root antlr.Tree, ref columnRef, bind parser.IBind_parameterContext) bool {
	left, right := ref.ctx.GetText(), bind.GetText()
	text := root.(interface{ GetText() string }).GetText()
	for _, operator := range []string{"=", "==", "!=", "<>", "<", "<=", ">", ">="} {
		if text == left+operator+right || text == right+operator+left {
			return true
		}
	}
	return false
}

func inferInsert(block queryBlock, statement *parser.Into_table_stmtContext, table *model.Table, bindings, inferred map[string]model.Type) []model.Diagnostic {
	source := statement.Into_values_source()
	if source == nil || source.Values_source() == nil || source.Values_source().Values_stmt() == nil || source.Pure_column_list() == nil {
		return []model.Diagnostic{diagnosticAt(block.file, block.line-1, statement, "INSERT/UPSERT currently requires an explicit column list and VALUES rows")}
	}
	ids := source.Pure_column_list().AllAn_id()
	rows := source.Values_source().Values_stmt().Values_source_row_list().AllValues_source_row()
	var diagnostics []model.Diagnostic
	for _, row := range rows {
		if row.Expr_list() == nil {
			continue
		}
		expressions := directExprs(row.Expr_list())
		if len(expressions) != len(ids) {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, row, fmt.Sprintf("VALUES has %d expressions for %d target columns", len(expressions), len(ids))))
			continue
		}
		for i, id := range ids {
			column := tableColumn(table, identifier(id.GetText()))
			if column == nil {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, id, fmt.Sprintf("unknown column %q", id.GetText())))
				continue
			}
			if err := validateDMLValue(expressions[i], *column, expressionScope{bindings: bindings, lambdas: block.lambdas, functions: block.functions}, inferred); err != nil {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, expressions[i], err.Error()))
			}
		}
	}
	return diagnostics
}

func inferUpdate(block queryBlock, statement *parser.Update_stmtContext, table *model.Table, bindings, inferred map[string]model.Type) []model.Diagnostic {
	var diagnostics []model.Diagnostic
	if statement.Set_clause_choice() == nil || statement.Set_clause_choice().Set_clause_list() == nil {
		return []model.Diagnostic{diagnosticAt(block.file, block.line-1, statement, "only individual UPDATE SET assignments are supported")}
	}
	var clauses []*parser.Set_clauseContext
	descendants(statement.Set_clause_choice().Set_clause_list(), func(node antlr.Tree) {
		if ctx, ok := node.(*parser.Set_clauseContext); ok {
			clauses = append(clauses, ctx)
		}
	})
	seen := map[string]bool{}
	for _, clause := range clauses {
		if clause.Set_target() == nil || clause.Set_target().Column_name() == nil || clause.Expr() == nil {
			continue
		}
		name := identifier(clause.Set_target().Column_name().An_id().GetText())
		if seen[name] {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, clause.Set_target(), fmt.Sprintf("duplicate UPDATE SET column %q; combine the expressions into one assignment", name)))
			continue
		}
		seen[name] = true
		column := tableColumn(table, name)
		if column == nil {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, clause, fmt.Sprintf("unknown column %q", name)))
			continue
		}
		if err := validateDMLValue(clause.Expr(), *column, expressionScope{relations: []relation{{table: table, alias: simpleTableName(statement.Simple_table_ref())}}, bindings: bindings, lambdas: block.lambdas, functions: block.functions}, inferred); err != nil {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, clause.Expr(), err.Error()))
		}
	}
	return diagnostics
}

func directExprs(root antlr.Tree) []parser.IExprContext {
	var expressions []parser.IExprContext
	for i := 0; i < root.GetChildCount(); i++ {
		if expr, ok := root.GetChild(i).(parser.IExprContext); ok {
			expressions = append(expressions, expr)
		}
	}
	return expressions
}

func inferDirectDMLBind(root antlr.ParserRuleContext, typeValue model.Type, inferred map[string]model.Type) bool {
	if bind := directBind(root); bind != nil {
		inferParameter(inferred, bindName(bind), typeValue)
		return true
	}
	return false
}

func inferParameter(inferred map[string]model.Type, name string, typeValue model.Type) {
	previous, ok := inferred[name]
	if !ok || previous.Equal(typeValue) {
		inferred[name] = typeValue
		return
	}
	if previous.Kind == "" {
		return
	}
	if previous.UnwrapOptional().Equal(typeValue.UnwrapOptional()) {
		if previous.IsOptional() && !typeValue.IsOptional() {
			inferred[name] = typeValue
		}
		return
	}
	inferred[name] = model.Type{}
}

func externalParameters(block queryBlock, binds []parser.IBind_parameterContext, declared, inferred map[string]model.Type, declarationPositions, localPositions map[int]bool, localNames map[string]bool) ([]model.Parameter, []model.Diagnostic) {
	sort.SliceStable(binds, func(i, j int) bool { return binds[i].GetStart().GetStart() < binds[j].GetStart().GetStart() })
	seen := map[string]bool{}
	var parameters []model.Parameter
	var diagnostics []model.Diagnostic
	for _, bind := range binds {
		if bind.GetStart() == nil || localPositions[bind.GetStart().GetStart()] {
			continue
		}
		name := bindName(bind)
		if localNames[name] && !declarationPositions[bind.GetStart().GetStart()] {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		typeValue, ok := declared[name]
		if !ok {
			typeValue, ok = inferred[name]
		}
		if ok && typeValue.Kind == "" {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, bind, fmt.Sprintf("external parameter $%s has incompatible inferred types; add DECLARE to specify its intended type", name)))
			continue
		}
		if !ok {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, bind, fmt.Sprintf("cannot resolve type of external parameter $%s; add DECLARE", name)))
			continue
		}
		if inferredType, inferredOK := inferred[name]; inferredOK && !compatibleTypes(typeValue, inferredType) {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, bind, fmt.Sprintf("parameter $%s declared as %s but used with %s", name, typeValue.String(), inferredType.String())))
			continue
		}
		parameters = append(parameters, model.Parameter{Name: name, Type: typeValue})
	}
	return parameters, diagnostics
}

func returningProjection(block queryBlock, returning parser.IReturning_columns_listContext, table *model.Table) ([]model.Column, []model.Diagnostic) {
	if returning.ASTERISK() != nil {
		if block.wildcards != nil {
			columns := make([]string, len(table.Columns))
			for i, column := range table.Columns {
				columns[i] = quotedYQLIdentifier(column.Name)
			}
			block.wildcards.add(returning.ASTERISK().GetSymbol(), columns)
		}
		return append([]model.Column(nil), table.Columns...), nil
	}
	var columns []model.Column
	var diagnostics []model.Diagnostic
	for _, id := range returning.AllAn_id() {
		column := tableColumn(table, identifier(id.GetText()))
		if column == nil {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, id, fmt.Sprintf("unknown RETURNING column %q", id.GetText())))
			continue
		}
		columns = append(columns, *column)
	}
	return columns, diagnostics
}

func targetTable(catalog model.Catalog, block queryBlock, name string, ctx antlr.ParserRuleContext, diagnostics []model.Diagnostic) (*model.Table, []model.Diagnostic) {
	name = resolveTablePath(block.tablePathPrefix, name)
	table := findTable(catalog, name)
	if table == nil {
		diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, ctx, fmt.Sprintf("unknown table %q", name)))
	}
	return table, diagnostics
}

func intoTableName(ctx parser.IInto_table_stmtContext) string {
	if ctx == nil || ctx.Into_simple_table_ref() == nil || ctx.Into_simple_table_ref().Simple_table_ref() == nil {
		return ""
	}
	return simpleTableName(ctx.Into_simple_table_ref().Simple_table_ref())
}

func simpleTableName(ctx parser.ISimple_table_refContext) string {
	if ctx == nil || ctx.Simple_table_ref_core() == nil {
		return ""
	}
	return identifier(ctx.Simple_table_ref_core().GetText())
}

func tableKeyName(ctx parser.ITable_keyContext) string {
	return identifier(ctx.Id_table_or_type().GetText())
}

func findTable(catalog model.Catalog, name string) *model.Table {
	if i, ok := catalogTableIndex(catalog, name); ok {
		return &catalog.Tables[i]
	}
	return nil
}

func tableColumn(table *model.Table, name string) *model.Column {
	if i, ok := catalogColumnIndex(*table, name); ok {
		return &table.Columns[i]
	}
	return nil
}

func compatibleTypes(left, right model.Type) bool {
	return left.Equal(right) || right.IsOptional() && left.Equal(right.UnwrapOptional())
}

func recordColumnBindings(syntax *model.QuerySyntax, root antlr.Tree, relations []relation) {
	var bindings []model.TableBinding
	for _, relation := range relations {
		bindings = append(bindings, model.TableBinding{Table: relation.table.Name, Alias: relation.alias})
	}
	if core, ok := root.(*parser.Select_coreContext); ok {
		syntax.Selects[core.GetStart().GetTokenIndex()] = model.SelectBinding{Relations: bindings}
	}
	nested := false
	for parent := root.GetParent(); parent != nil; parent = parent.GetParent() {
		if _, ok := parent.(*parser.In_exprContext); ok {
			nested = true
			break
		}
	}
	if !nested {
		syntax.Relations = append(syntax.Relations, bindings...)
	}
	for _, ref := range columnRefs(root) {
		for _, relation := range relations {
			if ref.qualifier != "" && ref.qualifier != relation.alias {
				continue
			}
			if column := tableColumn(relation.table, ref.name); column != nil {
				syntax.Columns[ref.ctx.GetStart().GetTokenIndex()] = model.ColumnBinding{
					TableBinding: model.TableBinding{Table: relation.table.Name, Alias: relation.alias}, Column: *column,
				}
				break
			}
		}
	}
}
