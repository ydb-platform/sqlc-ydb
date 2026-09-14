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

func structItemName(q model.AnalyzedQuery, p model.Parameter) string {
	return q.Name + goName(p.Name) + "Item"
}

func structListBuilderName(q model.AnalyzedQuery, p model.Parameter) string {
	return "bind" + structItemName(q, p)
}

func parameterGoType(q model.AnalyzedQuery, p model.Parameter) (string, error) {
	if isStructList(p.Type) {
		return "[]" + structItemName(q, p), nil
	}
	return goType(p.Type)
}

func validateStructList(t model.Type) error {
	if len(t.Elem.Fields) == 0 {
		return fmt.Errorf("List<Struct> requires at least one field")
	}
	names := map[string]bool{}
	for _, f := range t.Elem.Fields {
		name := goName(f.Name)
		if f.Name == "" || !ident(name) || names[name] {
			return fmt.Errorf("List<Struct> has invalid or colliding field %q", f.Name)
		}
		names[name] = true
		scalar := f.Type
		if scalar.IsOptional() && scalar.Elem != nil {
			scalar = *scalar.Elem
		}
		if scalar.IsOptional() || strings.EqualFold(scalar.Kind, "List") || strings.EqualFold(scalar.Kind, "Struct") {
			return fmt.Errorf("List<Struct> field %s must be a scalar or Optional<scalar>", f.Name)
		}
		if _, err := goType(scalar); err != nil {
			return fmt.Errorf("List<Struct> field %s: %w", f.Name, err)
		}
		if extendedListTemporal(scalar) {
			return fmt.Errorf("List<Struct> field %s: extended temporal type %s is unsupported", f.Name, scalar.Kind)
		}
		if err := validateDecimal(f.Type); err != nil {
			return fmt.Errorf("List<Struct> field %s: %w", f.Name, err)
		}
	}
	return nil
}

func writeStructModel(b *bytes.Buffer, q model.AnalyzedQuery, p model.Parameter, o Options, imports *typeImports) {
	b.WriteString("type " + structItemName(q, p) + " struct {\n")
	for _, f := range p.Type.Elem.Fields {
		typ, _ := goType(f.Type)
		imports.add(f.Type)
		b.WriteString(goName(f.Name) + " " + typ)
		if o.EmitJSONTags {
			b.WriteString(" `json:" + strconv.Quote(f.Name) + "`")
		}
		b.WriteByte('\n')
	}
	b.WriteString("}\n\n")
}

// Both Go adapters pass the SDK's typed value through unchanged. Constructing
// the empty list from its declared type also preserves Struct field types.
func writeStructListBuilder(b *bytes.Buffer, q model.AnalyzedQuery, p model.Parameter) {
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
		b.WriteString("types.StructFieldValue(" + strconv.Quote(f.Name) + ", " + structScalarValue(f.Type, "item."+goName(f.Name)) + "),\n")
	}
	b.WriteString(") }\nreturn types.ListValue(items...)\n}\n\n")
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
	b.WriteString("for _, item := range " + varRef(q, p) + " {\n")
	for _, f := range p.Type.Elem.Fields {
		scalar := f.Type.UnwrapOptional()
		if !strings.EqualFold(scalar.Kind, "Decimal") {
			continue
		}
		value := "item." + goName(f.Name)
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
	b.WriteString("}\n")
}

func structParameterName(p model.Parameter) string {
	name := p.Name
	if !ident(name) || name == "ctx" || name == "opts" || name == "q" || name == "parameters" || name == "callOptions" || name == "err" || name == "item" || name == "row" || name == "rows" || name == "result" || name == "items" || name == "resultSet" || name == "ydb" || name == "query" || name == "sql" || name == "types" || name == "xerrors" || name == "errors" || name == "io" {
		return "arg"
	}
	return name
}

func validateStructDeclarations(in *model.AnalysisResult) error {
	names := map[string]bool{"Queries": true, "DBTX": true, "Querier": true, "New": true, "validateDecimalParameter": true}
	for _, q := range in.Queries {
		if len(q.Parameters) > 1 {
			names[q.Name+"Params"] = true
		}
		if q.Command == model.One || q.Command == model.Many {
			names[q.Name+"Row"] = true
		}
	}
	for _, q := range in.Queries {
		for _, p := range q.Parameters {
			if !isStructList(p.Type) {
				continue
			}
			for _, name := range []string{structItemName(q, p), structListBuilderName(q, p)} {
				if names[name] {
					return fmt.Errorf("%s parameter %s: generated declaration %s collides with another declaration", q.Name, p.Name, name)
				}
				names[name] = true
			}
		}
	}
	return nil
}
