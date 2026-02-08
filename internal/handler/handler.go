package handler

import (
	"errors"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/sqlc-dev/sqlc-engine-ydb/internal/schema"
	"github.com/sqlc-dev/sqlc-engine-ydb/internal/schema/ddl"
	"github.com/sqlc-dev/sqlc-engine-ydb/internal/schema/runtime"
	"github.com/sqlc-dev/sqlc/pkg/engine"
	parser "github.com/ydb-platform/yql-parsers/go"
)

// YDBCommentSyntax: query files use "--" for annotations (e.g. "-- name: GetAuthor :one").
var ydbCommentSyntax = engine.CommentSyntax{Dash: true}

// Parse implements the engine plugin Parse RPC.
// It splits the request SQL by " name: X :cmd" (via engine.QueryBlocks), parses each block
// with the YDB parser, and returns one Statement per block with parameters and result columns.
func Parse(req *engine.ParseRequest) (*engine.ParseResponse, error) {
	reg, err := buildRegistryFromRequest(req)
	if err != nil {
		return nil, err
	}
	return ParseWithRegistry(req, reg)
}

// ParseWithRegistry is for tests: call with a schema.Registry to control schema.
func ParseWithRegistry(req *engine.ParseRequest, reg schema.Registry) (*engine.ParseResponse, error) {
	sql := strings.TrimSpace(req.GetSql())
	if sql == "" {
		return &engine.ParseResponse{Statements: nil}, nil
	}

	blocks, err := engine.QueryBlocks(sql, ydbCommentSyntax)
	if err != nil || len(blocks) == 0 {
		return &engine.ParseResponse{Statements: []*engine.Statement{}}, nil
	}

	statements := make([]*engine.Statement, 0, len(blocks))
	for _, block := range blocks {
		st, err := parseOneBlock(block.SQL, block.Name, block.Cmd, reg)
		if err != nil {
			return nil, err
		}
		statements = append(statements, st)
	}
	return &engine.ParseResponse{Statements: statements}, nil
}

// parseOneBlock parses a single query block and returns a Statement with name, cmd, sql, parameters, columns.
func parseOneBlock(sql, name string, cmd engine.Cmd, reg schema.Registry) (*engine.Statement, error) {
	st := engine.StatementMeta(name, cmd, sql)
	sql = strings.TrimSpace(sql)
	if sql == "" {
		return st, nil
	}
	tree, err := parseSQL(sql)
	if err != nil {
		return nil, err
	}
	collect := runCollect(tree)
	st.Parameters = paramsFromCollector(collect, reg)
	st.Columns, _ = columnsFromCollector(collect, reg, sql)
	return st, nil
}

func buildRegistryFromRequest(req *engine.ParseRequest) (schema.Registry, error) {
	if s := req.GetSchemaSql(); s != "" {
		return ddl.Registry(s)
	}
	if req.GetConnectionParams() != nil {
		return runtime.Registry(req.GetConnectionParams()), nil
	}
	return schema.Empty(), nil
}

type errListener struct {
	*antlr.DefaultErrorListener
	err string
}

func (e *errListener) SyntaxError(recognizer antlr.Recognizer, offendingSymbol interface{}, line, column int, msg string, _ antlr.RecognitionException) {
	e.err = msg
}

func parseSQL(sql string) (parser.ISql_queryContext, error) {
	input := antlr.NewInputStream(sql)
	lexer := parser.NewYQLLexer(input)
	stream := antlr.NewCommonTokenStream(lexer, 0)
	p := parser.NewYQLParser(stream)
	el := &errListener{DefaultErrorListener: antlr.NewDefaultErrorListener()}
	p.AddErrorListener(el)
	tree := p.Sql_query()
	if el.err != "" {
		return nil, errors.New(el.err)
	}
	return tree, nil
}

func paramsFromCollector(c *collectListener, reg schema.Registry) []*engine.Parameter {
	names := c.OrderedParams()
	out := make([]*engine.Parameter, 0, len(names))
	for i, name := range names {
		dataType := "Any"
		nullable := true
		if reg != nil {
			for _, tableName := range c.TableNames() {
				cols, ok := reg.Columns(strings.ToLower(tableName))
				if !ok {
					continue
				}
				for _, col := range cols {
					if strings.EqualFold(col.Name, name) {
						dataType = col.DataType
						nullable = col.Nullable
						break
					}
				}
				if dataType != "Any" {
					break
				}
			}
		}
		out = append(out, &engine.Parameter{
			Name:     name,
			Position: int32(i + 1),
			DataType: dataType,
			Nullable: nullable,
		})
	}
	return out
}

func columnsFromCollector(c *collectListener, reg schema.Registry, sql string) ([]*engine.Column, string) {
	if reg == nil || !c.ReturnsRows() {
		return nil, sql
	}
	tableNames := c.TableNames()
	if len(tableNames) == 0 {
		return nil, sql
	}
	needStar := (c.StmtKind() == StmtKindSelect && c.HasSelectStar()) ||
		(c.HasReturningStar() && (c.StmtKind() == StmtKindInsert || c.StmtKind() == StmtKindUpsert || c.StmtKind() == StmtKindReplace || c.StmtKind() == StmtKindUpdate || c.StmtKind() == StmtKindDelete))
	if !needStar {
		return nil, sql
	}
	firstTable := strings.ToLower(tableNames[0])
	cols, ok := reg.Columns(firstTable)
	if !ok {
		return nil, sql
	}
	out := make([]*engine.Column, 0, len(cols))
	for _, col := range cols {
		out = append(out, &engine.Column{
			Name:      col.Name,
			DataType:  col.DataType,
			Nullable:  col.Nullable,
			IsArray:   col.IsArray,
			ArrayDims: col.ArrayDims,
			TableName: firstTable,
		})
	}
	return out, sql
}