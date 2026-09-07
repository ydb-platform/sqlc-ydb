// Package model defines semantic analysis results shared by built-in generators.
// These types describe resolved queries and YQL types; they are not a syntax tree.
package model

import "fmt"

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
// Kind is the canonical YQL spelling (e.g. Uint64, Utf8, Optional, List, Struct).
type Type struct {
	Kind      string
	Elem      *Type
	Key       *Type
	Fields    []Field
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

type Field struct {
	Name string
	Type Type
}
type Column struct {
	Name  string
	Type  Type
	Table string
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

type AnalyzedQuery struct {
	Name       string
	Command    Command
	SQL        string
	Parameters []Parameter // names without the leading dollar sign
	ResultSets []ResultSet
	Source     Position
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
