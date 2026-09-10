// Package javascript generates modern ESM modules for the modular YDB JavaScript SDK.
package javascript

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
	jsType     string
	valueClass string
	typeClass  string
	validator  string
}

var javascriptTypes = map[string]typeInfo{
	"Bool":         {"boolean", "Bool", "BoolType", "boolean"},
	"Int8":         {"number", "Int8", "Int8Type", "int8"},
	"Uint8":        {"number", "Uint8", "Uint8Type", "uint8"},
	"Int16":        {"number", "Int16", "Int16Type", "int16"},
	"Uint16":       {"number", "Uint16", "Uint16Type", "uint16"},
	"Int32":        {"number", "Int32", "Int32Type", "int32"},
	"Uint32":       {"number", "Uint32", "Uint32Type", "uint32"},
	"Int64":        {"bigint", "Int64", "Int64Type", "int64"},
	"Uint64":       {"bigint", "Uint64", "Uint64Type", "uint64"},
	"Float":        {"number", "Float", "FloatType", "float"},
	"Double":       {"number", "Double", "DoubleType", "number"},
	"Utf8":         {"string", "Utf8", "Utf8Type", "string"},
	"String":       {"Uint8Array", "Bytes", "BytesType", "bytes"},
	"Json":         {"string", "Json", "JsonType", "json"},
	"JsonDocument": {"string", "JsonDocument", "JsonDocumentType", "json"},
	"Timestamp":    {"bigint", "Primitive", "TimestampType", "timestamp"},
}

var reserved = func() map[string]bool {
	m := map[string]bool{"constructor": true, "client": true}
	for _, word := range strings.Fields("await break case catch class const continue debugger default delete do else enum export extends false finally for function if implements import in instanceof interface let new null package private protected public return static super switch this throw true try typeof var void while with yield arguments eval") {
		m[word] = true
	}
	return m
}()

func Generate(a *model.AnalysisResult, options Options) ([]model.File, error) {
	if a == nil {
		return nil, fmt.Errorf("javascript generator: nil analysis result")
	}
	if len(a.Diagnostics) != 0 {
		return nil, fmt.Errorf("javascript generator: analysis has diagnostics: %s", a.Diagnostics[0])
	}
	if options.Runtime == "" {
		options.Runtime = "ydb"
	}
	if options.Runtime != "ydb" {
		return nil, fmt.Errorf("javascript generator: unsupported JavaScript runtime %q", options.Runtime)
	}
	if err := validate(a); err != nil {
		return nil, err
	}
	js, err := renderJavaScript(a)
	if err != nil {
		return nil, err
	}
	declarations := renderDeclarations(a)
	return []model.File{
		{Name: "queries.js", Content: []byte(strings.TrimRight(js, "\n") + "\n")},
		{Name: "queries.d.ts", Content: []byte(strings.TrimRight(declarations, "\n") + "\n")},
	}, nil
}

