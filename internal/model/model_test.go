package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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
			require.Equal(t, test.want, WithoutQueryAnnotation(test.in))
		})
	}
}

func TestQueryAnnotation(t *testing.T) {
	q := AnalyzedQuery{Name: "GetAuthor", Command: One}
	require.Equal(t, "-- name: GetAuthor :one", QueryAnnotation(q))
}

func TestStructTypeIdentity(t *testing.T) {
	a := Type{Kind: "Struct", Fields: []StructField{{Name: "id", Type: Type{Kind: "Uint64"}}, {Name: "data", Type: Optional(Type{Kind: "Json"})}}}
	b := Type{Kind: "Struct", Fields: []StructField{{Name: "id", Type: Type{Kind: "Uint64"}}, {Name: "data", Type: Optional(Type{Kind: "Json"})}}}
	require.True(t, a.Equal(b))
	require.Equal(t, "Struct<`id`:Uint64,`data`:Optional<Json>>", a.String())
	b.Fields[0].Name = "other"
	require.False(t, a.Equal(b), "field names are part of type identity")
	b.Fields[0].Name = "id"
	b.Fields[0].Type.Kind = "Int64"
	require.False(t, a.Equal(b), "field types are part of type identity")
}

func TestStructTypeIdentityIgnoresDeclarationOrder(t *testing.T) {
	a := Type{Kind: "Struct", Fields: []StructField{{Name: "id", Type: Type{Kind: "Uint64"}}, {Name: "data", Type: Optional(Type{Kind: "Json"})}}}
	b := Type{Kind: "Struct", Fields: []StructField{a.Fields[1], a.Fields[0]}}
	require.True(t, a.Equal(b), "Struct fields have name-based type identity")
	require.True(t, b.Equal(a), "Struct fields have name-based type identity")
	require.Equal(t, "id", a.Fields[0].Name, "type comparison reordered declarations")
	require.Equal(t, "data", b.Fields[0].Name, "type comparison reordered declarations")
}

func TestStructIdentityIncludesAllFields(t *testing.T) {
	short := Type{Kind: "Struct", Fields: []StructField{{Name: "id", Type: Type{Kind: "Uint64"}}}}
	long := Type{Kind: "Struct", Fields: append(append([]StructField{}, short.Fields...), StructField{Name: "data", Type: Type{Kind: "Json"}})}
	require.False(t, short.Equal(long) || long.Equal(short), "different field counts must not compare equal")
}

func TestUDFIntermediateTypeIdentity(t *testing.T) {
	stringType := Type{Kind: "String"}
	boolType := Type{Kind: "Bool"}
	tagged := Type{Kind: "Tagged", Elem: &stringType, Tag: "FloatVector"}
	require.Equal(t, "Tagged<String,\"FloatVector\">", tagged.String())
	require.False(t, tagged.Equal(Type{Kind: "Tagged", Elem: &stringType, Tag: "BitVector"}))
	callable := Type{Kind: "Callable", Items: []Type{Optional(stringType)}, Elem: &boolType}
	require.Equal(t, "Callable<(Optional<String>)->Bool>", callable.String())
	require.False(t, callable.Equal(Type{Kind: "Callable", Items: []Type{stringType}, Elem: &boolType}))
}
