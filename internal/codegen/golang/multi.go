package golang

import (
	"bytes"
	"strconv"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func hasMulti(queries []model.AnalyzedQuery) bool {
	for _, q := range queries {
		if q.Command == model.Multi {
			return true
		}
	}
	return false
}

func writeSQLMulti(b *bytes.Buffer, q model.AnalyzedQuery, parameters []string, o Options) {
	result := q.Name + "Result"
	b.WriteString("rows, err := " + generatedCall("q.db.QueryContext", querySQL(q), parameters) + "\n")
	b.WriteString("if err != nil { return " + result + "{}, err }\n")
	b.WriteString("defer func() { err = errors.Join(err, rows.Close()); if err != nil { out = " + result + "{} } }()\n\n")
	for i, rs := range q.ResultSets {
		index := strconv.Itoa(i + 1)
		if i > 0 {
			b.WriteString("if !rows.NextResultSet() {\n")
			b.WriteString("if err := rows.Err(); err != nil { return " + result + "{}, err }\n")
			b.WriteString("return " + result + "{}, fmt.Errorf(\"" + q.Name + ": missing result set " + index + "\")\n}\n")
		}
		b.WriteString("{\n")
		b.WriteString("columns, err := rows.Columns()\nif err != nil { return " + result + "{}, err }\n")
		b.WriteString("columnTypes, err := rows.ColumnTypes()\nif err != nil { return " + result + "{}, err }\n")
		writeMultiSchemaCheck(b, q, rs, i, "DatabaseTypeName")
		if o.EmitEmptySlices {
			b.WriteString("out." + rs.Name + " = make([]" + q.Name + rs.Name + "Row, 0)\n")
		}
		b.WriteString("for rows.Next() {\nvar row " + q.Name + rs.Name + "Row\n")
		b.WriteString("if err := " + scanCall("rows.Scan", scanDestinations(rs, o)) + "; err != nil { return " + result + "{}, err }\n")
		b.WriteString("out." + rs.Name + " = append(out." + rs.Name + ", row)\n}\n")
		b.WriteString("if err := rows.Err(); err != nil { return " + result + "{}, err }\n}\n\n")
	}
	b.WriteString("if rows.NextResultSet() { return " + result + "{}, fmt.Errorf(\"" + q.Name + ": unexpected extra result set\") }\n")
	b.WriteString("if err := rows.Err(); err != nil { return " + result + "{}, err }\n")
	b.WriteString("if err := ctx.Err(); err != nil { return " + result + "{}, err }\n")
	b.WriteString("return out, nil\n")
}

func writeYDBMulti(b *bytes.Buffer, q model.AnalyzedQuery, opt string, o Options) {
	result := q.Name + "Result"
	b.WriteString("stream, err := " + ydbCall("q.db.Query", querySQL(q), opt) + "\n")
	b.WriteString("if err != nil { return " + result + "{}, xerrors.WithStackTrace(err) }\n")
	b.WriteString("defer func() { err = errors.Join(err, stream.Close(ctx)); if err != nil { out = " + result + "{} } }()\n\n")
	for i, rs := range q.ResultSets {
		index := strconv.Itoa(i + 1)
		b.WriteString("{\n")
		b.WriteString("resultSet, err := stream.NextResultSet(ctx)\n")
		b.WriteString("if errors.Is(err, io.EOF) { return " + result + "{}, fmt.Errorf(\"" + q.Name + ": missing result set " + index + "\") }\n")
		b.WriteString("if err != nil { return " + result + "{}, xerrors.WithStackTrace(err) }\n")
		b.WriteString("columns := resultSet.Columns()\ncolumnTypes := resultSet.ColumnTypes()\n")
		writeMultiSchemaCheck(b, q, rs, i, "Yql")
		if o.EmitEmptySlices {
			b.WriteString("out." + rs.Name + " = make([]" + q.Name + rs.Name + "Row, 0)\n")
		}
		b.WriteString("for {\nrowResult, err := resultSet.NextRow(ctx)\n")
		b.WriteString("if errors.Is(err, io.EOF) { break }\n")
		b.WriteString("if err != nil { return " + result + "{}, xerrors.WithStackTrace(err) }\n")
		b.WriteString("var row " + q.Name + rs.Name + "Row\n")
		b.WriteString("if err := rowResult.ScanNamed(\n" + scanNamed(rs, o) + ",\n); err != nil { return " + result + "{}, xerrors.WithStackTrace(err) }\n")
		b.WriteString("out." + rs.Name + " = append(out." + rs.Name + ", row)\n}\n}\n\n")
	}
	b.WriteString("_, err = stream.NextResultSet(ctx)\n")
	b.WriteString("if err == nil { return " + result + "{}, fmt.Errorf(\"" + q.Name + ": unexpected extra result set\") }\n")
	b.WriteString("if !errors.Is(err, io.EOF) { return " + result + "{}, xerrors.WithStackTrace(err) }\n")
	b.WriteString("if err := ctx.Err(); err != nil { return " + result + "{}, err }\n")
	b.WriteString("return out, nil\n")
}

func writeMultiSchemaCheck(b *bytes.Buffer, q model.AnalyzedQuery, rs model.ResultSet, index int, typeMethod string) {
	result := q.Name + "Result"
	count := strconv.Itoa(len(rs.Columns))
	number := strconv.Itoa(index + 1)
	b.WriteString("if len(columns) != " + count + " || len(columnTypes) != " + count + " { return " + result + "{}, fmt.Errorf(\"" + q.Name + ": result set " + number + " has %d columns and %d types; want " + count + "\", len(columns), len(columnTypes)) }\n")
	for j, c := range rs.Columns {
		column := "columns[" + strconv.Itoa(j) + "]"
		actualType := "columnTypes[" + strconv.Itoa(j) + "]." + typeMethod + "()"
		b.WriteString("if " + column + " != " + strconv.Quote(c.ResultName()) + " || " + actualType + " != " + strconv.Quote(c.Type.String()) + " {\n")
		b.WriteString("return " + result + "{}, fmt.Errorf(\"" + q.Name + ": result set " + number + " column " + strconv.Itoa(j+1) + " has schema %q %q; want %q %q\", " + column + ", " + actualType + ", " + strconv.Quote(c.ResultName()) + ", " + strconv.Quote(c.Type.String()) + ")\n}\n")
	}
}
