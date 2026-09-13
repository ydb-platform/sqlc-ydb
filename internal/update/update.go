// Package update checks and installs published sqlc-ydb releases.
package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	CheckTimeout = 2 * time.Second
	maxArchive   = 128 << 20
	maxBinary    = 128 << 20
)

var versionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-rc(0|[1-9][0-9]*))?$`)

// Client uses the public release site without GitHub credentials.
type Client struct {
	HTTP        *http.Client
	ReleasesURL string
	Executable  func() (string, error)
}

func NewClient() *Client {
	return &Client{
		HTTP: &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" || len(via) >= 10 {
				return errors.New("unsafe or excessive release redirect")
			}
			return nil
		}},
		ReleasesURL: "https://github.com/ydb-platform/sqlc-ydb/releases",
		Executable:  os.Executable,
	}
}

// Newer compares numeric release components, including rcN suffixes.
// Unknown development versions do not produce update notifications.
func Newer(latest, current string) bool {
	a, b := versionPattern.FindStringSubmatch(latest), versionPattern.FindStringSubmatch(current)
	if a == nil || b == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		if a[i] != b[i] {
			return numericGreater(a[i], b[i])
		}
	}
	if a[4] == "" || b[4] == "" {
		return a[4] == "" && b[4] != ""
	}
	return numericGreater(a[4], b[4])
}

func numericGreater(a, b string) bool {
	if len(a) != len(b) {
		return len(a) > len(b)
	}
	return a > b
}

// Latest follows GitHub's stable-release redirect, excluding prereleases.
func (c *Client) Latest(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, CheckTimeout)
	defer cancel()
	resp, err := c.get(ctx, c.ReleasesURL+"/latest")
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	prefix := c.ReleasesURL + "/tag/"
	url := resp.Request.URL.String()
	tag := strings.TrimPrefix(url, prefix)
	parts := versionPattern.FindStringSubmatch(tag)
	if !strings.HasPrefix(url, prefix) || !strings.HasPrefix(tag, "v") || parts == nil || parts[4] != "" {
		return "", errors.New("no stable sqlc-ydb release found")
	}
	return strings.TrimPrefix(tag, "v"), nil
}

func (c *Client) get(ctx context.Context, url string) (*http.Response, error) {
	if !strings.HasPrefix(url, "https://") {
		return nil, errors.New("release downloads require HTTPS")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "sqlc-ydb")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("release request returned HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

func (c *Client) download(ctx context.Context, url string, limit int64) ([]byte, error) {
	resp, err := c.get(ctx, url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err == nil && int64(len(data)) > limit {
		err = errors.New("release file exceeds size limit")
	}
	return data, err
}

type Result struct {
	Version, Path string
	Updated       bool
}

// Update stages a verified executable beside the real running binary before
// replacing it. Symlinks retain their paths and contents.
func (c *Client) Update(ctx context.Context, current string) (Result, error) {
	if runtime.GOOS == "windows" {
		return Result{}, errors.New("automatic upgrades are not supported on Windows")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	latest, err := c.Latest(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("check for updates: %w", err)
	}
	result := Result{Version: latest}
	if versionPattern.MatchString(current) && !Newer(latest, current) {
		result.Version = current
		return result, nil
	}
	base, binary, extension, err := artifact(latest, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return result, err
	}
	executable, err := c.Executable()
	if err != nil {
		return result, fmt.Errorf("locate executable: %w", err)
	}
	target, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return result, fmt.Errorf("resolve executable: %w", err)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return result, err
	}
	// Capture the identity before downloading to detect concurrent replacement.
	original, err := os.Open(target)
	if err != nil {
		return result, err
	}
	info, err := original.Stat()
	_ = original.Close()
	if err != nil {
		return result, err
	}
	if !info.Mode().IsRegular() {
		return result, errors.New("executable is not a regular file")
	}
	staged, err := os.CreateTemp(filepath.Dir(target), ".sqlc-ydb-update-*")
	if err != nil {
		return result, fmt.Errorf("cannot write to installation directory: %w", err)
	}
	defer func() {
		_ = staged.Close()
		_ = os.Remove(staged.Name())
	}()
	url := c.ReleasesURL + "/download/v" + latest + "/"
	sums, err := c.download(ctx, url+"SHA256SUMS", 1<<20)
	if err != nil {
		return result, fmt.Errorf("download checksums: %w", err)
	}
	archive := base + extension
	expected, err := checksum(sums, archive)
	if err != nil {
		return result, err
	}
	data, err := c.download(ctx, url+archive, maxArchive)
	if err != nil {
		return result, fmt.Errorf("download update: %w", err)
	}
	actual := sha256.Sum256(data)
	if !bytes.Equal(actual[:], expected) {
		return result, errors.New("update checksum mismatch")
	}
	if err := extract(staged, data, base+"/"+binary); err != nil {
		return result, fmt.Errorf("extract update: %w", err)
	}
	if err := staged.Chmod(info.Mode().Perm()); err != nil {
		return result, err
	}
	if err := staged.Sync(); err != nil {
		return result, err
	}
	if err := staged.Close(); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := install(staged.Name(), target, info); err != nil {
		return result, err
	}
	result.Path, result.Updated = target, true
	return result, nil
}

func install(staged, target string, original os.FileInfo) error {
	// Keep the lock independent of the executable inode, which rename replaces.
	lock := target + ".update-lock"
	if err := os.Mkdir(lock, 0700); err != nil {
		return fmt.Errorf("cannot acquire update lock %s (confirm no updater is running; remove the empty directory only if no updater is running): %w", lock, err)
	}
	defer func() { _ = os.Remove(lock) }()
	// Do not replace a different file installed while the download was running.
	now, err := os.Stat(target)
	if err != nil || !os.SameFile(original, now) {
		return errors.New("executable changed during update; retry the command")
	}
	if err := os.Rename(staged, target); err != nil {
		return fmt.Errorf("replace executable: %w", err)
	}
	return nil
}

func artifact(version, goos, goarch string) (string, string, string, error) {
	if (goos != "linux" && goos != "darwin") || (goarch != "amd64" && goarch != "arm64") {
		return "", "", "", fmt.Errorf("no release binary for %s/%s", goos, goarch)
	}
	base := "sqlc-ydb_" + version + "_" + goos + "_" + goarch
	return base, "sqlc-ydb", ".tar.gz", nil
}

func checksum(sums []byte, name string) ([]byte, error) {
	var found []byte
	for line := range strings.SplitSeq(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[1] != name {
			continue
		}
		value, err := hex.DecodeString(fields[0])
		if found != nil || err != nil || len(value) != sha256.Size {
			return nil, errors.New("invalid or ambiguous update checksum")
		}
		found = value
	}
	if found == nil {
		return nil, errors.New("update checksum is missing")
	}
	return found, nil
}

func extract(dst io.Writer, data []byte, name string) error {
	found := false
	copyBinary := func(src io.Reader, size int64) error {
		if found || size <= 0 || size > maxBinary {
			return errors.New("invalid or duplicate executable in update")
		}
		found = true
		n, err := io.Copy(dst, io.LimitReader(src, maxBinary+1))
		if err == nil && n != size {
			err = errors.New("invalid executable size")
		}
		return err
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()
	reader := tar.NewReader(io.LimitReader(gz, maxArchive+1))
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if header.Name != name {
			continue
		}
		if header.Typeflag != tar.TypeReg {
			return errors.New("executable in update is not a regular file")
		}
		if err := copyBinary(reader, header.Size); err != nil {
			return err
		}
	}
	if !found {
		return errors.New("executable is missing from update")
	}
	return nil
}
