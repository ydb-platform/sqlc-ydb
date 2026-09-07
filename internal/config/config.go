// Package config loads the supported sqlc configuration contract.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

type Paths []string

func (p *Paths) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Tag != "!!str" || n.Value == "" {
			return fmt.Errorf("line %d: expected a non-empty path", n.Line)
		}
		*p = []string{n.Value}
	case yaml.SequenceNode:
		for _, child := range n.Content {
			if child.Kind != yaml.ScalarNode || child.Tag != "!!str" || child.Value == "" {
				return fmt.Errorf("line %d: expected a non-empty path", child.Line)
			}
			*p = append(*p, child.Value)
		}
	default:
		return fmt.Errorf("line %d: expected a path or list of paths", n.Line)
	}
	return nil
}

type Go struct {
	Package         string `yaml:"package"`
	Out             string `yaml:"out"`
	SQLPackage      string `yaml:"sql_package"`
	EmitJSONTags    bool   `yaml:"emit_json_tags"`
	EmitInterface   bool   `yaml:"emit_interface"`
	EmitEmptySlices bool   `yaml:"emit_empty_slices"`
}

type Python struct {
	Package          string `yaml:"package"`
	Out              string `yaml:"out"`
	Runtime          string `yaml:"runtime"`
	EmitSyncQuerier  *bool  `yaml:"emit_sync_querier"`
	EmitAsyncQuerier bool   `yaml:"emit_async_querier"`
}

type Gen struct {
	Go     *Go     `yaml:"go"`
	Python *Python `yaml:"python"`
}

type SQL struct {
	Name    string    `yaml:"name"`
	Engine  string    `yaml:"engine"`
	Schema  Paths     `yaml:"schema"`
	Queries Paths     `yaml:"queries"`
	Gen     Gen       `yaml:"gen"`
	Codegen yaml.Node `yaml:"codegen"`
}

type v1Package struct {
	Name            string `yaml:"name"`
	Engine          string `yaml:"engine"`
	Path            string `yaml:"path"`
	Schema          Paths  `yaml:"schema"`
	Queries         Paths  `yaml:"queries"`
	SQLPackage      string `yaml:"sql_package"`
	EmitJSONTags    bool   `yaml:"emit_json_tags"`
	EmitInterface   bool   `yaml:"emit_interface"`
	EmitEmptySlices bool   `yaml:"emit_empty_slices"`
}

type Config struct {
	Version  string      `yaml:"version"`
	SQL      []SQL       `yaml:"sql"`
	Packages []v1Package `yaml:"packages"`
	Plugins  yaml.Node   `yaml:"plugins"`
	Engines  yaml.Node   `yaml:"engines"`
	Path     string      `yaml:"-"`
	Dir      string      `yaml:"-"`
}

// Load resolves source/output paths relative to the configuration, not the cwd.
func Load(path string) (*Config, error) {
	if path == "" {
		for _, candidate := range []string{"sqlc.yaml", "sqlc.yml", "sqlc.json"} {
			_, err := os.Stat(candidate)
			if err == nil {
				path = candidate
				break
			}
			if !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
		}
		if path == "" {
			return nil, errors.New("no sqlc.yaml, sqlc.yml or sqlc.json configuration found")
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	c, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	c.Path, c.Dir = abs, filepath.Dir(abs)
	return c, nil
}

func Parse(data []byte) (*Config, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var c Config
	if err := dec.Decode(&c); err != nil {
		return nil, err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("configuration must contain exactly one document")
	}
	if c.Plugins.Kind != 0 || c.Engines.Kind != 0 {
		return nil, pluginError()
	}
	switch c.Version {
	case "1":
		if len(c.SQL) != 0 {
			return nil, errors.New("version 1 uses packages, not sql")
		}
		for _, p := range c.Packages {
			c.SQL = append(c.SQL, SQL{Name: p.Name, Engine: p.Engine, Schema: p.Schema, Queries: p.Queries, Gen: Gen{Go: &Go{
				Package: p.Name, Out: p.Path, SQLPackage: p.SQLPackage, EmitJSONTags: p.EmitJSONTags, EmitInterface: p.EmitInterface, EmitEmptySlices: p.EmitEmptySlices,
			}}})
		}
	case "2":
		if len(c.Packages) != 0 {
			return nil, errors.New("version 2 uses sql, not packages")
		}
	default:
		return nil, errors.New("version must be \"1\" or \"2\"")
	}
	if len(c.SQL) == 0 {
		return nil, errors.New("configuration contains no SQL query sets")
	}
	for i := range c.SQL {
		s := &c.SQL[i]
		if s.Codegen.Kind != 0 {
			return nil, pluginError()
		}
		if s.Engine != "ydb" {
			return nil, fmt.Errorf("sql[%d]: engine must be ydb; other engines are not supported", i)
		}
		if len(s.Schema) == 0 || len(s.Queries) == 0 {
			return nil, fmt.Errorf("sql[%d]: schema and queries paths are required", i)
		}
		if g := s.Gen.Go; g != nil {
			if g.Out == "" {
				return nil, fmt.Errorf("sql[%d].gen.go.out is required", i)
			}
			if g.Package == "" {
				g.Package = filepath.Base(filepath.Clean(g.Out))
			}
			if g.SQLPackage == "" {
				g.SQLPackage = "database/sql"
			}
			switch g.SQLPackage {
			case "ydb", "database/sql":
			default:
				return nil, fmt.Errorf("sql[%d]: unsupported Go sql_package %q (use ydb or database/sql)", i, g.SQLPackage)
			}
		}
		if p := s.Gen.Python; p != nil {
			if p.Out == "" {
				return nil, fmt.Errorf("sql[%d].gen.python.out is required", i)
			}
			if p.Package == "" {
				p.Package = filepath.Base(filepath.Clean(p.Out))
			}
			if p.Runtime == "" {
				p.Runtime = "ydb"
			}
			switch p.Runtime {
			case "ydb", "dbapi", "sqlalchemy":
			default:
				return nil, fmt.Errorf("sql[%d]: unsupported Python runtime %q", i, p.Runtime)
			}
			if p.EmitSyncQuerier == nil {
				v := true
				p.EmitSyncQuerier = &v
			}
			if !*p.EmitSyncQuerier && !p.EmitAsyncQuerier {
				return nil, errors.New("Python requires emit_sync_querier or emit_async_querier")
			}
		}
	}
	return &c, nil
}

func pluginError() error {
	return errors.New("external plugins and codegen are not supported: migrate to sql[].gen.go or sql[].gen.python; generators are built into sqlc-ydb")
}
