package analyzer

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	parser "github.com/ydb-platform/yql-parsers/go"
)

func buildCatalog(sources []model.Source) (model.Catalog, []model.Diagnostic) {
	catalog := model.Catalog{}
	var diagnostics []model.Diagnostic
	for _, source := range sources {
		parsed, syntaxDiagnostics := parseYQL(source.Name, source.Text, 0)
		diagnostics = append(diagnostics, syntaxDiagnostics...)
		if len(syntaxDiagnostics) != 0 {
			continue
		}
		statementList := parsed.tree.Sql_stmt_list()
		if statementList == nil {
			diagnostics = append(diagnostics, model.Diagnostic{Position: model.Position{File: source.Name, Line: 1, Column: 1}, Message: "unsupported schema query form"})
			continue
		}
		for _, statement := range statementList.AllSql_stmt() {
			core := statement.Sql_stmt_core()
			if statement.EXPLAIN() != nil || core == nil {
				diagnostics = append(diagnostics, diagnosticAt(source.Name, 0, statement, fmt.Sprintf("unsupported schema statement %q; supported statements are CREATE TABLE, ALTER TABLE, and DROP TABLE", statement.GetText())))
				continue
			}
			if create := core.Create_table_stmt(); create != nil {
				diagnostics = append(diagnostics, applyCreateTable(&catalog, source.Name, create)...)
				continue
			}
			if alter := core.Alter_table_stmt(); alter != nil {
				diagnostics = append(diagnostics, applyAlterTable(&catalog, source.Name, alter)...)
				continue
			}
			if drop := core.Drop_table_stmt(); drop != nil {
				diagnostics = append(diagnostics, applyDropTable(&catalog, source.Name, drop)...)
				continue
			}
			diagnostics = append(diagnostics, diagnosticAt(source.Name, 0, statement, fmt.Sprintf("unsupported schema statement %q; supported statements are CREATE TABLE, ALTER TABLE, and DROP TABLE", statement.GetText())))
		}
	}
	return catalog, diagnostics
}

func applyCreateTable(catalog *model.Catalog, file string, create parser.ICreate_table_stmtContext) []model.Diagnostic {
	if diagnostic := validateCreateTableShape(file, create); diagnostic != nil {
		return []model.Diagnostic{*diagnostic}
	}
	name := simpleTableName(create.Simple_table_ref())
	if name != "" {
		if _, exists := catalogTableIndex(*catalog, name); exists {
			if create.IF() != nil && create.NOT() != nil && create.EXISTS() != nil {
				// YDB skips the entire CREATE TABLE IF NOT EXISTS statement when the
				// object exists, including validation of the proposed replacement schema.
				return nil
			}
			return []model.Diagnostic{diagnosticAt(file, 0, create, fmt.Sprintf("table %q already exists", name))}
		}
	}
	table, diagnostics := catalogTable(file, create)
	if len(diagnostics) != 0 {
		return diagnostics
	}
	catalog.Tables = append(catalog.Tables, table)
	return nil
}

func applyDropTable(catalog *model.Catalog, file string, drop parser.IDrop_table_stmtContext) []model.Diagnostic {
	if drop.TABLE() == nil || drop.EXTERNAL() != nil || drop.TABLESTORE() != nil {
		return []model.Diagnostic{diagnosticAt(file, 0, drop, "only ordinary DROP TABLE is supported")}
	}
	name := simpleTableName(drop.Simple_table_ref())
	if name == "" {
		return []model.Diagnostic{diagnosticAt(file, 0, drop, "DROP TABLE has no resolvable table name")}
	}
	index, exists := catalogTableIndex(*catalog, name)
	if !exists {
		if drop.IF() != nil && drop.EXISTS() != nil {
			return nil
		}
		return []model.Diagnostic{diagnosticAt(file, 0, drop, fmt.Sprintf("table %q does not exist", name))}
	}
	catalog.Tables = append(catalog.Tables[:index], catalog.Tables[index+1:]...)
	return nil
}

func applyAlterTable(catalog *model.Catalog, file string, alter parser.IAlter_table_stmtContext) []model.Diagnostic {
	name := simpleTableName(alter.Simple_table_ref())
	if name == "" {
		return []model.Diagnostic{diagnosticAt(file, 0, alter, "ALTER TABLE has no resolvable table name")}
	}
	index, exists := catalogTableIndex(*catalog, name)
	if !exists {
		return []model.Diagnostic{diagnosticAt(file, 0, alter, fmt.Sprintf("table %q does not exist", name))}
	}
	working := cloneTable(catalog.Tables[index])
	var diagnostics []model.Diagnostic
	actions := alter.AllAlter_table_action()
	if len(actions) > 1 {
		for _, action := range actions {
			if action.Alter_table_rename_to() != nil {
				return []model.Diagnostic{diagnosticAt(file, 0, action, "RENAME TO must be the only action in an ALTER TABLE statement")}
			}
		}
	}
	for _, action := range actions {
		diagnostics = append(diagnostics, applyAlterTableAction(*catalog, index, &working, file, action)...)
		if len(diagnostics) != 0 {
			return diagnostics
		}
	}
	catalog.Tables[index] = working
	return nil
}

