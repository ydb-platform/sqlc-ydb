package analyzer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

var typedDMLSchema = []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (
    owner_hash Uint64 NOT NULL,
    record_id Utf8 NOT NULL,
    owner_id Uint64 NOT NULL,
    group_id Utf8 NOT NULL,
    payload String NOT NULL,
    attributes Json NOT NULL,
    created_at Timestamp NOT NULL,
    updated_at Timestamp NOT NULL,
    PRIMARY KEY (owner_hash, record_id)
);`}}

func TestInsertSelectAcceptsUnaliasedComputedColumns(t *testing.T) {
	query := `-- name: InsertRecords :exec
DECLARE $owner_id AS Uint64;
DECLARE $rows AS List<Struct<record_id:Utf8,payload:String,group_id:Utf8,attributes:Json,created_at:Timestamp,updated_at:Timestamp>>;
INSERT INTO records (owner_hash, record_id, owner_id, payload, group_id, attributes, created_at, updated_at)
SELECT CAST($owner_id AS Uint64), r.record_id, $owner_id, r.payload, r.group_id, r.attributes, r.created_at, r.updated_at
FROM AS_TABLE($rows) AS r;`
	{
		{
			_, err := Analyze(typedDMLSchema, []model.Source{{Name: "query.sql", Text: query}})
			require.NoError(t, err)
		}
	}
}

func TestInsertSelectUsesAnySupportedRowSource(t *testing.T) {
	query := `-- name: CopyRecords :exec
INSERT INTO records (owner_hash, record_id, owner_id, group_id, payload, attributes, created_at, updated_at)
SELECT owner_hash, record_id, owner_id, group_id, payload, attributes, created_at, updated_at
FROM records WHERE owner_hash = $owner_hash;`
	{
		_, err := Analyze(typedDMLSchema, []model.Source{{Name: "query.sql", Text: query}})
		require.NoError(t, err)
	}
}

func TestInsertAndUpsertExplicitTargetsArePositional(t *testing.T) {
	remaining := ", owner_id, group_id, payload, attributes, created_at, updated_at"
	for _, target := range []string{"owner_hash, record_id", "record_id, owner_hash"} {
		for _, verb := range []string{"INSERT", "UPSERT"} {
			projection := "owner_hash AS record_id, record_id AS owner_hash"
			if strings.HasPrefix(target, "record_id") {
				projection = "record_id AS owner_hash, owner_hash AS record_id"
			}
			query := "-- name: Write :exec\n" + verb + " INTO records (" + target + remaining + ") SELECT " + projection + remaining + " FROM records;"
			{
				_, err := Analyze(typedDMLSchema, []model.Source{{Name: "query.sql", Text: query}})
				require.NoError(t, err)
			}
		}
	}
}

func TestDMLSelectContextualizesNullForOptionalTargets(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, note Utf8, PRIMARY KEY(id));`}}
	for _, statement := range []string{
		"INSERT INTO records (id, note) SELECT 1ul, NULL",
		"UPSERT INTO records (id, note) SELECT 1ul, NULL",
		"UPDATE records ON SELECT 1ul AS id, NULL AS note",
		"DELETE FROM records ON SELECT 1ul AS id, NULL AS note",
	} {
		{
			_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Write :exec\n" + statement + ";"}})
			require.NoError(t, err)
		}
	}
}

func TestDMLSelectRejectsNullForRequiredTargets(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, required Utf8 NOT NULL, PRIMARY KEY(id));`}}
	for _, statement := range []string{
		"INSERT INTO records (id) SELECT NULL",
		"UPSERT INTO records (id, required) SELECT 1ul, NULL",
		"UPDATE records ON SELECT NULL AS id",
		"DELETE FROM records ON SELECT NULL AS id",
	} {
		_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Write :exec\n" + statement + ";"}})
		require.ErrorContains(t, err, "requires")
	}
}

func TestUpdateOnSelectMatchesColumnsByResultName(t *testing.T) {
	query := `-- name: UpdateRecords :exec
DECLARE $rows AS List<Struct<record_id:Utf8,owner_hash:Uint64,payload:String>>;
UPDATE records ON
SELECT r.owner_hash, r.record_id, r.payload FROM AS_TABLE($rows) AS r;`
	{
		_, err := Analyze(typedDMLSchema, []model.Source{{Name: "query.sql", Text: query}})
		require.NoError(t, err)
	}
}

func TestUpdateOnSelectExpandsQualifiedStarAndOverride(t *testing.T) {
	query := `-- name: UpdateRecords :exec
