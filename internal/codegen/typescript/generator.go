// Package typescript generates modern ESM modules for the modular YDB JavaScript SDK.
package typescript

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

type Options struct {
	Runtime string
}

type typeInfo struct {
	tsType     string
	valueClass string
	typeClass  string
}

var typescriptTypes = map[string]typeInfo{
	"Bool":         {"boolean", "Bool", "BoolType"},
	"Int8":         {"number", "Int8", "Int8Type"},
	"Uint8":        {"number", "Uint8", "Uint8Type"},
	"Int16":        {"number", "Int16", "Int16Type"},
	"Uint16":       {"number", "Uint16", "Uint16Type"},
	"Int32":        {"number", "Int32", "Int32Type"},
	"Uint32":       {"number", "Uint32", "Uint32Type"},
	"Int64":        {"bigint", "Int64", "Int64Type"},
	"Uint64":       {"bigint", "Uint64", "Uint64Type"},
	"Float":        {"number", "Float", "FloatType"},
	"Double":       {"number", "Double", "DoubleType"},
	"Utf8":         {"string", "Utf8", "Utf8Type"},
	"String":       {"Uint8Array", "Bytes", "BytesType"},
	"Json":         {"string", "Json", "JsonType"},
	"JsonDocument": {"string", "JsonDocument", "JsonDocumentType"},
	"Timestamp":    {"Date", "Timestamp", "TimestampType"},
}

var reserved = func() map[string]bool {
	m := map[string]bool{"constructor": true, "configure": true, "sql": true, "stmt": true, "rows": true, "row": true, "args": true}
	for _, word := range strings.Fields("await break case catch class const continue debugger default delete do else enum export extends false finally for function if implements import in instanceof interface let new null package private protected public return static super switch this throw true try typeof var void while with yield arguments eval") {
		m[word] = true
	}
	return m
}()

func Generate(a *model.AnalysisResult, options Options) ([]model.File, error) {
	if a == nil {
		return nil, fmt.Errorf("typescript generator: nil analysis result")
	}
	if len(a.Diagnostics) != 0 {
		return nil, fmt.Errorf("typescript generator: analysis has diagnostics: %s", a.Diagnostics[0])
	}
	if options.Runtime == "" {
		options.Runtime = "ydb"
	}
	if options.Runtime != "ydb" {
		return nil, fmt.Errorf("typescript generator: unsupported TypeScript runtime %q", options.Runtime)
	}
	if err := validate(a); err != nil {
		return nil, err
	}
	ts, err := renderTypeScript(a)
	if err != nil {
		return nil, err
	}
	return []model.File{{Name: "queries.ts", Content: []byte(strings.TrimRight(ts, "\n") + "\n")}}, nil
}

func validate(a *model.AnalysisResult) error {
	methods := map[string]string{}
	for _, query := range a.Queries {
		switch query.Command {
		case model.One, model.Many, model.Exec:
		case model.ExecRows:
			return fmt.Errorf("typescript generator: query %q: :execrows is unsupported because the YDB JavaScript SDK does not expose a portable affected-row count", query.Name)
		default:
			return fmt.Errorf("typescript generator: query %q: unsupported command %q", query.Name, query.Command)
		}
		if !utf8.ValidString(query.SQL) {
			return fmt.Errorf("typescript generator: query %q: SQL is not valid UTF-8", query.Name)
		}
		method, err := identifier(query.Name, false)
		if err != nil {
			return fmt.Errorf("typescript generator: query %q: %w", query.Name, err)
		}
		if previous, exists := methods[method]; exists {
			return fmt.Errorf("typescript generator: method name collision %q between %q and %q", method, previous, query.Name)
		}
		methods[method] = query.Name
		seenParameters := map[string]string{}
		for _, parameter := range query.Parameters {
			field, err := identifier(parameter.Name, false)
			if err != nil {
				return fmt.Errorf("typescript generator: query %q parameter %q: %w", query.Name, parameter.Name, err)
			}
			if previous, exists := seenParameters[field]; exists {
				return fmt.Errorf("typescript generator: query %q: parameter name collision %q between %q and %q", query.Name, field, previous, parameter.Name)
			}
			seenParameters[field] = parameter.Name
			if _, err := tsType(parameter.Type); err != nil {
				return fmt.Errorf("typescript generator: query %q parameter %q: %w", query.Name, parameter.Name, err)
			}
		}
		if len(query.Parameters) != 0 && query.SQLWithoutDeclarations == "" {
			return fmt.Errorf("typescript generator: query %q: analyzer did not provide SQL without declarations required for typed SDK parameters", query.Name)
		}
		if !utf8.ValidString(query.SQLWithoutDeclarations) {
			return fmt.Errorf("typescript generator: query %q: SQL without declarations is not valid UTF-8", query.Name)
		}
		if query.Command == model.One || query.Command == model.Many {
			if len(query.ResultSets) != 1 {
				return fmt.Errorf("typescript generator: query %q: expected one result set, got %d", query.Name, len(query.ResultSets))
			}
			seenColumns := map[string]string{}

			for _, column := range query.ResultSets[0].Columns {
				field := column.ResultName()
				if !utf8.ValidString(field) {
					return fmt.Errorf("typescript generator: query %q: result key is not valid UTF-8", query.Name)
				}
				if previous, exists := seenColumns[field]; exists {
					return fmt.Errorf("typescript generator: query %q: column name collision %q between %q and %q", query.Name, field, previous, column.Name)
				}
				seenColumns[field] = column.Name
				if _, err := tsType(column.Type); err != nil {
					return fmt.Errorf("typescript generator: query %q column %q: %w", query.Name, column.Name, err)
				}
			}
		}
	}
	return nil
}