func applyAlterTableAction(catalog model.Catalog, tableIndex int, table *model.Table, file string, action parser.IAlter_table_actionContext) []model.Diagnostic {
	if add := action.Alter_table_add_column(); add != nil {
		column, err := catalogColumn(table.Name, add.Column_schema())
		if err != nil {
			return []model.Diagnostic{diagnosticAt(file, 0, add, err.Error())}
		}
		if _, exists := catalogColumnIndex(*table, column.Name); exists {
			return []model.Diagnostic{diagnosticAt(file, 0, add.Column_schema(), fmt.Sprintf("column %q already exists in table %q", column.Name, table.Name))}
		}
		if column.SequenceGenerated {
			return []model.Diagnostic{diagnosticAt(file, 0, add.Column_schema(), serialPrimaryKeyError(column.Name))}
		}
		table.Columns = append(table.Columns, column)
		return nil
	}
	if drop := action.Alter_table_drop_column(); drop != nil {
		name := identifier(drop.An_id().GetText())
		columnIndex, exists := catalogColumnIndex(*table, name)
		if !exists {
			return []model.Diagnostic{diagnosticAt(file, 0, drop, fmt.Sprintf("column %q does not exist in table %q", name, table.Name))}
		}
		for _, key := range table.PrimaryKey {
			if key == name {
				return []model.Diagnostic{diagnosticAt(file, 0, drop, fmt.Sprintf("cannot drop primary key column %q from table %q", name, table.Name))}
			}
		}
		table.Columns = append(table.Columns[:columnIndex], table.Columns[columnIndex+1:]...)
		return nil
	}
	if rename := action.Alter_table_rename_to(); rename != nil {
		newName := identifier(rename.An_id_table().GetText())
		if otherIndex, exists := catalogTableIndex(catalog, newName); exists && otherIndex != tableIndex {
			return []model.Diagnostic{diagnosticAt(file, 0, rename, fmt.Sprintf("table %q already exists", newName))}
		}
		table.Name = newName
		for i := range table.Columns {
			table.Columns[i].Table = newName
		}
		return nil
	}
	return []model.Diagnostic{diagnosticAt(file, 0, action, fmt.Sprintf("unsupported ALTER TABLE action %q; supported actions are ADD COLUMN, DROP COLUMN, and RENAME TO", action.GetText()))}
}

func catalogTableIndex(catalog model.Catalog, name string) (int, bool) {
	for i := range catalog.Tables {
		if catalog.Tables[i].Name == name {
			return i, true
		}
	}
	return -1, false
}

func catalogColumnIndex(table model.Table, name string) (int, bool) {
	for i := range table.Columns {
		if table.Columns[i].Name == name {
			return i, true
		}
	}
	return -1, false
}

func cloneTable(table model.Table) model.Table {
	table.Columns = append([]model.Column(nil), table.Columns...)
	table.PrimaryKey = append([]string(nil), table.PrimaryKey...)
	return table
}

func validateCreateTableShape(file string, create parser.ICreate_table_stmtContext) *model.Diagnostic {
	if create.TABLE() == nil || create.EXTERNAL() != nil || create.TABLESTORE() != nil || create.Table_as_source() != nil {
		diagnostic := diagnosticAt(file, 0, create, "only CREATE TABLE with an explicit column list is supported")
		return &diagnostic
	}
	return nil
}

