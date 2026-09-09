package endtoend

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-engine-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-engine-ydb/internal/model"
	"github.com/ydb-platform/sqlc-engine-ydb/internal/source"
)

var updateLegacy = flag.Bool("update-legacy", false, "update reviewed legacy YDB semantic expectations")

const legacyRoot = "../../testdata/legacy-ydb"

type legacyManifest struct {
	Revision           string `json:"revision"`
	ScenarioCount      int    `json:"scenario_count"`
	ConfigurationCount int    `json:"configuration_count"`
	CaseCount          int    `json:"case_count"`
	Files              map[string]struct {
		SHA256 string `json:"sha256"`
		Source string `json:"source"`
	} `json:"files"`
	Cases []struct {
		Name          string   `json:"name"`
		Schema        []string `json:"schema"`
		Queries       []string `json:"queries"`
		SourceConfigs []string `json:"source_configs"`
	} `json:"cases"`
}

type legacyExpectation struct {
	Status      string             `json:"status"`
	Catalog     *model.Catalog     `json:"catalog,omitempty"`
	Queries     []legacyQuery      `json:"queries,omitempty"`
	Diagnostics []model.Diagnostic `json:"diagnostics,omitempty"`
}

type legacyQuery struct {
	Name       string            `json:"name"`
	Command    model.Command     `json:"command"`
	Parameters []model.Parameter `json:"parameters"`
	ResultSets []model.ResultSet `json:"result_sets"`
}

func readLegacyManifest(t *testing.T) legacyManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(legacyRoot, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest legacyManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

// The fork may be deleted. Verify the local corpus and the complete historical
// patch against the recorded import, without Git/network access at test time.
func TestLegacySourceIntegrity(t *testing.T) {
	manifest := readLegacyManifest(t)
	if manifest.Revision != "8eed5d890396eb03953248a3ec4ab7e28dfaed45" ||
		manifest.ScenarioCount != 126 || manifest.ConfigurationCount != 181 || manifest.CaseCount != 131 || len(manifest.Cases) != manifest.CaseCount {
		t.Fatal("incomplete or unexpected legacy corpus inventory")
	}
	for path, file := range manifest.Files {
		if !filepath.IsLocal(path) {
			t.Fatalf("non-local imported path %q", path)
		}
		data, err := os.ReadFile(filepath.Join(legacyRoot, path))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != file.SHA256 {
			t.Errorf("imported source changed: %s; adapt a separate fixture instead", path)
		}
	}
	configs, names := map[string]bool{}, map[string]bool{}
	for _, fixture := range manifest.Cases {
		if !filepath.IsLocal(fixture.Name) || strings.ContainsAny(fixture.Name, "/\\") || names[fixture.Name] {
			t.Fatalf("invalid or duplicate case %q", fixture.Name)
		}
		names[fixture.Name] = true
		for _, path := range append(append([]string{}, fixture.Schema...), fixture.Queries...) {
			if _, ok := manifest.Files[path]; !ok {
				t.Errorf("case %s references unpreserved input %s", fixture.Name, path)
			}
		}
		for _, config := range fixture.SourceConfigs {
			configs[config] = true
		}
	}
	if len(configs) != manifest.ConfigurationCount {
		t.Errorf("case registry accounts for %d configurations, expected %d", len(configs), manifest.ConfigurationCount)
	}
}

// Accepted cases assert resolved semantic data. Rejected cases assert explicit
// diagnostics; they are not skipped or counted as implemented SQL support.
func TestLegacyCorpus(t *testing.T) {
	for _, fixture := range readLegacyManifest(t).Cases {
		t.Run(fixture.Name, func(t *testing.T) {
			schema, err := source.Read(legacyRoot, fixture.Schema, true)
			if err != nil {
				t.Fatal(err)
			}
			queries, err := source.Read(legacyRoot, fixture.Queries, false)
			if err != nil {
				t.Fatal(err)
			}
			// Stable original-relative names keep diagnostics independent of the
			// checkout path, operating system and temporary build directory.
			for _, sources := range [][]model.Source{schema, queries} {
				for i := range sources {
					name, err := filepath.Rel(legacyRoot, sources[i].Name)
					if err != nil {
						t.Fatal(err)
					}
					sources[i].Name = filepath.ToSlash(name)
				}
			}
			result, analysisErr := analyzer.Analyze(schema, queries)
			actual := legacyExpectation{Status: "accepted"}
			if analysisErr != nil {
				if len(result.Diagnostics) == 0 {
					t.Fatalf("analysis failed without source diagnostics: %v", analysisErr)
				}
				actual.Status = "rejected"
				actual.Diagnostics = result.Diagnostics
			} else {
				if len(result.Diagnostics) != 0 {
					t.Fatal("successful analysis retained diagnostics")
				}
				actual.Catalog = &result.Catalog
				for _, query := range result.Queries {
					actual.Queries = append(actual.Queries, legacyQuery{query.Name, query.Command, query.Parameters, query.ResultSets})
				}
			}
			got, err := json.MarshalIndent(actual, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')
			path := filepath.Join(legacyRoot, "expected", fixture.Name+".json")
			want, readErr := os.ReadFile(path)
			if readErr != nil && !os.IsNotExist(readErr) {
				t.Fatal(readErr)
			}
			if *updateLegacy {
				if readErr == nil {
					var previous legacyExpectation
					if err := json.Unmarshal(want, &previous); err != nil {
						t.Fatal(err)
					}
					if previous.Status == "accepted" && actual.Status != "accepted" {
						t.Fatalf("refusing to turn a previously accepted case into a rejection:\n%s", got)
					}
				}
				write(t, path, got)
				return
			}
			if readErr != nil {
				t.Fatalf("missing expectation for imported case: %v", readErr)
			}
			if string(got) != string(want) {
				t.Fatal(fmt.Sprintf("legacy semantic result changed; review before updating\nwant: %s\ngot: %s", want, got))
			}
		})
	}
}
