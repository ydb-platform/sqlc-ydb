package analyzer

import (
	"fmt"
	"path"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	parser "github.com/ydb-platform/yql-parsers/go"
)

func directEmbedCall(expr parser.IExprContext) (*parser.Invoke_exprContext, bool) {
	var unary *parser.Unary_subexprContext
	descendants(expr, func(node antlr.Tree) {
		if ctx, ok := node.(*parser.Unary_subexprContext); ok && sameSpan(expr, ctx) {
			unary = ctx
		}
	})
	if unary == nil || unary.Unary_casual_subexpr() == nil {
		return nil, false
	}
	casual := unary.Unary_casual_subexpr()
	suffix := casual.Unary_subexpr_suffix()
	if casual.Id_expr() == nil || suffix == nil || !strings.EqualFold(identifier(casual.Id_expr().GetText()), "sqlc") || len(suffix.AllAn_id_or_type()) != 1 || len(suffix.AllInvoke_expr()) != 1 || !strings.EqualFold(identifier(suffix.An_id_or_type(0).GetText()), "embed") {
		return nil, false
	}
	invoke, ok := suffix.Invoke_expr(0).(*parser.Invoke_exprContext)
	return invoke, ok && suffix.GetText() == "."+suffix.An_id_or_type(0).GetText()+invoke.GetText()
}

func embedProjection(block queryBlock, core *parser.Select_coreContext, result parser.IResult_columnContext, invoke *parser.Invoke_exprContext, relations []relation, ordinal, start int) ([]model.Column, model.Embedding, []string, error) {
	if block.wildcards == nil || block.wildcards.embedCore != core {
		return nil, model.Embedding{}, nil, fmt.Errorf("sqlc.embed is supported only in a single top-level SELECT result")
	}
	if result.An_id_or_type() != nil || result.An_id_as_compat() != nil {
		return nil, model.Embedding{}, nil, fmt.Errorf("sqlc.embed cannot have an AS alias; alias the table in FROM instead")
	}
	if invoke.ASTERISK() != nil || invoke.Named_expr_list() == nil || len(invoke.Named_expr_list().AllNamed_expr()) != 1 {
		return nil, model.Embedding{}, nil, fmt.Errorf("sqlc.embed expects one table or relation alias")
	}
	arg := invoke.Named_expr_list().Named_expr(0)
	if arg.AS() != nil || arg.Expr() == nil || !isPureColumnExpression(arg.Expr()) {
		return nil, model.Embedding{}, nil, fmt.Errorf("sqlc.embed expects a table or relation alias, such as sqlc.embed(t)")
	}
	refs := columnRefs(arg.Expr())
	if len(refs) != 1 || refs[0].qualifier != "" {
		return nil, model.Embedding{}, nil, fmt.Errorf("sqlc.embed expects a table or relation alias, such as sqlc.embed(t)")
	}
	var matched *relation
	for i := range relations {
		if relations[i].alias == refs[0].name {
			matched = &relations[i]
			break
		}
	}
	if matched == nil {
		return nil, model.Embedding{}, nil, fmt.Errorf("sqlc.embed refers to unknown table or alias %q", refs[0].name)
	}
	if !matched.physical {
		return nil, model.Embedding{}, nil, fmt.Errorf("sqlc.embed requires a physical catalog table; %q is a derived or tabular source", refs[0].name)
	}
	if matched.optional {
		return nil, model.Embedding{}, nil, fmt.Errorf("sqlc.embed of nullable OUTER JOIN side %q is unsupported; select explicit columns or use an INNER JOIN", refs[0].name)
	}
	field := path.Base(matched.table.Name)
	for _, previous := range block.wildcards.embeds {
		if strings.EqualFold(previous.Field, field) {
			return nil, model.Embedding{}, nil, fmt.Errorf("sqlc.embed of table %q repeats logical result field %q; use explicit columns", matched.table.Name, field)
		}
	}
	if len(matched.table.Columns) == 0 {
		return nil, model.Embedding{}, nil, fmt.Errorf("sqlc.embed table %q has no columns", matched.table.Name)
	}
	columns := make([]model.Column, 0, len(matched.table.Columns))
	expressions := make([]string, 0, len(matched.table.Columns))
	for i, column := range matched.table.Columns {
		wire := fmt.Sprintf("__sqlc_embed_%d_%d", ordinal, i)
		column.WireName = wire
		columns = append(columns, column)
		expressions = append(expressions, quotedYQLIdentifier(matched.alias)+"."+quotedYQLIdentifier(column.Name)+" AS "+quotedYQLIdentifier(wire))
	}
	return columns, model.Embedding{Start: start, End: start + len(columns), Table: matched.table.Name, Alias: matched.alias, Field: field}, expressions, nil
}
