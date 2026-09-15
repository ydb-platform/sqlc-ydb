package java

import (
	"fmt"
	"sort"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	parser "github.com/ydb-platform/yql-parsers/go"
)

// Preserve declared YQL and apply jOOQ table mappings at resolved table spans.
func jooqDeclaredSQL(q model.AnalyzedQuery, sql string) (string, error) {
	if q.Syntax == nil {
		return "", fmt.Errorf("%s: declarations require analyzed syntax for jOOQ table mapping", q.Name)
	}
	type replacement struct {
		start, end int
		expression string
	}
	var replacements []replacement
	seen := map[int]bool{}
	shift := len([]rune(sql)) - len([]rune(q.SQL))
	add := func(ctx antlr.ParserRuleContext, alias bool) error {
		if ctx == nil || seen[ctx.GetStart().GetStart()] {
			return nil
		}
		original := jooqID(ctx.GetText())
		constant, err := jooqConstant(original)
		if err != nil {
			return err
		}
		expression := "dsl.render(" + constant + ")"
		if alias {
			expression += " + " + quoted(" AS `"+strings.ReplaceAll(original, "`", "``")+"`")
		}
		replacements = append(replacements, replacement{ctx.GetStart().GetStart() + shift, ctx.GetStop().GetStop() + 1 + shift, expression})
		seen[ctx.GetStart().GetStart()] = true
		return nil
	}
	for _, named := range jooqNodes[*parser.Named_single_sourceContext](q.Syntax.Root) {
		hinted := named.Hinted_single_source()
		if hinted == nil || hinted.Single_source() == nil || hinted.Single_source().Table_ref() == nil {
			continue
		}
		ref := hinted.Single_source().Table_ref()
		if ref.Table_key() != nil {
			if err := add(ref.Table_key(), named.An_id() == nil && named.An_id_as_compat() == nil); err != nil {
				return "", err
			}
		}
	}
	targets := map[string]bool{}
	for _, ref := range jooqNodes[*parser.Simple_table_ref_coreContext](q.Syntax.Root) {
		if err := add(ref, false); err != nil {
			return "", err
		}
		targets[jooqID(ref.GetText())] = true
	}
	// DML targets have no SELECT alias to retain the original qualifier after mapping.
	terminals := jooqNodes[antlr.TerminalNode](q.Syntax.Root)
	for i, ref := range terminals {
		token := ref.GetSymbol()
		binding, ok := q.Syntax.Columns[token.GetTokenIndex()]
		if !ok || !targets[binding.Table] || binding.Alias != binding.Table || i+1 == len(terminals) || terminals[i+1].GetText() != "." || seen[token.GetStart()] {
			continue
		}
		constant, err := jooqConstant(binding.Table)
		if err != nil {
			return "", err
		}
		replacements = append(replacements, replacement{token.GetStart() + shift, token.GetStop() + 1 + shift, "dsl.render(" + constant + ")"})
		seen[token.GetStart()] = true
	}
	sort.Slice(replacements, func(i, j int) bool { return replacements[i].start < replacements[j].start })
	runes := []rune(sql)
	var parts []string
	cursor := 0
	for _, replacement := range replacements {
		if replacement.start < cursor || replacement.end < replacement.start || replacement.end > len(runes) {
			return "", fmt.Errorf("%s: invalid resolved table span", q.Name)
		}
		parts = append(parts, sqlLiteral(string(runes[cursor:replacement.start])), replacement.expression)
		cursor = replacement.end
	}
	parts = append(parts, sqlLiteral(string(runes[cursor:])))
	return strings.Join(parts, " + "), nil
}

func jooqDeclaredValue(p model.Parameter, n string) string {
	kind := p.Type.UnwrapOptional().Kind
	input := n
	switch kind {
	case "Uint8", "Uint16":
		input += ".intValue()"
	case "Uint32", "Uint64":
		input += ".longValue()"
	case "Json", "JsonDocument":
		input += ".data()"
	}
	sdk := kind
	if kind == "Utf8" {
		sdk = "Text"
	}
	if kind == "String" {
		sdk = "Bytes"
	}
	value := "tech.ydb.table.values.PrimitiveValue.new" + sdk + "(" + input + ")"
	if p.Type.IsOptional() {
		return n + " == null ? tech.ydb.table.values.OptionalType.of(tech.ydb.table.values.PrimitiveType." + sdk + ").emptyValue() : " + value + ".makeOptional()"
	}
	return value
}

func emitJooqDeclared(b *strings.Builder, q model.AnalyzedQuery, names []string, sql, row string) {
	sql = indentExpression(sql, "    ")
	callback := "dsl.connection"
	if q.Command != model.Exec {
		callback = "return dsl.connectionResult"
	}
	fmt.Fprintf(b, "        %s(_connection -> {\n            try (var _prepared = _connection.unwrap(tech.ydb.jdbc.YdbConnection.class).prepareStatement(%s, tech.ydb.jdbc.YdbPrepareMode.DATA_QUERY)) {\n", callback, sql)
	for i, p := range q.Parameters {
		_, _, scalarErr := typeInfo(p.Type)
		if p.Type.UnwrapOptional().Kind == "Uint64" || scalarErr != nil {
			fmt.Fprintf(b, "                _prepared.setObject(%s, %s);\n", quoted(p.Name), jooqDeclaredValue(p, names[i]))
		} else {
			input := names[i]
			switch p.Type.UnwrapOptional().Kind {
			case "Uint8", "Uint16":
				input += ".intValue()"
			case "Uint32":
				input += ".longValue()"
			case "Json", "JsonDocument":
				input += ".data()"
			}
			if input != names[i] && p.Type.IsOptional() {
				input = "(" + names[i] + " == null ? null : " + input + ")"
			}
			emitJDBCNamedParameter(b, p, input, "                ")
		}
	}
	if q.Command == model.Exec {
		b.WriteString("                _prepared.execute();\n")
	} else {
		b.WriteString("                try (var _rows = _prepared.executeQuery()) {\n")
		var types, values []string
		for i, c := range q.ResultSets[0].Columns {
			typ, dt, _ := jooqType(c.Type)
			types = append(types, dt)
			values = append(values, fmt.Sprintf("_row.get(%d, %s.class)", i, typ))
		}
		fmt.Fprintf(b, "                    var _result = dsl.fetch(_rows, %s).map(_row -> new %s(%s));\n", strings.Join(types, ", "), row, strings.Join(values, ", "))
		if q.Command == model.One {
			b.WriteString("                    if (_result.size() > 1) throw new org.jooq.exception.TooManyRowsException(\"Expected at most one row\");\n                    return _result.stream().findFirst();\n")
		} else {
			b.WriteString("                    return _result;\n")
		}
		b.WriteString("                }\n")
	}
	b.WriteString("            }\n        });\n")
}
