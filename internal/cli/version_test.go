package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/update"
)

func TestVersionUpdateNotice(t *testing.T) {
	for _, tc := range []struct {
		name, version string
		args          []string
		notice        bool
	}{
		{"newer", "999.0.0", []string{"version"}, true},
		{"verbose", "999.0.0", []string{"version", "--verbose"}, true},
		{"same", Version, []string{"version"}, false},
		{"older", "0.0.0", []string{"version"}, false},
		{"offline flag", "999.0.0", []string{"version", "--no-remote"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.name == "offline flag" {
					t.Error("unexpected network check")
				}
				if r.URL.Path == "/latest" {
					http.Redirect(w, r, "/tag/v"+tc.version, http.StatusFound)
				}
			}))
			defer server.Close()
			client := update.NewClient()
			client.HTTP.Transport = server.Client().Transport
			client.ReleasesURL = server.URL
			var out, stderr bytes.Buffer
			code := run(tc.args, &out, &stderr, client)
			want := Version + "\n"
			if tc.name == "verbose" {
				want += "commit: " + Commit + "\n"
			}
			if tc.notice {
				want += fmt.Sprintf("New version available: %s. Run sqlc-ydb version --upgrade to install it.\n", tc.version)
			}
			if code != 0 || stderr.Len() != 0 || out.String() != want {
				t.Fatalf("%d %q %q", code, out.String(), stderr.String())
			}
		})
	}
}

func TestVersionUpgradeInstallsBinary(t *testing.T) {
	for _, outputFails := range []bool{false, true} {
		t.Run(fmt.Sprint("output failure=", outputFails), func(t *testing.T) {
			const version = "999.0.0"
			const binary = "replacement binary"
			base := "sqlc-ydb_" + version + "_" + runtime.GOOS + "_" + runtime.GOARCH
			var archive bytes.Buffer
			if runtime.GOOS == "windows" {
				z := zip.NewWriter(&archive)
				w, err := z.Create(base + "/sqlc-ydb.exe")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := io.WriteString(w, binary); err != nil {
					t.Fatal(err)
				}
				if err := z.Close(); err != nil {
					t.Fatal(err)
				}
				base += ".zip"
			} else {
				gz := gzip.NewWriter(&archive)
				tw := tar.NewWriter(gz)
				if err := tw.WriteHeader(&tar.Header{Name: base + "/sqlc-ydb", Mode: 0755, Size: int64(len(binary))}); err != nil {
					t.Fatal(err)
				}
				if _, err := io.WriteString(tw, binary); err != nil {
					t.Fatal(err)
				}
				if err := tw.Close(); err != nil {
					t.Fatal(err)
				}
				if err := gz.Close(); err != nil {
					t.Fatal(err)
				}
				base += ".tar.gz"
			}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/latest":
					http.Redirect(w, r, "/tag/v"+version, http.StatusFound)
				case "/tag/v" + version:
				case "/download/v" + version + "/SHA256SUMS":
					_, _ = fmt.Fprintf(w, "%x  %s\n", sha256.Sum256(archive.Bytes()), base)
				case "/download/v" + version + "/" + base:
					_, _ = w.Write(archive.Bytes())
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			target := filepath.Join(t.TempDir(), "sqlc-ydb")
			if err := os.WriteFile(target, []byte("old binary"), 0755); err != nil {
				t.Fatal(err)
			}
			client := update.NewClient()
			client.HTTP.Transport = server.Client().Transport
			client.ReleasesURL = server.URL
			client.Executable = func() (string, error) { return target, nil }
			var out, stderr bytes.Buffer
			var stdout io.Writer = &out
			if outputFails {
				stdout = brokenWriter{}
			}
			code := run([]string{"version", "--upgrade"}, stdout, &stderr, client)
			if outputFails {
				if code != 1 || !strings.Contains(stderr.String(), io.ErrClosedPipe.Error()) {
					t.Fatalf("%d %q", code, stderr.String())
				}
			} else {
				realTarget, err := filepath.EvalSymlinks(target)
				if err != nil {
					t.Fatal(err)
				}
				want := "Updated sqlc-ydb to " + version + ".\nLocation: " + realTarget + "\n"
				if code != 0 || stderr.Len() != 0 || out.String() != want {
					t.Fatalf("%d %q %q", code, out.String(), stderr.String())
				}
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != binary {
				t.Fatalf("installed %q: %v", data, err)
			}
		})
	}
}

func TestVersionNetworkFailureIsSilent(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
			defer server.Close()
			client := update.NewClient()
			client.HTTP.Transport = server.Client().Transport
			client.ReleasesURL = server.URL
			var out, stderr bytes.Buffer
			if code := run([]string{"version"}, &out, &stderr, client); code != 0 || out.String() != Version+"\n" || stderr.Len() != 0 {
				t.Fatalf("%d %q %q", code, out.String(), stderr.String())
			}
		})
	}
	// A refused connection has the same output contract as DNS/offline errors.
	if code, out, stderr := invoke("version"); code != 0 || out != Version+"\n" || stderr != "" {
		t.Fatalf("%d %q %q", code, out, stderr)
	}
}

func TestSelfUpdateCommand(t *testing.T) {
	if code, _, stderr := invoke("version", "--upgrade", "--verbose"); code != 1 || !strings.Contains(stderr, "--verbose cannot be combined with --upgrade") {
		t.Fatalf("%d %q", code, stderr)
	}
	if code, _, stderr := invoke("version", "--upgrade"); code != 1 || !strings.Contains(stderr, "check for updates") {
		t.Fatalf("%d %q", code, stderr)
	}
	for _, args := range [][]string{{"version", "--upgrade", "--no-remote"}, {"version", "--upgrade", "extra"}, {"version", "update"}, {"version", "--update"}, {"self-update"}, {"generate", "--upgrade"}, {"--upgrade"}} {
		if code, _, _ := invoke(args...); code != 1 {
			t.Fatalf("accepted %v", args)
		}
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest" {
			http.Redirect(w, r, "/tag/v"+Version, http.StatusFound)
		}
	}))
	defer server.Close()
	client := update.NewClient()
	client.HTTP.Transport = server.Client().Transport
	client.ReleasesURL = server.URL
	var out, stderr bytes.Buffer
	if code := run([]string{"version", "--upgrade"}, &out, &stderr, client); code != 0 || out.String() != "sqlc-ydb "+Version+" is already up to date.\n" || stderr.Len() != 0 {
		t.Fatalf("%d %q %q", code, out.String(), stderr.String())
	}
}
