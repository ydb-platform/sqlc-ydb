package golang

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func isStructList(t model.Type) bool {
	return strings.EqualFold(t.Kind, "List") && t.Elem != nil && strings.EqualFold(t.Elem.Kind, "Struct")
}

func isStructParameter(t model.Type) bool {
	return strings.EqualFold(t.Kind, "Struct") || isStructList(t)
}

func structFields(t model.Type) []model.StructField {
	if isStructList(t) {
		return t.Elem.Fields
	}
	return t.Fields
}

func structItemName(q model.AnalyzedQuery, p model.Parameter) string {
	if strings.EqualFold(p.Type.Kind, "Struct") {
		return q.Name + goName(p.Name)
	}
	return q.Name + goName(p.Name) + "Item"
}

func structListBuilderName(q model.AnalyzedQuery, p model.Parameter) string {
	return "bind" + structItemName(q, p)
}

func parameterGoType(q model.AnalyzedQuery, p model.Parameter) (string, error) {
	if isStructParameter(p.Type) {
		prefix := ""
		if isStructList(p.Type) {
			prefix = "[]"
		}
		return prefix + structItemName(q, p), nil
	}
	return goType(p.Type)
}

func validateStructParameter(t model.Type, o Options) error {
	kind := "Struct"
	if isStructList(t) {
		kind = "List<Struct>"
	}
	fields := structFields(t)
	if len(fields) == 0 {
		return fmt.Errorf("%s requires at least one field", kind)
	}
	names := map[string]bool{}
	for _, f := range fields {
		name := o.fieldName(f.Name)
		if f.Name == "" || !ident(name) || names[name] {
			return fmt.Errorf("%s has invalid or colliding field %q", kind, f.Name)
		}
		names[name] = true
		scalar := f.Type
		if scalar.IsOptional() && scalar.Elem != nil {
			scalar = *scalar.Elem
		}
		if scalar.IsOptional() || strings.EqualFold(scalar.Kind, "List") || strings.EqualFold(scalar.Kind, "Struct") || strings.EqualFold(scalar.Kind, "Dict") {
			return fmt.Errorf("%s field %s must be a scalar or Optional<scalar>", kind, f.Name)
		}
		if _, err := goType(scalar); err != nil {
			return fmt.Errorf("%s field %s: %w", kind, f.Name, err)
		}
		if extendedListTemporal(scalar) {
			return fmt.Errorf("%s field %s: extended temporal type %s is unsupported", kind, f.Name, scalar.Kind)
		}
		if err := validateDecimal(f.Type); err != nil {
			return fmt.Errorf("%s field %s: %w", kind, f.Name, err)
		}
	}
	return nil
}

func writeStructModel(b *bytes.Buffer, q model.AnalyzedQuery, p model.Parameter, o Options, imports *typeImports) {
	b.WriteString("type " + structItemName(q, p) + " struct {\n")
	for _, f := range structFields(p.Type) {
		typ, _ := goType(f.Type)
		imports.add(f.Type)
		b.WriteString(o.fieldName(f.Name) + " " + typ)
		if o.EmitJSONTags {
			b.WriteString(" `json:" + strconv.Quote(f.Name) + "`")
		}
		b.WriteByte('\n')
	}
	b.WriteString("}\n\n")
}

// Both Go adapters pass the SDK's typed value through unchanged. Constructing
// the empty list from its declared type also preserves Struct field types.
func writeStructListBuilder(b *bytes.Buffer, q model.AnalyzedQuery, p model.Parameter, o Options) {
	b.WriteString("func " + structListBuilderName(q, p) + "(values []" + structItemName(q, p) + ") types.Value {\n")
	b.WriteString("if len(values) == 0 { return types.ZeroValue(types.List(types.Struct(\n")
	for _, f := range p.Type.Elem.Fields {
		typ := ydbTypeExpr(f.Type.UnwrapOptional())
		if f.Type.IsOptional() {
			typ = "types.Optional(" + typ + ")"
		}
		b.WriteString("types.StructField(" + strconv.Quote(f.Name) + ", " + typ + "),\n")
	}
	b.WriteString("))) }\nitems := make([]types.Value,len(values))\nfor i, item := range values { items[i] = types.StructValue(\n")
	for _, f := range p.Type.Elem.Fields {
		b.WriteString("types.StructFieldValue(" + strconv.Quote(f.Name) + ", " + structScalarValue(f.Type, "item."+o.fieldName(f.Name)) + "),\n")
	}
	b.WriteString(") }\nreturn types.ListValue(items...)\n}\n\n")
}

func writeStructBuilder(b *bytes.Buffer, q model.AnalyzedQuery, p model.Parameter, o Options) {
	b.WriteString("func " + structListBuilderName(q, p) + "(item " + structItemName(q, p) + ") types.Value {\nreturn types.StructValue(\n")
	for _, f := range p.Type.Fields {
		b.WriteString("types.StructFieldValue(" + strconv.Quote(f.Name) + ", " + structScalarValue(f.Type, "item."+o.fieldName(f.Name)) + "),\n")
	}
	b.WriteString(")\n}\n\n")
}