DECLARE $updated_at AS Timestamp;
DECLARE $rows AS List<Struct<record_id:Utf8,owner_hash:Uint64,payload:String>>;
UPDATE records ON
SELECT r.*, $updated_at AS updated_at FROM AS_TABLE($rows) AS r;`
	{
		_, err := Analyze(typedDMLSchema, []model.Source{{Name: "query.sql", Text: query}})
		require.NoError(t, err)
	}
}

func TestOnSelectStarMatchesNamesIndependentOfStructOrder(t *testing.T) {
	for _, fields := range []string{
		"record_id:Utf8,owner_hash:Uint64,payload:String",
		"payload:String,owner_hash:Uint64,record_id:Utf8",
	} {
		query := "-- name: UpdateRecords :exec\nDECLARE $rows AS List<Struct<" + fields + ">>;\nUPDATE records ON SELECT r.* FROM AS_TABLE($rows) r;"
		_, err := Analyze(typedDMLSchema, []model.Source{{Name: "query.sql", Text: query}})
		require.NoError(t, err)
	}
}

func TestDeleteOnSelectAllowsNonKeyColumns(t *testing.T) {
	query := `-- name: DeleteRecords :exec
DELETE FROM records ON
SELECT owner_hash, record_id, payload FROM records WHERE owner_hash = $owner_hash;`
	{
		_, err := Analyze(typedDMLSchema, []model.Source{{Name: "query.sql", Text: query}})
		require.NoError(t, err)
	}
}

func TestOnSelectDiagnostics(t *testing.T) {
	for _, tt := range []struct {
		name, statement, want string
	}{
		{"update missing key", "UPDATE records ON SELECT owner_hash, payload FROM records", `missing primary key column "record_id"`},
		{"delete missing key", "DELETE FROM records ON SELECT owner_hash FROM records", `missing primary key column "record_id"`},
		{"unknown result", "DELETE FROM records ON SELECT owner_hash, record_id, 1 AS extra FROM records", `unknown target column "extra"`},
		{"duplicate result", "UPDATE records ON SELECT owner_hash, record_id, payload, group_id AS payload FROM records", `duplicate source column "payload"`},
		{"wrong key type", "DECLARE $rows AS List<Struct<owner_hash:Utf8,record_id:Utf8>>; DELETE FROM records ON SELECT owner_hash, record_id FROM AS_TABLE($rows)", `source column "owner_hash" has type Utf8`},
		{"optional key", "DECLARE $rows AS List<Struct<owner_hash:Uint64?,record_id:Utf8>>; DELETE FROM records ON SELECT owner_hash, record_id FROM AS_TABLE($rows)", `source column "owner_hash" has type Optional<Uint64>`},
		{"missing source", "UPDATE records ON SELECT owner_hash, record_id FROM absent", `unknown table "absent"`},
		{"update values", "UPDATE records ON VALUES (1, \"id\")", `UPDATE ON currently requires a SELECT source`},
		{"delete values", "DELETE FROM records ON VALUES (1, \"id\")", `DELETE ON currently requires a SELECT source`},
		{"update source list", "UPDATE records ON (owner_hash, record_id) SELECT owner_hash, record_id FROM records", `UPDATE ON SELECT with an explicit source column list is unsupported`},
		{"delete source list", "DELETE FROM records ON (owner_hash, record_id) SELECT owner_hash, record_id FROM records", `DELETE ON SELECT with an explicit source column list is unsupported`},
		{"ambiguous stars", "UPDATE records ON SELECT * FROM records a JOIN records b ON a.owner_hash = b.owner_hash", `duplicate source column "owner_hash"`},
		{"update union", "UPDATE records ON SELECT owner_hash, record_id FROM records UNION ALL SELECT owner_hash, record_id FROM records", `DML SELECT supports one SELECT input`},
		{"delete cte", "DELETE FROM records ON WITH keys AS (SELECT owner_hash, record_id FROM records) SELECT owner_hash, record_id FROM keys", `CTEs are not yet supported`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Analyze(typedDMLSchema, []model.Source{{Name: "query.sql", Text: "-- name: Change :exec\n" + tt.statement + ";"}})
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestOrdinaryDMLInfersINListParametersBeforeValidation(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, name Utf8, PRIMARY KEY(id));`}}
	for _, statement := range []string{
		"DELETE FROM records WHERE id IN $ids",
		"UPDATE records SET name = $name WHERE id IN $ids",
	} {
		got, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Change :exec\n" + statement + ";"}})
		require.NoError(t, err)
		params := map[string]string{}
		for _, parameter := range got.Queries[0].Parameters {
			params[parameter.Name] = parameter.Type.String()
		}
		require.Equal(t, "List<Uint64>", params["ids"])
	}
}

func TestOnSelectSourceErrorDoesNotCascade(t *testing.T) {
	_, err := Analyze(typedDMLSchema, []model.Source{{Name: "query.sql", Text: "-- name: Change :exec\nDELETE FROM records ON SELECT missing FROM absent;"}})
	require.ErrorContains(t, err, `unknown table "absent"`)
	require.NotContains(t, err.Error(), "missing primary key")
}

func TestOnSelectQualifiedResultSuggestsAlias(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, note Utf8, PRIMARY KEY(id));`}}
	query := `-- name: Write :exec
UPDATE records ON SELECT r.id, r.note FROM records r JOIN records other ON r.id = other.id;`
	_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
	require.ErrorContains(t, err, `unknown target column "r.id"; use AS id`)
}
