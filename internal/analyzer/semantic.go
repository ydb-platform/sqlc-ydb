package analyzer

import (
	"fmt"
	"sort"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-engine-ydb/internal/model"
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
	conds      []*parser.Cond_exprContext
	eqs        []*parser.Eq_subexprContext
	xors       []*parser.Xor_subexprContext
}

func collectQueryTree(tree antlr.Tree) queryTree {
	var out queryTree
	descendants(tree, func(node antlr.Tree) {
		switch ctx := node.(type) {
		case *parser.Sql_stmtContext:
			out.statements = append(out.statements, ctx)
		case *parser.Declare_stmtContext:
			out.declares = append(out.declares, ctx)
		case *parser.Named_nodes_stmtContext:
			out.named = append(out.named, ctx)
		case *parser.Into_table_stmtContext:
			out.insert = append(out.insert, ctx)
		case *parser.Update_stmtContext:
			out.updates = append(out.updates, ctx)
		case *parser.Delete_stmtContext:
			out.deletes = append(out.deletes, ctx)
		case *parser.Bind_parameterContext:
			out.binds = append(out.binds, ctx)
		case *parser.Cond_exprContext:
			out.conds = append(out.conds, ctx)
		case *parser.Eq_subexprContext:
			out.eqs = append(out.eqs, ctx)
		case *parser.Xor_subexprContext:
			out.xors = append(out.xors, ctx)
		}
	})
	return out
}

