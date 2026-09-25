// Package java generates SQL-first Java APIs for the YDB SDK and JDBC integrations.
package java

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ydb-platform/sqlc-ydb/internal/codegen/jdbc"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

type Options struct{ Package, Runtime string }

type scalar struct{ typ, boxed, sdk, jdbc string }

var scalars = map[string]scalar{
	"Bool":      {"boolean", "Boolean", "Bool", "Boolean"},
	"Int8":      {"byte", "Byte", "Int8", "Byte"},
	"Uint8":     {"int", "Integer", "Uint8", "Int"},
	"Int16":     {"short", "Short", "Int16", "Short"},
	"Uint16":    {"int", "Integer", "Uint16", "Int"},
	"Int32":     {"int", "Integer", "Int32", "Int"},
	"Uint32":    {"long", "Long", "Uint32", "Long"},
	"Int64":     {"long", "Long", "Int64", "Long"},
	"Uint64":    {"long", "Long", "Uint64", "Long"},
	"Float":     {"float", "Float", "Float", "Float"},
	"Double":    {"double", "Double", "Double", "Double"},
	"Utf8":      {"String", "String", "Text", "String"},
	"String":    {"byte[]", "byte[]", "Bytes", "Bytes"},
	"Json":      {"String", "String", "Json", "String"},
	"Timestamp": {"java.time.Instant", "java.time.Instant", "Timestamp", "Timestamp"},
}

func typeInfo(t model.Type) (scalar, string, error) {
	s, ok := scalars[t.UnwrapOptional().Kind]
	if !ok {
		return s, "", fmt.Errorf("unsupported Java type %s", t.Kind)
	}
	if t.IsOptional() {
		return s, s.boxed, nil
	}
	return s, s.typ, nil
}

var reserved = func() map[string]bool {
	m := map[string]bool{}
	for _, s := range strings.Fields("abstract assert boolean break byte case catch char class const continue default do double else enum extends final finally float for goto if implements import instanceof int interface long native new package private protected public return short static strictfp super switch synchronized this throw throws transient try void volatile while true false null _ record sealed permits var yield when clone finalize getClass hashCode notify notifyAll toString wait") {
		m[s] = true
	}
	return m
}()

