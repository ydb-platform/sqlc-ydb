package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewer(t *testing.T) {
	for _, tc := range []struct {
		latest, current string
		want            bool
	}{
		{"0.2.0", "0.1.0", true}, {"0.10.0", "0.9.0", true},
		{"2.0.0", "1.99.99", true}, {"1.0.1", "1.0.0", true},
		{"v1.0.0", "1.0.0-rc10", true}, {"1.0.0-rc10", "1.0.0-rc9", true},
		{"1.0.0", "1.1.0-rc1", false}, {"1.0.0-rc1", "1.0.0", false},
		{"1.0.0", "1.0.0", false}, {"1.0.0", "dev", false},
		{"bad", "1.0.0", false}, {"1.01.0", "1.0.0", false},
		{"1.0.0-rc2", "1.0.0-rc2", false}, {"1.0.0", "2.0.0", false},
	} {
		assert.Equal(t, tc.want, Newer(tc.latest, tc.current), "Newer(%q, %q)", tc.latest, tc.current)
	}
}

type entry struct {
	name, body string
	symlink    bool
}

func archive(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	w := tar.NewWriter(gz)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: 0755, Size: int64(len(e.body)), Typeflag: tar.TypeReg}
		if e.symlink {
			h.Typeflag = tar.TypeSymlink
			h.Size = 0
			h.Linkname = "elsewhere"
		}
		require.NoError(t, w.WriteHeader(h))
		if !e.symlink {
			_, err := io.WriteString(w, e.body)
			require.NoError(t, err)
		}
	}
	require.NoError(t, w.Close())
	require.NoError(t, gz.Close())
	return out.Bytes()
}

func TestExtraction(t *testing.T) {
	{
		for _, tc := range []struct {
			name    string
			entries []entry
			valid   bool
		}{
			{"regular", []entry{{name: "base/sqlc-ydb", body: "binary"}}, true},
			{"ignore-other-paths", []entry{{name: "../../outside", body: "bad"}, {name: "base/sqlc-ydb", body: "binary"}}, true},
			{"missing", []entry{{name: "other", body: "bad"}}, false},
			{"empty", []entry{{name: "base/sqlc-ydb"}}, false},
			{"duplicate", []entry{{name: "base/sqlc-ydb", body: "one"}, {name: "base/sqlc-ydb", body: "two"}}, false},
			{"symlink", []entry{{name: "base/sqlc-ydb", symlink: true}}, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				var out bytes.Buffer
				err := extract(&out, archive(t, tc.entries...), "base/sqlc-ydb")
				require.Equal(t, tc.valid, (err == nil), "extract: %v", err)
				require.False(t, tc.valid && out.String() != "binary", "unexpected bytes: %q", out.String())
			})
		}
		require.Error(t, extract(io.Discard, []byte("broken"), "base/sqlc-ydb"), "accepted broken archive")
	}
}

func TestChecksums(t *testing.T) {
	valid := strings.Repeat("ab", 32) + "  artifact\n"
	for _, sums := range []string{"", "bad  artifact\n", valid + valid, strings.Repeat("a", 63) + " artifact\n"} {
		_, err := checksum([]byte(sums), "artifact")
		require.Error(t, err, "accepted %q", sums)
	}
	got, err := checksum([]byte(valid+"other garbage\n"), "artifact")
	require.NoError(t, err)
	require.Len(t, got, 32)
}

func TestConcurrentInstallation(t *testing.T) {
	target := writeTarget(t)
	f, err := os.Open(target)
	require.NoError(t, err)
	original, err := f.Stat()
	_ = f.Close()
	require.NoError(t, err)
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := range 2 {
		staged := filepath.Join(filepath.Dir(target), fmt.Sprint("staged", i))
		require.NoError(t, os.WriteFile(staged, []byte("new binary"), 0755))
		go func() { <-start; results <- install(staged, target, original) }()
	}
	close(start)
	successes := 0
	for range 2 {
		if <-results == nil {
			successes++
		}
	}
	require.Equal(t, 1, successes, "installed %d concurrent updates; want exactly one", successes)
	_, err = os.Stat(target + ".update-lock")
	require.ErrorIs(t, err, os.ErrNotExist, "lock not released")
}

