package handler

import (
	"sort"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	parser "github.com/ydb-platform/yql-parsers/go"
)

// paramPos stores parameter name and its start offset for stable ordering.
type paramPos struct {
	start int
	name  string
}

// paramAndTablesVisitor collects bind parameters (only $name, not $true/$false)
// and table names from FROM/JOIN/UPDATE/DELETE/INSERT.
type paramAndTablesVisitor struct {
	parser.BaseYQLVisitor
	params     []paramPos
	tableNames []string
	seenTables map[string]bool
}

func newParamAndTablesVisitor() *paramAndTablesVisitor {
	return &paramAndTablesVisitor{seenTables: make(map[string]bool)}
}

// StmtKind is the kind of the single statement we care about for result columns.
type StmtKind int

const (
	StmtKindOther StmtKind = iota
	StmtKindSelect
	StmtKindInsert
	StmtKindUpsert
	StmtKindReplace
	StmtKindUpdate
	StmtKindDelete
)

// collectListener is used with ParseTreeWalker to collect params, table names,
// statement kind, and whether the query has SELECT * or RETURNING * (for result columns).
// All detection is done via ANTLR nodes, no regex.
type collectListener struct {
	parser.BaseYQLListener
	params          []paramPos
	tableNames      []string
	seenTables      map[string]bool
	stmtKind        StmtKind
	hasSelectStar   bool
	hasReturningStar bool
}

func newCollectListener() *collectListener {
	return &collectListener{seenTables: make(map[string]bool)}
}

// EnterEveryRule is called by the tree walker for every node; we dispatch by type.
// Statement kind and SELECT * / RETURNING * are detected here (no regex).
func (c *collectListener) EnterEveryRule(ctx antlr.ParserRuleContext) {
	if ctx == nil {
		return
	}
	switch n := ctx.(type) {
	case *parser.Bind_parameterContext:
		c.enterBindParam(n)
	case *parser.Table_refContext:
		c.enterTableRef(n)
	case *parser.Delete_stmtContext:
		c.stmtKind = StmtKindDelete
		collectTableFromDelete(n, c)
	case *parser.Update_stmtContext:
		c.stmtKind = StmtKindUpdate
		collectTableFromUpdate(n, c)
	case *parser.Into_table_stmtContext:
		c.enterIntoTableStmt(n)
	case *parser.Select_stmtContext:
		c.stmtKind = StmtKindSelect
	case *parser.Result_columnContext:
		if n.ASTERISK() != nil {
			c.hasSelectStar = true
		}
	case *parser.Returning_columns_listContext:
		if n.ASTERISK() != nil {
			c.hasReturningStar = true
		}
	}
}

func (c *collectListener) enterBindParam(bc *parser.Bind_parameterContext) {
	if bc == nil || bc.DOLLAR() == nil {
		return
	}
	if bc.TRUE() != nil || bc.FALSE() != nil {
		return
	}
	var name string
	if an := bc.An_id_or_type(); an != nil {
		name = anIdOrTypeToName(an)
	} else {
		text := strings.TrimPrefix(bc.GetText(), "$")
		if text == "" || strings.EqualFold(text, "true") || strings.EqualFold(text, "false") {
			return
		}
		name = identifier(text)
	}
	if name != "" {
		start := 0
		if t := bc.GetStart(); t != nil {
			start = t.GetStart()
		}
		c.params = append(c.params, paramPos{start: start, name: name})
	}
}

func (c *collectListener) enterTableRef(tc *parser.Table_refContext) {
	if tc == nil || tc.Table_key() == nil {
		return
	}
	name := identifier(tc.Table_key().GetText())
	if name != "" {
		if !c.seenTables[name] {
			c.seenTables[name] = true
			c.tableNames = append(c.tableNames, name)
		}
	}
}

func collectTableFromDelete(ctx parser.IDelete_stmtContext, c *collectListener) {
	if ctx == nil {
		return
	}
	dc, ok := ctx.(*parser.Delete_stmtContext)
	if !ok || dc.Simple_table_ref() == nil {
		return
	}
	if core := dc.Simple_table_ref().Simple_table_ref_core(); core != nil {
		addTable(c, core.GetText())
	}
}

func collectTableFromUpdate(ctx parser.IUpdate_stmtContext, c *collectListener) {
	if ctx == nil {
		return
	}
	uc, ok := ctx.(*parser.Update_stmtContext)
	if !ok || uc.Simple_table_ref() == nil {
		return
	}
	if core := uc.Simple_table_ref().Simple_table_ref_core(); core != nil {
		addTable(c, core.GetText())
	}
}

func (c *collectListener) enterIntoTableStmt(n *parser.Into_table_stmtContext) {
	if n == nil {
		return
	}
	switch {
	case n.UPSERT() != nil:
		c.stmtKind = StmtKindUpsert
	case n.REPLACE() != nil:
		c.stmtKind = StmtKindReplace
	default:
		c.stmtKind = StmtKindInsert
	}
	if n.Into_simple_table_ref() != nil {
		str := n.Into_simple_table_ref().Simple_table_ref()
		if str != nil && str.Simple_table_ref_core() != nil {
			addTable(c, str.Simple_table_ref_core().GetText())
		}
	}
}

func addTable(c *collectListener, s string) {
	name := identifier(s)
	if name != "" && !c.seenTables[name] {
		c.seenTables[name] = true
		c.tableNames = append(c.tableNames, name)
	}
}

