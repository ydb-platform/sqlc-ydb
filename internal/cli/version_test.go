package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
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
				want += fmt.Sprintf("New version available: %s. Run sqlc-ydb self-update to install it.\n", tc.version)
			}
			if code != 0 || stderr.Len() != 0 || out.String() != want {
				t.Fatalf("%d %q %q", code, out.String(), stderr.String())
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
	if code, _, stderr := invoke("self-update"); code != 1 || !strings.Contains(stderr, "check for updates") {
		t.Fatalf("%d %q", code, stderr)
	}
	for _, args := range [][]string{{"self-update", "--no-remote"}, {"self-update", "extra"}, {"version", "update"}, {"version", "--update"}, {"self-update", "--verbose"}} {
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
	if code := run([]string{"self-update"}, &out, &stderr, client); code != 0 || out.String() != "sqlc-ydb "+Version+" is already up to date.\n" || stderr.Len() != 0 {
		t.Fatalf("%d %q %q", code, out.String(), stderr.String())
	}
}