func identifier(s string) bool {
	if s == "" || reserved[s] {
		return false
	}
	for i, r := range s {
		if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_' || (i > 0 && r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

func name(s string, upper bool) (string, error) {
	var b strings.Builder
	for _, r := range s {
		if r == '_' || r == '-' || r == ' ' || r == '.' {
			upper = true
			continue
		}
		if upper && r >= 'a' && r <= 'z' {
			r -= 'a' - 'A'
		} else if b.Len() == 0 && !upper && r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		b.WriteRune(r)
		upper = false
	}
	n := b.String()
	if reserved[n] {
		n += "_"
	}
	if !identifier(n) {
		return "", fmt.Errorf("cannot represent %q as a Java identifier", s)
	}
	return n, nil
}

// Table paths retain their namespace in generated types; other identifiers do
// not admit path separators.
func tableName(path string) (string, error) {
	n, err := name(strings.ReplaceAll(path, "/", "_"), true)
	if err != nil {
		return "", fmt.Errorf("cannot represent %q as a Java identifier", path)
	}
	return n, nil
}

// quoted also escapes backslashes preceding u: Java Unicode escapes are processed
// before tokenization. Doubling every input backslash keeps them literal.
func quoted(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\', '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 32 || r == 127 {
				fmt.Fprintf(&b, "\\%03o", r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// sqlLiteral emits a Java 17 text block. Escaped quotes cannot close the block;
// escaped trailing spaces survive incidental whitespace stripping. The final
// continuation suppresses only the newline introduced by the closing delimiter.
func sqlLiteral(s string) string {
	var b strings.Builder
	b.WriteString("\"\"\"\n")
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		encoded := quoted(line)
		encoded = encoded[1 : len(encoded)-1]
		if strings.HasSuffix(encoded, " ") {
			encoded = strings.TrimSuffix(encoded, " ") + `\s`
		}
		if encoded != "" || i == len(lines)-1 {
			b.WriteString("            " + encoded)
		}
	}
	b.WriteString("\\\n            \"\"\"")
	return b.String()
}

func Generate(a *model.AnalysisResult, o Options) ([]model.File, error) {
	if a == nil {
		return nil, fmt.Errorf("nil analysis result")
	}
	if len(a.Diagnostics) != 0 {
		return nil, fmt.Errorf("cannot generate Java with analysis diagnostics")
	}
	if o.Package == "" {
		o.Package = "db"
	}
	if o.Package == "java" || strings.HasPrefix(o.Package, "java.") {
		return nil, fmt.Errorf("invalid Java package %q: java packages are reserved by the JVM", o.Package)
	}
	for _, p := range strings.Split(o.Package, ".") {
		if !identifier(p) {
			return nil, fmt.Errorf("invalid Java package %q", o.Package)
		}
	}
	if o.Runtime == "" || o.Runtime == "native" {
		o.Runtime = "ydb"
	}
	if o.Runtime == "jooq" {
		return generateJooq(a, o)
	}
	if o.Runtime != "ydb" && o.Runtime != "jdbc" {
		return nil, fmt.Errorf("unsupported Java runtime %q", o.Runtime)
	}
	header := "// Code generated by sqlc-ydb. DO NOT EDIT.\npackage " + o.Package + ";\n\n"
	files := []model.File{}
	types := map[string]bool{"Queries": true, "String": true, "Long": true, "Integer": true, "Short": true, "Byte": true, "Boolean": true, "Float": true, "Double": true}
	for _, n := range []string{"QueryTransaction", "QueryReader", "Params", "PrimitiveValue", "PrimitiveType", "OptionalType", "IllegalStateException", "IllegalArgumentException"} {
		types[n] = true
	}
	addRecord := func(n string, cols []model.Column) error {
		if types[n] {
			return fmt.Errorf("Java type name collision: %s", n)
		}
		types[n] = true
		fields := []string{}
		seen := map[string]bool{}
		for _, c := range cols {
			field, err := name(c.Name, false)
			if err != nil {
				return err
			}
			if seen[field] {
				return fmt.Errorf("Java field name collision in %s: %s", n, field)
			}
			seen[field] = true
			_, t, err := typeInfo(c.Type)
			if err != nil {
				return fmt.Errorf("%s.%s: %w", n, c.Name, err)
			}
			fields = append(fields, t+" "+field)
		}
		files = append(files, model.File{Name: n + ".java", Content: []byte(header + "public record " + n + "(" + strings.Join(fields, ", ") + ") {}\n")})
		return nil
	}
	addResultRecord := func(n string, result model.ResultSet) error {
		if len(result.Embeds) == 0 {
			return addRecord(n, result.Columns)
		}
		if types[n] {
			return fmt.Errorf("Java type name collision: %s", n)
		}
		types[n] = true
		fields := []string{}
		seen := map[string]bool{}
		for i, c := range result.Columns {
			field, typ := c.Name, ""
			if embed := javaEmbeddingAt(result, i); embed != nil {
				if embed.Start != i {
					continue
				}
				field = embed.Field
				var err error
				typ, err = tableName(embed.Table)
				if err != nil {
					return err
				}
			} else {
				var err error
				_, typ, err = typeInfo(c.Type)
				if err != nil {
					return fmt.Errorf("%s.%s: %w", n, c.Name, err)
				}
			}
			field, err := name(field, false)
			if err != nil {
				return err
			}
			if seen[field] {
				return fmt.Errorf("Java field name collision in %s: %s", n, field)
			}
			seen[field] = true
			fields = append(fields, typ+" "+field)
		}
		files = append(files, model.File{Name: n + ".java", Content: []byte(header + "public record " + n + "(" + strings.Join(fields, ", ") + ") {}\n")})
		return nil
	}
	for _, table := range a.Catalog.Tables {
		n, err := tableName(table.Name)
		if err != nil {
			return nil, err
		}
		if err := addRecord(n, table.Columns); err != nil {
			return nil, err
		}
	}
	var b strings.Builder
	b.WriteString(header)
	if o.Runtime == "ydb" {
		b.WriteString("import tech.ydb.query.QueryTransaction;\n")
		for _, q := range a.Queries {
			if q.Command != model.Exec {
				b.WriteString("import tech.ydb.query.tools.QueryReader;\n")
				break
			}
		}
		b.WriteString("import tech.ydb.table.query.Params;\n")
	}
	needsValues, needsOptional := false, false
	for _, q := range a.Queries {
		for _, p := range q.Parameters {
			if o.Runtime == "ydb" || jdbc.HasDeclarations(q) || isStructList(p.Type) || strings.HasPrefix(p.Type.UnwrapOptional().Kind, "Uint") || p.Type.UnwrapOptional().Kind == "Json" || p.Type.UnwrapOptional().Kind == "Timestamp" {
				needsValues = true
				needsOptional = needsOptional || p.Type.IsOptional()
				if isStructList(p.Type) {
					for _, field := range p.Type.Elem.Fields {
						needsOptional = needsOptional || field.Type.IsOptional()
					}
				}
			}
		}
	}
	if needsValues {
		b.WriteString("import tech.ydb.table.values.PrimitiveValue;\n")
	}
	if needsOptional {
		b.WriteString("import tech.ydb.table.values.PrimitiveType;\nimport tech.ydb.table.values.OptionalType;\n")
	}
	if needsValues || o.Runtime == "ydb" {
		b.WriteByte('\n')
	}

	owner := map[string]string{"ydb": "QueryTransaction", "jdbc": "java.sql.Connection"}[o.Runtime]
	fmt.Fprintf(&b, "// The caller owns the injected client and its lifecycle.\npublic final class Queries {\n    private final %s client;\n\n    public Queries(%s client) {\n        this.client = java.util.Objects.requireNonNull(client);\n    }\n", owner, owner)
	methods := map[string]bool{}
	for _, q := range a.Queries {
		if !utf8.ValidString(q.SQL) {
			return nil, fmt.Errorf("%s: Java SQL must be valid UTF-8", q.Name)
		}
		method, err := name(q.Name, false)
		if err != nil {
			return nil, err
		}
		if methods[method] {
			return nil, fmt.Errorf("Java method name collision: %s", method)
		}
		methods[method] = true
		row, _ := name(q.Name, true)
		row += "Row"
		sql := sqlLiteral(model.WithoutQueryAnnotation(q.SQL))
		preparedSQL := sql
		var bindings []int
		if o.Runtime != "ydb" {
			var text string
			text, bindings = jdbc.SQL(q)
			preparedSQL = sqlLiteral(text)
		}
		ret := "void"
		switch q.Command {
		case model.One, model.Many:
			if len(q.ResultSets) != 1 || len(q.ResultSets[0].Columns) == 0 {
				return nil, fmt.Errorf("%s: %s requires one nonempty result set", q.Name, q.Command)
			}
			if err := addResultRecord(row, q.ResultSets[0]); err != nil {
				return nil, err
			}
			if q.Command == model.One {
				ret = "java.util.Optional<" + row + ">"
			} else {
				ret = "java.util.List<" + row + ">"
			}
		case model.Exec:
		default:
			return nil, fmt.Errorf("%s: Java does not support %s", q.Name, q.Command)
		}
		params := []string{}
		paramNames := []string{}
		seen := map[string]bool{"client": true, "_params": true, "_query": true, "_connection": true, "_statement": true, "_prepared": true, "_rows": true, "_items": true, "_batchItem": true, "tech": true}
		for _, p := range q.Parameters {
			n, err := name(p.Name, false)
			if err != nil {
				return nil, err
			}
			if seen[n] {
				return nil, fmt.Errorf("%s: Java parameter name collision: %s", q.Name, n)
			}
			seen[n] = true
			var typ string
			if isStructList(p.Type) {
				queryName, _ := name(q.Name, true)
				parameterName, _ := name(p.Name, true)
				itemName := queryName + parameterName + "Item"
				columns := make([]model.Column, len(p.Type.Elem.Fields))
				for i, field := range p.Type.Elem.Fields {
					columns[i] = model.Column{Name: field.Name, Type: field.Type}
				}
				err = addRecord(itemName, columns)
				typ = "java.util.List<" + itemName + ">"
			} else {
				_, typ, err = typeInfo(p.Type)
			}
			if err != nil {
				return nil, fmt.Errorf("%s parameter %s: %w", q.Name, p.Name, err)
			}
			params = append(params, typ+" "+n)
			paramNames = append(paramNames, n)
		}
		throws := ""
		if o.Runtime == "jdbc" {
			throws = " throws java.sql.SQLException"
		}
		fmt.Fprintf(&b, "\n    // %s\n    public %s %s(%s)%s {\n", model.QueryAnnotation(q), ret, method, strings.Join(params, ", "), throws)
		// Java's wider signed carriers must not be silently narrowed by the SDK.
		// Uint64 deliberately uses all 64 bits of long and needs no range check.
		emitUnsignedChecks(&b, q, paramNames)
		if o.Runtime == "ydb" {
			emitNative(&b, q, paramNames, sql, row)
		} else {
			emitJDBC(&b, q, paramNames, bindings, preparedSQL, row)
		}
		b.WriteString("    }\n")
	}
	b.WriteString("}\n")
	files = append(files, model.File{Name: "Queries.java", Content: []byte(b.String())})
	return files, nil
}

func emitNative(b *strings.Builder, q model.AnalyzedQuery, names []string, sql, row string) {
	b.WriteString("        var _params = Params.create();\n")
	for i, p := range q.Parameters {
		fmt.Fprintf(b, "        _params.put(%s, %s);\n", quoted("$"+p.Name), indentExpression(parameterValue(p, names[i]), "        "))
	}
	if q.Command == model.Exec {
		fmt.Fprintf(b, "        client.createQuery(%s, _params).execute().join().getStatus().expectSuccess();\n", sql)
		return
	}
	sql = strings.ReplaceAll(sql, "\n", "\n        ")
	b.WriteString("        var _query = QueryReader.readFrom(\n")
	fmt.Fprintf(b, "                client.createQuery(%s, _params)).join().getValue();\n", sql)
	b.WriteString("        if (_query.getResultSetCount() != 1) throw new IllegalStateException(\"Expected one result set\");\n        var _rows = _query.getResultSet(0);\n")
	emitRows(b, q, row, "        ", true)
}

func parameterValue(p model.Parameter, name string) string {
	if isStructList(p.Type) {
		return structListValue(p.Type, name)
	}
	s, _, _ := typeInfo(p.Type)
	value := "PrimitiveValue.new" + s.sdk + "(" + name + ")"
	if p.Type.IsOptional() {
		o := "OptionalType.of(PrimitiveType." + s.sdk + ")"
		value = name + " == null ? " + o + ".emptyValue() : " + value + ".makeOptional()"
	}
	return value
}

func emitJDBC(b *strings.Builder, q model.AnalyzedQuery, names []string, bindings []int, sql, row string) {
	emitJDBCOn(b, q, names, bindings, sql, row, "client", "        ")
}

func emitJDBCOn(b *strings.Builder, q model.AnalyzedQuery, names []string, bindings []int, sql, row, connection, indent string) {
	sql = indentExpression(sql, strings.TrimPrefix(indent, "        "))
	if jdbc.HasDeclarations(q) {
		fmt.Fprintf(b, "%stry (var _prepared = %s.unwrap(tech.ydb.jdbc.YdbConnection.class).prepareStatement(%s, tech.ydb.jdbc.YdbPrepareMode.DATA_QUERY)) {\n", indent, connection, sql)
	} else {
		fmt.Fprintf(b, "%stry (var _prepared = %s.prepareStatement(%s)) {\n", indent, connection, sql)
	}
	indent += "    "
	if jdbc.HasDeclarations(q) {
		for i, p := range q.Parameters {
			emitJDBCNamedParameter(b, p, names[i], indent)
		}
	}
	for position, i := range bindings {
		p := q.Parameters[i]
		s, _, _ := typeInfo(p.Type)
		if p.Type.UnwrapOptional().Kind == "Timestamp" {
			value := "java.sql.Timestamp.from(" + names[i] + ")"
			if p.Type.IsOptional() {
				value = names[i] + " == null ? null : " + value
			}
			fmt.Fprintf(b, "%s_prepared.setTimestamp(%d, %s);\n", indent, position+1, value)
			continue
		}
		if isStructList(p.Type) || strings.HasPrefix(p.Type.UnwrapOptional().Kind, "Uint") || p.Type.UnwrapOptional().Kind == "Json" {
			fmt.Fprintf(b, "%s_prepared.setObject(%d, %s);\n", indent, position+1, indentExpression(parameterValue(p, names[i]), indent))
		} else {
			sqlType := map[string]string{"Bool": "BOOLEAN", "Int8": "TINYINT", "Int16": "SMALLINT", "Int32": "INTEGER", "Int64": "BIGINT", "Float": "REAL", "Double": "DOUBLE"}[p.Type.UnwrapOptional().Kind]
			if p.Type.IsOptional() && sqlType != "" {
				fmt.Fprintf(b, "%sif (%s == null) _prepared.setNull(%d, java.sql.Types.%s);\n%selse ", indent, names[i], position+1, sqlType, indent)
			} else {
				b.WriteString(indent)
			}
			fmt.Fprintf(b, "_prepared.set%s(%d, %s);\n", s.jdbc, position+1, names[i])
		}
	}
	if q.Command == model.Exec {
		b.WriteString(indent + "_prepared.execute();\n")
	} else {
		emitJDBCResultStart(b, q, indent)
		emitRows(b, q, row, indent+"    ", false)
		b.WriteString(indent + "}\n")
	}
	indent = strings.TrimSuffix(indent, "    ")
	b.WriteString(indent + "}\n")
}

func emitJDBCResultStart(b *strings.Builder, q model.AnalyzedQuery, indent string) {
	if !q.MultipleStatements {
		b.WriteString(indent + "try (var _rows = _prepared.executeQuery()) {\n")
		return
	}
	b.WriteString(indent + "_prepared.execute();\n" + indent + "while (_prepared.getResultSet() == null && _prepared.getUpdateCount() != -1) {\n" + indent + "    _prepared.getMoreResults();\n" + indent + "}\n" + indent + "try (var _rows = _prepared.getResultSet()) {\n" + indent + "    if (_rows == null) throw new java.sql.SQLException(\"Expected one result set\");\n")
}

func emitJDBCScriptFinish(b *strings.Builder, indent string) {
	b.WriteString(indent + "while (_prepared.getMoreResults() || _prepared.getUpdateCount() != -1) {\n" + indent + "    if (_prepared.getResultSet() != null) throw new java.sql.SQLException(\"Expected one result set\");\n" + indent + "}\n")
}

func emitRows(b *strings.Builder, q model.AnalyzedQuery, row, indent string, native bool) {
	if q.Command == model.One {
		if q.MultipleStatements && !native {
			b.WriteString(indent + "if (!_rows.next()) {\n")
			emitJDBCScriptFinish(b, indent+"    ")
			b.WriteString(indent + "    return java.util.Optional.empty();\n" + indent + "}\n")
		} else {
			b.WriteString(indent + "if (!_rows.next()) return java.util.Optional.empty();\n")
		}
	} else {
		fmt.Fprintf(b, "%svar _items = new java.util.ArrayList<%s>();\n%swhile (_rows.next()) {\n", indent, row, indent)
		indent += "    "
	}
	values := []string{}
	for i, c := range q.ResultSets[0].Columns {
		s, typ, _ := typeInfo(c.Type)
		n := fmt.Sprintf("_value%d", i)
		if native {
			reader := fmt.Sprintf("_rows.getColumn(%d)", i)
			if c.Type.IsOptional() && s.typ != "String" && s.typ != "byte[]" && s.sdk != "Timestamp" {
				fmt.Fprintf(b, "%s%s %s = %s.isOptionalItemPresent() ? %s.getOptionalItem().get%s() : null;\n", indent, typ, n, reader, reader, s.sdk)
			} else {
				fmt.Fprintf(b, "%s%s %s = %s.get%s();\n", indent, typ, n, reader, s.sdk)
			}
		} else {
			if c.Type.UnwrapOptional().Kind == "Timestamp" {
				fmt.Fprintf(b, "%svar %sRaw = _rows.getTimestamp(%d);\n", indent, n, i+1)
				fmt.Fprintf(b, "%s%s %s = %sRaw == null ? null : %sRaw.toInstant();\n", indent, typ, n, n, n)
			} else if c.Type.IsOptional() && s.typ != "String" && s.typ != "byte[]" {
				fmt.Fprintf(b, "%s%s %s = _rows.getObject(%d, %s.class);\n", indent, typ, n, i+1, typ)
			} else {
				fmt.Fprintf(b, "%s%s %s = _rows.get%s(%d);\n", indent, typ, n, s.jdbc, i+1)
			}
		}
		values = append(values, n)
	}
	newRow := "new " + row + "(" + strings.Join(javaRowValues(q.ResultSets[0], values), ", ") + ")"
	if q.Command == model.One {
		if q.MultipleStatements && !native {
			b.WriteString(indent + "while (_rows.next()) {}\n")
			emitJDBCScriptFinish(b, indent)
		}
		fmt.Fprintf(b, "%sreturn java.util.Optional.of(%s);\n", indent, newRow)
	} else {
		fmt.Fprintf(b, "%s_items.add(%s);\n", indent, newRow)
		indent = strings.TrimSuffix(indent, "    ")
		b.WriteString(indent + "}\n")
		if q.MultipleStatements && !native {
			emitJDBCScriptFinish(b, indent)
		}
		b.WriteString(indent + "return _items;\n")
	}
}

func javaEmbeddingAt(result model.ResultSet, index int) *model.Embedding {
	for i := range result.Embeds {
		embed := &result.Embeds[i]
		if embed.Start <= index && index < embed.End {
			return embed
		}
	}
	return nil
}

func javaRowValues(result model.ResultSet, values []string) []string {
	fields := make([]string, 0, len(values))
	for i, value := range values {
		if embed := javaEmbeddingAt(result, i); embed != nil {
			if embed.Start == i {
				typ, _ := tableName(embed.Table)
				fields = append(fields, "new "+typ+"("+strings.Join(values[embed.Start:embed.End], ", ")+")")
			}
			continue
		}
		fields = append(fields, value)
	}
	return fields
}

func indentExpression(s, indent string) string {
	lines := strings.Split(s, "\n")
	for i := 1; i < len(lines); i++ {
		if lines[i] != "" {
			lines[i] = indent + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

func emitJDBCNamedParameter(b *strings.Builder, p model.Parameter, n, indent string) {
	kind := p.Type.UnwrapOptional().Kind
	if !isStructList(p.Type) && kind != "Uint64" {
		scalar, _, _ := typeInfo(p.Type)
		value := n
		if kind == "Timestamp" {
			value = "java.sql.Timestamp.from(" + value + ")"
			if p.Type.IsOptional() {
				value = n + " == null ? null : " + value
			}
		}
		if p.Type.IsOptional() && scalar.typ != "String" && scalar.typ != "byte[]" && kind != "Timestamp" {
			fmt.Fprintf(b, "%sif (%s == null) _prepared.setNull(%s, java.sql.Types.NULL);\n%selse ", indent, n, quoted(p.Name), indent)
		} else {
			b.WriteString(indent)
		}
		fmt.Fprintf(b, "_prepared.set%s(%s, %s);\n", scalar.jdbc, quoted(p.Name), value)
		return
	}
	fmt.Fprintf(b, "%s_prepared.setObject(%s, %s);\n", indent, quoted(p.Name), indentExpression(parameterValue(p, n), indent))
}
