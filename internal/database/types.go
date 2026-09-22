package database

import (
	"fmt"

	"github.com/ydb-platform/ydb-go-genproto/protos/Ydb"
	"github.com/ydb-platform/ydb-go-genproto/protos/Ydb_Table"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func decodeTable(name string, description *Ydb_Table.DescribeTableResult) (model.Table, error) {
	table := model.Table{Name: name, PrimaryKey: append([]string(nil), description.GetPrimaryKey()...)}
	if len(description.GetColumns()) == 0 {
		return table, fmt.Errorf("server returned no columns")
	}
	names := make(map[string]bool, len(description.GetColumns()))
	for _, column := range description.GetColumns() {
		if column.GetName() == "" || names[column.GetName()] {
			return table, fmt.Errorf("server returned an empty or duplicate column name %q", column.GetName())
		}
		names[column.GetName()] = true
		typ, err := decodeType(column.GetType())
		if err != nil {
			return table, fmt.Errorf("column %q: %w", column.GetName(), err)
		}
		if column.NotNull != nil && column.GetNotNull() == typ.IsOptional() {
			return table, fmt.Errorf("column %q: server returned inconsistent type and not_null metadata", column.GetName())
		}
		sequence := false
		switch value := column.GetDefaultValue().(type) {
		case nil:
		case *Ydb_Table.ColumnMeta_FromSequence:
			if value.FromSequence == nil || value.FromSequence.GetName() == "" {
				return table, fmt.Errorf("column %q: server returned an invalid sequence default", column.GetName())
			}
			sequence = true
		default:
			return table, fmt.Errorf("column %q: literal column defaults are not supported by database analysis", column.GetName())
		}
		table.Columns = append(table.Columns, model.Column{Name: column.GetName(), Table: name, Type: typ, SequenceGenerated: sequence})
	}
	keys := make(map[string]bool, len(table.PrimaryKey))
	for _, key := range table.PrimaryKey {
		if !names[key] || keys[key] {
			return table, fmt.Errorf("server returned an unknown or duplicate primary key column %q", key)
		}
		keys[key] = true
	}
	return table, nil
}

func decodeType(typ *Ydb.Type) (model.Type, error) {
	if typ == nil {
		return model.Type{}, fmt.Errorf("server returned no type")
	}
	switch value := typ.GetType().(type) {
	case *Ydb.Type_TypeId:
		kind, ok := primitiveNames[value.TypeId]
		if !ok {
			return model.Type{}, fmt.Errorf("unsupported YDB primitive type %s (%d)", value.TypeId, value.TypeId)
		}
		return model.Type{Kind: kind}, nil
	case *Ydb.Type_DecimalType:
		precision, scale := value.DecimalType.GetPrecision(), value.DecimalType.GetScale()
		if precision < 1 || precision > 35 || scale > precision {
			return model.Type{}, fmt.Errorf("invalid Decimal(%d,%d) metadata", precision, scale)
		}
		return model.Type{Kind: "Decimal", Precision: int(precision), Scale: int(scale)}, nil
	case *Ydb.Type_OptionalType:
		elem, err := decodeType(value.OptionalType.GetItem())
		return model.Optional(elem), err
	case *Ydb.Type_ListType:
		elem, err := decodeType(value.ListType.GetItem())
		return model.Type{Kind: "List", Elem: &elem}, err
	case *Ydb.Type_TupleType:
		result := model.Type{Kind: "Tuple"}
		for _, element := range value.TupleType.GetElements() {
			t, err := decodeType(element)
			if err != nil {
				return model.Type{}, err
			}
			result.Items = append(result.Items, t)
		}
		return result, nil
	case *Ydb.Type_StructType:
		result := model.Type{Kind: "Struct"}
		names := make(map[string]bool)
		for _, member := range value.StructType.GetMembers() {
			if member.GetName() == "" || names[member.GetName()] {
				return model.Type{}, fmt.Errorf("empty or duplicate Struct member %q", member.GetName())
			}
			names[member.GetName()] = true
			t, err := decodeType(member.GetType())
			if err != nil {
				return model.Type{}, fmt.Errorf("Struct member %q: %w", member.GetName(), err)
			}
			result.Fields = append(result.Fields, model.StructField{Name: member.GetName(), Type: t})
		}
		return result, nil
	case *Ydb.Type_DictType:
		key, err := decodeType(value.DictType.GetKey())
		if err != nil {
			return model.Type{}, err
		}
		elem, err := decodeType(value.DictType.GetPayload())
		return model.Type{Kind: "Dict", Key: &key, Elem: &elem}, err
	default:
		return model.Type{}, fmt.Errorf("unsupported YDB type %T; use a supported YQL table-column type", typ.GetType())
	}
}

var primitiveNames = map[Ydb.Type_PrimitiveTypeId]string{
	Ydb.Type_BOOL: "Bool", Ydb.Type_INT8: "Int8", Ydb.Type_UINT8: "Uint8", Ydb.Type_INT16: "Int16", Ydb.Type_UINT16: "Uint16",
	Ydb.Type_INT32: "Int32", Ydb.Type_UINT32: "Uint32", Ydb.Type_INT64: "Int64", Ydb.Type_UINT64: "Uint64",
	Ydb.Type_FLOAT: "Float", Ydb.Type_DOUBLE: "Double", Ydb.Type_DATE: "Date", Ydb.Type_DATETIME: "Datetime",
	Ydb.Type_TIMESTAMP: "Timestamp", Ydb.Type_INTERVAL: "Interval", Ydb.Type_TZ_DATE: "TzDate",
	Ydb.Type_TZ_DATETIME: "TzDatetime", Ydb.Type_TZ_TIMESTAMP: "TzTimestamp", Ydb.Type_DATE32: "Date32",
	Ydb.Type_DATETIME64: "Datetime64", Ydb.Type_TIMESTAMP64: "Timestamp64", Ydb.Type_INTERVAL64: "Interval64",
	Ydb.Type_STRING: "String", Ydb.Type_UTF8: "Utf8", Ydb.Type_YSON: "Yson", Ydb.Type_JSON: "Json",
	Ydb.Type_UUID: "Uuid", Ydb.Type_JSON_DOCUMENT: "JsonDocument", Ydb.Type_DYNUMBER: "DyNumber",
}
