package golang

import (
	"bytes"
	"strconv"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func writeEachPreamble(b *bytes.Buffer, q model.AnalyzedQuery, o Options) {
	if o.Runtime == "ydb" {
		b.WriteString("defer func() { err = xerrors.WithStackTrace(err) }()\n\n")
	}
	b.WriteString("if consume == nil { return errors.New(" + strconv.Quote(q.Name+": :each requires a non-nil callback") + ") }\n")
	b.WriteString("if err := ctx.Err(); err != nil { return err }\n\n")
}

func writeSQLEach(b *bytes.Buffer, q model.AnalyzedQuery, parameters []string) {
	b.WriteString("ctx, cancel := context.WithCancel(ctx)\ndefer cancel()\n\n")
	b.WriteString("rows, err := " + generatedCall("q.db.QueryContext", querySQL(q), parameters) + "\n")
	b.WriteString(`if err != nil { return err }
 exhausted := false
 defer func() {
  // Cancel an unfinished stream before Close so early exit does not drain it.
  if !exhausted { cancel() }
  err = errors.Join(err, rows.Close())
 }()

 columns, err := rows.Columns()
 if err != nil { return err }
 if len(columns) == 0 { return errors.New(":each requires a result set with columns") }

 for rows.Next() {
  if err := ctx.Err(); err != nil { return err }
 `)
	b.WriteString("var row " + q.Name + "Row\n")
	b.WriteString("if err := " + scanCall("rows.Scan", scanDestinations(q.ResultSets[0])) + "; err != nil { return err }\n")
	b.WriteString(`if err := ctx.Err(); err != nil { return err }
  if err := consume(row); err != nil { return err }
 }

 if err := rows.Err(); err != nil { return err }
 if rows.NextResultSet() { return errors.New(":each requires exactly one result set") }
 if err := rows.Err(); err != nil { return err }
 if err := ctx.Err(); err != nil { return err }
 exhausted = true

 return nil
`)
}

func writeYDBEach(b *bytes.Buffer, q model.AnalyzedQuery, opt string) {
	b.WriteString("ctx, cancel := context.WithCancel(ctx)\ndefer cancel()\n\n")
	b.WriteString("result, err := " + ydbCall("q.db.Query", querySQL(q), opt) + "\n")
	b.WriteString(`if err != nil { return err }
 exhausted := false
 defer func() {
  // Cancel an unfinished stream before Close, including during panic unwinding.
  if !exhausted { cancel() }
  err = errors.Join(err, result.Close(ctx))
 }()

 resultSet, err := result.NextResultSet(ctx)
 if errors.Is(err, io.EOF) { return query.ErrNoResultSets }
 if err != nil { return err }

 for {
  if err := ctx.Err(); err != nil { return err }
  r, err := resultSet.NextRow(ctx)
  if errors.Is(err, io.EOF) { break }
  if err != nil { return err }
 `)
	b.WriteString("var row " + q.Name + "Row\n")
	b.WriteString("if err := r.ScanNamed(\n" + scanNamed(q.ResultSets[0]) + ",\n); err != nil { return err }\n")
	b.WriteString(`if err := ctx.Err(); err != nil { return err }
  if err := consume(row); err != nil { return err }
 }

 _, err = result.NextResultSet(ctx)
 if err == nil { return query.ErrMoreThanOneResultSet }
 if !errors.Is(err, io.EOF) { return err }
 if err := ctx.Err(); err != nil { return err }
 exhausted = true

 return nil
`)
}
