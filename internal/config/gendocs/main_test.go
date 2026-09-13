package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandUpdatesOnlyOptionSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets.md")
	input := "# Handwritten introduction\n" + startMarker + "\nstale options\n" + endMarker + "\nHandwritten runtime contracts\n"
	if err := os.WriteFile(path, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if code := run([]string{"-file", path}, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("run: %d %s", code, stderr.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "# Handwritten introduction\n"+startMarker) || !strings.HasSuffix(string(data), endMarker+"\nHandwritten runtime contracts\n") || strings.Contains(string(data), "stale options") || !strings.Contains(string(data), "emit_json_tags") {
		t.Fatalf("incorrect reference update: %s", data)
	}
	if err := update(path); err != nil {
		t.Fatal(err)
	}
	again, err := os.ReadFile(path)
	if err != nil || string(again) != string(data) {
		t.Fatalf("update is not idempotent: %v", err)
	}
}

func TestCommandErrorsAndHelp(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.md")
	for _, tc := range []struct {
		args    []string
		code    int
		message string
	}{
		{[]string{"-help"}, 0, "Target reference to update"},
		{[]string{"-unknown"}, 1, "flag provided but not defined"},
		{[]string{"-file"}, 1, "flag needs an argument"},
		{[]string{"unexpected"}, 1, "does not accept positional arguments"},
		{[]string{"-file", missing}, 1, "missing.md"},
	} {
		var stderr bytes.Buffer
		if code := run(tc.args, &stderr); code != tc.code || !strings.Contains(stderr.String(), tc.message) {
			t.Errorf("run(%v): %d %q", tc.args, code, stderr.String())
		}
	}
}

func TestUpdateRejectsMissingOrMalformedReference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets.md")
	if err := update(path); !os.IsNotExist(err) {
		t.Fatalf("missing reference: %v", err)
	}
	const input = "Handwritten document without generator markers\n"
	if err := os.WriteFile(path, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	if err := update(path); err == nil {
		t.Fatal("accepted malformed reference")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != input {
		t.Fatalf("damaged reference after failed update: %s, %v", data, err)
	}
}

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