func analyzeQuery(catalog model.Catalog, block queryBlock) (model.AnalyzedQuery, []model.Diagnostic) {
	parsed, diagnostics := parseYQL(block.file, block.text, block.line-1)
	query := model.AnalyzedQuery{
		Name: block.name, Command: block.command, SQL: block.text,
		Source: model.Position{File: block.file, Line: block.line, Column: 1},
	}
	if len(diagnostics) != 0 {
		return query, diagnostics
	}
	tree := collectQueryTree(parsed.tree)
	query.SQLWithoutDeclarations = withoutDeclarations(block.text, parsed.tokens, tree.declares)
	if diagnostics = unsupportedSQLCMacroDiagnostics(block, parsed.tokens); len(diagnostics) != 0 {
		return query, diagnostics
	}
	diagnostics = append(diagnostics, validateQueryStatements(block, tree)...)
	selectStatement := topLevelSelect(tree.statements)
	dataStatements := len(tree.insert) + len(tree.updates) + len(tree.deletes)
	if selectStatement != nil {
		dataStatements++
	}
	if dataStatements != 1 {
		return query, []model.Diagnostic{diagnosticAt(block.file, block.line-1, parsed.tree, fmt.Sprintf("query must contain exactly one supported SELECT, INSERT/UPSERT, UPDATE, or DELETE statement; found %d", dataStatements))}
	}

	declared, declarationPositions, declarationDiagnostics := declarations(block, tree)
	diagnostics = append(diagnostics, declarationDiagnostics...)
	localPositions, localNames, localTypes, localDiagnostics := localBindings(block, tree, declared)
	diagnostics = append(diagnostics, localDiagnostics...)

	inferred := map[string]model.Type{}
	var relations []relation
	var resultColumns []model.Column
	var target *model.Table
	switch {
	case selectStatement != nil:
		bindings := make(map[string]model.Type, len(declared)+len(localTypes))
		for name, typeValue := range declared {
			bindings[name] = typeValue
		}
		for name, typeValue := range localTypes {
			bindings[name] = typeValue
		}
		var selectDiagnostics []model.Diagnostic
		var arms [][]model.Column
		var partials []parser.ISelect_kind_partialContext
		var cores []*parser.Select_coreContext
		cores, partials, selectDiagnostics = selectArms(block, selectStatement)
		diagnostics = append(diagnostics, selectDiagnostics...)
		for i, core := range cores {
			armRelations, relationDiagnostics := selectRelations(catalog, block, core)
			diagnostics = append(diagnostics, relationDiagnostics...)
			if len(relationDiagnostics) != 0 {
				continue
			}
			armTree := collectQueryTree(core)
			inferFromComparisons(armTree, armRelations, inferred)
			inferFromInLists(armTree.conds, armRelations, inferred)
			if i < len(partials) {
				inferLimitOffset(partials[i], inferred)
			}
			for name, typeValue := range inferred {
				if _, exists := bindings[name]; !exists && typeValue.Kind != "" {
					bindings[name] = typeValue
				}
			}
			columns, projectionDiagnostics := selectProjection(block, core, armRelations, bindings)
			diagnostics = append(diagnostics, projectionDiagnostics...)
			diagnostics = append(diagnostics, validateColumnReferences(block, core, armRelations)...)
			diagnostics = append(diagnostics, validateGrouping(block, core, armRelations, bindings)...)
			arms = append(arms, columns)
		}
		if len(arms) == len(cores) && len(arms) != 0 {
			resultColumns, selectDiagnostics = reconcileUnionColumns(block, selectStatement, arms)
			diagnostics = append(diagnostics, selectDiagnostics...)
		}
	case len(tree.insert) == 1:
		target, diagnostics = targetTable(catalog, block, intoTableName(tree.insert[0]), tree.insert[0], diagnostics)
		if target != nil {
			relations = []relation{{table: target, alias: target.Name}}
		}
	case len(tree.updates) == 1:
		target, diagnostics = targetTable(catalog, block, simpleTableName(tree.updates[0].Simple_table_ref()), tree.updates[0], diagnostics)
		if target != nil {
			relations = []relation{{table: target, alias: target.Name}}
		}
	case len(tree.deletes) == 1:
		target, diagnostics = targetTable(catalog, block, simpleTableName(tree.deletes[0].Simple_table_ref()), tree.deletes[0], diagnostics)
		if target != nil {
			relations = []relation{{table: target, alias: target.Name}}
		}
	}

	if selectStatement == nil && len(relations) != 0 {
		diagnostics = append(diagnostics, validateColumnReferences(block, parsed.tree, relations)...)
		inferFromComparisons(tree, relations, inferred)
	}
	if target != nil && len(tree.insert) == 1 {
		diagnostics = append(diagnostics, inferInsert(block, tree.insert[0], target, inferred)...)
	}
	if target != nil && len(tree.updates) == 1 {
		diagnostics = append(diagnostics, inferUpdate(block, tree.updates[0], target, inferred)...)
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

	parameters, parameterDiagnostics := externalParameters(block, tree.binds, declared, inferred, declarationPositions, localPositions, localNames)
	diagnostics = append(diagnostics, parameterDiagnostics...)
	query.Parameters = parameters

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
			resultColumns, parameterDiagnostics = returningProjection(block, returning, target)
			diagnostics = append(diagnostics, parameterDiagnostics...)
		}
	}

	for _, column := range resultColumns {
		if column.Type.Kind == "Null" {
			diagnostics = append(diagnostics, model.Diagnostic{Position: query.Source, Message: fmt.Sprintf("result column %q has unresolved Null type; cast it or combine it with a concrete compatible type", column.Name)})
		}
	}
	returnsRows := len(resultColumns) != 0
	if len(diagnostics) == 0 {
		if (block.command == model.One || block.command == model.Many) && !returnsRows {
			diagnostics = append(diagnostics, model.Diagnostic{Position: query.Source, Message: fmt.Sprintf("command %s requires a result set", block.command)})
		}
		if (block.command == model.Exec || block.command == model.ExecRows) && returnsRows {
			diagnostics = append(diagnostics, model.Diagnostic{Position: query.Source, Message: fmt.Sprintf("command %s cannot be used with a row-returning statement", block.command)})
		}
	}
	if returnsRows {
		query.ResultSets = []model.ResultSet{{Columns: resultColumns}}
	}
	return query, diagnostics
}

