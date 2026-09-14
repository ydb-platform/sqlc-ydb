package java

import (
	"fmt"
	"strings"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func isStructList(t model.Type) bool {
	return t.Kind == "List" && t.Elem != nil && t.Elem.Kind == "Struct" && len(t.Elem.Fields) > 0
}

// Build the element type independently of values so empty batches retain their schema.
func structListValue(t model.Type, parameter string) string {
	types, values := []string{}, []string{}
	for _, field := range t.Elem.Fields {
		scalar, _, _ := typeInfo(field.Type)
		fieldType := "tech.ydb.table.values.PrimitiveType." + scalar.sdk
		if field.Type.IsOptional() {
			fieldType = "tech.ydb.table.values.OptionalType.of(" + fieldType + ")"
		}
		fieldName, _ := name(field.Name, false)
		value := parameterValue(model.Parameter{Type: field.Type}, "_batchItem."+fieldName+"()")
		value = strings.ReplaceAll(value, "OptionalType.of(PrimitiveType.", "tech.ydb.table.values.OptionalType.of(tech.ydb.table.values.PrimitiveType.")
		types = append(types, "java.util.Map.entry("+quoted(field.Name)+", "+fieldType+")")
		values = append(values, "java.util.Map.entry("+quoted(field.Name)+", "+value+")")
	}
	return "tech.ydb.table.values.ListType.of(tech.ydb.table.values.StructType.of(java.util.Map.ofEntries(" + strings.Join(types, ", ") + "))).newValue(" + parameter + ".stream().map(_batchItem -> tech.ydb.table.values.StructValue.of(java.util.Map.ofEntries(" + strings.Join(values, ", ") + "))).toList())"
}

func hasStructList(q model.AnalyzedQuery) bool {
	for _, p := range q.Parameters {
		if isStructList(p.Type) {
			return true
		}
	}
	return false
}

func batchImports(a *model.AnalysisResult) string {
	for _, q := range a.Queries {
		if hasStructList(q) {
			return "import tech.ydb.table.values.PrimitiveValue;\nimport tech.ydb.table.values.PrimitiveType;\nimport tech.ydb.table.values.OptionalType;\n\n"
		}
	}
	return ""
}

func emitUnsignedChecks(b *strings.Builder, q model.AnalyzedQuery, names []string) {
	for i, p := range q.Parameters {
		if isStructList(p.Type) {
			needed := false
			for _, field := range p.Type.Elem.Fields {
				kind := field.Type.UnwrapOptional().Kind
				needed = needed || kind == "Uint8" || kind == "Uint16" || kind == "Uint32"
			}
			if !needed {
				continue
			}
			fmt.Fprintf(b, "        for (var _batchItem : %s) {\n", names[i])
			for _, field := range p.Type.Elem.Fields {
				fieldName, _ := name(field.Name, false)
				emitUnsignedCheck(b, field.Type, "_batchItem."+fieldName+"()", "$"+p.Name+"."+field.Name, "            ")
			}
			b.WriteString("        }\n")
		} else {
			emitUnsignedCheck(b, p.Type, names[i], "$"+p.Name, "        ")
		}
	}
}

func emitUnsignedCheck(b *strings.Builder, t model.Type, n, where, indent string) {
	max := map[string]string{"Uint8": "255", "Uint16": "65535", "Uint32": "4294967295L"}[t.UnwrapOptional().Kind]
	if max == "" {
		return
	}
	condition := n + " < 0 || " + n + " > " + max
	if t.IsOptional() {
		condition = n + " != null && (" + condition + ")"
	}
	fmt.Fprintf(b, "%sif (%s) throw new IllegalArgumentException(%s);\n", indent, condition, quoted("parameter "+where+" is outside "+t.UnwrapOptional().Kind+" range"))
}
