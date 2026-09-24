package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandUpdatesOnlyOptionSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets.md")
	input := "# Handwritten introduction\n" + startMarker + "\nstale options\n" + endMarker + "\nHandwritten runtime contracts\n"
	require.NoError(t, os.WriteFile(path, []byte(input), 0600))
	var stderr bytes.Buffer
	code := run([]string{"-file", path}, &stderr)
	require.Zero(t, code, stderr.String())
	require.Empty(t, stderr.String())
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.False(t, !strings.HasPrefix(string(data), "# Handwritten introduction\n"+startMarker) || !strings.HasSuffix(string(data), endMarker+"\nHandwritten runtime contracts\n") || strings.Contains(string(data), "stale options") || !strings.Contains(string(data), "emit_json_tags"), "incorrect reference update: %s", data)
	require.NoError(t, update(path))
	again, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, data, again, "update is not idempotent")
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
		code := run(tc.args, &stderr)
		assert.Equal(t, tc.code, code, "run(%v): %q", tc.args, stderr.String())
		assert.Contains(t, stderr.String(), tc.message, "run(%v)", tc.args)
	}
}

func TestUpdateRejectsMissingOrMalformedReference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets.md")
	require.True(t, os.IsNotExist(update(path)), "missing reference")
	const input = "Handwritten document without generator markers\n"
	require.NoError(t, os.WriteFile(path, []byte(input), 0600))
	require.Error(t, update(path), "accepted malformed reference")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, input, string(data), "damaged reference after failed update")
}

func TestReferenceMatchesGeneratorOptions(t *testing.T) {
	data, err := os.ReadFile("../../../docs/targets.md")
	require.NoError(t, err)
	want, err := replaceSection(string(data))
	require.NoError(t, err)
	require.Equal(t, want, string(data), "generator option reference is stale; run go generate ./internal/config")
}

func TestInvalidMarkers(t *testing.T) {
	for _, text := range []string{"", startMarker, endMarker + startMarker, startMarker + startMarker + endMarker} {
		_, err := replaceSection(text)
		assert.Error(t, err, "accepted malformed section %q", text)
	}
}
