package analyzer

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-engine-ydb/internal/model"
	parser "github.com/ydb-platform/yql-parsers/go"
)

func buildCatalog(sources []model.Source) (model.Catalog, []model.Diagnostic) {
	catalog := model.Catalog{}
	var diagnostics []model.Diagnostic
	seen := map[string]bool{}
	for _, source := range sources {
		parsed, syntaxDiagnostics := parseYQL(source.Name, source.Text, 0)
		diagnostics = append(diagnostics, syntaxDiagnostics...)
		if len(syntaxDiagnostics) != 0 {
			continue
		}
		var statements []*parser.Sql_stmtContext
		descendants(parsed.tree, func(node antlr.Tree) {
			if ctx, ok := node.(*parser.Sql_stmtContext); ok {
				statements = append(statements, ctx)
			}
		})
		for _, statement := range statements {
			var creates []*parser.Create_table_stmtContext
			descendants(statement, func(node antlr.Tree) {
				if ctx, ok := node.(*parser.Create_table_stmtContext); ok {
					creates = append(creates, ctx)
				}
			})
			if len(creates) != 1 {
				diagnostics = append(diagnostics, diagnosticAt(source.Name, 0, statement, fmt.Sprintf("unsupported schema statement %q; only CREATE TABLE is currently supported", statement.GetText())))
				continue
			}
			create := creates[0]
			table, tableDiagnostics := catalogTable(source.Name, create)
			diagnostics = append(diagnostics, tableDiagnostics...)
			if len(tableDiagnostics) != 0 {
				continue
			}
			key := strings.ToLower(table.Name)
			if seen[key] {
				diagnostics = append(diagnostics, diagnosticAt(source.Name, 0, create, fmt.Sprintf("table %q is declared more than once", table.Name)))
				continue
			}
			seen[key] = true
			catalog.Tables = append(catalog.Tables, table)
		}
	}
	return catalog, diagnostics
}

func catalogTable(file string, create *parser.Create_table_stmtContext) (model.Table, []model.Diagnostic) {
	var diagnostics []model.Diagnostic
	if create.TABLE() == nil || create.EXTERNAL() != nil || create.TABLESTORE() != nil || create.Table_as_source() != nil {
		return model.Table{}, []model.Diagnostic{diagnosticAt(file, 0, create, "only CREATE TABLE with an explicit column list is supported")}
	}
	ref := create.Simple_table_ref()
	if ref == nil || ref.Simple_table_ref_core() == nil {
		return model.Table{}, []model.Diagnostic{diagnosticAt(file, 0, create, "CREATE TABLE has no resolvable table name")}
	}
	table := model.Table{Name: identifier(ref.Simple_table_ref_core().GetText())}
	columnNames := map[string]bool{}
	for _, entry := range create.AllCreate_table_entry() {
		if columnContext := entry.Column_schema(); columnContext != nil {
			column, err := catalogColumn(table.Name, columnContext)
			if err != nil {
				diagnostics = append(diagnostics, diagnosticAt(file, 0, columnContext, err.Error()))
				continue
			}
			key := strings.ToLower(column.Name)
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
			for _, id := range constraint.AllAn_id() {
				table.PrimaryKey = append(table.PrimaryKey, identifier(id.GetText()))
			}
			continue
		}
		diagnostics = append(diagnostics, diagnosticAt(file, 0, entry, "unsupported CREATE TABLE entry"))
	}
	if len(table.Columns) == 0 {
		diagnostics = append(diagnostics, diagnosticAt(file, 0, create, fmt.Sprintf("table %q has no columns", table.Name)))
	}
	for _, key := range table.PrimaryKey {
		if !columnNames[strings.ToLower(key)] {
			diagnostics = append(diagnostics, diagnosticAt(file, 0, create, fmt.Sprintf("primary key column %q does not exist", key)))
		}
	}
	return table, diagnostics
}

func catalogColumn(table string, ctx parser.IColumn_schemaContext) (model.Column, error) {
	if ctx.An_id_schema() == nil || ctx.Type_name_or_bind() == nil || ctx.Type_name_or_bind().Type_name() == nil {
		return model.Column{}, fmt.Errorf("column must have a literal name and type")
	}
	typeValue, err := parseType(ctx.Type_name_or_bind().Type_name().GetText())
	if err != nil {
		return model.Column{}, err
	}
	notNull := false
	descendants(ctx, func(node antlr.Tree) {
		if nullability, ok := node.(*parser.NullabilityContext); ok && strings.EqualFold(nullability.GetText(), "NOTNULL") {
			notNull = true
		}
	})
	if !notNull && !typeValue.IsOptional() {
		typeValue = model.Optional(typeValue)
	}
	return model.Column{Name: identifier(ctx.An_id_schema().GetText()), Type: typeValue, Table: table}, nil
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
	value, _ := strconv.Atoi(r.text[start:r.index])
	return value, true
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
