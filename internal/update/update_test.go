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
		if got := Newer(tc.latest, tc.current); got != tc.want {
			t.Errorf("Newer(%q, %q) = %v", tc.latest, tc.current, got)
		}
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
		if err := w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if !e.symlink {
			if _, err := io.WriteString(w, e.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
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
				if (err == nil) != tc.valid {
					t.Fatalf("extract: %v", err)
				}
				if tc.valid && out.String() != "binary" {
					t.Fatalf("unexpected bytes: %q", out.String())
				}
			})
		}
		if err := extract(io.Discard, []byte("broken"), "base/sqlc-ydb"); err == nil {
			t.Fatal("accepted broken archive")
		}
	}
}

func TestChecksums(t *testing.T) {
	valid := strings.Repeat("ab", 32) + "  artifact\n"
	for _, sums := range []string{"", "bad  artifact\n", valid + valid, strings.Repeat("a", 63) + " artifact\n"} {
		if _, err := checksum([]byte(sums), "artifact"); err == nil {
			t.Fatalf("accepted %q", sums)
		}
	}
	if got, err := checksum([]byte(valid+"other garbage\n"), "artifact"); err != nil || len(got) != 32 {
		t.Fatalf("%x %v", got, err)
	}
}

func TestConcurrentInstallation(t *testing.T) {
	target := writeTarget(t)
	f, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	original, err := f.Stat()
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := range 2 {
		staged := filepath.Join(filepath.Dir(target), fmt.Sprint("staged", i))
		if err := os.WriteFile(staged, []byte("new binary"), 0755); err != nil {
			t.Fatal(err)
		}
		go func() { <-start; results <- install(staged, target, original) }()
	}
	close(start)
	successes := 0
	for range 2 {
		if <-results == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("installed %d concurrent updates; want exactly one", successes)
	}
	if _, err := os.Stat(target + ".update-lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock not released: %v", err)
	}
}

func TestInstallationLockPreservesFiles(t *testing.T) {
	target := writeTarget(t)
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	lock := target + ".update-lock"
	if err := os.Mkdir(lock, 0700); err != nil {
		t.Fatal(err)
	}
	if err := install("unused-stage", target, info); err == nil || !strings.Contains(err.Error(), "update lock") {
		t.Fatalf("%v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "old executable" {
		t.Fatalf("%q %v", data, err)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("removed another updater's lock: %v", err)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"SHA256SUMS", extension} {
		t.Run(suffix, func(t *testing.T) {
			target := writeTarget(t)
			client := fixture(t, target, nil)
			client.HTTP.Transport = failingTransport{client.HTTP.Transport, suffix}
			if _, err := client.Update(context.Background(), "0.1.0"); !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("unexpected failure: %v", err)
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "old executable" {
				t.Fatalf("%q %v", data, err)
			}
			staged, err := filepath.Glob(filepath.Join(filepath.Dir(target), ".sqlc-ydb-update-*"))
			if err != nil || len(staged) != 0 {
				t.Fatalf("staging files leaked: %v %v", staged, err)
			}
		})
	}
}

func TestRejectInvalidReleaseURLAndRedirectLoop(t *testing.T) {
	client := NewClient()
	if _, err := client.get(context.Background(), "https://invalid\x00host"); err == nil {
		t.Fatal("accepted invalid URL")
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/latest", http.StatusFound)
	}))
	defer server.Close()
	client.HTTP.Transport = server.Client().Transport
	client.ReleasesURL = server.URL
	if _, err := client.Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "excessive release redirect") {
		t.Fatalf("%v", err)
	}
}

func TestMalformedTarBody(t *testing.T) {
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	if _, err := gz.Write([]byte("truncated tar header")); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := extract(io.Discard, data.Bytes(), "sqlc-ydb"); err == nil {
		t.Fatal("accepted invalid tar body")
	}
}