func validateQueryStatements(block queryBlock, tree queryTree) []model.Diagnostic {
	var diagnostics []model.Diagnostic
	mainStatements := 0
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
		case core.Declare_stmt() != nil:
		case core.Named_nodes_stmt() != nil:
		case core.Select_stmt() != nil, core.Into_table_stmt() != nil, core.Update_stmt() != nil, core.Delete_stmt() != nil:
			mainStatements++
		default:
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, statement, fmt.Sprintf("unsupported statement in named query: %q", statement.GetText())))
		}
	}
	if mainStatements != 1 {
		diagnostics = append(diagnostics, model.Diagnostic{Position: model.Position{File: block.file, Line: block.line, Column: 1}, Message: fmt.Sprintf("named query must have exactly one top-level data statement; found %d", mainStatements)})
	}
	return diagnostics
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

func localBindings(block queryBlock, tree queryTree, declared map[string]model.Type) (map[int]bool, map[string]bool, map[string]model.Type, []model.Diagnostic) {
	positions := map[int]bool{}
	names := map[string]bool{}
	types := map[string]model.Type{}
	var diagnostics []model.Diagnostic
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
		if len(lhs) != 1 || statement.Expr() == nil {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, statement, "only single scalar local assignments are supported"))
			continue
		}
		name := bindName(lhs[0])
		if _, exists := types[name]; exists {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, statement, fmt.Sprintf("local $%s is assigned more than once", name)))
			continue
		}
		var rhsBinds []parser.IBind_parameterContext
		descendants(statement.Expr(), func(node antlr.Tree) {
			if bind, ok := node.(*parser.Bind_parameterContext); ok {
				rhsBinds = append(rhsBinds, bind)
			}
		})
		if len(rhsBinds) == 1 && statement.Expr().GetText() == rhsBinds[0].GetText() {
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
			continue
		}
		bindings := make(map[string]model.Type, len(declared)+len(types))
		for bindingName, typeValue := range declared {
			bindings[bindingName] = typeValue
		}
		for bindingName, typeValue := range types {
			bindings[bindingName] = typeValue
		}
		typeValue, err := resolveExpression(statement.Expr(), expressionScope{bindings: bindings})
		if err == nil {
			types[name] = typeValue
			continue
		}
		diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, statement.Expr(), fmt.Sprintf("cannot resolve type of local $%s: %v", name, err)))
	}
	return positions, names, types, diagnostics
}

type relation struct {
	table    *model.Table
	alias    string
	optional bool
}

func selectRelations(catalog model.Catalog, block queryBlock, selectCore *parser.Select_coreContext) ([]relation, []model.Diagnostic) {
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
			if named == nil || named.Hinted_single_source() == nil || named.Hinted_single_source().Single_source() == nil || named.Hinted_single_source().Single_source().Table_ref() == nil {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, source, "only named catalog tables are supported in FROM and JOIN"))
				continue
			}
			tableRef := named.Hinted_single_source().Single_source().Table_ref()
			if named.Hinted_single_source().Table_hints() != nil || named.Sample_clause() != nil || named.Tablesample_clause() != nil {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, source, "table hints and sampling are not yet supported"))
				continue
			}
			if tableRef.Table_key() == nil {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, source, "dynamic table references are unsupported"))
				continue
			}
			name := identifier(tableRef.Table_key().GetText())
			table := findTable(catalog, name)
			if table == nil {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, source, fmt.Sprintf("unknown table %q", name)))
				continue
			}
			alias := table.Name
			if named.An_id() != nil {
				alias = identifier(named.An_id().GetText())
			}
			if named.An_id_as_compat() != nil {
				alias = identifier(named.An_id_as_compat().GetText())
			}
			relations = append(relations, relation{table: table, alias: alias})
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
	if len(relations) == 0 && len(selectCore.AllJoin_source()) != 0 {
		diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, selectCore, "SELECT without a catalog table is unsupported"))
	}
	return relations, diagnostics
}

