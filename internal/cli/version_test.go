package cli

import (
	"archive/tar"
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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
				assert.NotEqual(t, "offline flag", tc.name, "unexpected network check")
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
			require.Zero(t, code, stderr.String())
			require.Empty(t, stderr.String())
			require.Equal(t, want, out.String())
		})
	}
}

func TestVersionUpgradeInstallsBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("manual upgrade instructions are tested separately")
	}
	for _, outputFails := range []bool{false, true} {
		t.Run(fmt.Sprint("output failure=", outputFails), func(t *testing.T) {
			const version = "999.0.0"
			const binary = "replacement binary"
			base := "sqlc-ydb_" + version + "_" + runtime.GOOS + "_" + runtime.GOARCH
			var archive bytes.Buffer
			gz := gzip.NewWriter(&archive)
			tw := tar.NewWriter(gz)
			require.NoError(t, tw.WriteHeader(&tar.Header{Name: base + "/sqlc-ydb", Mode: 0755, Size: int64(len(binary))}))
			_, err := io.WriteString(tw, binary)
			require.NoError(t, err)
			require.NoError(t, tw.Close())
			require.NoError(t, gz.Close())
			base += ".tar.gz"
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
			require.NoError(t, os.WriteFile(target, []byte("old binary"), 0755))
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
				require.Equal(t, 1, code)
				require.Contains(t, stderr.String(), io.ErrClosedPipe.Error())
			} else {
				realTarget, err := filepath.EvalSymlinks(target)
				require.NoError(t, err)
				want := "Updated sqlc-ydb to " + version + ".\nLocation: " + realTarget + "\n"
				require.Zero(t, code, stderr.String())
				require.Empty(t, stderr.String())
				require.Equal(t, want, out.String())
			}
			data, err := os.ReadFile(target)
			require.NoError(t, err)
			require.Equal(t, binary, string(data))
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
			code := run([]string{"version"}, &out, &stderr, client)
			require.Zero(t, code, stderr.String())
			require.Equal(t, Version+"\n", out.String())
			require.Empty(t, stderr.String())
		})
	}
	// A refused connection has the same output contract as DNS/offline errors.
	code, out, stderr := invoke("version")
	require.Zero(t, code, stderr)
	require.Equal(t, Version+"\n", out)
	require.Empty(t, stderr)
}

func TestSelfUpdateCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("manual upgrade instructions are tested separately")
	}
	code, _, message := invoke("version", "--upgrade", "--verbose")
	require.Equal(t, 1, code, message)
	require.Contains(t, message, "--verbose cannot be combined with --upgrade")
	code, _, message = invoke("version", "--upgrade")
	require.Equal(t, 1, code, message)
	require.Contains(t, message, "check for updates")
	for _, args := range [][]string{{"version", "--upgrade", "--no-remote"}, {"version", "--upgrade", "extra"}, {"version", "update"}, {"version", "--update"}, {"self-update"}, {"generate", "--upgrade"}, {"--upgrade"}} {
		code, _, _ := invoke(args...)
		require.Equal(t, 1, code, "accepted %v", args)
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
	code = run([]string{"version", "--upgrade"}, &out, &stderr, client)
	require.Zero(t, code, stderr.String())
	require.Equal(t, "sqlc-ydb "+Version+" is already up to date.\n", out.String())
	require.Empty(t, stderr.String())
}
