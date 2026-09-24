package model

import "testing"

func TestWithoutQueryAnnotation(t *testing.T) {
	for _, test := range []struct {
		name string
		in   string
		want string
	}{
		{"annotation", "-- name: GetAuthor :one\nSELECT 1;", "SELECT 1;"},
		{"windows annotation", "-- name: GetAuthor :one\r\nSELECT 1;", "SELECT 1;"},
		{"uppercase annotation", "-- NAME: GetAuthor :one\nSELECT 1;", "SELECT 1;"},
		{"annotation without space", "--name: GetAuthor :one\nSELECT 1;", "SELECT 1;"},
		{"sqlc annotation", "-- sqlc -- name: GetAuthor :one\nSELECT 1;", "SELECT 1;"},
		{"sqlc annotation without separator", "-- SQLC name: GetAuthor :one\nSELECT 1;", "SELECT 1;"},
		{"ordinary comment", "-- explain\nSELECT 1;", "-- explain\nSELECT 1;"},
		{"non-comment", "name: GetAuthor :one\nSELECT 1;", "name: GetAuthor :one\nSELECT 1;"},
		{"annotation not first", "\n-- name: GetAuthor :one\nSELECT 1;", "\n-- name: GetAuthor :one\nSELECT 1;"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := WithoutQueryAnnotation(test.in); got != test.want {
				t.Fatalf("WithoutQueryAnnotation() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestQueryAnnotation(t *testing.T) {
	q := AnalyzedQuery{Name: "GetAuthor", Command: One}
	if got, want := QueryAnnotation(q), "-- name: GetAuthor :one"; got != want {
		t.Fatalf("QueryAnnotation() = %q, want %q", got, want)
	}
}

func TestStructTypeIdentity(t *testing.T) {
	a := Type{Kind: "Struct", Fields: []StructField{{Name: "id", Type: Type{Kind: "Uint64"}}, {Name: "data", Type: Optional(Type{Kind: "Json"})}}}
	b := Type{Kind: "Struct", Fields: []StructField{{Name: "id", Type: Type{Kind: "Uint64"}}, {Name: "data", Type: Optional(Type{Kind: "Json"})}}}
	if !a.Equal(b) || a.String() != "Struct<`id`:Uint64,`data`:Optional<Json>>" {
		t.Fatalf("identity: %s", a.String())
	}
	b.Fields[0].Name = "other"
	if a.Equal(b) {
		t.Fatal("field names are part of type identity")
	}
	b.Fields[0].Name = "id"
	b.Fields[0].Type.Kind = "Int64"
	if a.Equal(b) {
		t.Fatal("field types are part of type identity")
	}
}

func TestStructTypeIdentityIgnoresDeclarationOrder(t *testing.T) {
	a := Type{Kind: "Struct", Fields: []StructField{{Name: "id", Type: Type{Kind: "Uint64"}}, {Name: "data", Type: Optional(Type{Kind: "Json"})}}}
	b := Type{Kind: "Struct", Fields: []StructField{a.Fields[1], a.Fields[0]}}
	if !a.Equal(b) || !b.Equal(a) {
		t.Fatal("Struct fields have name-based type identity")
	}
	if a.Fields[0].Name != "id" || b.Fields[0].Name != "data" {
		t.Fatal("type comparison reordered declarations")
	}
}

func TestStructIdentityIncludesAllFields(t *testing.T) {
	short := Type{Kind: "Struct", Fields: []StructField{{Name: "id", Type: Type{Kind: "Uint64"}}}}
	long := Type{Kind: "Struct", Fields: append(append([]StructField{}, short.Fields...), StructField{Name: "data", Type: Type{Kind: "Json"}})}
	if short.Equal(long) || long.Equal(short) {
		t.Fatal("different field counts must not compare equal")
	}
}

func TestUDFIntermediateTypeIdentity(t *testing.T) {
	stringType := Type{Kind: "String"}
	boolType := Type{Kind: "Bool"}
	tagged := Type{Kind: "Tagged", Elem: &stringType, Tag: "FloatVector"}
	if tagged.String() != "Tagged<String,\"FloatVector\">" || tagged.Equal(Type{Kind: "Tagged", Elem: &stringType, Tag: "BitVector"}) {
		t.Fatalf("tagged identity: %s", tagged.String())
	}
	callable := Type{Kind: "Callable", Items: []Type{Optional(stringType)}, Elem: &boolType}
	if callable.String() != "Callable<(Optional<String>)->Bool>" || callable.Equal(Type{Kind: "Callable", Items: []Type{stringType}, Elem: &boolType}) {
		t.Fatalf("callable identity: %s", callable.String())
	}
}