func selectProjection(block queryBlock, selectCore *parser.Select_coreContext, relations []relation, declared map[string]model.Type) ([]model.Column, []model.Diagnostic) {
	var columns []model.Column
	var diagnostics []model.Diagnostic
	if selectCore.Without_column_list() != nil {
		return nil, []model.Diagnostic{diagnosticAt(block.file, block.line-1, selectCore.Without_column_list(), "SELECT WITHOUT is not yet supported")}
	}
	for _, result := range selectCore.AllResult_column() {
		if result.ASTERISK() != nil {
			prefix := strings.TrimSuffix(result.Opt_id_prefix().GetText(), ".")
			matched := false
			for _, rel := range relations {
				if prefix != "" && prefix != rel.alias && prefix != rel.table.Name {
					continue
				}
				matched = true
				for _, column := range rel.table.Columns {
					columns = append(columns, joinedColumn(column, rel.optional))
				}
			}
			if !matched {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, result, fmt.Sprintf("unknown table or alias %q", prefix)))
			}
			continue
		}
		expr := result.Expr()
		column, pure, err := expressionColumn(expr, relations, declared, selectCore.Group_by_clause() != nil)
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
		if alias == "" && !pure {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, result, "computed result expressions require an explicit AS alias"))
			continue
		}
		if alias == "" && pure && len(relations) > 1 {
			refs := columnRefs(expr)
			if len(refs) == 1 && refs[0].qualifier != "" {
				column.WireName = qualifiedName(refs[0])
			}
		}
		columns = append(columns, column)
	}
	return columns, diagnostics
}

func expressionColumn(expr parser.IExprContext, relations []relation, declared map[string]model.Type, grouped bool) (model.Column, bool, error) {
	refs := columnRefs(expr)
	if len(refs) == 1 && isPureColumnExpression(expr) {
		column, err := resolveColumn(relations, refs[0])
		if err != nil {
			return model.Column{}, false, err
		}
		return column, true, nil
	}
	typeValue, err := resolveExpression(expr, expressionScope{relations: relations, bindings: declared, grouped: grouped})
	return model.Column{Type: typeValue}, false, err
}