// OrderedParams returns unique parameter names in order of first occurrence (by token start).
func (c *collectListener) OrderedParams() []string {
	sort.Slice(c.params, func(i, j int) bool { return c.params[i].start < c.params[j].start })
	seen := make(map[string]bool)
	var out []string
	for _, p := range c.params {
		if seen[p.name] {
			continue
		}
		seen[p.name] = true
		out = append(out, p.name)
	}
	return out
}

// TableNames returns table names in visit order (no duplicates).
func (c *collectListener) TableNames() []string {
	return c.tableNames
}

// StmtKind returns the detected statement kind (select/insert/update/delete/upsert/replace).
func (c *collectListener) StmtKind() StmtKind {
	return c.stmtKind
}

// HasSelectStar reports whether the query has SELECT *.
func (c *collectListener) HasSelectStar() bool {
	return c.hasSelectStar
}

// HasReturningStar reports whether the query has RETURNING *.
func (c *collectListener) HasReturningStar() bool {
	return c.hasReturningStar
}

// ReturnsRows is true for SELECT and for DML with RETURNING (insert/upsert/replace/update/delete ... RETURNING ...).
func (c *collectListener) ReturnsRows() bool {
	switch c.stmtKind {
	case StmtKindSelect:
		return true
	case StmtKindInsert, StmtKindUpsert, StmtKindReplace, StmtKindUpdate, StmtKindDelete:
		return c.hasReturningStar
	default:
		return false
	}
}

// runCollect runs the listener over the parse tree and returns it for params/table access.
func runCollect(tree antlr.ParseTree) *collectListener {
	lis := newCollectListener()
	antlr.NewParseTreeWalker().Walk(lis, tree)
	return lis
}

// VisitBind_parameter treats only $name (An_id_or_type) as a parameter;
// $true / $false and other named expressions like $myFunc in value context
// are identified by the parser — here we only collect when DOLLAR + An_id_or_type.
func (v *paramAndTablesVisitor) VisitBind_parameter(n *parser.Bind_parameterContext) interface{} {
	if n == nil || n.DOLLAR() == nil {
		return v.VisitChildren(n)
	}
	if n.TRUE() != nil || n.FALSE() != nil {
		return v.VisitChildren(n)
	}
	if an := n.An_id_or_type(); an != nil {
		name := anIdOrTypeToName(an)
		if name != "" {
			start := 0
			if t := n.GetStart(); t != nil {
				start = t.GetStart()
			}
			v.params = append(v.params, paramPos{start: start, name: name})
		}
	}
	return v.VisitChildren(n)
}

// VisitTable_ref collects table name from Table_key (FROM t / JOIN t).
func (v *paramAndTablesVisitor) VisitTable_ref(n *parser.Table_refContext) interface{} {
	if n != nil && n.Table_key() != nil {
		name := identifier(n.Table_key().GetText())
		if name != "" {
			v.addTable(name)
		}
	}
	return v.VisitChildren(n)
}

// VisitDelete_stmt collects target table from Simple_table_ref.
func (v *paramAndTablesVisitor) VisitDelete_stmt(n *parser.Delete_stmtContext) interface{} {
	if n != nil && n.Simple_table_ref() != nil {
		if core := n.Simple_table_ref().Simple_table_ref_core(); core != nil {
			name := identifier(core.GetText())
			if name != "" {
				v.addTable(name)
			}
		}
	}
	return v.VisitChildren(n)
}

// VisitUpdate_stmt collects target table from Simple_table_ref.
func (v *paramAndTablesVisitor) VisitUpdate_stmt(n *parser.Update_stmtContext) interface{} {
	if n != nil && n.Simple_table_ref() != nil {
		if core := n.Simple_table_ref().Simple_table_ref_core(); core != nil {
			name := identifier(core.GetText())
			if name != "" {
				v.addTable(name)
			}
		}
	}
	return v.VisitChildren(n)
}

// VisitInto_table_stmt collects target table (INSERT INTO t).
func (v *paramAndTablesVisitor) VisitInto_table_stmt(n *parser.Into_table_stmtContext) interface{} {
	if n != nil && n.Into_simple_table_ref() != nil {
		str := n.Into_simple_table_ref().Simple_table_ref()
		if str != nil && str.Simple_table_ref_core() != nil {
			name := identifier(str.Simple_table_ref_core().GetText())
			if name != "" {
				v.addTable(name)
			}
		}
	}
	return v.VisitChildren(n)
}

func (v *paramAndTablesVisitor) addTable(name string) {
	if !v.seenTables[name] {
		v.seenTables[name] = true
		v.tableNames = append(v.tableNames, name)
	}
}

// orderedParams returns unique parameter names in order of first occurrence (by token start).
func (v *paramAndTablesVisitor) orderedParams() []string {
	sort.Slice(v.params, func(i, j int) bool { return v.params[i].start < v.params[j].start })
	seen := make(map[string]bool)
	var out []string
	for _, p := range v.params {
		if seen[p.name] {
			continue
		}
		seen[p.name] = true
		out = append(out, p.name)
	}
	return out
}

func identifier(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		// minimal unquote: strip and leave as-is for uniqueness
		s = s[1 : len(s)-1]
	}
	return strings.ToLower(s)
}

// anIdOrTypeToName returns the identifier from An_id_or_type (used for $param names).
func anIdOrTypeToName(ctx parser.IAn_id_or_typeContext) string {
	if ctx == nil {
		return ""
	}
	// IAn_id_or_typeContext can be Id_or_type or STRING_VALUE; GetText() gives the token text.
	return identifier(ctx.GetText())
}