func tsType(t model.Type) (string, error) {
	if t.IsOptional() {
		if t.Elem == nil || t.Elem.IsOptional() {
			return "", fmt.Errorf("malformed or nested Optional type")
		}
		inner, err := tsType(*t.Elem)
		if err != nil {
			return "", err
		}
		return inner + " | null", nil
	}
	info, ok := typescriptTypes[t.Kind]
	if !ok {
		return "", fmt.Errorf("unsupported YQL type %q", t.Kind)
	}
	return info.tsType, nil
}

func resultType(t model.Type) string {
	base := t.UnwrapOptional()
	if base.Kind == "Json" || base.Kind == "JsonDocument" {
		return "JSValue"
	}
	typ, _ := tsType(t)
	return typ
}

func renderTypeScript(a *model.AnalysisResult) (string, error) {

	classes := map[string]bool{}
	optional, jsonResult := false, false
	for _, q := range a.Queries {
		for _, p := range q.Parameters {
			info := typescriptTypes[p.Type.UnwrapOptional().Kind]
			classes[info.valueClass] = true
			if p.Type.IsOptional() {
				optional = true
				classes[info.typeClass] = true
			}
		}
		for _, rs := range q.ResultSets {
			for _, c := range rs.Columns {
				kind := c.Type.UnwrapOptional().Kind
				jsonResult = jsonResult || kind == "Json" || kind == "JsonDocument"
			}
		}
	}
	imports := make([]string, 0, len(classes))
	for class := range classes {
		imports = append(imports, class)
	}
	sort.Strings(imports)
	var b strings.Builder
	b.WriteString("// Code generated by sqlc-ydb. DO NOT EDIT.\nimport type { Query, SQL } from \"@ydbjs/query\";\n")
	if jsonResult {
		b.WriteString("import type { JSValue } from \"@ydbjs/value\";\n")
	}
	if len(imports) > 0 {
		b.WriteString("import { " + strings.Join(imports, ", ") + " } from \"@ydbjs/value/primitive\";\n")
	}
	if optional {
		b.WriteString("import { Optional } from \"@ydbjs/value/optional\";\n")
	}
	b.WriteString("\nexport type ConfigureQuery = (query: Query) => void;\n\n")
	for _, q := range a.Queries {
		if len(q.Parameters) > 1 {
			b.WriteString("export type " + exportedName(q.Name) + "Params = {\n")
			for _, p := range q.Parameters {
				field, _ := identifier(p.Name, false)
				typ, _ := tsType(p.Type)
				b.WriteString("  readonly " + field + ": " + typ + ";\n")
			}
			b.WriteString("};\n\n")
		}
		if len(q.ResultSets) > 0 {
			renderRowType(&b, "export type "+exportedName(q.Name)+"Row", q)
		}
	}

	b.WriteString("export class Queries {\n  readonly #sql: SQL;\n\n  constructor(sql: SQL) {\n    if (typeof sql !== \"function\") throw new TypeError(\"Queries requires a YDB SQL function\");\n    this.#sql = sql;\n  }\n")
	for _, q := range a.Queries {
		renderMethod(&b, q)
	}
	b.WriteString("}\n")
	return b.String(), nil
}