func validate(a *model.AnalysisResult) error {
	methods := map[string]string{}
	constants := map[string]string{}
	for _, query := range a.Queries {
		switch query.Command {
		case model.One, model.Many, model.Exec:
		case model.ExecRows:
			return fmt.Errorf("javascript generator: query %q: :execrows is unsupported because the YDB JavaScript SDK does not expose a portable affected-row count", query.Name)
		default:
			return fmt.Errorf("javascript generator: query %q: unsupported command %q", query.Name, query.Command)
		}
		if !utf8.ValidString(query.SQL) {
			return fmt.Errorf("javascript generator: query %q: SQL is not valid UTF-8", query.Name)
		}
		method, err := identifier(query.Name, false)
		if err != nil {
			return fmt.Errorf("javascript generator: query %q: %w", query.Name, err)
		}
		if previous, exists := methods[method]; exists {
			return fmt.Errorf("javascript generator: method name collision %q between %q and %q", method, previous, query.Name)
		}
		methods[method] = query.Name
		constant := constantName(query.Name)
		if previous, exists := constants[constant]; exists {
			return fmt.Errorf("javascript generator: SQL constant name collision %q between %q and %q", constant, previous, query.Name)
		}
		constants[constant] = query.Name
		seenParameters := map[string]string{}
		for _, parameter := range query.Parameters {
			field, err := identifier(parameter.Name, false)
			if err != nil {
				return fmt.Errorf("javascript generator: query %q parameter %q: %w", query.Name, parameter.Name, err)
			}
			if previous, exists := seenParameters[field]; exists {
				return fmt.Errorf("javascript generator: query %q: parameter name collision %q between %q and %q", query.Name, field, previous, parameter.Name)
			}
			seenParameters[field] = parameter.Name
			if _, err := jsType(parameter.Type); err != nil {
				return fmt.Errorf("javascript generator: query %q parameter %q: %w", query.Name, parameter.Name, err)
			}
		}
		if len(query.Parameters) != 0 && query.SQLWithoutDeclarations == "" {
			return fmt.Errorf("javascript generator: query %q: analyzer did not provide SQL without declarations required for typed SDK parameters", query.Name)
		}
		if !utf8.ValidString(query.SQLWithoutDeclarations) {
			return fmt.Errorf("javascript generator: query %q: SQL without declarations is not valid UTF-8", query.Name)
		}
		if query.Command == model.One || query.Command == model.Many {
			if len(query.ResultSets) != 1 {
				return fmt.Errorf("javascript generator: query %q: expected one result set, got %d", query.Name, len(query.ResultSets))
			}
			seenColumns := map[string]string{}
			for _, column := range query.ResultSets[0].Columns {
				field, err := identifier(column.Name, false)
				if err != nil {
					return fmt.Errorf("javascript generator: query %q column %q: %w", query.Name, column.Name, err)
				}
				if previous, exists := seenColumns[field]; exists {
					return fmt.Errorf("javascript generator: query %q: column name collision %q between %q and %q", query.Name, field, previous, column.Name)
				}
				seenColumns[field] = column.Name
				if _, err := jsType(column.Type); err != nil {
					return fmt.Errorf("javascript generator: query %q column %q: %w", query.Name, column.Name, err)
				}
			}
		}
	}
	return nil
}

func jsType(t model.Type) (string, error) {
	if t.IsOptional() {
		if t.Elem == nil || t.Elem.IsOptional() {
			return "", fmt.Errorf("malformed or nested Optional type")
		}
		inner, err := jsType(*t.Elem)
		if err != nil {
			return "", err
		}
		return inner + " | null", nil
	}
	info, ok := javascriptTypes[t.Kind]
	if !ok {
		return "", fmt.Errorf("unsupported YQL type %q", t.Kind)
	}
	return info.jsType, nil
}

func renderJavaScript(a *model.AnalysisResult) (string, error) {
	classes := map[string]bool{}
	needsOptional := false
	validators := map[string]bool{}
	needsRawResults := false
	for _, query := range a.Queries {
		for _, parameter := range query.Parameters {
			base := parameter.Type.UnwrapOptional()
			info := javascriptTypes[base.Kind]
			classes[info.valueClass] = true
			validators[info.validator] = true
			if base.Kind == "Timestamp" {
				classes[info.typeClass] = true
			}
			if parameter.Type.IsOptional() {
				needsOptional = true
				classes[info.typeClass] = true
			}
		}
		needsRawResults = needsRawResults || resultNeedsRaw(query)
	}
	imports := make([]string, 0, len(classes))
	for class := range classes {
		imports = append(imports, class)
	}
	sort.Strings(imports)
	var b strings.Builder
	b.WriteString("// Code generated by sqlc-ydb. DO NOT EDIT.\n")
	if len(imports) != 0 {
		b.WriteString("import { " + strings.Join(imports, ", ") + " } from \"@ydbjs/value/primitive\";\n")
	}
	if needsOptional {
		b.WriteString("import { Optional } from \"@ydbjs/value/optional\";\n")
	}
	if len(imports) != 0 || needsOptional {
		b.WriteByte('\n')
	}
	for _, query := range a.Queries {
		constant := constantName(query.Name)
		b.WriteString("export const " + constant + " = " + sqlLiteral(query.SQL) + ";\n")
		if len(query.Parameters) != 0 {
			b.WriteString("const _" + constant + "_EXEC = " + sqlLiteral(query.SQLWithoutDeclarations) + ";\n")
		}
		b.WriteByte('\n')
	}
	renderValidationHelpers(&b, validators, needsOptional)
	if needsRawResults {
		b.WriteString("function _rawValue(value, name, expectedCase) { if (value === null || typeof value !== \"object\" || value.value?.case !== expectedCase) throw new TypeError(`${name} has an unexpected YDB value shape`); return value.value.value; }\n")
		b.WriteString("function _rawOptional(value, name, read) { if (value === null || typeof value !== \"object\") throw new TypeError(`${name} has an unexpected YDB Optional shape`); return value.value?.case === \"nullFlagValue\" ? null : read(value); }\n\n")
	}
	for _, query := range a.Queries {
		if query.Command == model.One || query.Command == model.Many {
			renderDecoder(&b, query)
		}
	}
	b.WriteString("export class Queries {\n  #client;\n\n  /** @param {import(\"@ydbjs/query\").QueryClient} client */\n  constructor(client) {\n    if (typeof client !== \"function\") throw new TypeError(\"Queries requires a YDB QueryClient\");\n    this.#client = client;\n  }\n")
	for _, query := range a.Queries {
		if err := renderMethod(&b, query); err != nil {
			return "", err
		}
	}
	b.WriteString("}\n")
	return b.String(), nil
}