func TestArtifactMatrix(t *testing.T) {
	for _, osName := range []string{"linux", "darwin"} {
		for _, arch := range []string{"amd64", "arm64"} {
			base, bin, ext, err := artifact("1.2.3", osName, arch)
			if err != nil || base != "sqlc-ydb_1.2.3_"+osName+"_"+arch {
				t.Fatal(base, err)
			}

			if bin != "sqlc-ydb" || ext != ".tar.gz" {
				t.Fatal(bin, ext)
			}
		}
	}
	for _, pair := range [][2]string{{"linux", "386"}, {"freebsd", "amd64"}, {"windows", "amd64"}} {
		if _, _, _, err := artifact("1.2.3", pair[0], pair[1]); err == nil {
			t.Fatal("unsupported platform")
		}
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
	if err := os.WriteFile(p, []byte("old executable"), 0751); err != nil {
		t.Fatal(err)
	}
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
			realTarget, resolveErr := filepath.EvalSymlinks(target)
			if err != nil || resolveErr != nil || !result.Updated || result.Path != realTarget || result.Version != "0.2.0" {
				t.Fatalf("%+v %v", result, err)
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "new executable" {
				t.Fatalf("%q %v", data, err)
			}
			if symlink {
				if dest, err := os.Readlink(path); err != nil || dest != filepath.Base(target) {
					t.Fatal(dest, err)
				}
			}
			info, err := os.Stat(target)
			if err != nil {
				t.Fatal(err)
			}
			if runtime.GOOS != "windows" && info.Mode().Perm() != 0751 {
				t.Fatal(info.Mode())
			}
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
					if err := os.Rename(target, target+".saved"); err != nil {
						t.Error(err)
					}
					if err := os.WriteFile(target, []byte("other installation"), 0755); err != nil {
						t.Error(err)
					}
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
				if err := os.Chmod(dir, 0555); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(dir, 0755) })
			}
			if _, err := c.Update(context.Background(), "0.1.0"); err == nil {
				t.Fatal("expected error")
			}
			data, err := os.ReadFile(target)
			want := "old executable"
			if failure == "changed" {
				want = "other installation"
			}
			if err != nil || string(data) != want {
				t.Fatalf("%q %v", data, err)
			}
			files, err := filepath.Glob(filepath.Join(filepath.Dir(target), ".sqlc-ydb-update-*"))
			if err != nil || len(files) != 0 {
				t.Fatal(files, err)
			}
		})
	}
}

func TestNoDowngradeOrUnnecessaryInstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("automatic upgrades are not supported on Windows")
	}
	for _, version := range []string{"0.2.0", "0.3.0", "0.3.0-rc1"} {
		c := fixture(t, "", func(string, []byte) []byte { t.Error("downloaded an unnecessary update"); return nil })
		c.Executable = func() (string, error) { t.Error("looked up executable unnecessarily"); return "", nil }
		result, err := c.Update(context.Background(), version)
		if err != nil || result.Updated || result.Version != version {
			t.Fatal(result, err)
		}
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
				if err != nil || version != "0.2.0" {
					t.Fatal(version, err)
				}
			} else if err == nil {
				t.Fatal("accepted invalid release")
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
	if _, err := c.Latest(context.Background()); err == nil {
		t.Fatal("accepted HTTP 503")
	}
	c.ReleasesURL = server.URL + "/wait"
	start := time.Now()
	if _, err := c.Latest(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if time.Since(start) > CheckTimeout+time.Second {
		t.Fatal("check exceeded deadline")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Update(ctx, "0.1.0"); runtime.GOOS != "windows" && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	c.ReleasesURL = "http://example.invalid"
	if _, err := c.Latest(context.Background()); err == nil {
		t.Fatal("accepted HTTP")
	}
}

func TestDownloadSizeLimit(t *testing.T) {
	c := fixture(t, "", nil)
	if _, err := c.download(context.Background(), c.ReleasesURL+"/data", 1); err == nil {
		t.Fatal("accepted oversized download")
	}
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
		if _, err := c.Update(context.Background(), "0.1.0"); err != nil {
			t.Fatal(err)
		}
		return
	}
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "running.exe")
	if err := os.WriteFile(target, data, 0755); err != nil {
		t.Fatal(err)
	}
	c := fixture(t, target, nil)
	command := exec.Command(target, "-test.run=^TestRunningExecutable$")
	command.Env = append(os.Environ(), "SQLC_UPDATE_TEST_SERVER="+c.ReleasesURL)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "new executable" {
		t.Fatalf("update not installed: %v", err)
	}
}
