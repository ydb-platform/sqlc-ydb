// Package golang renders the resolved YQL model as ordinary Go source.
package golang

import (
	"bytes"
	"fmt"
	"go/format"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/ydb-platform/sqlc-engine-ydb/internal/model"
)

type Options struct {
	Package         string
	Runtime         string // ydb or database/sql
	EmitJSONTags    bool
	EmitInterface   bool
	EmitEmptySlices bool
}

func Generate(in *model.AnalysisResult, o Options) ([]model.File, error) {
	if in == nil {
		return nil, fmt.Errorf("analysis result is nil")
	}
	if len(in.Diagnostics) != 0 {
		return nil, fmt.Errorf("cannot generate with diagnostics: %s", in.Diagnostics[0])
	}
	if o.Package == "" {
		o.Package = "db"
	}
	if !ident(o.Package) {
		return nil, fmt.Errorf("invalid Go package %q", o.Package)
	}
	if o.Runtime == "" {
		o.Runtime = "ydb"
	}
	if o.Runtime != "ydb" && o.Runtime != "database/sql" {
		return nil, fmt.Errorf("unsupported Go runtime %q", o.Runtime)
	}
	if err := validate(in, o); err != nil {
		return nil, err
	}
	files := []model.File{{Name: "models.go", Content: models(in, o)}, {Name: "db.go", Content: db(o)}}
	bySource := map[string][]model.AnalyzedQuery{}
	for _, q := range in.Queries {
		n := q.Source.File
		if n == "" {
			n = "query.sql"
		}
		bySource[n] = append(bySource[n], q)
	}
	names := make([]string, 0, len(bySource))
	for n := range bySource {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		files = append(files, model.File{Name: outputName(n), Content: queryFile(n, bySource[n], o)})
	}
	for i := range files {
		formatted, err := format.Source(files[i].Content)
		if err != nil {
			return nil, fmt.Errorf("format %s: %w", files[i].Name, err)
		}
		files[i].Content = formatted
	}
	return files, nil
}

func validate(in *model.AnalysisResult, o Options) error {
	seen := map[string]bool{}
	outputs := map[string]string{}
	for _, q := range in.Queries {
		if !ident(q.Name) || seen[q.Name] {
			return fmt.Errorf("invalid or duplicate query name %q", q.Name)
		}
		seen[q.Name] = true
		source := sourceName(q)
		out := outputName(source)
		if previous, ok := outputs[out]; ok && previous != source {
			return fmt.Errorf("multiple query sources map to generated file %q", out)
		}
		outputs[out] = source
		if q.Command != model.One && q.Command != model.Many && q.Command != model.Exec && q.Command != model.ExecRows {
			return fmt.Errorf("%s: unsupported command %q", q.Name, q.Command)
		}
		if q.Command == model.ExecRows {
			return fmt.Errorf("%s: :execrows is unavailable for %s", q.Name, o.Runtime)
		}
		if (q.Command == model.One || q.Command == model.Many) && (len(q.ResultSets) != 1 || len(q.ResultSets[0].Columns) == 0) {
			return fmt.Errorf("%s: %s requires one non-empty result set", q.Name, q.Command)
		}
		field := map[string]bool{}
		for _, p := range q.Parameters {
			if !ident(goName(p.Name)) || field[goName(p.Name)] {
				return fmt.Errorf("%s: colliding parameter %q", q.Name, p.Name)
			}
			field[goName(p.Name)] = true
			if _, err := goType(p.Type); err != nil {
				return fmt.Errorf("%s parameter %s: %w", q.Name, p.Name, err)
			}
			if containsList(p.Type) {
				return fmt.Errorf("%s parameter %s: List parameters are not supported by the Go runtimes", q.Name, p.Name)
			}
		}
		for _, rs := range q.ResultSets {
			field := map[string]bool{}
			for _, c := range rs.Columns {
				n := goName(c.Name)
				if !ident(n) || field[n] {
					return fmt.Errorf("%s: colliding result column %q", q.Name, c.Name)
				}
				field[n] = true
				if _, err := goType(c.Type); err != nil {
					return fmt.Errorf("%s column %s: %w", q.Name, c.Name, err)
				}
				if o.Runtime == "database/sql" && containsList(c.Type) {
					return fmt.Errorf("%s column %s: List results are unsupported by database/sql", q.Name, c.Name)
				}
				if optionalList(c.Type) {
					return fmt.Errorf("%s column %s: Optional<List> results are unsupported", q.Name, c.Name)
				}
			}
		}
	}
	return nil
}

