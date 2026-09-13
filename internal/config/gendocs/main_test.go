package main

import (
	"os"
	"testing"
)

func TestReferenceMatchesGeneratorOptions(t *testing.T) {
	data, err := os.ReadFile("../../../docs/targets.md")
	if err != nil {
		t.Fatal(err)
	}
	want, err := replaceSection(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatal("generator option reference is stale; run go generate ./internal/config")
	}
}

func TestInvalidMarkers(t *testing.T) {
	for _, text := range []string{"", startMarker, endMarker + startMarker, startMarker + startMarker + endMarker} {
		if _, err := replaceSection(text); err == nil {
			t.Errorf("accepted malformed section %q", text)
		}
	}
}
