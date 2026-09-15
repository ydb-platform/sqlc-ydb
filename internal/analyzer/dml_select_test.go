package analyzer

import (
	"strings"
	"testing"

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
DECLARE $rows AS List<Struct<record_id:Utf8,payload:String>>;
INSERT INTO records (owner_hash, record_id, owner_id, payload)
SELECT CAST($owner_id AS Uint64), r.record_id, $owner_id, r.payload
FROM AS_TABLE($rows) AS r;`
	if _, err := Analyze(typedDMLSchema, []model.Source{{Name: "query.sql", Text: query}}); err != nil {
		t.Fatal(err)
	}
}

func TestInsertSelectUsesAnySupportedRowSource(t *testing.T) {
	query := `-- name: CopyRecords :exec
INSERT INTO records (owner_hash, record_id, owner_id, group_id, payload, attributes, created_at, updated_at)
SELECT owner_hash, record_id, owner_id, group_id, payload, attributes, created_at, updated_at
FROM records WHERE owner_hash = $owner_hash;`
	if _, err := Analyze(typedDMLSchema, []model.Source{{Name: "query.sql", Text: query}}); err != nil {
		t.Fatal(err)
	}
}

func TestInsertAndUpsertExplicitTargetsArePositional(t *testing.T) {
	for _, target := range []string{"owner_hash, record_id", "record_id, owner_hash"} {
		for _, verb := range []string{"INSERT", "UPSERT"} {
			projection := "owner_hash AS record_id, record_id AS owner_hash"
			if strings.HasPrefix(target, "record_id") {
				projection = "record_id AS owner_hash, owner_hash AS record_id"
			}
			query := "-- name: Write :exec\n" + verb + " INTO records (" + target + ") SELECT " + projection + " FROM records;"
			if _, err := Analyze(typedDMLSchema, []model.Source{{Name: "query.sql", Text: query}}); err != nil {
				t.Fatalf("%s (%s): %v", verb, target, err)
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
		if _, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Write :exec\n" + statement + ";"}}); err != nil {
			t.Fatalf("%s: %v", statement, err)
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
		if err == nil || !strings.Contains(err.Error(), "requires") {
			t.Fatalf("%s: %v", statement, err)
		}
	}
}

func TestUpdateOnSelectMatchesColumnsByResultName(t *testing.T) {
	query := `-- name: UpdateRecords :exec
DECLARE $rows AS List<Struct<record_id:Utf8,owner_hash:Uint64,payload:String>>;
UPDATE records ON
SELECT r.owner_hash, r.record_id, r.payload FROM AS_TABLE($rows) AS r;`
	if _, err := Analyze(typedDMLSchema, []model.Source{{Name: "query.sql", Text: query}}); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateOnSelectExpandsQualifiedStarAndOverride(t *testing.T) {
	query := `-- name: UpdateRecords :exec
DECLARE $updated_at AS Timestamp;
DECLARE $rows AS List<Struct<record_id:Utf8,owner_hash:Uint64,payload:String>>;
UPDATE records ON
SELECT r.*, $updated_at AS updated_at FROM AS_TABLE($rows) AS r;`
	if _, err := Analyze(typedDMLSchema, []model.Source{{Name: "query.sql", Text: query}}); err != nil {
		t.Fatal(err)
	}
}

func TestOnSelectStarMatchesNamesIndependentOfStructOrder(t *testing.T) {
	for _, fields := range []string{
		"record_id:Utf8,owner_hash:Uint64,payload:String",
		"payload:String,owner_hash:Uint64,record_id:Utf8",
	} {
		query := "-- name: UpdateRecords :exec\nDECLARE $rows AS List<Struct<" + fields + ">>;\nUPDATE records ON SELECT r.* FROM AS_TABLE($rows) r;"
		if _, err := Analyze(typedDMLSchema, []model.Source{{Name: "query.sql", Text: query}}); err != nil {
			t.Fatalf("%s: %v", fields, err)
		}
	}
}

func TestDeleteOnSelectAllowsNonKeyColumns(t *testing.T) {
	query := `-- name: DeleteRecords :exec
DELETE FROM records ON
SELECT owner_hash, record_id, payload FROM records WHERE owner_hash = $owner_hash;`
	if _, err := Analyze(typedDMLSchema, []model.Source{{Name: "query.sql", Text: query}}); err != nil {
		t.Fatal(err)
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
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
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
		if err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
		params := map[string]string{}
		for _, parameter := range got.Queries[0].Parameters {
			params[parameter.Name] = parameter.Type.String()
		}
		if params["ids"] != "List<Uint64>" {
			t.Fatalf("%s: parameters %v", statement, params)
		}
	}
}

func TestOnSelectSourceErrorDoesNotCascade(t *testing.T) {
	_, err := Analyze(typedDMLSchema, []model.Source{{Name: "query.sql", Text: "-- name: Change :exec\nDELETE FROM records ON SELECT missing FROM absent;"}})
	if err == nil || !strings.Contains(err.Error(), `unknown table "absent"`) {
		t.Fatal(err)
	}
	if strings.Contains(err.Error(), "missing primary key") {
		t.Fatalf("unexpected cascade: %v", err)
	}
}

func TestOnSelectQualifiedResultSuggestsAlias(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE records (id Uint64 NOT NULL, note Utf8, PRIMARY KEY(id));`}}
	query := `-- name: Write :exec
UPDATE records ON SELECT r.id, r.note FROM records r JOIN records other ON r.id = other.id;`
	_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: query}})
	if err == nil || !strings.Contains(err.Error(), `unknown target column "r.id"; use AS id`) {
		t.Fatalf("error = %v", err)
	}
}
