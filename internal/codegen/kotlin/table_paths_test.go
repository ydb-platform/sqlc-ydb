package kotlin

import (
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestKotlinTablePaths(t *testing.T) {
	columns := []model.Column{{Name: "id", Type: model.Type{Kind: "Uint64"}}}
	a := &model.AnalysisResult{Catalog: model.Catalog{Tables: []model.Table{{Name: "/local/tenant_a/users", Columns: columns}, {Name: "/local/tenant_b/users", Columns: columns}}}}
	files, err := Generate(a, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"data class LocalTenantAUsers", "data class LocalTenantBUsers"} {
		if !strings.Contains(string(files[i].Content), want) {
			t.Fatalf("missing %q in %s", want, files[i].Content)
		}
	}
	a.Catalog.Tables = []model.Table{{Name: "tenant/users", Columns: columns}, {Name: "tenant_users", Columns: columns}}
	files, err = Generate(a, Options{})
	if files != nil || err == nil || !strings.Contains(err.Error(), "name collision") {
		t.Fatalf("colliding table paths: files=%v, error=%v", files, err)
	}
}
