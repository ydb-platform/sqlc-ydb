package analyzer

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	parser "github.com/ydb-platform/yql-parsers/go"
)

// Database supplies read-only schema descriptions and non-executing query checks.
// Implementations must not apply schema statements or execute user queries.
type Database interface {
	DescribeTable(context.Context, string) (model.Table, error)
	ValidateQuery(context.Context, string) error
}

// AnalyzeWithDatabase validates each named query without execution, then discovers
// referenced tables or checks the supplied schema before semantic analysis.
func AnalyzeWithDatabase(ctx context.Context, schema, queries []model.Source, options Options, database Database) (*model.AnalysisResult, error) {
	if database == nil {
		return &model.AnalysisResult{}, fmt.Errorf("database analysis requires a database connection")
	}
	return analyze(ctx, schema, queries, options, database)
}

func queryValidationSQL(block queryBlock) (string, []model.Diagnostic) {
	if len(block.parameters) == 0 {
		return block.text, nil
	}
	parsed, diagnostics := parseYQL(block.file, block.text, block.line-1)
	if len(diagnostics) != 0 {
		return "", diagnostics
	}
	declared, _, declarationDiagnostics := declarations(block, collectQueryTree(parsed.tree))
	_, configuredDiagnostics := configuredDeclarations(block, declared)
	diagnostics = append(declarationDiagnostics, configuredDiagnostics...)
	if len(diagnostics) != 0 {
		return "", diagnostics
	}
	var prefix strings.Builder
	for _, name := range slices.Sorted(maps.Keys(block.parameters)) {
		if _, sourceDeclared := declared[name]; sourceDeclared {
			continue
		}
		fmt.Fprintf(&prefix, "DECLARE $%s AS %s; ", quotedYQLIdentifier(name), block.parameters[name].String())
	}
	return prefix.String() + block.text, nil
}

type databaseTableReference struct {
	name     string
	position model.Position
}

func databaseCatalog(ctx context.Context, database Database, schema []model.Source, catalog model.Catalog, blocks []queryBlock) (model.Catalog, []model.Diagnostic) {
	references, diagnostics := databaseTableReferences(blocks)
	if len(diagnostics) != 0 {
		return catalog, diagnostics
	}
	positions := make(map[string]model.Position, len(references))
	for _, reference := range references {
		positions[reference.name] = reference.position
	}
	if len(schema) != 0 {
		references = nil
		for _, table := range catalog.Tables {
			position, ok := positions[table.Name]
			if !ok {
				position = model.Position{File: schema[0].Name, Line: 1, Column: 1}
			}
			references = append(references, databaseTableReference{name: table.Name, position: position})
		}
	}
	for _, reference := range references {
		if err := ctx.Err(); err != nil {
			return catalog, []model.Diagnostic{{Position: reference.position, Message: fmt.Sprintf("database analysis canceled: %v", err)}}
		}
		table, err := database.DescribeTable(ctx, reference.name)
		if err == nil {
			table, err = normalizeDatabaseTable(reference.name, table)
		}
		if err != nil {
			diagnostics = append(diagnostics, model.Diagnostic{Position: reference.position, Message: fmt.Sprintf("describe database table %q: %v", reference.name, err)})
			continue
		}
		if len(schema) == 0 {
			catalog.Tables = append(catalog.Tables, table)
			continue
		}
		local := findTable(catalog, reference.name)
		if err := compareDatabaseTable(*local, table); err != nil {
			diagnostics = append(diagnostics, model.Diagnostic{Position: reference.position, Message: fmt.Sprintf("database schema drift for table %q: %v", reference.name, err)})
		}
	}
	return catalog, diagnostics
}

