package csharp

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func isStructList(t model.Type) bool {
	return t.Kind == "List" && t.Elem != nil && t.Elem.Kind == "Struct"
}
func structItemName(q model.AnalyzedQuery, p model.Parameter) string {
	return csName(q.Name) + csName(p.Name) + "Item"
}
func structColumns(t model.Type) []model.Column {
	var cols []model.Column
	for _, f := range t.Elem.Fields {
		cols = append(cols, model.Column{Name: f.Name, Type: f.Type})
	}
	return cols
}
func parameterType(q model.AnalyzedQuery, p model.Parameter) (string, error) {
	if isStructList(p.Type) {
		if len(p.Type.Elem.Fields) == 0 {
			return "", fmt.Errorf("Struct parameters require at least one field")
		}
		for _, f := range p.Type.Elem.Fields {
			if _, err := csType(f.Type); err != nil {
				return "", fmt.Errorf("Struct field %q: %w", f.Name, err)
			}
		}
		return "IReadOnlyList<" + structItemName(q, p) + ">", nil
	}
	return csType(p.Type)
}
func containsTimestamp(t model.Type) bool {
	if strings.EqualFold(t.UnwrapOptional().Kind, "timestamp") {
		return true
	}
	if isStructList(t) {
		for _, f := range t.Elem.Fields {
			if containsTimestamp(f.Type) {
				return true
			}
		}
	}
	return false
}
func structTypeExpression(t model.Type) string {
	if t.IsOptional() {
		return "new global::Ydb.Type { OptionalType = new global::Ydb.OptionalType { Item = " + structTypeExpression(*t.Elem) + " } }"
	}
	return "new global::Ydb.Type { TypeId = global::Ydb.Type.Types.PrimitiveTypeId." + csName(t.Kind) + " }"
}
func structValueExpression(t model.Type, v string) string {
	if strings.EqualFold(t.UnwrapOptional().Kind, "timestamp") {
		v = "NormalizeTimestamp(" + v + ")"
	}
	prefix := "Make"
	if t.IsOptional() {
		prefix += "Optional"
	}
	return "YdbValue." + prefix + csName(t.UnwrapOptional().Kind) + "(" + v + ")"
}
func writeStructListHelpers(b *bytes.Buffer, in *model.AnalysisResult) {
	for _, q := range in.Queries {
		for _, p := range q.Parameters {
			if !isStructList(p.Type) {
				continue
			}
			name := structItemName(q, p)
			fmt.Fprintf(b, "\n    private static YdbValue Bind%s(IReadOnlyList<%s> items)\n    {\n        ArgumentNullException.ThrowIfNull(items);\n        // The SDK has no complex empty-list factory. GetProto exposes the mutable wire type.\n        var result = YdbValue.MakeEmptyList(YdbTypeId.Uint64);\n        var proto = result.GetProto();\n        proto.Type.ListType.Item = new global::Ydb.Type\n        {\n            StructType = new global::Ydb.StructType\n            {\n                Members =\n                {\n", name, name)
			for _, f := range p.Type.Elem.Fields {
				fmt.Fprintf(b, "                    new global::Ydb.StructMember { Name = %s, Type = %s },\n", csString(f.Name), structTypeExpression(f.Type))
			}
			b.WriteString("                }\n            }\n        };\n        foreach (var item in items)\n        {\n            ArgumentNullException.ThrowIfNull(item);\n            var row = YdbValue.MakeStruct(new global::System.Collections.Generic.Dictionary<string, YdbValue>\n            {\n")
			for _, f := range p.Type.Elem.Fields {
				fmt.Fprintf(b, "                [%s] = %s,\n", csString(f.Name), structValueExpression(f.Type, "item."+csName(f.Name)))
			}
			b.WriteString("            });\n            proto.Value.Items.Add(row.GetProto().Value);\n        }\n        return result;\n    }\n")
		}
	}
}