func TestInstallationLockPreservesFiles(t *testing.T) {
	target := writeTarget(t)
	info, err := os.Stat(target)
	require.NoError(t, err)
	lock := target + ".update-lock"
	require.NoError(t, os.Mkdir(lock, 0700))
	require.ErrorContains(t, install("unused-stage", target, info), "update lock")
	data, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "old executable", string(data))
	_, err = os.Stat(lock)
	require.NoError(t, err, "removed another updater's lock")
}

type failingTransport struct {
	base   http.RoundTripper
	suffix string
}

func (f failingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if strings.HasSuffix(r.URL.Path, f.suffix) {
		return nil, io.ErrUnexpectedEOF
	}
	return f.base.RoundTrip(r)
}

func TestDownloadFailuresKeepInstallation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("automatic upgrades are not supported on Windows")
	}
	_, _, extension, err := artifact("0.2.0", runtime.GOOS, runtime.GOARCH)
	require.NoError(t, err)
	for _, suffix := range []string{"SHA256SUMS", extension} {
		t.Run(suffix, func(t *testing.T) {
			target := writeTarget(t)
			client := fixture(t, target, nil)
			client.HTTP.Transport = failingTransport{client.HTTP.Transport, suffix}
			_, err := client.Update(context.Background(), "0.1.0")
			require.ErrorIs(t, err, io.ErrUnexpectedEOF)
			data, err := os.ReadFile(target)
			require.NoError(t, err)
			require.Equal(t, "old executable", string(data))
			staged, err := filepath.Glob(filepath.Join(filepath.Dir(target), ".sqlc-ydb-update-*"))
			require.NoError(t, err)
			require.Empty(t, staged, "staging files leaked")
		})
	}
}

func TestRejectInvalidReleaseURLAndRedirectLoop(t *testing.T) {
	client := NewClient()
	_, err := client.get(context.Background(), "https://invalid\x00host")
	require.Error(t, err, "accepted invalid URL")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/latest", http.StatusFound)
	}))
	defer server.Close()
	client.HTTP.Transport = server.Client().Transport
	client.ReleasesURL = server.URL
	_, err = client.Latest(context.Background())
	require.ErrorContains(t, err, "excessive release redirect")
}

func TestMalformedTarBody(t *testing.T) {
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	_, err := gz.Write([]byte("truncated tar header"))
	require.NoError(t, err)
	require.NoError(t, gz.Close())
	require.Error(t, extract(io.Discard, data.Bytes(), "sqlc-ydb"), "accepted invalid tar body")
}

func TestArtifactMatrix(t *testing.T) {
	for _, osName := range []string{"linux", "darwin"} {
		for _, arch := range []string{"amd64", "arm64"} {
			base, bin, ext, err := artifact("1.2.3", osName, arch)
			require.NoError(t, err)
			require.Equal(t, "sqlc-ydb_1.2.3_"+osName+"_"+arch, base)

			require.Equal(t, "sqlc-ydb", bin)
			require.Equal(t, ".tar.gz", ext)
		}
	}
	for _, pair := range [][2]string{{"linux", "386"}, {"freebsd", "amd64"}, {"windows", "amd64"}} {
		_, _, _, err := artifact("1.2.3", pair[0], pair[1])
		require.Error(t, err, "unsupported platform")
	}
}