func concatenationType(expr parser.IExprContext, declared map[string]model.Type) (model.Type, bool, error) {
	var concatenations []*parser.Mul_subexprContext
	descendants(expr, func(node antlr.Tree) {
		ctx, ok := node.(*parser.Mul_subexprContext)
		if ok && len(ctx.AllDOUBLE_PIPE()) != 0 {
			concatenations = append(concatenations, ctx)
		}
	})
	if len(concatenations) == 0 {
		return model.Type{}, false, nil
	}
	if len(concatenations) != 1 || concatenations[0].GetStart() != expr.GetStart() || concatenations[0].GetStop() != expr.GetStop() {
		return model.Type{}, true, fmt.Errorf("nested concatenation result expressions are not supported")
	}

	operands := concatenations[0].AllCon_subexpr()
	var result model.Type
	optional := false
	for _, operand := range operands {
		typeValue, err := concatenationOperandType(operand, declared)
		if err != nil {
			return model.Type{}, true, err
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
	if optional {
		result = model.Optional(result)
	}
	return result, true, nil
}

func concatenationOperandType(operand parser.ICon_subexprContext, declared map[string]model.Type) (model.Type, error) {
	var binds []parser.IBind_parameterContext
	var literals []parser.ILiteral_valueContext
	descendants(operand, func(node antlr.Tree) {
		switch ctx := node.(type) {
		case *parser.Bind_parameterContext:
			binds = append(binds, ctx)
		case parser.ILiteral_valueContext:
			literals = append(literals, ctx)
		}
	})
	if len(binds) == 1 && len(literals) == 0 && operand.GetText() == binds[0].GetText() {
		name := bindName(binds[0])
		typeValue, ok := declared[name]
		if !ok {
			return model.Type{}, fmt.Errorf("cannot resolve type of parameter $%s in concatenation", name)
		}
		return typeValue, nil
	}
	if len(literals) == 1 && len(binds) == 0 && operand.GetText() == literals[0].GetText() && literals[0].STRING_VALUE() != nil {
		return stringLiteralType(literals[0].GetText())
	}
	return model.Type{}, fmt.Errorf("unsupported concatenation operand %q; only string literals and declared parameters are supported", operand.GetText())
}

type columnRef struct {
	qualifier, name string
	ctx             antlr.ParserRuleContext
}

func columnRefs(root antlr.Tree) []columnRef {
	var refs []columnRef
	descendants(root, func(node antlr.Tree) {
		ctx, ok := node.(*parser.Unary_subexprContext)
		if !ok {
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
			refs = append(refs, columnRef{name: base, ctx: ctx})
		case 1:
			refs = append(refs, columnRef{qualifier: base, name: identifier(ids[0].GetText()), ctx: ctx})
		}
	})
	return refs
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

func validateColumnReferences(block queryBlock, root antlr.Tree, relations []relation) []model.Diagnostic {
	var diagnostics []model.Diagnostic
	seen := map[int]bool{}
	for _, ref := range columnRefs(root) {
		position := ref.ctx.GetStart().GetStart()
		if seen[position] {
			continue
		}
		seen[position] = true
		if _, err := resolveColumn(relations, ref); err != nil {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, ref.ctx, err.Error()))
		}
	}
	return diagnostics
}

func resolveColumn(relations []relation, ref columnRef) (model.Column, error) {
	var matches []model.Column
	for _, rel := range relations {
		if ref.qualifier != "" && ref.qualifier != rel.alias && ref.qualifier != rel.table.Name {
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

func inferFromComparisons(tree queryTree, relations []relation, inferred map[string]model.Type) {
	for _, root := range comparisonContexts(tree) {
		refs := columnRefs(root)
		if len(refs) != 1 {
			continue
		}
		var binds []parser.IBind_parameterContext
		descendants(root, func(node antlr.Tree) {
			if bind, ok := node.(*parser.Bind_parameterContext); ok {
				binds = append(binds, bind)
			}
		})
		if len(binds) != 1 || !isDirectComparison(root, refs[0], binds[0]) {
			continue
		}
		column, err := resolveColumn(relations, refs[0])
		if err != nil {
			continue
		}
		inferParameter(inferred, bindName(binds[0]), column.Type)
	}
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

func comparisonContexts(tree queryTree) []antlr.Tree {
	out := make([]antlr.Tree, 0, len(tree.xors)+len(tree.eqs))
	for _, ctx := range tree.xors {
		out = append(out, ctx)
	}
	for _, ctx := range tree.eqs {
		out = append(out, ctx)
	}
	return out
}

func inferInsert(block queryBlock, statement *parser.Into_table_stmtContext, table *model.Table, inferred map[string]model.Type) []model.Diagnostic {
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
			if !inferDirectDMLBind(expressions[i], column.Type, inferred) {
				diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, expressions[i], "DML values must be direct external parameters; use DECLARE and a $parameter"))
			}
		}
	}
	return diagnostics
}

func inferUpdate(block queryBlock, statement *parser.Update_stmtContext, table *model.Table, inferred map[string]model.Type) []model.Diagnostic {
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
	for _, clause := range clauses {
		if clause.Set_target() == nil || clause.Set_target().Column_name() == nil || clause.Expr() == nil {
			continue
		}
		name := identifier(clause.Set_target().Column_name().An_id().GetText())
		column := tableColumn(table, name)
		if column == nil {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, clause, fmt.Sprintf("unknown column %q", name)))
			continue
		}
		if !inferDirectDMLBind(clause.Expr(), column.Type, inferred) {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, clause.Expr(), "DML values must be direct external parameters; use DECLARE and a $parameter"))
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
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, bind, fmt.Sprintf("external parameter $%s is constrained by incompatible column types", name)))
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
