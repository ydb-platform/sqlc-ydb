package database

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/ydb-go-genproto/protos/Ydb"
	"github.com/ydb-platform/ydb-go-genproto/protos/Ydb_Operations"
	"github.com/ydb-platform/ydb-go-genproto/protos/Ydb_Table"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func indexedDescription() *Ydb_Table.DescribeTableResult {
	return &Ydb_Table.DescribeTableResult{
		Columns: []*Ydb_Table.ColumnMeta{
			{Name: "id", Type: primitive(Ydb.Type_UINT64)},
			{Name: "label", Type: optional(primitive(Ydb.Type_UTF8))},
			{Name: "payload", Type: optional(primitive(Ydb.Type_UTF8))},
		},
		PrimaryKey: []string{"id"},
		Indexes: []*Ydb_Table.TableIndexDescription{{
			Name: "by_label", IndexColumns: []string{"label", "id"}, DataColumns: []string{"payload"},
			Type: &Ydb_Table.TableIndexDescription_GlobalIndex{GlobalIndex: &Ydb_Table.GlobalIndex{}},
		}},
	}
}

func TestDescribeTablePreservesSecondaryIndexes(t *testing.T) {
	for _, async := range []bool{false, true} {
		for _, covering := range []bool{false, true} {
			for _, state := range []Ydb_Table.TableIndexDescription_Status{
				Ydb_Table.TableIndexDescription_STATUS_UNSPECIFIED,
				Ydb_Table.TableIndexDescription_STATUS_READY,
				Ydb_Table.TableIndexDescription_STATUS_BUILDING,
			} {
				description := indexedDescription()
				index := description.Indexes[0]
				kind := "GlobalSync"
				if async {
					index.Type = &Ydb_Table.TableIndexDescription_GlobalAsyncIndex{GlobalAsyncIndex: &Ydb_Table.GlobalAsyncIndex{}}
					kind = "GlobalAsync"
				}
				if !covering {
					index.DataColumns = nil
				}
				index.Status, index.SizeBytes = state, 12345
				client := testClient(t, tableServer{describe: func(_ context.Context, request *Ydb_Table.DescribeTableRequest) (*Ydb_Table.DescribeTableResponse, error) {
					assert.Equal(t, "/local/records", request.GetPath(), "DescribeTable path = %q", request.GetPath())
					result, err := anypb.New(description)
					if err != nil {
						return nil, err
					}
					return &Ydb_Table.DescribeTableResponse{Operation: &Ydb_Operations.Operation{Ready: true, Status: Ydb.StatusIds_SUCCESS, Result: result}}, nil
				}}, queryServer{})
				table, err := client.DescribeTable(context.Background(), "records")
				require.NoError(t, err)
				want := []model.Index{{Name: "by_label", Kind: kind, Columns: []string{"label", "id"}, DataColumns: index.DataColumns}}
				require.True(t, reflect.DeepEqual(table.Indexes, want), "async=%v covering=%v state=%v: indexes = %#v, want %#v", async, covering, state, table.Indexes, want)
			}
		}
	}
}

func TestDecodeTableRejectsUnsupportedAndInvalidIndexes(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Ydb_Table.DescribeTableResult)
		want string
	}{
		{"missing index", func(d *Ydb_Table.DescribeTableResult) { d.Indexes[0] = nil }, "empty or duplicate index name"},
		{"empty name", func(d *Ydb_Table.DescribeTableResult) { d.Indexes[0].Name = "" }, "empty or duplicate index name"},
		{"duplicate name", func(d *Ydb_Table.DescribeTableResult) {
			d.Indexes = append(d.Indexes, proto.Clone(d.Indexes[0]).(*Ydb_Table.TableIndexDescription))
		}, "empty or duplicate index name"},
		{"unspecified kind", func(d *Ydb_Table.DescribeTableResult) { d.Indexes[0].Type = nil }, "unsupported index type"},
		{"unique kind", func(d *Ydb_Table.DescribeTableResult) {
			d.Indexes[0].Type = &Ydb_Table.TableIndexDescription_GlobalUniqueIndex{GlobalUniqueIndex: &Ydb_Table.GlobalUniqueIndex{}}
		}, "unsupported index type"},
		{"no keys", func(d *Ydb_Table.DescribeTableResult) { d.Indexes[0].IndexColumns = nil }, "no key columns"},
		{"unknown key", func(d *Ydb_Table.DescribeTableResult) { d.Indexes[0].IndexColumns = []string{"missing"} }, "unknown or duplicate key column"},
		{"duplicate key", func(d *Ydb_Table.DescribeTableResult) { d.Indexes[0].IndexColumns = []string{"label", "label"} }, "unknown or duplicate key column"},
		{"unknown cover", func(d *Ydb_Table.DescribeTableResult) { d.Indexes[0].DataColumns = []string{"missing"} }, "unknown or duplicate covering column"},
		{"duplicate cover", func(d *Ydb_Table.DescribeTableResult) { d.Indexes[0].DataColumns = []string{"payload", "payload"} }, "unknown or duplicate covering column"},
		{"index key cover", func(d *Ydb_Table.DescribeTableResult) { d.Indexes[0].DataColumns = []string{"label"} }, "key column \"label\" cannot also be a covering column"},
		{"primary key cover", func(d *Ydb_Table.DescribeTableResult) {
			d.Indexes[0].IndexColumns = []string{"label"}
			d.Indexes[0].DataColumns = []string{"id"}
		}, "key column \"id\" cannot also be a covering column"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			description := indexedDescription()
			tc.edit(description)
			_, err := decodeTable("records", description)
			require.False(t, err == nil || !strings.Contains(err.Error(), tc.want), "error = %v, want %q", err, tc.want)
		})
	}
}
