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