func containsList(t model.Type) bool {
	if strings.EqualFold(t.Kind, "List") {
		return true
	}
	if t.Elem != nil {
		return containsList(*t.Elem)
	}
	return false
}

func optionalList(t model.Type) bool {
	return t.IsOptional() && t.Elem != nil && containsList(*t.Elem)
}

func sourceName(q model.AnalyzedQuery) string {
	if q.Source.File == "" {
		return "query.sql"
	}
	return q.Source.File
}

func goType(t model.Type) (string, error) {
	if t.IsOptional() {
		if t.Elem == nil {
			return "", fmt.Errorf("Optional lacks element")
		}
		e, err := goType(*t.Elem)
		return "*" + e, err
	}
	switch strings.ToLower(t.Kind) {
	case "bool":
		return "bool", nil
	case "int8":
		return "int8", nil
	case "int16":
		return "int16", nil
	case "int32":
		return "int32", nil
	case "int64":
		return "int64", nil
	case "uint8":
		return "uint8", nil
	case "uint16":
		return "uint16", nil
	case "uint32":
		return "uint32", nil
	case "uint64":
		return "uint64", nil
	case "float":
		return "float32", nil
	case "double":
		return "float64", nil
	case "utf8", "json", "jsondocument":
		return "string", nil
	case "string", "yson":
		return "[]byte", nil
	case "timestamp", "timestamp64", "date", "date32", "datetime", "datetime64":
		return "time.Time", nil
	case "interval", "interval64":
		return "time.Duration", nil
	case "list":
		if t.Elem == nil {
			return "", fmt.Errorf("List lacks element")
		}
		e, err := goType(*t.Elem)
		return "[]" + e, err
	default:
		return "", fmt.Errorf("unsupported YQL type %q", t.Kind)
	}
}

func models(in *model.AnalysisResult, o Options) []byte {
	var b bytes.Buffer
	needsTime := false
	for _, q := range in.Queries {
		if q.Command == model.One || q.Command == model.Many {
			r := q.ResultSets[0]
			b.WriteString("type " + q.Name + "Row struct {\n")
			for _, c := range r.Columns {
				typ, _ := goType(c.Type)
				if strings.Contains(typ, "time.") {
					needsTime = true
				}
				b.WriteString(goName(c.Name) + " " + typ)
				if o.EmitJSONTags {
					b.WriteString(" `json:" + strconv.Quote(c.Name) + "`")
				}
				b.WriteString("\n")
			}
			b.WriteString("}\n\n")
		}
		if len(q.Parameters) > 1 {
			b.WriteString("type " + q.Name + "Params struct {\n")
			for _, p := range q.Parameters {
				typ, _ := goType(p.Type)
				if strings.Contains(typ, "time.") {
					needsTime = true
				}
				b.WriteString(goName(p.Name) + " " + typ)
				if o.EmitJSONTags {
					b.WriteString(" `json:" + strconv.Quote(p.Name) + "`")
				}
				b.WriteString("\n")
			}
			b.WriteString("}\n\n")
		}
	}
	if o.EmitInterface {
		b.WriteString("type Querier interface {\n")
		for _, q := range in.Queries {
			_, sig := methodArgs(q)
			ret := "error"
			if q.Command == model.One {
				ret = "(" + q.Name + "Row, error)"
			}
			if q.Command == model.Many {
				ret = "([]" + q.Name + "Row, error)"
			}
			if q.Command == model.ExecRows {
				ret = "(int64, error)"
			}
			b.WriteString(q.Name + "(ctx context.Context" + sig + ") " + ret + "\n")
		}
		b.WriteString("}\n")
	}
	imports := []string{}
	if needsTime {
		imports = append(imports, "\"time\"")
	}
	if o.EmitInterface {
		imports = append(imports, "\"context\"")
	}
	var head strings.Builder
	head.WriteString("// Code generated by sqlc-ydb. DO NOT EDIT.\npackage " + o.Package + "\n\n")
	if len(imports) == 1 {
		head.WriteString("import " + imports[0] + "\n\n")
	} else if len(imports) > 1 {
		head.WriteString("import (" + strings.Join(imports, ";") + ")\n\n")
	}
	return append([]byte(head.String()), b.Bytes()...)
}

