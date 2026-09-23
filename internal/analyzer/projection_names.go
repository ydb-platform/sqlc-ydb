package analyzer

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
	parser "github.com/ydb-platform/yql-parsers/go"
)

type implicitProjection struct {
	column, ordinal int
	expression      parser.IExprContext
}

// YQL reserves every authored name before allocating columnN from each
// expression's projection ordinal. A collision discards its result order hint,
// so the wire result then follows lexically ordered struct members.
func nameImplicitProjections(columns []model.Column, unnamed []implicitProjection, wildcard bool, rewrites *wildcardRewrites) {
	if len(unnamed) == 0 {
		return
	}
	reserved := make(map[string]bool, len(columns))
	for _, column := range columns {
		if column.Name != "" {
			reserved[column.ResultName()] = true
		}
	}
	shifted := false
	for _, projection := range unnamed {
		ordinal := projection.ordinal
		name := fmt.Sprintf("column%d", ordinal)
		for reserved[name] {
			ordinal++
			name = fmt.Sprintf("column%d", ordinal)
		}
		reserved[name] = true
		columns[projection.column].Name = name
		shifted = shifted || ordinal != projection.ordinal
		if wildcard && rewrites != nil {
			// Expanding '*' changes projection ordinals. Pin the authored implicit
			// name so references such as ORDER BY column1 keep their meaning.
			offset := runeByteOffset(rewrites.source, projection.expression.GetStop().GetStop()+1)
			rewrites.replacements = append(rewrites.replacements, wildcardReplacement{start: offset, end: offset, text: " AS " + quotedYQLIdentifier(name)})
		}
	}
	if shifted {
		slices.SortStableFunc(columns, func(a, b model.Column) int { return cmp.Compare(a.ResultName(), b.ResultName()) })
	}
}