func fixture(t *testing.T, target string, mutate func(string, []byte) []byte) *Client {
	t.Helper()
	base, bin, ext, err := artifact("0.2.0", runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skip(err)
	}
	data := archive(t, entry{name: base + "/" + bin, body: "new executable"})
	sums := fmt.Sprintf("%x  %s\n", sha256.Sum256(data), base+ext)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			http.Redirect(w, r, "/tag/v0.2.0", http.StatusFound)
		case "/tag/v0.2.0":
			w.WriteHeader(http.StatusOK)
		default:
			body := data
			if strings.HasSuffix(r.URL.Path, "/SHA256SUMS") {
				body = []byte(sums)
			}
			if mutate != nil {
				body = mutate(r.URL.Path, body)
			}
			_, _ = w.Write(body)
		}
	}))
	t.Cleanup(server.Close)
	c := NewClient()
	c.HTTP.Transport = server.Client().Transport
	c.ReleasesURL = server.URL
	c.Executable = func() (string, error) { return target, nil }
	return c
}

func writeTarget(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "binary with spaces")
	require.NoError(t, os.WriteFile(p, []byte("old executable"), 0751))
	return p
}

func TestUpdate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("automatic upgrades are not supported on Windows")
	}
	for _, symlink := range []bool{false, true} {
		t.Run(fmt.Sprint("symlink=", symlink), func(t *testing.T) {
			target := writeTarget(t)
			path := target
			if symlink {
				path = filepath.Join(filepath.Dir(target), "link")
				if err := os.Symlink(filepath.Base(target), path); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			c := fixture(t, path, nil)
			result, err := c.Update(context.Background(), "0.1.0")
			require.NoError(t, err)
			realTarget, err := filepath.EvalSymlinks(target)
			require.NoError(t, err)
			require.True(t, result.Updated)
			require.Equal(t, realTarget, result.Path)
			require.Equal(t, "0.2.0", result.Version)
			data, err := os.ReadFile(target)
			require.NoError(t, err)
			require.Equal(t, "new executable", string(data))
			if symlink {
				dest, err := os.Readlink(path)
				require.NoError(t, err)
				require.Equal(t, filepath.Base(target), dest)
			}
			info, err := os.Stat(target)
			require.NoError(t, err)
			require.False(t, runtime.GOOS != "windows" && info.Mode().Perm() != 0751, info.Mode())
		})
	}
}

func TestUpdateFailuresPreserveExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("automatic upgrades are not supported on Windows")
	}
	for _, failure := range []string{"checksum", "download", "archive", "changed", "executable", "symlink", "directory", "stage"} {
		t.Run(failure, func(t *testing.T) {
			target := writeTarget(t)
			c := fixture(t, target, func(path string, data []byte) []byte {
				if failure == "changed" && strings.HasSuffix(path, "SHA256SUMS") {
					assert.NoError(t, os.Rename(target, target+".saved"))
					assert.NoError(t, os.WriteFile(target, []byte("other installation"), 0755))
				}
				if failure == "checksum" && strings.HasSuffix(path, "SHA256SUMS") {
					return []byte("missing")
				}
				if failure == "download" && !strings.HasSuffix(path, "SHA256SUMS") {
					return []byte("corrupt")
				}
				if failure == "archive" {
					if strings.HasSuffix(path, "SHA256SUMS") {
						fields := strings.Fields(string(data))
						return fmt.Appendf(nil, "%x  %s\n", sha256.Sum256([]byte("not archive")), fields[1])
					}
					return []byte("not archive")
				}
				return data
			})
			switch failure {
			case "executable":
				c.Executable = func() (string, error) { return "", errors.New("no executable") }
			case "symlink":
				c.Executable = func() (string, error) { return target + "missing", nil }
			case "directory":
				c.Executable = func() (string, error) { return filepath.Dir(target), nil }
			case "stage":
				if runtime.GOOS == "windows" || os.Geteuid() == 0 {
					t.Skip("requires Unix directory permissions")
				}
				dir := filepath.Dir(target)
				require.NoError(t, os.Chmod(dir, 0555))
				t.Cleanup(func() { _ = os.Chmod(dir, 0755) })
			}
			_, err := c.Update(context.Background(), "0.1.0")
			require.Error(t, err)
			data, err := os.ReadFile(target)
			want := "old executable"
			if failure == "changed" {
				want = "other installation"
			}
			require.NoError(t, err)
			require.Equal(t, want, string(data))
			files, err := filepath.Glob(filepath.Join(filepath.Dir(target), ".sqlc-ydb-update-*"))
			require.NoError(t, err)
			require.Empty(t, files)
		})
	}
}

func TestNoDowngradeOrUnnecessaryInstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("automatic upgrades are not supported on Windows")
	}
	for _, version := range []string{"0.2.0", "0.3.0", "0.3.0-rc1"} {
		c := fixture(t, "", func(string, []byte) []byte { assert.Fail(t, "downloaded an unnecessary update"); return nil })
		c.Executable = func() (string, error) { assert.Fail(t, "looked up executable unnecessarily"); return "", nil }
		result, err := c.Update(context.Background(), version)
		require.NoError(t, err)
		require.False(t, result.Updated)
		require.Equal(t, version, result.Version)
	}
}

func TestLatestAndNetworkErrors(t *testing.T) {
	for _, location := range []string{"/tag/v0.2.0", "/tag/v0.2.0-rc1", "/tag/garbage", "/tag/0.2.0", "/other/v0.2.0", "http://example.invalid"} {
		t.Run(location, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/latest" {
					http.Redirect(w, r, location, http.StatusFound)
				}
			}))
			defer server.Close()
			c := NewClient()
			c.HTTP.Transport = server.Client().Transport
			c.ReleasesURL = server.URL
			version, err := c.Latest(context.Background())
			if location == "/tag/v0.2.0" {
				require.NoError(t, err)
				require.Equal(t, "0.2.0", version)
			} else {
				require.Error(t, err, "accepted invalid release")
			}
		})
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/wait/latest" {
			<-r.Context().Done()
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	c := NewClient()
	c.HTTP.Transport = server.Client().Transport
	c.ReleasesURL = server.URL
	_, err := c.Latest(context.Background())
	require.Error(t, err, "accepted HTTP 503")
	c.ReleasesURL = server.URL + "/wait"
	start := time.Now()
	_, err = c.Latest(context.Background())
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.False(t, time.Since(start) > CheckTimeout+time.Second, "check exceeded deadline")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.Update(ctx, "0.1.0")
	if runtime.GOOS != "windows" {
		require.ErrorIs(t, err, context.Canceled)
	}
	c.ReleasesURL = "http://example.invalid"
	_, err = c.Latest(context.Background())
	require.Error(t, err, "accepted HTTP")
}

func TestDownloadSizeLimit(t *testing.T) {
	c := fixture(t, "", nil)
	_, err := c.download(context.Background(), c.ReleasesURL+"/data", 1)
	require.Error(t, err, "accepted oversized download")
}

// Exercise replacement while the target executable is actually running,
// on supported platforms. The child uses a trusted local TLS fixture.
func TestRunningExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("automatic upgrades are not supported on Windows")
	}
	if url := os.Getenv("SQLC_UPDATE_TEST_SERVER"); url != "" {
		c := NewClient()
		c.ReleasesURL = url
		pool := httptest.NewTLSServer(http.NotFoundHandler())
		c.HTTP.Transport = pool.Client().Transport
		pool.Close()
		_, err := c.Update(context.Background(), "0.1.0")
		require.NoError(t, err)
		return
	}
	source, err := os.Executable()
	require.NoError(t, err)
	data, err := os.ReadFile(source)
	require.NoError(t, err)
	target := filepath.Join(t.TempDir(), "running.exe")
	require.NoError(t, os.WriteFile(target, data, 0755))
	c := fixture(t, target, nil)
	command := exec.Command(target, "-test.run=^TestRunningExecutable$")
	command.Env = append(os.Environ(), "SQLC_UPDATE_TEST_SERVER="+c.ReleasesURL)
	out, err := command.CombinedOutput()
	require.NoError(t, err, string(out))
	got, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "new executable", string(got), "update not installed")
}
