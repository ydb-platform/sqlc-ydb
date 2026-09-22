package database

import (
	"strings"
	"testing"

	"github.com/ydb-platform/ydb-go-genproto/protos/Ydb"
	"github.com/ydb-platform/ydb-go-genproto/protos/Ydb_Table"
)

func primitive(id Ydb.Type_PrimitiveTypeId) *Ydb.Type {
	return &Ydb.Type{Type: &Ydb.Type_TypeId{TypeId: id}}
}
func optional(t *Ydb.Type) *Ydb.Type {
	return &Ydb.Type{Type: &Ydb.Type_OptionalType{OptionalType: &Ydb.OptionalType{Item: t}}}
}

func TestDecodeTypes(t *testing.T) {
	cases := []struct {
		name string
		typ  *Ydb.Type
		want string
	}{
		{"primitive", primitive(Ydb.Type_UINT64), "Uint64"},
		{"binary", primitive(Ydb.Type_STRING), "String"},
		{"uuid", primitive(Ydb.Type_UUID), "Uuid"},
		{"wide time", primitive(Ydb.Type_TIMESTAMP64), "Timestamp64"},
		{"nullable", optional(primitive(Ydb.Type_UTF8)), "Optional<Utf8>"},
		{"decimal", &Ydb.Type{Type: &Ydb.Type_DecimalType{DecimalType: &Ydb.DecimalType{Precision: 22, Scale: 9}}}, "Decimal(22,9)"},
		{"list", &Ydb.Type{Type: &Ydb.Type_ListType{ListType: &Ydb.ListType{Item: optional(primitive(Ydb.Type_INT32))}}}, "List<Optional<Int32>>"},
		{"tuple", &Ydb.Type{Type: &Ydb.Type_TupleType{TupleType: &Ydb.TupleType{Elements: []*Ydb.Type{primitive(Ydb.Type_UINT64), optional(primitive(Ydb.Type_UTF8))}}}}, "Tuple<Uint64,Optional<Utf8>>"},
		{"struct", &Ydb.Type{Type: &Ydb.Type_StructType{StructType: &Ydb.StructType{Members: []*Ydb.StructMember{{Name: "name", Type: primitive(Ydb.Type_UTF8)}}}}}, "Struct<`name`:Utf8>"},
		{"dict", &Ydb.Type{Type: &Ydb.Type_DictType{DictType: &Ydb.DictType{Key: primitive(Ydb.Type_UINT64), Payload: primitive(Ydb.Type_UTF8)}}}, "Dict<Uint64,Utf8>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeType(tc.typ)
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestDecodeTypeRejectsUnknownAndMalformedMetadata(t *testing.T) {
	cases := []*Ydb.Type{nil, {}, primitive(Ydb.Type_PrimitiveTypeId(99999)), {Type: &Ydb.Type_OptionalType{OptionalType: &Ydb.OptionalType{}}}, {Type: &Ydb.Type_DecimalType{DecimalType: &Ydb.DecimalType{Precision: 36, Scale: 2}}}, {Type: &Ydb.Type_PgType{PgType: &Ydb.PgType{TypeName: "int4"}}}, {Type: &Ydb.Type_TaggedType{TaggedType: &Ydb.TaggedType{Tag: "tag", Type: primitive(Ydb.Type_UINT64)}}}}
	for _, typ := range cases {
		if got, err := decodeType(typ); err == nil {
			t.Fatalf("accepted invalid type %v as %s", typ, got)
		}
	}
}

func TestDecodeTableRejectsLiteralDefaults(t *testing.T) {
	_, err := decodeTable("items", &Ydb_Table.DescribeTableResult{Columns: []*Ydb_Table.ColumnMeta{{Name: "id", Type: primitive(Ydb.Type_UINT64), DefaultValue: &Ydb_Table.ColumnMeta_FromLiteral{FromLiteral: &Ydb.TypedValue{}}}}})
	if err == nil || !strings.Contains(err.Error(), "literal column defaults") {
		t.Fatalf("got %v", err)
	}
}
