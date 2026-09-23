package java

import (
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestJavaTablePathsKeepNamespaceInNames(t *testing.T) {
	for _, runtime := range []string{"ydb", "jdbc", "jooq"} {
		t.Run(runtime, func(t *testing.T) {
			a := &model.AnalysisResult{Catalog: model.Catalog{Tables: []model.Table{
				{Name: "/local/tenant_a/users"}, {Name: "/local/tenant_b/users"},
			}}}
			files, err := Generate(a, Options{Runtime: runtime})
			if err != nil {
				t.Fatal(err)
			}
			if runtime == "jooq" {
				for _, want := range []string{"LocalTenantAUsersTable", "LocalTenantBUsersTable", "LOCAL_TENANT_A_USERS", "LOCAL_TENANT_B_USERS", `"/local/tenant_a/users"`, `"/local/tenant_b/users"`} {
					if !strings.Contains(string(files[0].Content), want) {
						t.Fatalf("missing %s in %s", want, files[0].Content)
					}
				}
			} else if files[0].Name != "LocalTenantAUsers.java" || files[1].Name != "LocalTenantBUsers.java" {
				t.Fatalf("table namespaces lost: %v", files)
			}
		})
	}
}

func TestJavaTablePathCollisions(t *testing.T) {
	for _, runtime := range []string{"ydb", "jdbc", "jooq"} {
		t.Run(runtime, func(t *testing.T) {
			a := &model.AnalysisResult{Catalog: model.Catalog{Tables: []model.Table{{Name: "tenant/users"}, {Name: "tenant_users"}}}}
			files, err := Generate(a, Options{Runtime: runtime})
			if files != nil || err == nil || !strings.Contains(err.Error(), "name collision") {
				t.Fatalf("colliding table paths: files=%v, error=%v", files, err)
			}
		})
	}
}
