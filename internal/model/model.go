// Package model defines semantic analysis results shared by built-in generators.
// These types describe resolved queries and YQL types; they are not a syntax tree.
package model

import (
	"fmt"
	"strings"

	"github.com/antlr4-go/antlr/v4"
)

type Source struct {
	Name string
	Text string
}

type Position struct {
	File   string
	Line   int
	Column int
}

type Diagnostic struct {
	Position Position
	Message  string
}

func (d Diagnostic) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", d.Position.File, d.Position.Line, d.Position.Column, d.Message)
}

// Type preserves YQL type identity, including nested containers and nullability.
// Kind is the canonical YQL spelling (e.g. Uint64, Utf8, Optional, List).
type Type struct {
	Kind      string
	Elem      *Type
	Key       *Type
	Items     []Type
	Precision int
	Scale     int
}

func Optional(t Type) Type      { return Type{Kind: "Optional", Elem: &t} }
func (t Type) IsOptional() bool { return t.Kind == "Optional" }
func (t Type) UnwrapOptional() Type {
	if t.IsOptional() && t.Elem != nil {
		return *t.Elem
	}
	return t
}

// Equal compares type structure, including container elements and Decimal metadata.
func (t Type) Equal(other Type) bool {
	if t.Kind != other.Kind || t.Precision != other.Precision || t.Scale != other.Scale {
		return false
	}
	if (t.Elem == nil) != (other.Elem == nil) || (t.Key == nil) != (other.Key == nil) || len(t.Items) != len(other.Items) {
		return false
	}
	if t.Elem != nil && !t.Elem.Equal(*other.Elem) {
		return false
	}
	if t.Key != nil && !t.Key.Equal(*other.Key) {
		return false
	}
	for i := range t.Items {
		if !t.Items[i].Equal(other.Items[i]) {
			return false
		}
	}
	return true
}

// String returns the canonical YQL spelling of a type.
func (t Type) String() string {
	switch t.Kind {
	case "Optional", "List", "Stream", "Flow", "Set":
		if t.Elem != nil {
			return t.Kind + "<" + t.Elem.String() + ">"
		}
	case "Dict":
		if t.Key != nil && t.Elem != nil {
			return "Dict<" + t.Key.String() + "," + t.Elem.String() + ">"
		}
	case "Tuple":
		items := make([]string, len(t.Items))
		for i := range t.Items {
			items[i] = t.Items[i].String()
		}
		return "Tuple<" + strings.Join(items, ",") + ">"
	case "Decimal":
		return fmt.Sprintf("Decimal(%d,%d)", t.Precision, t.Scale)
	}
	return t.Kind
}

type Column struct {
	Name              string
	WireName          string // exact YDB result-set key when it differs from Name
	Type              Type
	Table             string
	SequenceGenerated bool `json:",omitempty"` // YDB allocates omitted values from the column's private sequence.
}

func (c Column) ResultName() string {
	if c.WireName != "" {
		return c.WireName
	}
	return c.Name
}

type Table struct {
	Name       string
	Columns    []Column
	PrimaryKey []string
}
type Catalog struct{ Tables []Table }
type Parameter struct {
	Name string
	Type Type
}
type ResultSet struct{ Columns []Column }

type Command string

const (
	One      Command = ":one"
	Many     Command = ":many"
	Exec     Command = ":exec"
	ExecRows Command = ":execrows"
)

// QuerySyntax retains the original ANTLR contexts with resolved column bindings.
// DSL renderers consume these contexts without reparsing SQL or constructing an AST.
type QuerySyntax struct {
	Root      antlr.ParserRuleContext
	Columns   map[int]ColumnBinding
	Relations []TableBinding
}
type TableBinding struct{ Table, Alias string }
type ColumnBinding struct {
	TableBinding
	Column Column
}

type AnalyzedQuery struct {
	Syntax *QuerySyntax `json:"-"`

	Name    string
	Command Command
	SQL     string
	// SQLWithoutDeclarations preserves the source except DECLARE tokens. SDKs that
	// synthesize declarations from typed parameters execute this form instead.
	SQLWithoutDeclarations string
	Parameters             []Parameter // names without the leading dollar sign
	ResultSets             []ResultSet
	Source                 Position
}

// QueryAnnotation returns the sqlc metadata comment associated with q.
func QueryAnnotation(q AnalyzedQuery) string {
	return "-- name: " + q.Name + " " + string(q.Command)
}

// WithoutQueryAnnotation removes a leading sqlc metadata comment from SQL.
// It preserves declarations, ordinary comments, and all remaining whitespace.
func WithoutQueryAnnotation(sql string) string {
	line, rest, found := strings.Cut(sql, "\n")
	if !found {
		return sql
	}
	if strings.HasPrefix(strings.TrimSpace(strings.TrimSuffix(line, "\r")), "-- name:") {
		return rest
	}
	return sql
}

type AnalysisResult struct {
	Catalog     Catalog
	Queries     []AnalyzedQuery
	Diagnostics []Diagnostic
}

// File is a generated file with a relative, generator-owned name.
type File struct {
	Name    string
	Content []byte
}