func catalogTable(file string, create parser.ICreate_table_stmtContext) (model.Table, []model.Diagnostic) {
	var diagnostics []model.Diagnostic
	ref := create.Simple_table_ref()
	if ref == nil || ref.Simple_table_ref_core() == nil {
		return model.Table{}, []model.Diagnostic{diagnosticAt(file, 0, create, "CREATE TABLE has no resolvable table name")}
	}
	table := model.Table{Name: identifier(ref.Simple_table_ref_core().GetText())}
	columnNames := map[string]bool{}
	primaryKeyNames := map[string]bool{}
	primaryKeyDeclarations := 0
	for _, entry := range create.AllCreate_table_entry() {
		if columnContext := entry.Column_schema(); columnContext != nil {
			column, err := catalogColumn(table.Name, columnContext)
			if err != nil {
				diagnostics = append(diagnostics, diagnosticAt(file, 0, columnContext, err.Error()))
				continue
			}
			key := column.Name
			if columnNames[key] {
				diagnostics = append(diagnostics, diagnosticAt(file, 0, columnContext, fmt.Sprintf("column %q is declared more than once", column.Name)))
				continue
			}
			columnNames[key] = true
			table.Columns = append(table.Columns, column)
			continue
		}
		if constraint := entry.Table_constraint(); constraint != nil {
			if constraint.PRIMARY() == nil {
				diagnostics = append(diagnostics, diagnosticAt(file, 0, constraint, "only PRIMARY KEY table constraints are supported"))
				continue
			}
			primaryKeyDeclarations++
			for _, id := range constraint.AllAn_id() {
				name := identifier(id.GetText())
				if primaryKeyNames[name] {
					diagnostics = append(diagnostics, diagnosticAt(file, 0, id, fmt.Sprintf("primary key column %q is declared more than once", name)))
					continue
				}
				primaryKeyNames[name] = true
				table.PrimaryKey = append(table.PrimaryKey, name)
			}
			continue
		}
		diagnostics = append(diagnostics, diagnosticAt(file, 0, entry, "unsupported CREATE TABLE entry"))
	}
	if len(table.Columns) == 0 {
		diagnostics = append(diagnostics, diagnosticAt(file, 0, create, fmt.Sprintf("table %q has no columns", table.Name)))
	}
	if primaryKeyDeclarations == 0 {
		diagnostics = append(diagnostics, diagnosticAt(file, 0, create, fmt.Sprintf("table %q must declare a PRIMARY KEY", table.Name)))
	} else if primaryKeyDeclarations > 1 {
		diagnostics = append(diagnostics, diagnosticAt(file, 0, create, fmt.Sprintf("table %q declares PRIMARY KEY more than once", table.Name)))
	}
	for _, key := range table.PrimaryKey {
		if !columnNames[key] {
			diagnostics = append(diagnostics, diagnosticAt(file, 0, create, fmt.Sprintf("primary key column %q does not exist", key)))
		}
	}
	for _, column := range table.Columns {
		if column.SequenceGenerated && !primaryKeyNames[column.Name] {
			diagnostics = append(diagnostics, diagnosticAt(file, 0, create, serialPrimaryKeyError(column.Name)))
		}
	}
	// PARTITION BY and WITH describe physical storage and do not change the
	// tables, columns, types, or primary keys represented by model.Catalog.
	return table, diagnostics
}

func catalogColumn(table string, ctx parser.IColumn_schemaContext) (model.Column, error) {
	if ctx.An_id_schema() == nil || ctx.Type_name_or_bind() == nil || ctx.Type_name_or_bind().Type_name() == nil {
		return model.Column{}, fmt.Errorf("column must have a literal name and type")
	}
	typeText := ctx.Type_name_or_bind().Type_name().GetText()
	typeValue, sequenceGenerated := serialType(typeText)
	if !sequenceGenerated {
		var err error
		typeValue, err = parseType(typeText)
		if err != nil {
			return model.Column{}, err
		}
	}
	notNull := false
	descendants(ctx, func(node antlr.Tree) {
		if nullability, ok := node.(*parser.NullabilityContext); ok && strings.EqualFold(nullability.GetText(), "NOTNULL") {
			notNull = true
		}
	})
	if !sequenceGenerated && !notNull && !typeValue.IsOptional() {
		typeValue = model.Optional(typeValue)
	}
	return model.Column{Name: identifier(ctx.An_id_schema().GetText()), Type: typeValue, Table: table, SequenceGenerated: sequenceGenerated}, nil
}

func serialType(text string) (model.Type, bool) {
	if kind, ok := serialTypes[strings.ToLower(text)]; ok {
		return model.Type{Kind: kind}, true
	}
	return model.Type{}, false
}

func serialPrimaryKeyError(name string) string {
	return fmt.Sprintf("serial column %q must participate in the PRIMARY KEY", name)
}

// parseType parses the canonical type text supplied by a Type_name parse-tree
// node. The YQL parser has already established its syntax; this routine only
// converts supported type constructors to the semantic model.
func parseType(text string) (model.Type, error) {
	reader := typeReader{text: text}
	typeValue, err := reader.readType()
	if err != nil {
		return model.Type{}, err
	}
	if reader.index != len(reader.text) {
		return model.Type{}, fmt.Errorf("unsupported YQL type %q", text)
	}
	return typeValue, nil
}

type typeReader struct {
	text  string
	index int
}

