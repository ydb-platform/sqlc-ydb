package golang

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func nativeErrorInput() *model.AnalysisResult {
	in := sample()
	decimal := model.Type{Kind: "Decimal", Precision: 22, Scale: 9}
	for _, command := range []model.Command{model.One, model.Many, model.Exec} {
		q := model.AnalyzedQuery{
			Name: "Amount" + strings.TrimPrefix(string(command), ":"), Command: command,
			SQL:        "SELECT $amount AS amount;",
			Parameters: []model.Parameter{{Name: "amount", Type: decimal}},
		}
		if command != model.Exec {
			q.ResultSets = []model.ResultSet{{Columns: []model.Column{{Name: "amount", Type: decimal}}}}
		}
		in.Queries = append(in.Queries, q)
	}
	for i := range in.Queries {
		in.Queries[i].Source.File = in.Queries[i].Name + ".sql"
	}
	return in
}

func TestGeneratedYDBErrorReturnFormatting(t *testing.T) {
	files, err := Generate(nativeErrorInput(), Options{Package: "db", Runtime: "ydb"})
	require.NoError(t, err)
	for _, file := range files {
		fset := token.NewFileSet()
		syntax, err := parser.ParseFile(fset, file.Name, file.Content, 0)
		require.NoError(t, err)
		ast.Inspect(syntax, func(node ast.Node) bool {
			block, ok := node.(*ast.BlockStmt)
			if !ok {
				return true
			}
			for i, statement := range block.List {
				ret, ok := statement.(*ast.ReturnStmt)
				if !ok || i == 0 {
					continue
				}
				previous := fset.Position(block.List[i-1].End())
				position := fset.Position(ret.Pos())
				assert.False(t, position.Line-previous.Line < 2, "%s: return following another statement needs a blank line", position)
			}
			return true
		})
	}
}

func TestGeneratedYDBErrorStackTraces(t *testing.T) {
	runGeneratedRuntimeTest(t, nativeErrorInput(), Options{Package: "db", Runtime: "ydb"}, nativeErrorRuntimeTest)
}

const nativeErrorRuntimeTest = `package db

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ydb-platform/ydb-go-sdk/v3/pkg/xerrors"
	"github.com/ydb-platform/ydb-go-sdk/v3/query"
	"github.com/ydb-platform/ydb-go-sdk/v3/types"
)

type testError struct{}

func (*testError) Error() string { return "database failure" }

type errorDB struct {
	DBTX
	err  error
	scan bool
}

func (db errorDB) Exec(context.Context, string, ...query.ExecuteOption) error {
	return db.err
}

func (db errorDB) Query(context.Context, string, ...query.ExecuteOption) (query.Result, error) {
	return nil, db.err
}

func (db errorDB) QueryRow(context.Context, string, ...query.ExecuteOption) (query.Row, error) {
	if db.scan {
		return errorRow{err: db.err}, nil
	}
	return nil, db.err
}

type errorRow struct {
	query.Row
	err error
}

func (row errorRow) ScanNamed(...query.NamedDestination) error {
	return row.err
}

func TestReturnedErrors(t *testing.T) {
	ctx := context.Background()
	cause := &testError{}
	for _, original := range []error{cause, xerrors.WithStackTrace(cause)} {
		for _, tc := range []struct {
			name   string
			scan   bool
			method string
			call   func(*testing.T, *Queries) error
		}{
			{"exec", false, "UpdateUser", func(t *testing.T, q *Queries) error {
				return q.UpdateUser(ctx, UpdateUserParams{Name: "name"})
			}},
			{"query row", false, "GetUser", func(t *testing.T, q *Queries) error {
				row, err := q.GetUser(ctx, 7)
				if row != (GetUserRow{}) {
					t.Fatalf("error returned a nonzero row: %#v", row)
				}
				return err
			}},
			{"scan", true, "GetUser", func(t *testing.T, q *Queries) error {
				row, err := q.GetUser(ctx, 7)
				if row != (GetUserRow{}) {
					t.Fatalf("error returned a nonzero row: %#v", row)
				}
				return err
			}},
			{"query", false, "ListUsers", func(t *testing.T, q *Queries) error {
				rows, err := q.ListUsers(ctx)
				if rows != nil {
					t.Fatalf("error returned a non-nil slice: %#v", rows)
				}
				return err
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				err := tc.call(t, New(errorDB{err: original, scan: tc.scan}))
				if !errors.Is(err, cause) {
					t.Fatalf("original error lost: %v", err)
				}
				var typed *testError
				if !errors.As(err, &typed) || typed != cause {
					t.Fatalf("error type lost: %v", err)
				}
				want := "(*Queries)." + tc.method + "(" + tc.method + ".sql.go:"
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("missing query return location %q: %v", want, err)
				}
			})
		}
	}
	if err := New(errorDB{}).UpdateUser(ctx, UpdateUserParams{Name: "name"}); err != nil {
		t.Fatalf("successful Exec returned an error: %v", err)
	}
}

func TestDecimalErrors(t *testing.T) {
	ctx := context.Background()
	q := New(nil)
	wrong := types.Decimal{Precision: 21, Scale: 9}
	for _, tc := range []struct {
		method string
		call   func(*testing.T) error
	}{
		{"Amountone", func(t *testing.T) error { _, err := q.Amountone(ctx, wrong); return err }},
		{"Amountmany", func(t *testing.T) error {
			rows, err := q.Amountmany(ctx, wrong)
			if rows != nil {
				t.Fatal("decimal error returned non-nil rows")
			}
			return err
		}},
		{"Amountexec", func(t *testing.T) error { return q.Amountexec(ctx, wrong) }},
	} {
		t.Run(tc.method, func(t *testing.T) {
			err := tc.call(t)
			if err == nil || !strings.Contains(err.Error(), "Decimal parameter $amount expects Decimal(22,9)") {
				t.Fatalf("decimal validation error lost: %v", err)
			}
			want := "(*Queries)." + tc.method + "(" + tc.method + ".sql.go:"
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("missing query return location %q: %v", want, err)
			}
			if !strings.Contains(err.Error(), "validateDecimalParameter(decimal.go:") {
				t.Fatalf("missing decimal helper return location: %v", err)
			}
		})
	}
}
`