func scalarListBuilderName(q model.AnalyzedQuery, p model.Parameter) string {
	return "bind" + q.Name + goName(p.Name)
}

func writeScalarListBuilder(b *bytes.Buffer, q model.AnalyzedQuery, p model.Parameter) {
	e := *p.Type.Elem
	b.WriteString("func " + scalarListBuilderName(q, p) + "(values []")
	typ, _ := goType(e)
	b.WriteString(typ + ") types.Value {\n")
	typeExpr := ydbTypeExpr(e.UnwrapOptional())
	if e.IsOptional() {
		typeExpr = "types.Optional(" + typeExpr + ")"
	}
	b.WriteString("if len(values) == 0 { return types.ZeroValue(types.List(" + typeExpr + ")) }\nitems := make([]types.Value,len(values))\nfor i,item := range values { items[i] = " + structScalarValue(e, "item") + " }\nreturn types.ListValue(items...)\n}\n\n")
}

func structScalarValue(t model.Type, value string) string {
	if t.IsOptional() {
		return nullableValueExpr(*t.Elem, value)
	}
	switch strings.ToLower(t.Kind) {
	case "decimal":
		return "types.DecimalValue(&" + value + ")"
	case "date":
		return "types.DateValueFromTime(" + value + ")"
	case "datetime":
		return "types.DatetimeValueFromTime(" + value + ")"
	case "timestamp":
		return "types.TimestampValueFromTime(" + value + ")"
	case "interval":
		return "types.IntervalValueFromDuration(" + value + ")"
	case "yson":
		return "types.YSONValueFromBytes(" + value + ")"
	default:
		return "types." + paramMethod(t) + "Value(" + value + ")"
	}
}

func writeStructDecimalValidations(b *bytes.Buffer, q model.AnalyzedQuery, p model.Parameter, o Options) {
	if !hasKind(p.Type, "decimal") {
		return
	}
	valuePrefix := varRef(q, p, o) + "."
	if isStructList(p.Type) {
		b.WriteString("for _, item := range " + varRef(q, p, o) + " {\n")
		valuePrefix = "item."
	}
	for _, f := range structFields(p.Type) {
		scalar := f.Type.UnwrapOptional()
		if !strings.EqualFold(scalar.Kind, "Decimal") {
			continue
		}
		value := valuePrefix + o.fieldName(f.Name)
		if f.Type.IsOptional() {
			b.WriteString("if " + value + " != nil {\n")
			value = "*" + value
		}
		precision, scale := decimalArgs(scalar)
		b.WriteString("if err := validateDecimalParameter(" + strconv.Quote("$"+p.Name+"."+f.Name) + ", " + value + ", " + precision + ", " + scale + "); err != nil { " + decimalValidationFailure(q, o) + " }\n")
		if f.Type.IsOptional() {
			b.WriteString("}\n")
		}
	}
	if isStructList(p.Type) {
		b.WriteString("}\n")
	}
}

func validateStructDeclarations(in *model.AnalysisResult, o Options) error {
	names := map[string]bool{"Queries": true, "DBTX": true, "Querier": true, "New": true, "validateDecimalParameter": true}
	for _, q := range in.Queries {
		if len(q.Parameters) > 1 {
			names[q.Name+"Params"] = true
		}
		if q.Command == model.One || q.Command == model.Many || q.Command == model.Each {
			names[q.Name+"Row"] = true
		}
	}
	for _, table := range in.Catalog.Tables {
		if !embeddedTableUsed(in, table.Name) {
			continue
		}
		name := embeddedGoType(table.Name)
		if names[name] {
			return fmt.Errorf("embedded table %q: generated model name %s collides with another declaration", table.Name, name)
		}
		names[name] = true
		fields := map[string]bool{}
		for _, column := range table.Columns {
			field := o.fieldName(column.Name)
			if fields[field] {
				return fmt.Errorf("embedded table %q: colliding model field %q", table.Name, column.Name)
			}
			fields[field] = true
		}
	}
	for _, q := range in.Queries {
		for _, p := range q.Parameters {
			declarations := []string{}
			if isStructParameter(p.Type) {
				declarations = append(declarations, structItemName(q, p), structListBuilderName(q, p))
			} else if strings.EqualFold(p.Type.Kind, "List") {
				declarations = append(declarations, scalarListBuilderName(q, p))
			}
			for _, name := range declarations {
				if names[name] {
					return fmt.Errorf("%s parameter %s: generated declaration %s collides with another declaration", q.Name, p.Name, name)
				}
				names[name] = true
			}
		}
	}
	return nil
}
