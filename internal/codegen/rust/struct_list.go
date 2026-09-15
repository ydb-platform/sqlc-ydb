package rust

import (
	"fmt"
	"strings"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func structList(t model.Type) bool {
	return strings.EqualFold(t.Kind, "List") && t.Elem != nil && strings.EqualFold(t.Elem.Kind, "Struct")
}
func structName(q model.AnalyzedQuery, p model.Parameter) string {
	return pascalName(q.Name) + pascalName(p.Name) + "Item"
}
func structTypeFunction(q model.AnalyzedQuery, p model.Parameter) string {
	return snakeName(q.Name) + "_" + snakeName(p.Name) + "_item_type"
}
func parameterElementType(q model.AnalyzedQuery, p model.Parameter) (string, error) {
	if structList(p.Type) {
		return structName(q, p), nil
	}
	return rustType(*p.Type.Elem)
}
func parameterRustType(q model.AnalyzedQuery, p model.Parameter) (string, error) {
	if !structList(p.Type) {
		return rustType(p.Type)
	}
	if len(p.Type.Elem.Fields) == 0 {
		return "", fmt.Errorf("List<Struct> requires at least one field")
	}
	seen := map[string]bool{}
	for _, f := range p.Type.Elem.Fields {
		n := snakeName(f.Name)
		if f.Name == "" || !rustIdent(n) || rustKeywords[n] || seen[n] {
			return "", fmt.Errorf("List<Struct> invalid or colliding field %q", f.Name)
		}
		seen[n] = true
		scalar := f.Type.UnwrapOptional()
		if scalar.Kind == "List" || scalar.Kind == "Struct" || scalar.IsOptional() {
			return "", fmt.Errorf("List<Struct> field %s must be scalar or Optional<scalar>", f.Name)
		}
		if _, err := rustType(scalar); err != nil {
			return "", fmt.Errorf("List<Struct> field %s: %w", f.Name, err)
		}
	}
	return "Vec<" + structName(q, p) + ">", nil
}
func renderStructModel(b *strings.Builder, q model.AnalyzedQuery, p model.Parameter) {
	fmt.Fprintf(b, "#[derive(Debug, Clone, PartialEq)]\npub struct %s {\n", structName(q, p))
	for _, f := range p.Type.Elem.Fields {
		typ, _ := rustType(f.Type)
		fmt.Fprintf(b, "    pub %s: %s,\n", snakeName(f.Name), typ)
	}
	b.WriteString("}\n\n")
}
func renderStructBinding(b *strings.Builder, q model.AnalyzedQuery, p model.Parameter) {
	fmt.Fprintf(b, "impl From<%s> for ydb::Value {\n    fn from(item: %s) -> Self {\n        ydb::Value::struct_from_fields(vec![\n", structName(q, p), structName(q, p))
	for _, f := range p.Type.Elem.Fields {
		value := bindValue(f.Type, "item."+snakeName(f.Name))
		writeStructFieldValue(b, f.Name, value, "            ")
	}
	b.WriteString("        ])\n    }\n}\n\n")
	fmt.Fprintf(b, "fn %s() -> ydb::Value {\n    ydb::Value::struct_from_fields(vec![\n", structTypeFunction(q, p))
	for _, f := range p.Type.Elem.Fields {
		typ, _ := rustType(f.Type)
		value := "<" + typ + ">::default()"
		if temporalVariant(f.Type.Kind) != "" {
			value = "std::time::SystemTime::UNIX_EPOCH"
		}
		binding := bindValue(f.Type, value)
		writeStructFieldValue(b, f.Name, binding, "        ")
	}
	b.WriteString("    ])\n}\n\n")
}
func writeStructFieldValue(b *strings.Builder, name, value, indent string) {
	line := fmt.Sprintf("%s(%s.to_string(), %s.into()),", indent, rustString(name), value)
	if len(line)-len(indent)-3 <= 60 {
		b.WriteString(line + "\n")
		return
	}
	fmt.Fprintf(b, "%s(\n%s    %s.to_string(),\n%s    %s.into(),\n%s),\n", indent, indent, rustString(name), indent, value, indent)
}