func db(o Options) []byte {
	if o.Runtime == "database/sql" {
		return []byte(`// Code generated by sqlc-ydb. DO NOT EDIT.
package ` + o.Package + `
import ("context"; "database/sql")
type DBTX interface { ExecContext(context.Context,string,...any)(sql.Result,error); QueryContext(context.Context,string,...any)(*sql.Rows,error); QueryRowContext(context.Context,string,...any)*sql.Row }
type Queries struct { db DBTX }
func New(db DBTX) *Queries { return &Queries{db:db} }
func (q *Queries) WithTx(tx *sql.Tx) *Queries { return &Queries{db:tx} }
`)
	}
	return []byte(`// Code generated by sqlc-ydb. DO NOT EDIT.
package ` + o.Package + `
import ("context"; "github.com/ydb-platform/ydb-go-sdk/v3/query")
type DBTX interface { Exec(context.Context,string,...query.ExecuteOption) error; QueryResultSet(context.Context,string,...query.ExecuteOption)(query.ClosableResultSet,error); QueryRow(context.Context,string,...query.ExecuteOption)(query.Row,error) }
type Queries struct { db DBTX }
func New(db DBTX) *Queries { return &Queries{db:db} }
`)
}

func queryFile(source string, qs []model.AnalyzedQuery, o Options) []byte {
	var b bytes.Buffer
	b.WriteString("// Code generated by sqlc-ydb. DO NOT EDIT.\n// source: " + filepath.Base(source) + "\npackage " + o.Package + "\n\n")
	if o.Runtime == "database/sql" {
		b.WriteString("import (\"context\"; \"database/sql\")\n\n")
	} else {
		b.WriteString("import (\"context\"; ydb \"github.com/ydb-platform/ydb-go-sdk/v3\"; \"github.com/ydb-platform/ydb-go-sdk/v3/query\")\n\n")
	}
	for _, q := range qs {
		writeQuery(&b, q, o)
	}
	return b.Bytes()
}

func writeQuery(b *bytes.Buffer, q model.AnalyzedQuery, o Options) {
	c := "const " + lower(q.Name) + " = " + strconv.Quote(q.SQL) + "\n\n"
	b.WriteString(c)
	ret := "error"
	if q.Command == model.One {
		ret = "(" + q.Name + "Row, error)"
	}
	if q.Command == model.Many {
		ret = "([]" + q.Name + "Row, error)"
	}
	if q.Command == model.ExecRows {
		ret = "(int64, error)"
	}
	args, sig := methodArgs(q)
	b.WriteString("func (q *Queries) " + q.Name + "(ctx context.Context" + sig + ") " + ret + " {\n")
	if o.Runtime == "database/sql" {
		writeSQL(b, q, args, o)
	} else {
		writeYDB(b, q, args, o)
	}
	b.WriteString("}\n\n")
}

