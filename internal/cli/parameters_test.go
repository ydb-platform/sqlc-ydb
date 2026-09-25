package cli

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/config"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestConfiguredParameterTypesReachAnalyzerOptions(t *testing.T) {
	options, err := functionOptions(config.Analyzer{Parameters: map[string]map[string]string{
		"CreateBooks": {"books": "List<Struct<id:Uint64,title:Utf8>>", "имя": "Utf8"},
	}})
	require.NoError(t, err)
	require.Equal(t, map[string]map[string]model.Type{
		"CreateBooks": {
			"books": {Kind: "List", Elem: &model.Type{Kind: "Struct", Fields: []model.StructField{
				{Name: "id", Type: model.Type{Kind: "Uint64"}},
				{Name: "title", Type: model.Type{Kind: "Utf8"}},
			}}},
			"имя": {Kind: "Utf8"},
		},
	}, options.Parameters)
}

func TestConfiguredParameterTypesRejectInvalidYQLType(t *testing.T) {
	for _, typ := range []string{"Mystery", "Uint64; SELECT 1"} {
		_, err := functionOptions(config.Analyzer{Parameters: map[string]map[string]string{
			"Read": {"id": typ},
		}})
		require.ErrorContains(t, err, "query \"Read\" parameter $id")
	}
}