func renderValidationHelpers(b *strings.Builder, validators map[string]bool, optional bool) {
	if validators["boolean"] {
		b.WriteString("function _boolean(value, name) { if (typeof value !== \"boolean\") throw new TypeError(`${name} must be a boolean`); return value; }\n")
	}
	if validators["number"] {
		b.WriteString("function _number(value, name) { if (typeof value !== \"number\" || !Number.isFinite(value)) throw new TypeError(`${name} must be a finite number`); return value; }\n")
	}
	if validators["float"] {
		b.WriteString("function _float(value, name) { if (typeof value !== \"number\" || !Number.isFinite(value)) throw new TypeError(`${name} must be a finite number`); if (!Number.isFinite(Math.fround(value))) throw new RangeError(`${name} is outside YQL Float range`); return value; }\n")
	}
	ranges := []struct{ name, min, max string }{
		{"int8", "-128", "127"}, {"uint8", "0", "255"}, {"int16", "-32768", "32767"}, {"uint16", "0", "65535"},
		{"int32", "-2147483648", "2147483647"}, {"uint32", "0", "4294967295"},
	}
	for _, item := range ranges {
		if validators[item.name] {
			fmt.Fprintf(b, "function _%s(value, name) { if (!Number.isInteger(value) || value < %s || value > %s) throw new RangeError(`${name} is outside YQL %s range`); return value; }\n", item.name, item.min, item.max, strings.ToUpper(item.name[:1])+item.name[1:])
		}
	}
	for _, item := range []struct{ name, min, max string }{{"int64", "-9223372036854775808n", "9223372036854775807n"}, {"uint64", "0n", "18446744073709551615n"}} {
		if validators[item.name] {
			fmt.Fprintf(b, "function _%s(value, name) { if (typeof value !== \"bigint\" || value < %s || value > %s) throw new RangeError(`${name} is outside YQL %s range`); return value; }\n", item.name, item.min, item.max, strings.ToUpper(item.name[:1])+item.name[1:])
		}
	}
	if validators["string"] {
		b.WriteString("function _string(value, name) { if (typeof value !== \"string\") throw new TypeError(`${name} must be a string`); return value; }\n")
	}
	if validators["bytes"] {
		b.WriteString("function _bytes(value, name) { if (!(value instanceof Uint8Array)) throw new TypeError(`${name} must be a Uint8Array`); return value; }\n")
	}
	if validators["timestamp"] {
		b.WriteString("function _timestamp(value, name) { if (typeof value !== \"bigint\" || value < 0n || value > 4291747199999999n) throw new RangeError(`${name} is outside YQL Timestamp microsecond range`); return value; }\n")
	}
	if validators["json"] {
		b.WriteString("function _json(value, name) { if (typeof value !== \"string\") throw new TypeError(`${name} must be JSON text`); try { JSON.parse(value); } catch (error) { throw new TypeError(`${name} must contain valid JSON`, { cause: error }); } return value; }\n")
	}
	if optional {
		b.WriteString("function _optional(value, name, itemType, makeValue) { if (value === undefined) throw new TypeError(`${name} must not be undefined; use null for an empty Optional`); return new Optional(value === null ? null : makeValue(value), itemType); }\n")
	}
	if len(validators) != 0 || optional {
		b.WriteByte('\n')
	}
}

