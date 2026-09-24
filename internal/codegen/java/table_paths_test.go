package java

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestJavaTablePathsKeepNamespaceInNames(t *testing.T) {
	for _, runtime := range []string{"ydb", "jdbc", "jooq"} {
		t.Run(runtime, func(t *testing.T) {
			a := &model.AnalysisResult{Catalog: model.Catalog{Tables: []model.Table{
				{Name: "/local/tenant_a/users"}, {Name: "/local/tenant_b/users"},
			}}}
			files, err := Generate(a, Options{Runtime: runtime})
			require.NoError(t, err)
			if runtime == "jooq" {
				for _, want := range []string{"LocalTenantAUsersTable", "LocalTenantBUsersTable", "LOCAL_TENANT_A_USERS", "LOCAL_TENANT_B_USERS", `"/local/tenant_a/users"`, `"/local/tenant_b/users"`} {
					require.Contains(t, string(files[0].Content), want, "missing %s in %s", want, files[0].Content)
				}
			} else {
				require.False(t, files[0].Name != "LocalTenantAUsers.java" || files[1].Name != "LocalTenantBUsers.java", "table namespaces lost: %v", files)
			}
		})
	}
}

func TestJavaTablePathCollisions(t *testing.T) {
	for _, runtime := range []string{"ydb", "jdbc", "jooq"} {
		t.Run(runtime, func(t *testing.T) {
			a := &model.AnalysisResult{Catalog: model.Catalog{Tables: []model.Table{{Name: "tenant/users"}, {Name: "tenant_users"}}}}
			files, err := Generate(a, Options{Runtime: runtime})
			require.False(t, files != nil || err == nil || !strings.Contains(err.Error(), "name collision"), "colliding table paths: files=%v, error=%v", files, err)
		})
	}
}
