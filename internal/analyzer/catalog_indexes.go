package analyzer

import (
	"fmt"
	"slices"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
	parser "github.com/ydb-platform/yql-parsers/go"
)

func parseTableIndex(ctx parser.ITable_indexContext) (model.Index, error) {
	typ := ctx.Table_index_type()
	if typ.Global_index() == nil || typ.USING() != nil || typ.Global_index().UNIQUE() != nil {
		return model.Index{}, fmt.Errorf("unsupported index type %q; supported types are GLOBAL SYNC and GLOBAL ASYNC", typ.GetText())
	}
	if ctx.With_index_settings() != nil {
		return model.Index{}, fmt.Errorf("index settings are not yet supported")
	}
	index := model.Index{Name: identifier(ctx.An_id().GetText()), Kind: "GlobalSync"}
	if typ.Global_index().ASYNC() != nil {
		index.Kind = "GlobalAsync"
	}
	for _, id := range ctx.AllAn_id_schema() {
		name := identifier(id.GetText())
		if ctx.COVER() != nil && id.GetStart().GetTokenIndex() > ctx.COVER().GetSymbol().GetTokenIndex() {
			index.DataColumns = append(index.DataColumns, name)
		} else {
			index.Columns = append(index.Columns, name)
		}
	}
	return index, nil
}

func validateTableIndex(table model.Table, index model.Index) error {
	if index.Name == "" {
		return fmt.Errorf("index name must not be empty")
	}
	for _, group := range []struct {
		name    string
		columns []string
	}{{"key", index.Columns}, {"covering", index.DataColumns}} {
		seen := map[string]bool{}
		for _, name := range group.columns {
			if tableColumn(&table, name) == nil {
				return fmt.Errorf("index %q references unknown %s column %q", index.Name, group.name, name)
			}
			if seen[name] {
				return fmt.Errorf("index %q repeats %s column %q", index.Name, group.name, name)
			}
			if group.name == "covering" && (slices.Contains(index.Columns, name) || slices.Contains(table.PrimaryKey, name)) {
				return fmt.Errorf("index %q covering column %q is already part of the index key", index.Name, name)
			}
			seen[name] = true
		}
	}
	return nil
}

func catalogIndexPosition(table model.Table, name string) (int, bool) {
	for i := range table.Indexes {
		if table.Indexes[i].Name == name {
			return i, true
		}
	}
	return -1, false
}
