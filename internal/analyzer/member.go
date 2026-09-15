package analyzer

import (
	"fmt"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	parser "github.com/ydb-platform/yql-parsers/go"
)

// Resolve member access from the type of its base, preserving named Struct
// identity independently of declaration order. Table qualification is handled
// separately from access to a field of a structured value.
func resolveMemberAccess(root antlr.ParserRuleContext, scope expressionScope) (model.Type, bool, error) {
	var unary *parser.Unary_subexprContext
	descendants(root, func(node antlr.Tree) {
		if ctx, ok := node.(*parser.Unary_subexprContext); ok && sameSpan(root, ctx) {
			unary = ctx
		}
	})
	if unary == nil || unary.Unary_casual_subexpr() == nil {
		return model.Type{}, false, nil
	}
	casual := unary.Unary_casual_subexpr()
	suffix := casual.Unary_subexpr_suffix()
	if suffix == nil || len(suffix.AllAn_id_or_type()) == 0 {
		return model.Type{}, false, nil
	}
	fields := suffix.AllAn_id_or_type()
	if len(suffix.AllKey_expr()) != 0 || len(suffix.AllBind_parameter()) != 0 || len(suffix.AllDIGITS()) != 0 || suffix.COLLATE() != nil {
		return model.Type{}, true, fmt.Errorf("unsupported member access %q", root.GetText())
	}
	var typ model.Type
	var err error
	invokes := suffix.AllInvoke_expr()
	if len(invokes) != 0 {
		if len(invokes) != 1 || invokes[0].GetStart().GetTokenIndex() > fields[0].GetStart().GetTokenIndex() {
			return model.Type{}, true, fmt.Errorf("unsupported member invocation %q", root.GetText())
		}
		name := ""
		if casual.Id_expr() != nil {
			name = identifier(casual.Id_expr().GetText())
		}
		if atom := casual.Atom_expr(); atom != nil && atom.NAMESPACE() != nil {
			name = identifier(atom.An_id_or_type().GetText()) + "::" + identifier(atom.Id_or_type().GetText())
		}
		if name == "" {
			return model.Type{}, true, fmt.Errorf("unsupported member base %q", casual.GetText())
		}
		typ, err = resolveFunction(name, invokes[0].(*parser.Invoke_exprContext), scope)
	} else if atom := casual.Atom_expr(); atom != nil && atom.Bind_parameter() != nil {
		name := bindName(atom.Bind_parameter())
		var exists bool
		typ, exists = scope.bindings[name]
		if !exists {
			err = fmt.Errorf("cannot resolve type of parameter $%s", name)
		}
	} else if casual.Id_expr() != nil {
		base := identifier(casual.Id_expr().GetText())
		qualified := false
		for _, rel := range scope.relations {
			if rel.alias == base || rel.table.Name == base {
				qualified = true
				break
			}
		}
		ref := columnRef{name: base}
		if qualified {
			// A simple table.column remains a pure column projection.
			if len(fields) == 1 {
				return model.Type{}, false, nil
			}
			ref = columnRef{qualifier: base, name: identifier(fields[0].GetText())}
			fields = fields[1:]
		}
		var col model.Column
		col, err = resolveColumn(scope.relations, ref)
		typ = col.Type
	} else {
		return model.Type{}, true, fmt.Errorf("unsupported member base %q", casual.GetText())
	}
	if err != nil {
		return model.Type{}, true, err
	}
	for _, field := range fields {
		if typ.IsOptional() {
			return model.Type{}, true, fmt.Errorf("member access on Optional<Struct> is not yet supported")
		}
		if typ.Kind != "Struct" {
			return model.Type{}, true, fmt.Errorf("member access requires Struct, got %s", typ.String())
		}
		name := identifier(field.GetText())
		found := false
		for _, member := range typ.Fields {
			if member.Name == name {
				typ = member.Type
				found = true
				break
			}
		}
		if !found {
			return model.Type{}, true, fmt.Errorf("unknown struct field %q", name)
		}
	}
	return typ, true, nil
}