// DECLARE removal leaves whitespace so analyzer source offsets stay stable.
// Omit only the emptied lines, retaining original blank lines and comments.
func omitDeclarationLines(original, executable string) string {
	originalLines, lines := strings.Split(original, "\n"), strings.Split(executable, "\n")
	if len(originalLines) != len(lines) {
		return executable
	}
	kept := make([]string, 0, len(lines))
	for i, line := range lines {
		if strings.TrimSpace(line) == "" && strings.TrimSpace(originalLines[i]) != "" {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func renderRowType(b *strings.Builder, name string, q model.AnalyzedQuery) {
	b.WriteString(name + " = {\n")
	for _, c := range q.ResultSets[0].Columns {
		field := c.ResultName()
		if !plainProperty(field) || reserved[field] {
			field = strconv.Quote(field)
		}
		b.WriteString("  readonly " + field + ": " + resultType(c.Type) + ";\n")
	}
	b.WriteString("};\n\n")
}

func plainProperty(s string) bool {
	for i, r := range s {
		if !(r == '_' || r == '$' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return s != ""
}

func renderMethod(b *strings.Builder, q model.AnalyzedQuery) {
	method, _ := identifier(q.Name, false)
	params := ""
	if len(q.Parameters) == 1 {
		p := q.Parameters[0]
		field, _ := identifier(p.Name, false)
		typ, _ := tsType(p.Type)
		params = field + ": " + typ + ", "
	}
	if len(q.Parameters) > 1 {
		params = "args: " + exportedName(q.Name) + "Params, "
	}
	ret, generic := "void", ""
	if q.Command != model.Exec {
		row := exportedName(q.Name) + "Row"
		ret = row + "[]"
		if q.Command == model.One {
			ret = row + " | null"
		}
		generic = "<[" + row + "]>"
	}
	b.WriteString("\n  // " + model.QueryAnnotation(q) + "\n")
	b.WriteString("  async " + method + "(" + params + "configure?: ConfigureQuery): Promise<" + ret + "> {\n")
	sql := q.SQL
	if len(q.Parameters) > 0 {
		sql = omitDeclarationLines(q.SQL, q.SQLWithoutDeclarations)
	}
	sql = model.WithoutQueryAnnotation(sql)
	b.WriteString("    const stmt = this.#sql" + generic + sqlLiteral(strings.TrimSpace(sql)))
	for _, p := range q.Parameters {
		field, _ := identifier(p.Name, false)
		if len(q.Parameters) > 1 {
			field = "args." + field
		}
		b.WriteString("\n      .parameter(" + strconv.Quote(p.Name) + ", " + bindExpression(p.Type, field) + ")")
	}
	b.WriteString(";\n    configure?.(stmt);\n")
	if q.Command == model.Exec {
		b.WriteString("    await stmt;\n  }\n")
		return
	}
	b.WriteString("    const [rows] = await stmt;\n")
	if q.Command == model.One {
		b.WriteString("\n    return rows[0] ?? null;\n")
	} else {
		b.WriteString("\n    return rows;\n")
	}
	b.WriteString("  }\n")
}

func bindExpression(t model.Type, value string) string {
	info := typescriptTypes[t.UnwrapOptional().Kind]
	wrapped := "new " + info.valueClass + "(" + value + ")"
	if t.IsOptional() {
		return "new Optional(" + value + " === null ? null : " + wrapped + ", new " + info.typeClass + "())"
	}
	return wrapped
}

func sqlLiteral(value string) string {
	var b strings.Builder
	b.WriteByte('`')
	for i := 0; i < len(value); {
		if value[i] == ' ' || value[i] == '\t' {
			end := i
			for end < len(value) && (value[end] == ' ' || value[end] == '\t') {
				end++
			}
			if end == len(value) || value[end] == '\r' || value[end] == '\n' {
				for ; i < end; i++ {
					if value[i] == ' ' {
						b.WriteString(`\x20`)
					} else {
						b.WriteString(`\t`)
					}
				}
			} else {
				b.WriteString(value[i:end])
				i = end
			}
			continue
		}
		r, size := utf8.DecodeRuneInString(value[i:])
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '`':
			b.WriteString("\\`")
		case '$':
			if i+size < len(value) && value[i+size] == '{' {
				b.WriteString(`\$`)
			} else {
				b.WriteRune(r)
			}
		case '\r':
			b.WriteString(`\r`)
		case '\n':
			b.WriteString("\n      ")
		default:
			if r < 0x20 && r != '\n' && r != '\t' {
				fmt.Fprintf(&b, "\\x%02x", r)
			} else if r == 0x7f || r == '\u2028' || r == '\u2029' {
				fmt.Fprintf(&b, "\\u%04x", r)
			} else {
				b.WriteRune(r)
			}
		}
		i += size
	}
	b.WriteByte('`')
	return b.String()
}

func identifier(value string, exported bool) (string, error) {
	for _, r := range value {
		if r > unicode.MaxASCII || !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == ' ' || r == '.') {
			return "", fmt.Errorf("cannot represent %q as a TypeScript identifier", value)
		}
	}
	words := splitWords(value)
	if len(words) == 0 {
		return "", fmt.Errorf("cannot represent %q as a TypeScript identifier", value)
	}
	var b strings.Builder
	for i, word := range words {
		if i == 0 && !exported {
			b.WriteString(strings.ToLower(word[:1]) + word[1:])
		} else {
			b.WriteString(strings.ToUpper(word[:1]) + word[1:])
		}
	}
	result := b.String()
	if reserved[result] {
		result += "_"
	}
	if result == "" || result[0] >= '0' && result[0] <= '9' {
		return "", fmt.Errorf("cannot represent %q as a TypeScript identifier", value)
	}
	return result, nil
}

func splitWords(value string) []string {
	var words []string
	start := -1
	runes := []rune(value)
	flush := func(end int) {
		if start >= 0 && end > start {
			words = append(words, string(runes[start:end]))
		}
		start = -1
	}
	for i, r := range runes {
		if r > unicode.MaxASCII || !(unicode.IsLetter(r) || unicode.IsDigit(r)) {
			flush(i)
			continue
		}
		if start < 0 {
			start = i
			continue
		}
		if unicode.IsUpper(r) && i > start && unicode.IsLower(runes[i-1]) {
			flush(i)
			start = i
		}
	}
	flush(len(runes))
	return words
}

func exportedName(value string) string {
	result, _ := identifier(value, true)
	return result
}