func databaseTableReferences(blocks []queryBlock) ([]databaseTableReference, []model.Diagnostic) {
	var references []databaseTableReference
	var diagnostics []model.Diagnostic
	seen := map[string]bool{}
	for i := range blocks {
		block := &blocks[i]
		var parsed parsedYQL
		var parseDiagnostics []model.Diagnostic
		if block.parsed == nil {
			parsed, parseDiagnostics = parseYQL(block.file, block.text, block.line-1)
		} else {
			parsed = *block.parsed
		}
		diagnostics = append(diagnostics, parseDiagnostics...)
		if len(parseDiagnostics) != 0 {
			continue
		}
		block.parsed = &parsed
		prefix, prefixDiagnostics := tablePathPrefix(block.file, block.line-1, parsed.tree)
		diagnostics = append(diagnostics, prefixDiagnostics...)
		if len(prefixDiagnostics) != 0 {
			continue
		}
		tree := collectQueryTree(parsed.tree)
		localNames := map[string]bool{}
		for _, assignment := range tree.named {
			if assignment.Bind_parameter_list() != nil {
				descendants(assignment.Bind_parameter_list(), func(node antlr.Tree) {
					if bind, ok := node.(*parser.Bind_parameterContext); ok {
						localNames[bindName(bind)] = true
					}
				})
			}
		}
		diagnostics = append(diagnostics, validateQueryStatements(*block, tree)...)
		diagnostics = append(diagnostics, unsupportedSQLCMacroDiagnostics(*block, parsed.tokens)...)
		_, _, declarationDiagnostics := declarations(*block, tree)
		diagnostics = append(diagnostics, declarationDiagnostics...)
		add := func(name string, node antlr.ParserRuleContext) {
			name = resolveTablePath(prefix, name)
			if name != "" && !seen[name] {
				seen[name] = true
				references = append(references, databaseTableReference{name: name, position: diagnosticAt(block.file, block.line-1, node, "").Position})
			}
		}
		reject := func(node antlr.ParserRuleContext, message string) {
			diagnostics = append(diagnostics, diagnosticAt(block.file, block.line-1, node, message))
		}
		descendants(parsed.tree, func(node antlr.Tree) {
			switch source := node.(type) {
			case *parser.Select_stmtContext:
				if source.Cte_with_clause() != nil {
					reject(source.Cte_with_clause(), "CTEs are not yet supported")
				}
			case *parser.Named_single_sourceContext:
				if source.Hinted_single_source() == nil || source.Hinted_single_source().Single_source() == nil || source.Hinted_single_source().Single_source().Table_ref() == nil && source.Hinted_single_source().Single_source().Select_stmt() == nil {
					reject(source, "unsupported FROM or JOIN source")
				}
			case *parser.Table_refContext:
				if source.Cluster_expr() != nil || source.COMMAT() != nil {
					reject(source, "cluster-qualified and temporary table references are unsupported in database analysis")
				} else if source.Table_key() != nil {
					add(tableKeyName(source.Table_key()), source)
				} else if source.Bind_parameter() != nil && localNames[bindName(source.Bind_parameter())] {
					return
				} else if source.An_id_expr() == nil || !strings.EqualFold(source.An_id_expr().GetText(), "AS_TABLE") {
					reject(source, "dynamic table references are unsupported; use AS_TABLE($parameter) with DECLARE List<Struct<...>>")
				}
			case *parser.Simple_table_refContext:
				core := source.Simple_table_ref_core()
				if core == nil || core.Object_ref() == nil || core.Cluster_expr() != nil || core.COMMAT() != nil {
					reject(source, "only literal table paths are supported in database analysis")
				} else {
					add(simpleTableName(source), source)
				}
			}
		})
	}
	return references, diagnostics
}

func normalizeDatabaseTable(name string, table model.Table) (model.Table, error) {
	table.Name = name
	table.Columns = slices.Clone(table.Columns)
	seen := make(map[string]bool, len(table.Columns))
	for i := range table.Columns {
		column := &table.Columns[i]
		if column.Name == "" || seen[column.Name] {
			return model.Table{}, fmt.Errorf("database returned an empty or duplicate column name %q", column.Name)
		}
		seen[column.Name] = true
		column.Table = name
		typ, err := parseType(column.Type.String())
		if err != nil || !typ.Equal(column.Type) {
			return model.Table{}, fmt.Errorf("database column %q has unsupported YQL type %s", column.Name, column.Type.String())
		}
	}
	for _, key := range table.PrimaryKey {
		if !seen[key] {
			return model.Table{}, fmt.Errorf("database primary key references missing column %q", key)
		}
	}
	return table, nil
}

func compareDatabaseTable(local, remote model.Table) error {
	for _, column := range local.Columns {
		other := tableColumn(&remote, column.Name)
		if other == nil {
			return fmt.Errorf("column %q is missing from the database", column.Name)
		}
		if !column.Type.Equal(other.Type) {
			return fmt.Errorf("column %q has local type %s and database type %s", column.Name, column.Type.String(), other.Type.String())
		}
		if column.SequenceGenerated != other.SequenceGenerated {
			return fmt.Errorf("column %q has different sequence generation settings", column.Name)
		}
	}
	for _, column := range remote.Columns {
		if tableColumn(&local, column.Name) == nil {
			return fmt.Errorf("database column %q is missing from the local schema", column.Name)
		}
	}
	if !slices.Equal(local.PrimaryKey, remote.PrimaryKey) {
		return fmt.Errorf("local primary key %v differs from database primary key %v", local.PrimaryKey, remote.PrimaryKey)
	}
	for _, index := range local.Indexes {
		position := slices.IndexFunc(remote.Indexes, func(other model.Index) bool { return other.Name == index.Name })
		if position < 0 {
			return fmt.Errorf("index %q is missing from the database", index.Name)
		}
		other := remote.Indexes[position]
		if index.Kind != other.Kind {
			return fmt.Errorf("index %q has local kind %s and database kind %s", index.Name, index.Kind, other.Kind)
		}
		if !slices.Equal(index.Columns, other.Columns) {
			return fmt.Errorf("index %q has local key columns %v and database key columns %v", index.Name, index.Columns, other.Columns)
		}
		localCover := slices.Sorted(slices.Values(index.DataColumns))
		remoteCover := slices.Sorted(slices.Values(other.DataColumns))
		if !slices.Equal(localCover, remoteCover) {
			return fmt.Errorf("index %q has local covering columns %v and database covering columns %v", index.Name, index.DataColumns, other.DataColumns)
		}
	}
	for _, index := range remote.Indexes {
		if !slices.ContainsFunc(local.Indexes, func(other model.Index) bool { return other.Name == index.Name }) {
			return fmt.Errorf("database index %q is missing from the local schema", index.Name)
		}
	}
	return nil
}
