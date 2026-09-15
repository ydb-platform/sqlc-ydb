package java

import (
	"fmt"
	"strings"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
	parser "github.com/ydb-platform/yql-parsers/go"
)

// Structured batches use the driver's typed values on jOOQ's borrowed connection.
// Render the target through jOOQ so its table mapping applies to this SQL path too.
func jooqBatchSQL(q model.AnalyzedQuery, sql string) (string, error) {
	if q.Syntax == nil {
		return "", fmt.Errorf("%s: jOOQ batch insert requires analyzed YQL syntax", q.Name)
	}
	statements := jooqNodes[*parser.Into_table_stmtContext](q.Syntax.Root)
	if len(statements) != 1 {
		return "", fmt.Errorf("%s: jOOQ structured parameters require INSERT/UPSERT SELECT FROM AS_TABLE", q.Name)
	}
	for _, ref := range jooqNodes[*parser.Table_refContext](q.Syntax.Root) {
		if !strings.HasPrefix(strings.ToUpper(ref.GetText()), "AS_TABLE(") {
			return "", fmt.Errorf("%s: jOOQ batch insert supports AS_TABLE sources only", q.Name)
		}
	}
	ref := statements[0].Into_simple_table_ref()
	if ref == nil || ref.Simple_table_ref() == nil || ref.Simple_table_ref().Simple_table_ref_core() == nil {
		return "", fmt.Errorf("%s: jOOQ batch insert requires a static target table", q.Name)
	}
	target := ref.Simple_table_ref().Simple_table_ref_core().GetText()
	constant, err := jooqConstant(jooqID(target))
	if err != nil {
		return "", err
	}
	span := ref.Simple_table_ref().Simple_table_ref_core()
	shift := len([]rune(sql)) - len([]rune(q.SQL))
	start, end := span.GetStart().GetStart()+shift, span.GetStop().GetStop()+1+shift
	runes := []rune(sql)
	if start < 0 || end > len(runes) || start > end {
		return "", fmt.Errorf("%s: invalid resolved batch target span", q.Name)
	}
	return sqlLiteral(string(runes[:start])) + " + dsl.render(" + constant + ") + " + sqlLiteral(string(runes[end:])), nil
}