func methodArgs(q model.AnalyzedQuery) (string, string) {
	if len(q.Parameters) == 0 {
		return "", ""
	}
	if len(q.Parameters) > 1 {
		return "arg", ", arg " + q.Name + "Params"
	}
	t, _ := goType(q.Parameters[0].Type)
	return lower(q.Parameters[0].Name), ", " + lower(q.Parameters[0].Name) + " " + t
}
func varRef(q model.AnalyzedQuery, p model.Parameter) string {
	if len(q.Parameters) > 1 {
		return "arg." + goName(p.Name)
	}
	return lower(p.Name)
}
func sqlArgs(q model.AnalyzedQuery) string {
	x := make([]string, len(q.Parameters))
	for i, p := range q.Parameters {
		x[i] = "sql.Named(" + strconv.Quote(p.Name) + ", " + varRef(q, p) + ")"
	}
	if len(x) == 0 {
		return ""
	}
	return ", " + strings.Join(x, ", ")
}
func writeSQL(b *bytes.Buffer, q model.AnalyzedQuery, args string, o Options) {
	a := sqlArgs(q)
	switch q.Command {
	case model.Exec:
		b.WriteString("_, err := q.db.ExecContext(ctx, " + lower(q.Name) + a + ")\nreturn err\n")
	case model.ExecRows:
		b.WriteString("result, err := q.db.ExecContext(ctx, " + lower(q.Name) + a + ")\nif err != nil { return 0, err }; return result.RowsAffected()\n")
	case model.One:
		b.WriteString("var row " + q.Name + "Row\nerr := q.db.QueryRowContext(ctx, " + lower(q.Name) + a + ").Scan(" + scan(q.ResultSets[0]) + ")\nreturn row, err\n")
	case model.Many:
		init := "[]" + q.Name + "Row(nil)"
		if o.EmitEmptySlices {
			init = "make([]" + q.Name + "Row, 0)"
		}
		b.WriteString("rows, err := q.db.QueryContext(ctx, " + lower(q.Name) + a + ")\nif err != nil { return " + init + ", err }; defer rows.Close()\nitems := " + init + "\nfor rows.Next() { var row " + q.Name + "Row\nif err := rows.Scan(" + scan(q.ResultSets[0]) + "); err != nil { return nil, err }; items = append(items, row) }\nreturn items, rows.Err()\n")
	}
}
func writeYDB(b *bytes.Buffer, q model.AnalyzedQuery, args string, o Options) {
	opt := params(q)
	if q.Command == model.Exec {
		b.WriteString("return q.db.Exec(ctx, " + lower(q.Name) + opt + ")\n")
		return
	}
	if q.Command == model.One {
		b.WriteString("result, err := q.db.QueryRow(ctx, " + lower(q.Name) + opt + ")\nif err != nil { return " + q.Name + "Row{}, err }; var row " + q.Name + "Row\nif err := result.Scan(" + scan(q.ResultSets[0]) + "); err != nil { return " + q.Name + "Row{}, err }; return row, nil\n")
		return
	}
	init := "[]" + q.Name + "Row(nil)"
	if o.EmitEmptySlices {
		init = "make([]" + q.Name + "Row, 0)"
	}
	b.WriteString("result, err := q.db.QueryResultSet(ctx, " + lower(q.Name) + opt + ")\nif err != nil { return " + init + ", err }; defer result.Close(ctx)\nitems := " + init + "\nfor r, err := range result.Rows(ctx) { if err != nil { return nil, err }; var row " + q.Name + "Row; if err := r.Scan(" + scan(q.ResultSets[0]) + "); err != nil { return nil, err }; items = append(items, row) }\nreturn items, nil\n")
}
func scan(rs model.ResultSet) string {
	x := make([]string, len(rs.Columns))
	for i, c := range rs.Columns {
		x[i] = "&row." + goName(c.Name)
	}
	return strings.Join(x, ", ")
}
func params(q model.AnalyzedQuery) string {
	if len(q.Parameters) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("ydb.ParamsBuilder()")
	for _, p := range q.Parameters {
		method := paramMethod(p.Type)
		n := strconv.Quote("$" + p.Name)
		v := varRef(q, p)
		b.WriteString(".Param(" + n + ")")
		if p.Type.IsOptional() {
			b.WriteString(".BeginOptional()." + method + "(" + v + ").EndOptional()")
		} else {
			b.WriteString("." + method + "(" + v + ")")
		}
	}
	b.WriteString(".Build()")
	return ", query.WithParameters(" + b.String() + ")"
}
func paramMethod(t model.Type) string {
	if t.IsOptional() {
		t = *t.Elem
	}
	switch strings.ToLower(t.Kind) {
	case "utf8":
		return "Text"
	case "string":
		return "Bytes"
	case "float":
		return "Float"
	case "double":
		return "Double"
	case "timestamp":
		return "Timestamp"
	case "date":
		return "Date"
	case "datetime":
		return "Datetime"
	case "interval":
		return "Interval"
	default:
		return goName(t.Kind)
	}
}
func outputName(s string) string {
	s = filepath.Base(s)
	s = strings.TrimSuffix(s, ".sql")
	return s + ".sql.go"
}
func lower(s string) string {
	if s == "" {
		return "q"
	}
	return strings.ToLower(s[:1]) + s[1:]
}
func goName(s string) string {
	var b strings.Builder
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if strings.EqualFold(part, "id") {
			b.WriteString("ID")
			continue
		}
		for i, r := range part {
			if i == 0 {
				b.WriteRune(unicode.ToUpper(r))
			} else {
				b.WriteRune(r)
			}
		}
	}
	x := b.String()
	if x == "" {
		return "Value"
	}
	if x == "Id" {
		return "ID"
	}
	return x
}
func ident(s string) bool {
	if s == "" {
		return false
	}
	if goKeywords[s] {
		return false
	}
	for i, r := range s {
		if !(r == '_' || unicode.IsLetter(r) || (i > 0 && unicode.IsDigit(r))) {
			return false
		}
	}
	return true
}

var goKeywords = map[string]bool{"break": true, "default": true, "func": true, "interface": true, "select": true, "case": true, "defer": true, "go": true, "map": true, "struct": true, "chan": true, "else": true, "goto": true, "package": true, "switch": true, "const": true, "fallthrough": true, "if": true, "range": true, "type": true, "continue": true, "for": true, "import": true, "return": true, "var": true}
