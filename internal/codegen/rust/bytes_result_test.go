package rust

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestResultComparisonDerives(t *testing.T) {
	binary := model.Type{Kind: "String"}
	yson := model.Type{Kind: "Yson"}
	floating := model.Type{Kind: "Double"}
	for _, tc := range []struct {
		name    string
		types   []model.Type
		derives string
	}{
		{"integer", []model.Type{{Kind: "Uint64"}}, "Debug, Clone, Copy, PartialEq, Eq, Hash, PartialOrd, Ord"},
		{"float", []model.Type{model.Optional(floating)}, "Debug, Clone, Copy, PartialEq, PartialOrd"},
		{"bytes", []model.Type{binary}, "Debug, Clone, PartialEq, Eq"},
		{"optional yson", []model.Type{model.Optional(yson)}, "Debug, Clone, PartialEq, Eq"},
		{"bytes list", []model.Type{{Kind: "List", Elem: &binary}}, "Debug, Clone, PartialEq, Eq"},
		{"bytes then float", []model.Type{binary, floating}, "Debug, Clone, PartialEq"},
		{"float then bytes", []model.Type{floating, binary}, "Debug, Clone, PartialEq"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var columns []model.Column
			for i, typ := range tc.types {
				columns = append(columns, model.Column{Name: fmt.Sprintf("value%d", i), Type: typ})
			}
			analysis := &model.AnalysisResult{Queries: []model.AnalyzedQuery{{Name: "Read", Command: model.One, SQL: "SELECT value FROM readings;", ResultSets: []model.ResultSet{{Columns: columns}}}}}
			files, err := Generate(analysis, Options{})
			require.NoError(t, err)
			models := generatedFile(t, files, "models.rs")
			require.Contains(t, models, "#[derive("+tc.derives+")]\npub struct ReadRow", "unexpected public row traits:\n%s", models)
		})
	}
}

func TestRejectsNestedOptionalResultSDKTypes(t *testing.T) {
	for _, kind := range []string{"String", "Utf8"} {
		t.Run(kind, func(t *testing.T) {
			typ := model.Optional(model.Optional(model.Type{Kind: kind}))
			query := model.AnalyzedQuery{Name: "Read", Command: model.One, SQL: "SELECT value FROM readings;", ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "value", Type: typ}}}}}
			files, err := Generate(&model.AnalysisResult{Queries: []model.AnalyzedQuery{query}}, Options{})
			require.False(t, files != nil || err == nil || !strings.Contains(err.Error(), "nested optional result decoding is unsupported by ydb 0.18.2"), "files=%v error=%v", files, err)
		})
	}
}

func TestBytesResultsCompileAndRetainEquality(t *testing.T) {
	if os.Getenv("SQLC_YDB_RUST_SDK_CHECK") == "" {
		t.Skip("set SQLC_YDB_RUST_SDK_CHECK=1 for published SDK trait checks")
	}
	var queries []model.AnalyzedQuery
	for _, kind := range []string{"String", "Yson"} {
		scalar := model.Type{Kind: kind}
		for _, tc := range []struct {
			name string
			typ  model.Type
		}{
			{"Scalar", scalar}, {"Optional", model.Optional(scalar)},
			{"List", model.Type{Kind: "List", Elem: &scalar}},
		} {
			queries = append(queries, model.AnalyzedQuery{Name: "Read" + kind + tc.name, Command: model.One, SQL: "SELECT value FROM readings;", ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "value", Type: tc.typ}}}}})
		}
	}
	for _, first := range []bool{false, true} {
		columns := []model.Column{{Name: "payload", Type: model.Type{Kind: "String"}}, {Name: "ratio", Type: model.Type{Kind: "Double"}}}
		if first {
			columns[0], columns[1] = columns[1], columns[0]
		}
		queries = append(queries, model.AnalyzedQuery{Name: fmt.Sprintf("ReadMixed%t", first), Command: model.One, SQL: fmt.Sprintf("SELECT %s,%s FROM readings;", columns[0].Name, columns[1].Name), ResultSets: []model.ResultSet{{Columns: columns}}})
	}
	queries = append(queries, model.AnalyzedQuery{Name: "ReadText", Command: model.One, SQL: "SELECT value FROM readings;", ResultSets: []model.ResultSet{{Columns: []model.Column{{Name: "value", Type: model.Optional(model.Type{Kind: "Utf8"})}}}}})
	queries = append(queries, model.AnalyzedQuery{Name: "BindNestedText", Command: model.Exec, SQL: "DECLARE $value AS Optional<Optional<Utf8>>; SELECT $value;", Parameters: []model.Parameter{{Name: "value", Type: model.Optional(model.Optional(model.Type{Kind: "Utf8"}))}}})
	files, err := Generate(&model.AnalysisResult{Queries: queries}, Options{})
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "src"), 0700))
	for _, file := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "src", file.Name), file.Content, 0600))
	}
	manifest := "[package]\nname=\"bytes-result-check\"\nversion=\"0.0.0\"\nedition=\"2024\"\n[dependencies]\nydb=\"=0.18.2\"\nbon=\"=3.10.1\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(manifest), 0600))
	consumer := `use bytes_result_check::models::*;
async fn check_nested_parameter(q: &mut bytes_result_check::queries::Queries<'_, ydb::QueryClient>) -> ydb::YdbResult<()> {
    q.bind_nested_text().value(Some(Some("nested".to_string()))).call().await
}
fn require_eq<T: Eq>() {}
fn require_ordered_hash<T: Eq + std::hash::Hash + Ord>() {}
fn main() {
    require_eq::<ReadStringScalarRow>();
    require_eq::<ReadStringOptionalRow>();
    require_eq::<ReadStringListRow>();
    require_eq::<ReadYsonScalarRow>();
    require_eq::<ReadYsonOptionalRow>();
    require_eq::<ReadYsonListRow>();
    require_ordered_hash::<ReadTextRow>();
    let bytes = ydb::Bytes::from(vec![0, 255]);
    let scalar = ReadStringScalarRow { value: bytes.clone() };
    assert_eq!(scalar, scalar.clone());
    assert_ne!(scalar, ReadStringScalarRow { value: ydb::Bytes::from(vec![255, 0]) });
    let optional = ReadStringOptionalRow { value: None };
    assert_eq!(optional, optional.clone());
    assert_ne!(optional, ReadStringOptionalRow { value: Some(bytes.clone()) });
    let list = ReadYsonListRow { value: vec![bytes.clone()] };
    assert_eq!(list, list.clone());
    let mixed = ReadMixedfalseRow { payload: bytes.clone(), ratio: 1.5 };
    assert_eq!(mixed, mixed.clone());
    let nan = ReadMixedtrueRow { ratio: f64::NAN, payload: bytes };
    assert_ne!(nan, nan.clone());
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src", "main.rs"), []byte(consumer), 0600))
	cmd := exec.Command("cargo", "run", "--quiet")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		require.NoError(t, err, "byte result traits against ydb 0.18.2: %v\n%s", err, output)
	}
}
