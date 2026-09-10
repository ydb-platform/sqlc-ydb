package endtoend

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/cli"
)

var update = flag.Bool("update", false, "update end-to-end expected output")

var outputRoots = []string{"db", "py", "cpp", "cs", "java", "typescript", "rust", "php"}

func TestGolden(t *testing.T) {
	fixtures, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range fixtures {
		if !entry.IsDir() {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) { run(t, filepath.Join("testdata", entry.Name())) })
	}
}

func run(t *testing.T, fixture string) {
	t.Helper()
	dir := t.TempDir()
	copyFixture(t, fixture, dir)
	var out, stderr bytes.Buffer
	code := cli.Run([]string{"generate", "-f", filepath.Join(dir, "sqlc.yaml")}, &out, &stderr)
	wantErr, err := os.ReadFile(filepath.Join(fixture, "stderr.txt"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	negative := err == nil
	if out.Len() != 0 {
		t.Fatalf("generate wrote unexpected stdout: %s", out.String())
	}
	if negative {
		if code == 0 {
			t.Fatal("expected generate to fail")
		}
		got := normalize(stderr.String(), dir)
		if *update {
			write(t, filepath.Join(fixture, "stderr.txt"), []byte(got))
			return
		}
		if got != string(wantErr) {
			t.Fatalf("stderr mismatch\nwant: %s\ngot:  %s", wantErr, got)
		}
		return
	}
	if code != 0 {
		t.Fatalf("generate failed: %s", normalize(stderr.String(), dir))
	}
	if *update {
		updateExpected(t, fixture, dir)
		return
	}
	want := files(t, filepath.Join(fixture, "expected"))
	got := generated(t, dir, want)
	if diff := compare(want, got); diff != "" {
		t.Fatal(diff)
	}
}

func copyFixture(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == "expected" || e.Name() == "stderr.txt" {
			continue
		}
		from, to := filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := os.CopyFS(to, os.DirFS(from)); err != nil {
				t.Fatal(err)
			}
			continue
		}
		data, err := os.ReadFile(from)
		if err != nil {
			t.Fatal(err)
		}
		write(t, to, data)
	}
}

func updateExpected(t *testing.T, fixture, dir string) {
	t.Helper()
	expected := filepath.Join(fixture, "expected")
	if err := os.RemoveAll(expected); err != nil {
		t.Fatal(err)
	}
	for _, root := range outputRoots {
		from := filepath.Join(dir, root)
		if _, err := os.Stat(from); os.IsNotExist(err) {
			continue
		} else if err != nil {
			t.Fatal(err)
		}
		if err := os.CopyFS(filepath.Join(expected, root), os.DirFS(from)); err != nil {
			t.Fatal(err)
		}
	}
}

func files(t *testing.T, root string) map[string][]byte {
	t.Helper()
	result := map[string][]byte{}
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		if info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(root, path)
		result[filepath.ToSlash(rel)] = data
		return nil
	})
	return result
}
func generated(t *testing.T, root string, want map[string][]byte) map[string][]byte {
	t.Helper()
	got := map[string][]byte{}
	for rel := range want {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatal(err)
		}
		got[rel] = data
	}
	for _, d := range outputRoots {
		base := filepath.Join(root, d)
		if _, err := os.Stat(base); os.IsNotExist(err) {
			continue
		}
		actual := files(t, base)
		for rel, data := range actual {
			key := filepath.ToSlash(filepath.Join(d, rel))
			got[key] = data
		}
	}
	return got
}
func compare(want, got map[string][]byte) string {
	keys := map[string]bool{}
	for k := range want {
		keys[k] = true
	}
	for k := range got {
		keys[k] = true
	}
	var all []string
	for k := range keys {
		all = append(all, k)
	}
	sort.Strings(all)
	var b strings.Builder
	for _, k := range all {
		w, wok := want[k]
		g, gok := got[k]
		if !wok {
			fmt.Fprintf(&b, "unexpected generated file %s\n", k)
			continue
		}
		if !gok {
			fmt.Fprintf(&b, "missing generated file %s\n", k)
			continue
		}
		if !bytes.Equal(w, g) {
			fmt.Fprintf(&b, "generated contents differ: %s\n", k)
		}
	}
	return b.String()
}
func normalize(s, dir string) string {
	return strings.ReplaceAll(s, filepath.ToSlash(dir), "<fixture>")
}
func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}
