package golang

import (
	"fmt"
	"go/token"
	"path"
	"strconv"
	"strings"

	"github.com/ydb-platform/sqlc-ydb/internal/config"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func overrideBase(t model.Type) (string, error) {
	base := t.UnwrapOptional()
	switch strings.ToLower(base.Kind) {
	case "bool", "int32", "uint64", "double", "utf8", "string":
		return goType(base)
	default:
		return "", fmt.Errorf("Go type overrides support only Bool, Int32, Uint64, Double, Utf8, and String with the pinned YDB Go SDK; got %s", t.String())
	}
}

func overrideReference(override config.GoOverride) (string, string, error) {
	typeName := override.GoType.Type
	importPath := override.GoType.Import
	if importPath == "" && strings.Contains(typeName, ".") {
		if last := strings.LastIndex(override.GoType.Type, "."); last >= 0 {
			importPath, typeName = override.GoType.Type[:last], override.GoType.Type[last+1:]
		}
	}
	if !token.IsIdentifier(typeName) || typeName == "_" {
		return "", "", fmt.Errorf("go_type %q must name one Go type", override.GoType.Type)
	}
	if importPath == "" {
		if override.GoType.Package != "" {
			return "", "", fmt.Errorf("go_type.package requires go_type.import")
		}
		return typeName, "", nil
	}
	if strings.ContainsAny(importPath, " \t\n\r\"") || strings.HasPrefix(importPath, "/") || path.Clean(importPath) != importPath {
		return "", "", fmt.Errorf("invalid go_type.import %q", importPath)
	}
	if override.GoType.Package != "" && !token.IsIdentifier(override.GoType.Package) {
		return "", "", fmt.Errorf("invalid go_type.package %q", override.GoType.Package)
	}
	return typeName, importPath, nil
}

func (o Options) resolvedOverride(index int) (string, string, error) {
	typeName, importPath, err := overrideReference(o.Overrides[index])
	if err != nil || importPath == "" {
		return typeName, importPath, err
	}
	return fmt.Sprintf("sqlcOverride%d.%s", o.overrideAlias(index), typeName), importPath, nil
}

func (o Options) overrideAlias(index int) int {
	_, importPath, _ := overrideReference(o.Overrides[index])
	alias := index
	for i := range index {
		_, previous, _ := overrideReference(o.Overrides[i])
		if previous == importPath {
			alias = i
			break
		}
	}
	return alias
}

func (o Options) overrideForType(t model.Type) int {
	for i, override := range o.Overrides {
		if override.DBType != "" && strings.EqualFold(override.DBType, t.UnwrapOptional().Kind) && override.Nullable == t.IsOptional() {
			return i
		}
	}
	return -1
}

func (o Options) overrideForColumn(c model.Column) int {
	if c.Table == "" {
		return -1
	}
	for i, override := range o.Overrides {
		if override.Column == c.Table+"."+c.Name {
			return i
		}
	}
	return -1
}

func (o Options) columnOverride(c model.Column) int {
	if index := o.overrideForColumn(c); index >= 0 {
		return index
	}
	return o.overrideForType(c.Type)
}

func (o Options) parameterOverride(q model.AnalyzedQuery, p model.Parameter) (int, error) {
	selected := -1
	matched := false
	for _, column := range q.ParameterColumns[p.Name] {
		index := o.overrideForColumn(column)
		if index < 0 {
			index = o.overrideForType(p.Type)
		}
		if !matched {
			selected = index
			matched = true
			continue
		}
		if (selected < 0) != (index < 0) || selected >= 0 && index >= 0 && !o.sameOverrideType(selected, index) {
			return -1, fmt.Errorf("parameter $%s is used with columns having different Go type overrides; use separate parameters or a matching db_type override", p.Name)
		}
	}
	if matched {
		return selected, nil
	}
	return o.overrideForType(p.Type), nil
}

func (o Options) sameOverrideType(left, right int) bool {
	leftType, leftImport, _ := overrideReference(o.Overrides[left])
	rightType, rightImport, _ := overrideReference(o.Overrides[right])
	return leftType == rightType && leftImport == rightImport
}

func (o Options) columnGoType(c model.Column) (string, error) {
	if index := o.columnOverride(c); index >= 0 {
		reference, _, err := o.resolvedOverride(index)
		if err != nil {
			return "", err
		}
		if c.Type.IsOptional() {
			return "*" + reference, nil
		}
		return reference, nil
	}
	return goType(c.Type)
}

func (o Options) parameterGoType(q model.AnalyzedQuery, p model.Parameter) (string, error) {
	index, err := o.parameterOverride(q, p)
	if err != nil {
		return "", err
	}
	if index >= 0 {
		reference, _, err := o.resolvedOverride(index)
		if err != nil {
			return "", err
		}
		if p.Type.IsOptional() {
			return "*" + reference, nil
		}
		return reference, nil
	}
	return parameterGoType(q, p)
}

func (o Options) convertedParameter(q model.AnalyzedQuery, p model.Parameter, value string) string {
	index, _ := o.parameterOverride(q, p)
	if index < 0 {
		return value
	}
	base, _ := overrideBase(p.Type)
	if !p.Type.IsOptional() {
		return base + "(" + value + ")"
	}
	return "func() *" + base + " { if " + value + " == nil { return nil }; v := " + base + "(*" + value + "); return &v }()"
}

func (o Options) overrideImports(indices map[int]bool) []string {
	var imports []string
	seen := map[string]bool{}
	for i, override := range o.Overrides {
		if !indices[i] {
			continue
		}
		_, importPath, _ := overrideReference(override)
		if importPath != "" && !seen[importPath] {
			imports = append(imports, "sqlcOverride"+strconv.Itoa(o.overrideAlias(i))+" "+strconv.Quote(importPath))
			seen[importPath] = true
		}
	}
	return imports
}

func (o Options) validateOverrides() error {
	seen := map[string]bool{}
	for i, override := range o.Overrides {
		if _, err := overrideBase(model.Type{Kind: override.DBType}); override.DBType != "" && err != nil {
			return fmt.Errorf("gen.go.overrides[%d].db_type: %w", i, err)
		}
		if override.Column != "" {
			table, name, ok := strings.Cut(override.Column, ".")
			if !ok || table == "" || name == "" {
				return fmt.Errorf("gen.go.overrides[%d].column must be table.column", i)
			}
		}
		if _, _, err := overrideReference(override); err != nil {
			return fmt.Errorf("gen.go.overrides[%d]: %w", i, err)
		}
		key := "column:" + override.Column
		if override.Column == "" {
			key = "db_type:" + strings.ToLower(override.DBType) + "/" + strconv.FormatBool(override.Nullable)
		}
		if seen[key] {
			return fmt.Errorf("gen.go.overrides[%d]: duplicate override for %q", i, key)
		}
		seen[key] = true
	}
	return nil
}
