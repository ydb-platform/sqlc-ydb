package python

import (
	"fmt"
	"strings"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func structList(t model.Type) bool {
	return strings.EqualFold(t.Kind, "List") && t.Elem != nil && strings.EqualFold(t.Elem.Kind, "Struct")
}
func structClass(q model.AnalyzedQuery, p model.Parameter) string {
	return className(q.Name + "_" + p.Name + "_item")
}
func parameterType(q model.AnalyzedQuery, p model.Parameter) (string, error) {
	if !structList(p.Type) {
		return pyType(p.Type)
	}
	if len(p.Type.Elem.Fields) == 0 {
		return "", fmt.Errorf("List<Struct> requires at least one field")
	}
	seen := map[string]bool{}
	for _, f := range p.Type.Elem.Fields {
		n := fieldName(f.Name)
		if f.Name == "" || !validPythonName(n) || seen[n] {
			return "", fmt.Errorf("List<Struct> invalid or colliding field %q", f.Name)
		}
		seen[n] = true
		scalar := f.Type.UnwrapOptional()
		if _, ok := pythonPrimitiveTypes[strings.ToLower(scalar.Kind)]; !ok {
			return "", fmt.Errorf("List<Struct> field %s must be a supported scalar or Optional<scalar>, got %s", f.Name, f.Type.String())
		}
	}
	return "list[_models." + structClass(q, p) + "]", nil
}
func renderStructModel(b *strings.Builder, q model.AnalyzedQuery, p model.Parameter) {
	b.WriteString("@dataclass\nclass " + structClass(q, p) + ":\n")
	for _, f := range p.Type.Elem.Fields {
		typ, _ := pyType(f.Type)
		b.WriteString("    " + fieldName(f.Name) + ": " + typ + "\n")
	}
	b.WriteByte('\n')
}
func renderParameter(b *strings.Builder, p model.Parameter, typeExpr, runtime string) {
	key := p.Name
	if runtime != "sqlalchemy" {
		key = "$" + key
	}
	prefix := "("
	if runtime == "ydb" {
		prefix = "_ydb.TypedValue("
	}
	if !structList(p.Type) {
		b.WriteString("            " + pyString(key) + ": " + prefix + fieldName(p.Name) + ", " + typeExpr + "),\n")
		return
	}
	b.WriteString("            " + pyString(key) + ": " + prefix + "\n                [\n                    {\n")
	for _, f := range p.Type.Elem.Fields {
		b.WriteString("                        " + pyString(f.Name) + ": item." + fieldName(f.Name) + ",\n")
	}
	b.WriteString("                    }\n                    for item in " + fieldName(p.Name) + "\n                ],\n                _ydb.ListType(\n                    _ydb.StructType()\n")
	for _, f := range p.Type.Elem.Fields {
		typ, _ := ydbTypeExpr(f.Type)
		b.WriteString("                    .add_member(" + pyString(f.Name) + ", " + typ + ")\n")
	}
	b.WriteString("                ),\n            ),\n")
}
