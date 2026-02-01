package ddl

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Table DDL shared with runtime test (CREATE TABLE reg_t).
const testCreateTableDDL = `
CREATE TABLE reg_t (
    id Uint64,
    a Utf8,
    PRIMARY KEY (id)
);
`

func TestRegistry_Empty(t *testing.T) {
	reg, err := Registry("")
	require.NoError(t, err)
	_, ok := reg.Columns("any")
	require.False(t, ok)
}

func TestRegistry_CreateTable(t *testing.T) {
	schemaSQL := `
CREATE TABLE reg_t (
    id Uint64,
    a Utf8,
    PRIMARY KEY (id)
);
`
	reg, err := Registry(schemaSQL)
	require.NoError(t, err)
	cols, ok := reg.Columns("reg_t")
	require.True(t, ok)
	require.Len(t, cols, 2)
	require.Equal(t, "id", cols[0].Name)
	require.Equal(t, "Uint64", cols[0].DataType)
	require.Equal(t, "a", cols[1].Name)
	require.Equal(t, "Utf8", cols[1].DataType)
}

// TestRegistry_SameDDLAsRuntime uses the same CREATE TABLE as in runtime integration test.
func TestRegistry_SameDDLAsRuntime(t *testing.T) {
	reg, err := Registry(testCreateTableDDL)
	require.NoError(t, err)
	cols, ok := reg.Columns("reg_t")
	require.True(t, ok)
	require.Len(t, cols, 2)
	require.Equal(t, "id", cols[0].Name)
	require.Equal(t, "Uint64", cols[0].DataType)
	require.Equal(t, "a", cols[1].Name)
	require.Equal(t, "Utf8", cols[1].DataType)
}

func TestRegistry_NotNull(t *testing.T) {
	schemaSQL := `
CREATE TABLE t (
    id Uint64,
    name Utf8 NOT NULL,
    bio Utf8,
    PRIMARY KEY (id)
);
`
	reg, err := Registry(schemaSQL)
	require.NoError(t, err)
	cols, ok := reg.Columns("t")
	require.True(t, ok)
	require.Len(t, cols, 3)
	require.Equal(t, "id", cols[0].Name)
	require.True(t, cols[0].Nullable, "id: nullable by default")
	require.Equal(t, "name", cols[1].Name)
	require.False(t, cols[1].Nullable, "name: NOT NULL")
	require.Equal(t, "bio", cols[2].Name)
	require.True(t, cols[2].Nullable, "bio: nullable by default")
}

func TestRegistry_AlterTableAddColumn(t *testing.T) {
	schemaSQL := `
CREATE TABLE episodes (
    episode_id Uint64,
    title Utf8,
    PRIMARY KEY (episode_id)
);
ALTER TABLE episodes ADD COLUMN views Uint64;
`
	reg, err := Registry(schemaSQL)
	require.NoError(t, err)
	cols, ok := reg.Columns("episodes")
	require.True(t, ok)
	require.Len(t, cols, 3)
	require.Equal(t, "episode_id", cols[0].Name)
	require.Equal(t, "title", cols[1].Name)
	require.Equal(t, "views", cols[2].Name)
	require.Equal(t, "Uint64", cols[2].DataType)
}

func TestRegistry_AlterTableDropColumn(t *testing.T) {
	schemaSQL := `
CREATE TABLE episodes (
    episode_id Uint64,
    title Utf8,
    views Uint64,
    PRIMARY KEY (episode_id)
);
ALTER TABLE episodes DROP COLUMN views;
`
	reg, err := Registry(schemaSQL)
	require.NoError(t, err)
	cols, ok := reg.Columns("episodes")
	require.True(t, ok)
	require.Len(t, cols, 2)
	require.Equal(t, "episode_id", cols[0].Name)
	require.Equal(t, "title", cols[1].Name)
}

func TestRegistry_DropTable(t *testing.T) {
	schemaSQL := `
CREATE TABLE my_table (
    id Uint64,
    PRIMARY KEY (id)
);
DROP TABLE my_table;
`
	reg, err := Registry(schemaSQL)
	require.NoError(t, err)
	_, ok := reg.Columns("my_table")
	require.False(t, ok)
}

func TestRegistry_CreateView(t *testing.T) {
	schemaSQL := `
CREATE TABLE series (
    series_id Uint64,
    title Utf8,
    PRIMARY KEY (series_id)
);
CREATE VIEW recent_series AS SELECT * FROM series WHERE 1 = 1;
`
	reg, err := Registry(schemaSQL)
	require.NoError(t, err)
	cols, ok := reg.Columns("recent_series")
	require.True(t, ok)
	require.NotNil(t, cols)
	require.Len(t, cols, 0, "view registered with empty columns")
	_, ok = reg.Columns("series")
	require.True(t, ok)
}

func TestRegistry_DropView(t *testing.T) {
	schemaSQL := `
CREATE TABLE series (
    series_id Uint64,
    PRIMARY KEY (series_id)
);
CREATE VIEW recent_series AS SELECT * FROM series;
DROP VIEW recent_series;
`
	reg, err := Registry(schemaSQL)
	require.NoError(t, err)
	_, ok := reg.Columns("recent_series")
	require.False(t, ok)
	_, ok = reg.Columns("series")
	require.True(t, ok)
}

// TestRegistry_SequentialDDL runs a full sequence of DDL: CREATE TABLE -> ADD COLUMN ->
// DROP COLUMN -> CREATE VIEW -> DROP VIEW -> DROP TABLE and asserts schema at each step.
func TestRegistry_SequentialDDL(t *testing.T) {
	steps := []struct {
		name     string
		ddl      string
		table    string
		wantCols []string // column names; nil = table/view should not exist
	}{
		{
			name:     "create table",
			ddl:      "CREATE TABLE seq_t (id Uint64, a Utf8, PRIMARY KEY (id));",
			table:    "seq_t",
			wantCols: []string{"id", "a"},
		},
		{
			name:     "add column",
			ddl:      "ALTER TABLE seq_t ADD COLUMN b Utf8;",
			table:    "seq_t",
			wantCols: []string{"id", "a", "b"},
		},
		{
			name:     "drop column a",
			ddl:      "ALTER TABLE seq_t DROP COLUMN a;",
			table:    "seq_t",
			wantCols: []string{"id", "b"},
		},
		{
			name:     "create view",
			ddl:      "CREATE VIEW seq_v AS SELECT id, b FROM seq_t;",
			table:    "seq_v",
			wantCols: []string{}, // view registered with empty columns
		},
		{
			name:     "drop view",
			ddl:      "DROP VIEW seq_v;",
			table:    "seq_v",
			wantCols: nil,
		},
		{
			name:     "drop table",
			ddl:      "DROP TABLE seq_t;",
			table:    "seq_t",
			wantCols: nil,
		},
	}

	var fullDDL string
	for _, step := range steps {
		fullDDL += step.ddl + "\n"
		reg, err := Registry(fullDDL)
		require.NoError(t, err, "step %q", step.name)
		cols, ok := reg.Columns(step.table)
		if step.wantCols == nil {
			require.False(t, ok, "step %q: %s should not exist", step.name, step.table)
			continue
		}
		require.True(t, ok, "step %q: %s should exist", step.name, step.table)
		require.Len(t, cols, len(step.wantCols), "step %q: column count", step.name)
		for i, want := range step.wantCols {
			require.Equal(t, want, cols[i].Name, "step %q: col[%d]", step.name, i)
		}
	}
}
