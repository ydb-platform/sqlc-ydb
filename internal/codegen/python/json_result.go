package python

import (
	"strings"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

// QuerySessionPool decodes Json and JsonDocument to Python values by default.
// Inputs and the DB-API based adapters continue to use serialized JSON strings.
func resultPyType(t model.Type, o Options) (string, error) {
	return pythonType(t, o.Runtime == "ydb")
}

func nativeJSONResults(a *model.AnalysisResult, o Options) bool {
	if o.Runtime != "ydb" {
		return false
	}
	for _, table := range a.Catalog.Tables {
		for _, c := range table.Columns {
			if containsJSON(c.Type) {
				return true
			}
		}
	}
	for _, q := range a.Queries {
		for _, rs := range q.ResultSets {
			for _, c := range rs.Columns {
				if containsJSON(c.Type) {
					return true
				}
			}
		}
	}
	return false
}
func containsJSON(t model.Type) bool {
	if strings.EqualFold(t.Kind, "Json") || strings.EqualFold(t.Kind, "JsonDocument") {
		return true
	}
	return t.Elem != nil && containsJSON(*t.Elem) || t.Key != nil && containsJSON(*t.Key)
}