func renderDecoder(b *strings.Builder, query model.AnalyzedQuery) {
	decoder := "_decode" + exportedName(query.Name) + "Row"
	b.WriteString("function " + decoder + "(row) {\n")
	b.WriteString("  if (row === null || typeof row !== \"object\" || Array.isArray(row)) throw new TypeError(" + strconv.Quote(query.Name+": expected an object row") + ");\n")
	for _, column := range query.ResultSets[0].Columns {
		wireName := column.ResultName()
		b.WriteString("  if (!Object.hasOwn(row, " + strconv.Quote(wireName) + ")) throw new TypeError(" + strconv.Quote(query.Name+": result row is missing column "+wireName) + ");\n")
	}
	b.WriteString("  return {\n")
	for _, column := range query.ResultSets[0].Columns {
		field, _ := identifier(column.Name, false)
		wireName := column.ResultName()
		value := "row[" + strconv.Quote(wireName) + "]"
		if resultNeedsRaw(query) {
			value = rawDecodeExpression(column.Type, value, query.Name+"."+wireName)
		}
		b.WriteString("    " + field + ": " + value + ",\n")
	}
	b.WriteString("  };\n}\n\n")
}

func renderMethod(b *strings.Builder, query model.AnalyzedQuery) error {
	method, _ := identifier(query.Name, false)
	params := ""
	if len(query.Parameters) == 1 {
		params, _ = identifier(query.Parameters[0].Name, false)
	} else if len(query.Parameters) > 1 {
		params = "args"
	}
	b.WriteString("\n  async " + method + "(" + params + ") {\n")
	constant := constantName(query.Name)
	rawSuffix := ""
	if resultNeedsRaw(query) {
		rawSuffix = ".raw()"
	}
	if len(query.Parameters) == 0 {
		b.WriteString("    const _resultSets = await this.#client(" + constant + ")" + rawSuffix + ";\n")
	} else {
		b.WriteString("    const _pending = this.#client(_" + constant + "_EXEC)\n")
		for _, parameter := range query.Parameters {
			field, _ := identifier(parameter.Name, false)
			value := field
			if len(query.Parameters) > 1 {
				value = "args." + field
			}
			expr, err := bindExpression(parameter.Type, value, parameter.Name)
			if err != nil {
				return err
			}
			b.WriteString("      .parameter(" + strconv.Quote(parameter.Name) + ", " + expr + ")\n")
		}
		b.WriteString("    ;\n    const _resultSets = await _pending" + rawSuffix + ";\n")
	}
	if query.Command == model.Exec {
		b.WriteString("    return undefined;\n  }\n")
		return nil
	}
	b.WriteString("    if (!Array.isArray(_resultSets) || !Array.isArray(_resultSets[0])) throw new TypeError(" + strconv.Quote(query.Name+": expected the first YDB result set to be an array") + ");\n")
	b.WriteString("    const _rows = _resultSets[0];\n")
	decoder := "_decode" + exportedName(query.Name) + "Row"
	if query.Command == model.One {
		b.WriteString("    return _rows.length === 0 ? null : " + decoder + "(_rows[0]);\n")
	} else {
		b.WriteString("    return _rows.map(" + decoder + ");\n")
	}
	b.WriteString("  }\n")
	return nil
}

func bindExpression(t model.Type, value, originalName string) (string, error) {
	if t.IsOptional() {
		if t.Elem == nil {
			return "", fmt.Errorf("malformed Optional type")
		}
		base := t.Elem.UnwrapOptional()
		info, ok := javascriptTypes[base.Kind]
		if !ok {
			return "", fmt.Errorf("unsupported YQL type %q", base.Kind)
		}
		inner := valueExpression(info, "item", originalName)
		return "_optional(" + value + ", " + strconv.Quote(originalName) + ", new " + info.typeClass + "(), (item) => " + inner + ")", nil
	}
	info, ok := javascriptTypes[t.Kind]
	if !ok {
		return "", fmt.Errorf("unsupported YQL type %q", t.Kind)
	}
	return valueExpression(info, value, originalName), nil
}

