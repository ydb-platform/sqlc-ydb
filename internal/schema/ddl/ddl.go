package ddl

import (
	"errors"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/sqlc-dev/sqlc-engine-ydb/internal/schema"
	parser "github.com/ydb-platform/yql-parsers/go"
)

func errParse(msg string) error { return errors.New("schema/ddl: " + msg) }

// Registry parses schemaSQL (CREATE TABLE/…) with the YQL parser and returns a Registry. No regex.
func Registry(schemaSQL string) (schema.Registry, error) {
	schemaSQL = strings.TrimSpace(schemaSQL)
	if schemaSQL == "" {
		return &registry{tables: map[string][]schema.ColumnInfo{}}, nil
	}
	input := antlr.NewInputStream(schemaSQL)
	lexer := parser.NewYQLLexer(input)
	stream := antlr.NewCommonTokenStream(lexer, 0)
	p := parser.NewYQLParser(stream)
	el := &errListener{DefaultErrorListener: antlr.NewDefaultErrorListener()}
	p.AddErrorListener(el)
	tree := p.Sql_query()
	if el.err != "" {
		return nil, errParse(el.err)
	}
	collect := &listener{tables: make(map[string][]schema.ColumnInfo), schemaSQL: schemaSQL}
	antlr.NewParseTreeWalker().Walk(collect, tree)
	return &registry{tables: collect.tables}, nil
}

type registry struct {
	tables map[string][]schema.ColumnInfo
}

func (r *registry) Columns(tableOrView string) ([]schema.ColumnInfo, bool) {
	cols, ok := r.tables[strings.ToLower(tableOrView)]
	return cols, ok
}

func (r *registry) TableNames() []string {
	names := make([]string, 0, len(r.tables))
	for t := range r.tables {
		names = append(names, t)
	}
	return names
}

type errListener struct {
	*antlr.DefaultErrorListener
	err string
}

func (e *errListener) SyntaxError(recognizer antlr.Recognizer, offendingSymbol interface{}, line, column int, msg string, _ antlr.RecognitionException) {
	e.err = msg
}

type listener struct {
	parser.BaseYQLListener
	tables       map[string][]schema.ColumnInfo
	schemaSQL    string // full schema text for NOT NULL detection
	curTable     string
	curCols      []schema.ColumnInfo
	curColName   string // for NOT NULL check in Exit
	curAlterTable string // for ALTER TABLE … ADD/DROP COLUMN
}

func (s *listener) EnterEveryRule(ctx antlr.ParserRuleContext) {
	if ctx == nil {
		return
	}
	switch n := ctx.(type) {
	case *parser.Create_table_stmtContext:
		s.enterCreateTable(n)
	case *parser.Column_schemaContext:
		s.enterColumnSchema(n)
	case *parser.Alter_table_stmtContext:
		s.enterAlterTable(n)
	case *parser.Alter_table_add_columnContext:
		s.enterAlterTableAddColumn(n)
	case *parser.Alter_table_drop_columnContext:
		s.enterAlterTableDropColumn(n)
	case *parser.Drop_table_stmtContext:
		s.enterDropTable(n)
	case *parser.Create_view_stmtContext:
		s.enterCreateView(n)
	case *parser.Drop_view_stmtContext:
		s.enterDropView(n)
	}
}

func (s *listener) ExitEveryRule(ctx antlr.ParserRuleContext) {
	if ctx == nil {
		return
	}
	switch n := ctx.(type) {
	case *parser.Create_table_stmtContext:
		if s.curTable != "" && len(s.curCols) > 0 {
			s.tables[s.curTable] = s.curCols
		}
		s.curTable = ""
		s.curCols = nil
		_ = n
	case *parser.Column_schemaContext:
		s.exitColumnSchema(n)
	case *parser.Alter_table_stmtContext:
		s.curAlterTable = ""
	}
}

func (s *listener) enterCreateTable(n *parser.Create_table_stmtContext) {
	if n == nil || n.Simple_table_ref() == nil {
		return
	}
	core := n.Simple_table_ref().Simple_table_ref_core()
	if core == nil {
		return
	}
	s.curTable = identifier(core.GetText())
	s.curCols = nil
}

