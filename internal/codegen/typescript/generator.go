// Package typescript generates modern ESM modules for the modular YDB JavaScript SDK.
package typescript

import (
	"encoding/json"
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
	m := map[string]bool{"constructor": true, "configure": true, "structList": true, "sql": true, "stmt": true, "rows": true, "row": true, "args": true}
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
		return nil, fmt.Errorf("typescript generator: analysis has diagnostics: %w", a.Diagnostics[0])
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
	itemTypes := map[string]bool{}
	embeddedTypes := map[string]string{}
	for _, query := range a.Queries {
		for _, p := range query.Parameters {
			if isStructList(p.Type) {
				name := exportedName(query.Name) + exportedName(p.Name) + "Item"
				if itemTypes[name] {
					return fmt.Errorf("typescript generator: generated item type name collision %q", name)
				}
				itemTypes[name] = true
			}
		}
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
			if _, err := parameterType(query.Name, parameter.Name, parameter.Type); err != nil {
				return fmt.Errorf("typescript generator: query %q parameter %q: %w", query.Name, parameter.Name, err)
			}
		}

		if query.Command == model.One || query.Command == model.Many {
			if len(query.ResultSets) != 1 {
				return fmt.Errorf("typescript generator: query %q: expected one result set, got %d", query.Name, len(query.ResultSets))
			}
			seenColumns := map[string]string{}
			seenFields := map[string]bool{}
			result := query.ResultSets[0]
			for _, embed := range result.Embeds {
				if embed.Start < 0 || embed.End > len(result.Columns) || embed.Start >= embed.End {
					return fmt.Errorf("typescript generator: query %q: invalid embedded column range", query.Name)
				}
				if _, err := identifier(embed.Field, false); err != nil || seenFields[embed.Field] {
					return fmt.Errorf("typescript generator: query %q: embedded field name collision at %q", query.Name, embed.Field)
				}
				seenFields[embed.Field] = true
				table := embeddedTable(a.Catalog, embed.Table)
				if table == nil || len(table.Columns) != embed.End-embed.Start {
					return fmt.Errorf("typescript generator: query %q: embedded table %q does not match projected columns", query.Name, embed.Table)
				}
				typ := exportedName(embed.Field)
				if previous, ok := embeddedTypes[typ]; ok && previous != embed.Table {
					return fmt.Errorf("typescript generator: embedded model name collision %q", typ)
				}
				embeddedTypes[typ] = embed.Table
				for i, column := range table.Columns {
					projected := result.Columns[embed.Start+i]
					if projected.Name != column.Name || !projected.Type.Equal(column.Type) {
						return fmt.Errorf("typescript generator: query %q: embedded table %q does not match projected columns", query.Name, embed.Table)
					}
				}
			}

			for i, column := range result.Columns {
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
				if !embeddedColumn(result.Embeds, i) {
					if seenFields[field] {
						return fmt.Errorf("typescript generator: query %q: result field name collision at %q", query.Name, field)
					}
					seenFields[field] = true
				}
			}
		}
	}
	for name := range embeddedTypes {
		if name == "ConfigureQuery" || name == "Queries" || itemTypes[name] {
			return fmt.Errorf("typescript generator: embedded model type name collision %q", name)
		}
		for _, query := range a.Queries {
			base := exportedName(query.Name)
			if name == base+"Row" || name == base+"WireRow" || len(query.Parameters) > 1 && name == base+"Params" {
				return fmt.Errorf("typescript generator: embedded model type name collision %q", name)
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
	optional, jsonResult, batch := false, false, false
	for _, q := range a.Queries {
		for _, p := range q.Parameters {
			fields := []model.StructField{{Type: p.Type}}
			if isStructList(p.Type) {
				batch = true
				fields = p.Type.Elem.Fields
			}
			for _, field := range fields {
				info := typescriptTypes[field.Type.UnwrapOptional().Kind]
				classes[info.valueClass] = true
				if field.Type.IsOptional() {
					optional = true
				}
				if field.Type.IsOptional() || isStructList(p.Type) {
					classes[info.typeClass] = true
				}
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
	if batch {
		b.WriteString("import { List, ListType } from \"@ydbjs/value/list\";\nimport { Struct, StructType } from \"@ydbjs/value/struct\";\nimport type { Value } from \"@ydbjs/value\";\n")
	}
	if optional {
		if batch {
			b.WriteString("import { Optional, OptionalType } from \"@ydbjs/value/optional\";\n")
		} else {
			b.WriteString("import { Optional } from \"@ydbjs/value/optional\";\n")
		}
	}
	b.WriteString("\nexport type ConfigureQuery = (query: Query) => void;\n\n")
	if batch {
		b.WriteString("// The SDK infers Null for an empty List, so retain the declared item type.\nfunction structList(items: Struct[], type: StructType): Value<ListType> {\n  const list = new List<Struct>();\n  for (const item of items) list.items.push(item);\n  return { type: new ListType(type), encode: () => list.encode() };\n}\n\n")
	}
	renderEmbeddedTypes(&b, a)
	for _, q := range a.Queries {
		for _, p := range q.Parameters {
			if isStructList(p.Type) {
				b.WriteString("export type " + exportedName(q.Name) + exportedName(p.Name) + "Item = {\n")
				for _, f := range p.Type.Elem.Fields {
					field, _ := identifier(f.Name, false)
					typ, _ := tsType(f.Type)
					b.WriteString("  readonly " + field + ": " + typ + ";\n")
				}
				b.WriteString("};\n\n")
			}
		}
		if len(q.Parameters) > 1 {
			b.WriteString("export type " + exportedName(q.Name) + "Params = {\n")
			for _, p := range q.Parameters {
				field, _ := identifier(p.Name, false)
				typ, _ := parameterType(q.Name, p.Name, p.Type)
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

func renderRowType(b *strings.Builder, name string, q model.AnalyzedQuery) {
	result := q.ResultSets[0]
	if len(result.Embeds) > 0 {
		b.WriteString("type " + exportedName(q.Name) + "WireRow = {\n")
		for _, c := range result.Columns {
			b.WriteString("  readonly " + tsProperty(c.ResultName()) + ": " + resultType(c.Type) + ";\n")
		}
		b.WriteString("};\n\n")
	}
	b.WriteString(name + " = {\n")
	for i := 0; i < len(result.Columns); i++ {
		if embed := embeddingAt(result.Embeds, i); embed != nil {
			b.WriteString("  readonly " + tsProperty(embed.Field) + ": " + exportedName(embed.Field) + ";\n")
			i = embed.End - 1
			continue
		}
		c := result.Columns[i]
		b.WriteString("  readonly " + tsProperty(c.ResultName()) + ": " + resultType(c.Type) + ";\n")
	}
	b.WriteString("};\n\n")
}

func tsProperty(field string) string {
	if !plainProperty(field) || reserved[field] {
		return strconv.Quote(field)
	}
	return field
}

func embeddedTable(catalog model.Catalog, name string) *model.Table {
	for i := range catalog.Tables {
		if catalog.Tables[i].Name == name {
			return &catalog.Tables[i]
		}
	}
	return nil
}

func embeddingAt(embeds []model.Embedding, index int) *model.Embedding {
	for i := range embeds {
		if embeds[i].Start == index {
			return &embeds[i]
		}
	}
	return nil
}

func embeddedColumn(embeds []model.Embedding, index int) bool {
	for _, embed := range embeds {
		if index >= embed.Start && index < embed.End {
			return true
		}
	}
	return false
}

func renderEmbeddedTypes(b *strings.Builder, a *model.AnalysisResult) {
	seen := map[string]bool{}
	for _, q := range a.Queries {
		for _, result := range q.ResultSets {
			for _, embed := range result.Embeds {
				if seen[embed.Table] {
					continue
				}
				seen[embed.Table] = true
				b.WriteString("export type " + exportedName(embed.Field) + " = {\n")
				for _, column := range embeddedTable(a.Catalog, embed.Table).Columns {
					b.WriteString("  readonly " + tsProperty(column.Name) + ": " + resultType(column.Type) + ";\n")
				}
				b.WriteString("};\n\n")
			}
		}
	}
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
		typ, _ := parameterType(q.Name, p.Name, p.Type)
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
		wire := row
		if len(q.ResultSets[0].Embeds) > 0 {
			wire = exportedName(q.Name) + "WireRow"
		}
		generic = "<[" + wire + "]>"
	}
	b.WriteString("\n  // " + model.QueryAnnotation(q) + "\n")
	b.WriteString("  async " + method + "(" + params + "configure?: ConfigureQuery): Promise<" + ret + "> {\n")

	sql := model.WithoutQueryAnnotation(q.SQL)
	b.WriteString("    const stmt = this.#sql" + generic + sqlLiteral(sql))
	declared := len(q.DeclaredParameters) > 0
	for _, p := range q.Parameters {
		if q.IsDeclaredParameter(p.Name) {
			continue
		}
		field, _ := identifier(p.Name, false)
		if len(q.Parameters) > 1 {
			field = "args." + field
		}
		b.WriteString("\n      .parameter(" + strconv.Quote(p.Name) + ", " + bindExpression(p.Type, field) + ")")
	}
	b.WriteString(";\n")
	if declared {
		b.WriteString("    // Keep explicit DECLARE statements; the SDK otherwise prepends duplicates.\n")
		b.WriteString("    Object.defineProperty(stmt, \"text\", { value: stmt.text, writable: false });\n")
		b.WriteString("    stmt")
		for _, p := range q.Parameters {
			if !q.IsDeclaredParameter(p.Name) {
				continue
			}
			field, _ := identifier(p.Name, false)
			if len(q.Parameters) > 1 {
				field = "args." + field
			}
			b.WriteString("\n      .parameter(" + strconv.Quote(p.Name) + ", " + bindExpression(p.Type, field) + ")")
		}
		b.WriteString(";\n")
	}
	b.WriteString("    configure?.(stmt);\n")

	if q.Command == model.Exec {
		b.WriteString("    await stmt;\n  }\n")
		return
	}
	b.WriteString("    const [rows] = await stmt;\n")
	if len(q.ResultSets[0].Embeds) > 0 {
		b.WriteString("    const mapped = rows.map((row): " + exportedName(q.Name) + "Row => ({\n")
		result := q.ResultSets[0]
		for i := 0; i < len(result.Columns); i++ {
			if embed := embeddingAt(result.Embeds, i); embed != nil {
				b.WriteString("      " + tsProperty(embed.Field) + ": {\n")
				for j := embed.Start; j < embed.End; j++ {
					column := result.Columns[j]
					b.WriteString("        " + tsProperty(column.Name) + ": row[" + strconv.Quote(column.ResultName()) + "],\n")
				}
				b.WriteString("      },\n")
				i = embed.End - 1
				continue
			}
			column := result.Columns[i]
			b.WriteString("      " + tsProperty(column.ResultName()) + ": row[" + strconv.Quote(column.ResultName()) + "],\n")
		}
		b.WriteString("    }));\n")
	}
	if q.Command == model.One {
		if len(q.ResultSets[0].Embeds) > 0 {
			b.WriteString("\n    return mapped[0] ?? null;\n")
		} else {
			b.WriteString("\n    return rows[0] ?? null;\n")
		}
	} else {
		if len(q.ResultSets[0].Embeds) > 0 {
			b.WriteString("\n    return mapped;\n")
		} else {
			b.WriteString("\n    return rows;\n")
		}
	}
	b.WriteString("  }\n")
}

func bindExpression(t model.Type, value string) string {
	if isStructList(t) {
		var fields, names, types []string
		for _, f := range t.Elem.Fields {
			field, _ := identifier(f.Name, false)
			fields = append(fields, "["+strconv.Quote(f.Name)+"]: "+bindExpression(f.Type, "item."+field))
			names = append(names, strconv.Quote(f.Name))
			info := typescriptTypes[f.Type.UnwrapOptional().Kind]
			typ := "new " + info.typeClass + "()"
			if f.Type.IsOptional() {
				typ = "new OptionalType(" + typ + ")"
			}
			types = append(types, typ)
		}
		return "structList(\n        " + value + ".map(item => new Struct({\n          " + strings.Join(fields, ",\n          ") + ",\n        })),\n        new StructType(\n          [\n            " + strings.Join(names, ",\n            ") + ",\n          ],\n          [\n            " + strings.Join(types, ",\n            ") + ",\n          ],\n        ),\n      )"
	}
	info := typescriptTypes[t.UnwrapOptional().Kind]
	wrapped := "new " + info.valueClass + "(" + value + ")"
	if t.IsOptional() {
		return "new Optional(" + value + " === null ? null : " + wrapped + ", new " + info.typeClass + "())"
	}
	return wrapped
}

func sqlLiteral(value string) string {
	var lines []string
	for _, line := range strings.SplitAfter(value, "\n") {
		if line != "" {
			var quoted strings.Builder
			encoder := json.NewEncoder(&quoted)
			encoder.SetEscapeHTML(false)
			_ = encoder.Encode(line)
			lines = append(lines, strings.TrimSuffix(quoted.String(), "\n"))
		}
	}
	if len(lines) == 0 {
		lines = []string{`""`}
	}
	return "(\n      " + strings.Join(lines, " +\n      ") + "\n    )"
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

func isStructList(t model.Type) bool {
	return t.Kind == "List" && t.Elem != nil && t.Elem.Kind == "Struct"
}
func parameterType(query, name string, t model.Type) (string, error) {
	if !isStructList(t) {
		return tsType(t)
	}
	if len(t.Elem.Fields) == 0 {
		return "", fmt.Errorf("List<Struct> requires at least one scalar field")
	}
	seen := map[string]bool{}
	for _, f := range t.Elem.Fields {
		field, err := identifier(f.Name, false)
		if err != nil {
			return "", err
		}
		if seen[field] {
			return "", fmt.Errorf("struct field name collision %q", field)
		}
		seen[field] = true
		if _, err := tsType(f.Type); err != nil {
			return "", fmt.Errorf("struct field %q: %w", f.Name, err)
		}
	}
	return "ReadonlyArray<" + exportedName(query) + exportedName(name) + "Item>", nil
}
