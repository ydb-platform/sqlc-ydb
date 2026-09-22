package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Database configures optional server validation without resolving credentials during loading.
type Database struct {
	URI          string `yaml:"uri"`
	AuthTokenEnv string `yaml:"auth_token_env"`
	CAFile       string `yaml:"ca_file"`
	Timeout      string `yaml:"timeout"`
}

// ResolvedDatabase contains connection settings for one analysis invocation.
type ResolvedDatabase struct {
	Endpoint  string
	Database  string
	Secure    bool
	AuthToken string
	CAFile    string
	Timeout   time.Duration
}

// DatabaseEnabled reports the configured mode before a CLI offline override.
func (s SQL) DatabaseEnabled() bool {
	if s.Analyzer.Database != nil {
		return *s.Analyzer.Database
	}
	return s.Database != nil
}

func (d Database) validate() error {
	if strings.TrimSpace(d.URI) == "" {
		return errors.New("database.uri is required")
	}
	if d.AuthTokenEnv != "" && !environmentName(d.AuthTokenEnv) {
		return errors.New("database.auth_token_env must be an environment variable name, such as YDB_TOKEN")
	}
	_, err := d.timeout()
	return err
}

func (d Database) timeout() (time.Duration, error) {
	if d.Timeout == "" {
		return 10 * time.Second, nil
	}
	timeout, err := time.ParseDuration(d.Timeout)
	if err != nil || timeout <= 0 {
		return 0, errors.New("database.timeout must be a positive duration, such as 10s")
	}
	return timeout, nil
}

// Resolve expands URI variables and reads the configured token only when database analysis is used.
func (d Database) Resolve(baseDir string) (ResolvedDatabase, error) {
	var resolved ResolvedDatabase
	if err := d.validate(); err != nil {
		return resolved, err
	}
	uri, err := expandDatabaseURI(d.URI)
	if err != nil {
		return resolved, err
	}
	u, err := url.Parse(uri)
	if err != nil {
		// URL parsing errors include the original URI, which may contain credentials.
		return resolved, errors.New("invalid database.uri: expected grpc://host:port/database or grpcs://host:port/database")
	}
	if u.Scheme != "grpc" && u.Scheme != "grpcs" {
		return resolved, errors.New("database.uri scheme must be grpc or grpcs")
	}
	if u.Hostname() == "" {
		return resolved, errors.New("database.uri requires a host")
	}
	if u.Path == "" || !strings.HasPrefix(u.Path, "/") {
		return resolved, errors.New("database.uri requires an absolute database path")
	}
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(uri, "#") {
		return resolved, errors.New("database.uri must not contain userinfo, a query or a fragment; use auth_token_env for authentication")
	}
	if d.CAFile != "" && u.Scheme != "grpcs" {
		return resolved, errors.New("database.ca_file requires a grpcs URI")
	}
	resolved.Endpoint, resolved.Database, resolved.Secure = u.Host, u.Path, u.Scheme == "grpcs"
	resolved.Timeout, err = d.timeout()
	if err != nil {
		return ResolvedDatabase{}, err
	}
	if d.CAFile != "" {
		resolved.CAFile = d.CAFile
		if !filepath.IsAbs(resolved.CAFile) {
			resolved.CAFile = filepath.Join(baseDir, resolved.CAFile)
		}
		resolved.CAFile, err = filepath.Abs(resolved.CAFile)
		if err != nil {
			return ResolvedDatabase{}, fmt.Errorf("resolve database.ca_file: %w", err)
		}
	}
	if d.AuthTokenEnv != "" {
		resolved.AuthToken, err = requiredEnvironment(d.AuthTokenEnv)
		if err != nil {
			return ResolvedDatabase{}, fmt.Errorf("database.auth_token_env: %w", err)
		}
	}
	return resolved, nil
}

func expandDatabaseURI(uri string) (string, error) {
	var result strings.Builder
	for {
		start := strings.Index(uri, "${")
		if start < 0 {
			result.WriteString(uri)
			return result.String(), nil
		}
		result.WriteString(uri[:start])
		uri = uri[start+2:]
		end := strings.IndexByte(uri, '}')
		if end < 0 || !environmentName(uri[:end]) {
			return "", errors.New("database.uri has an invalid environment substitution; use ${NAME}")
		}
		value, err := requiredEnvironment(uri[:end])
		if err != nil {
			return "", fmt.Errorf("database.uri: %w", err)
		}
		result.WriteString(value)
		uri = uri[end+1:]
	}
}

func requiredEnvironment(name string) (string, error) {
	value := os.Getenv(name)
	if value == "" {
		return "", fmt.Errorf("environment variable %s is unset or empty", name)
	}
	return value, nil
}

func environmentName(name string) bool {
	if name == "" {
		return false
	}
	for i := range len(name) {
		c := name[i]
		if c != '_' && !(c >= 'A' && c <= 'Z') && !(c >= 'a' && c <= 'z') && !(i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