func (s *listener) enterColumnSchema(n *parser.Column_schemaContext) {
	if n == nil || s.curTable == "" {
		return
	}
	name := ""
	if id := n.An_id_schema(); id != nil {
		name = identifier(id.GetText())
	}
	if name == "" {
		return
	}
	dataType := "Any"
	if tnb := n.Type_name_or_bind(); tnb != nil {
		if tn := tnb.Type_name(); tn != nil {
			dataType = strings.TrimSpace(tn.GetText())
		}
	}
	s.curColName = name
	s.curCols = append(s.curCols, schema.ColumnInfo{Name: name, DataType: dataType, Nullable: true})
}

func (s *listener) exitColumnSchema(n *parser.Column_schemaContext) {
	if n == nil || len(s.curCols) == 0 || s.curColName == "" {
		return
	}
	last := len(s.curCols) - 1
	// NOT NULL => nullable = false. Check rule text first; fallback to full schema (YQL rule may not include NOT NULL in GetText()).
	notNull := strings.Contains(strings.ToUpper(n.GetText()), "NOT NULL") ||
		strings.Contains(strings.ToUpper(s.schemaSQL), strings.ToUpper(s.curColName+" "+s.curCols[last].DataType+" NOT NULL"))
	if notNull {
		s.curCols[last] = schema.ColumnInfo{
			Name:     s.curCols[last].Name,
			DataType: s.curCols[last].DataType,
			Nullable: false,
		}
	}
	s.curColName = ""
}

func (s *listener) enterAlterTable(n *parser.Alter_table_stmtContext) {
	if n == nil || n.Simple_table_ref() == nil {
		return
	}
	core := n.Simple_table_ref().Simple_table_ref_core()
	if core == nil {
		return
	}
	s.curAlterTable = identifier(core.GetText())
}

func (s *listener) enterAlterTableAddColumn(n *parser.Alter_table_add_columnContext) {
	if n == nil || s.curAlterTable == "" {
		return
	}
	colSchema := n.Column_schema()
	if colSchema == nil {
		return
	}
	name := ""
	if id := colSchema.An_id_schema(); id != nil {
		name = identifier(id.GetText())
	}
	if name == "" {
		return
	}
	dataType := "Any"
	if tnb := colSchema.Type_name_or_bind(); tnb != nil {
		if tn := tnb.Type_name(); tn != nil {
			dataType = strings.TrimSpace(tn.GetText())
		}
	}
	nullable := true
	if strings.Contains(strings.ToUpper(colSchema.GetText()), "NOT NULL") {
		nullable = false
	}
	cols := s.tables[s.curAlterTable]
	cols = append(cols, schema.ColumnInfo{Name: name, DataType: dataType, Nullable: nullable})
	s.tables[s.curAlterTable] = cols
}

func (s *listener) enterAlterTableDropColumn(n *parser.Alter_table_drop_columnContext) {
	if n == nil || s.curAlterTable == "" {
		return
	}
	anID := n.An_id()
	if anID == nil {
		return
	}
	colName := identifier(anID.GetText())
	if colName == "" {
		return
	}
	cols := s.tables[s.curAlterTable]
	for i, c := range cols {
		if c.Name == colName {
			s.tables[s.curAlterTable] = append(cols[:i], cols[i+1:]...)
			return
		}
	}
}

func (s *listener) enterDropTable(n *parser.Drop_table_stmtContext) {
	if n == nil || n.Simple_table_ref() == nil {
		return
	}
	core := n.Simple_table_ref().Simple_table_ref_core()
	if core == nil {
		return
	}
	tbl := identifier(core.GetText())
	delete(s.tables, tbl)
}

func (s *listener) enterCreateView(n *parser.Create_view_stmtContext) {
	if n == nil || n.Object_ref() == nil {
		return
	}
	viewName := identifier(n.Object_ref().GetText())
	if viewName == "" {
		return
	}
	// View columns could be derived from SELECT; for now register view with empty columns.
	// Registry.Columns(viewName) will return ([], true) so the view is known.
	s.tables[viewName] = nil
}

func (s *listener) enterDropView(n *parser.Drop_view_stmtContext) {
	if n == nil || n.Object_ref() == nil {
		return
	}
	viewName := identifier(n.Object_ref().GetText())
	if viewName != "" {
		delete(s.tables, viewName)
	}
}

func identifier(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	return strings.ToLower(s)
}