func valueExpression(info typeInfo, value, name string) string {
	if info.validator == "timestamp" {
		return "new Primitive({ value: { case: \"uint64Value\", value: _timestamp(" + value + ", " + strconv.Quote(name) + ") } }, new TimestampType())"
	}
	return "new " + info.valueClass + "(_" + info.validator + "(" + value + ", " + strconv.Quote(name) + "))"
}

func resultNeedsRaw(query model.AnalyzedQuery) bool {
	for _, resultSet := range query.ResultSets {
		for _, column := range resultSet.Columns {
			kind := column.Type.UnwrapOptional().Kind
			if kind == "Timestamp" || kind == "Json" || kind == "JsonDocument" {
				return true
			}
		}
	}
	return false
}

func rawDecodeExpression(t model.Type, value, name string) string {
	if t.IsOptional() && t.Elem != nil {
		return "_rawOptional(" + value + ", " + strconv.Quote(name) + ", (value) => " + rawDecodeExpression(*t.Elem, "value", name) + ")"
	}
	caseName := map[string]string{
		"Bool": "boolValue", "Int8": "int32Value", "Uint8": "uint32Value", "Int16": "int32Value", "Uint16": "uint32Value",
		"Int32": "int32Value", "Uint32": "uint32Value", "Int64": "int64Value", "Uint64": "uint64Value", "Float": "floatValue",
		"Double": "doubleValue", "Utf8": "textValue", "String": "bytesValue", "Json": "textValue", "JsonDocument": "textValue", "Timestamp": "uint64Value",
	}[t.Kind]
	return "_rawValue(" + value + ", " + strconv.Quote(name) + ", " + strconv.Quote(caseName) + ")"
}

func renderDeclarations(a *model.AnalysisResult) string {
	var b strings.Builder
	b.WriteString("// Code generated by sqlc-ydb. DO NOT EDIT.\nimport type { QueryClient } from \"@ydbjs/query\";\n\n")
	for _, query := range a.Queries {
		b.WriteString("export const " + constantName(query.Name) + ": string;\n")
	}
	if len(a.Queries) != 0 {
		b.WriteByte('\n')
	}
	for _, query := range a.Queries {
		if len(query.Parameters) > 1 {
			b.WriteString("export interface " + exportedName(query.Name) + "Params {\n")
			for _, parameter := range query.Parameters {
				field, _ := identifier(parameter.Name, false)
				typ, _ := jsType(parameter.Type)
				b.WriteString("  readonly " + field + ": " + typ + ";\n")
			}
			b.WriteString("}\n\n")
		}
		if query.Command == model.One || query.Command == model.Many {
			b.WriteString("export interface " + exportedName(query.Name) + "Row {\n")
			for _, column := range query.ResultSets[0].Columns {
				field, _ := identifier(column.Name, false)
				typ, _ := jsType(column.Type)
				b.WriteString("  readonly " + field + ": " + typ + ";\n")
			}
			b.WriteString("}\n\n")
		}
	}
	b.WriteString("export class Queries {\n  constructor(client: QueryClient);\n")
	for _, query := range a.Queries {
		method, _ := identifier(query.Name, false)
		params := ""
		if len(query.Parameters) == 1 {
			field, _ := identifier(query.Parameters[0].Name, false)
			typ, _ := jsType(query.Parameters[0].Type)
			params = field + ": " + typ
		} else if len(query.Parameters) > 1 {
			params = "args: " + exportedName(query.Name) + "Params"
		}
		ret := "void"
		if query.Command == model.One {
			ret = exportedName(query.Name) + "Row | null"
		} else if query.Command == model.Many {
			ret = exportedName(query.Name) + "Row[]"
		}
		b.WriteString("  " + method + "(" + params + "): Promise<" + ret + ">;\n")
	}
	b.WriteString("}\n")
	return b.String()
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
			return "", fmt.Errorf("cannot represent %q as a JavaScript identifier", value)
		}
	}
	words := splitWords(value)
	if len(words) == 0 {
		return "", fmt.Errorf("cannot represent %q as a JavaScript identifier", value)
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
		return "", fmt.Errorf("cannot represent %q as a JavaScript identifier", value)
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

func constantName(value string) string {
	words := splitWords(value)
	for i := range words {
		words[i] = strings.ToUpper(words[i])
	}
	return strings.Join(words, "_") + "_SQL"
}