func (r *typeReader) readType() (model.Type, error) {
	start := r.index
	for r.index < len(r.text) {
		c := r.text[r.index]
		if !(c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			break
		}
		r.index++
	}
	if start == r.index {
		return model.Type{}, fmt.Errorf("unsupported YQL type %q", r.text)
	}
	name := r.text[start:r.index]
	canonical, simple := simpleTypes[strings.ToLower(name)]
	var out model.Type
	if r.consume('<') {
		switch strings.ToLower(name) {
		case "optional", "list", "stream", "flow", "set":
			elem, err := r.readType()
			if err != nil || !r.consume('>') {
				return model.Type{}, fmt.Errorf("unsupported YQL type %q", r.text)
			}
			out = model.Type{Kind: canonicalConstructor(name), Elem: &elem}
		case "dict":
			key, err := r.readType()
			if err != nil || !r.consume(',') {
				return model.Type{}, fmt.Errorf("unsupported YQL type %q", r.text)
			}
			value, err := r.readType()
			if err != nil || !r.consume('>') {
				return model.Type{}, fmt.Errorf("unsupported YQL type %q", r.text)
			}
			out = model.Type{Kind: "Dict", Key: &key, Elem: &value}
		case "tuple":
			var items []model.Type
			for {
				item, err := r.readType()
				if err != nil {
					return model.Type{}, fmt.Errorf("unsupported YQL type %q", r.text)
				}
				items = append(items, item)
				if r.consume('>') {
					break
				}
				if !r.consume(',') {
					return model.Type{}, fmt.Errorf("unsupported YQL type %q", r.text)
				}
			}
			out = model.Type{Kind: "Tuple", Items: items}
		default:
			return model.Type{}, fmt.Errorf("unsupported YQL type constructor %q", name)
		}
	} else if r.consume('(') && strings.EqualFold(name, "decimal") {
		precision, ok := r.readNumber()
		if !ok || !r.consume(',') {
			return model.Type{}, fmt.Errorf("unsupported YQL type %q", r.text)
		}
		scale, ok := r.readNumber()
		if !ok || !r.consume(')') {
			return model.Type{}, fmt.Errorf("unsupported YQL type %q", r.text)
		}
		if precision < 1 || precision > 35 || scale > precision {
			return model.Type{}, fmt.Errorf("invalid Decimal(%d,%d): precision must be 1..35 and scale 0..precision", precision, scale)
		}
		out = model.Type{Kind: "Decimal", Precision: precision, Scale: scale}
	} else if simple {
		out = model.Type{Kind: canonical}
	} else {
		return model.Type{}, fmt.Errorf("unsupported YQL type %q", name)
	}
	for r.consume('?') {
		out = model.Optional(out)
	}
	return out, nil
}

func (r *typeReader) consume(c byte) bool {
	if r.index < len(r.text) && r.text[r.index] == c {
		r.index++
		return true
	}
	return false
}

func (r *typeReader) readNumber() (int, bool) {
	start := r.index
	for r.index < len(r.text) && r.text[r.index] >= '0' && r.text[r.index] <= '9' {
		r.index++
	}
	if start == r.index {
		return 0, false
	}
	value, err := strconv.Atoi(r.text[start:r.index])
	return value, err == nil
}

func canonicalConstructor(name string) string {
	switch strings.ToLower(name) {
	case "optional":
		return "Optional"
	case "list":
		return "List"
	case "stream":
		return "Stream"
	case "flow":
		return "Flow"
	case "set":
		return "Set"
	default:
		return name
	}
}

var simpleTypes = map[string]string{
	"bool": "Bool", "int8": "Int8", "int16": "Int16", "int32": "Int32", "int64": "Int64",
	"uint8": "Uint8", "uint16": "Uint16", "uint32": "Uint32", "uint64": "Uint64",
	"float": "Float", "double": "Double", "string": "String", "utf8": "Utf8", "text": "Utf8",
	"yson": "Yson", "json": "Json", "jsondocument": "JsonDocument", "uuid": "Uuid", "dynumber": "DyNumber",
	"date": "Date", "datetime": "Datetime", "timestamp": "Timestamp", "interval": "Interval",
	"date32": "Date32", "datetime64": "Datetime64", "timestamp64": "Timestamp64", "interval64": "Interval64",
	"tzdate": "TzDate", "tzdatetime": "TzDatetime", "tztimestamp": "TzTimestamp", "void": "Void", "null": "Null",
}

var serialTypes = map[string]string{
	"smallserial": "Int16", "serial2": "Int16",
	"serial": "Int32", "serial4": "Int32",
	"serial8": "Int64", "bigserial": "Int64",
}
