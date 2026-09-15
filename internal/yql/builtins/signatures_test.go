package builtins

import (
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestRegistryResolvesDigestSignatures(t *testing.T) {
	r, err := NewRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args []CallArgument
		want model.Type
	}{
		{"Digest::CityHash", []CallArgument{{Type: scalar("String")}}, scalar("Uint64")},
		{"Digest::CityHash", []CallArgument{{Type: model.Optional(scalar("String"))}}, model.Optional(scalar("Uint64"))},
		{"Digest::CityHash", []CallArgument{{Type: scalar("String")}, {Type: scalar("Uint64")}}, scalar("Uint64")},
		{"Digest::CityHash", []CallArgument{{Type: scalar("String")}, {Name: "Init", Type: model.Optional(scalar("Uint64"))}}, scalar("Uint64")},
		{"Digest::Md5Hex", []CallArgument{{Type: model.Optional(scalar("String"))}}, model.Optional(scalar("String"))},
		{"Digest::Sha256", []CallArgument{{Type: scalar("String")}}, scalar("String")},
		{"Digest::NumericHash", []CallArgument{{Type: model.Optional(scalar("Uint64"))}}, model.Optional(scalar("Uint64"))},
	} {
		got, err := r.ResolveCall(tc.name, tc.args)
		if err != nil {
			t.Fatalf("ResolveCall(%s): %v", tc.name, err)
		}
		if !got.Equal(tc.want) {
			t.Fatalf("ResolveCall(%s) = %s, want %s", tc.name, got.String(), tc.want.String())
		}
	}
}

func TestRegistryRejectsInvalidDigestCalls(t *testing.T) {
	r, _ := NewRegistry(nil)
	for _, tc := range []struct {
		name string
		args []CallArgument
		want string
	}{
		{"Digest::CityHash", nil, "expects"},
		{"Digest::CityHash", []CallArgument{{Type: scalar("Utf8")}}, "argument 1"},
		{"Digest::CityHash", []CallArgument{{Type: scalar("String")}, {Name: "seed", Type: scalar("Uint64")}}, "unknown named argument"},
		{"Digest::CityHash", []CallArgument{{Name: "Init", Type: scalar("Uint64")}, {Type: scalar("String")}}, "positional argument"},
	} {
		_, err := r.ResolveCall(tc.name, tc.args)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("ResolveCall(%s) error = %v, want %q", tc.name, err, tc.want)
		}
	}
}

func TestRegistryResolvesCustomConcreteSignature(t *testing.T) {
	r, err := NewRegistry([]Signature{{
		Name: "Acme::Score",
		Arguments: []Parameter{
			{Name: "value", Type: scalar("Utf8"), AutoMap: true},
			{Name: "mode", Type: scalar("Uint32"), Optional: true},
		},
		Returns: scalar("Double"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.ResolveCall("Acme::Score", []CallArgument{{Type: model.Optional(scalar("Utf8"))}, {Name: "mode", Type: scalar("Uint32")}})
	if err != nil {
		t.Fatal(err)
	}
	if want := model.Optional(scalar("Double")); !got.Equal(want) {
		t.Fatalf("got %s, want %s", got.String(), want.String())
	}
}

func TestRegistryAutoMapWrapsConcreteContainerResult(t *testing.T) {
	item := scalar("String")
	result := model.Type{Kind: "List", Elem: &item}
	r, err := NewRegistry([]Signature{{
		Name:      "Acme::Words",
		Arguments: []Parameter{{Type: scalar("Json"), AutoMap: true}},
		Returns:   result,
	}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.ResolveCall("Acme::Words", []CallArgument{{Type: model.Optional(scalar("Json"))}})
	if err != nil {
		t.Fatal(err)
	}
	if want := model.Optional(result); !got.Equal(want) {
		t.Fatalf("got %s, want %s", got.String(), want.String())
	}
}

func TestRegistryResolveCallDiagnostics(t *testing.T) {
	stringType := scalar("String")
	uint64Type := scalar("Uint64")
	r, err := NewRegistry([]Signature{
		{Name: "Acme::Pick", Arguments: []Parameter{{Name: "value", Type: model.Optional(stringType)}}, Returns: stringType},
		{Name: "Acme::Pick", Arguments: []Parameter{{Name: "value", Type: model.Optional(uint64Type)}}, Returns: uint64Type},
		{Name: "Acme::Strict", Arguments: []Parameter{{Name: "value", Type: stringType}}, Returns: stringType},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args []CallArgument
		want string
	}{
		{"Acme::Pick", []CallArgument{{Type: scalar("Null")}}, "ambiguous"},
		{"Acme::Pick", []CallArgument{{Type: scalar("Bool")}}, "no matching overload"},
		{"Acme::Strict", []CallArgument{{Type: stringType}, {Type: stringType}}, "at most 1"},
		{"Acme::Strict", []CallArgument{{Type: stringType}, {Name: "value", Type: stringType}}, "more than once"},
		{"Acme::Strict", []CallArgument{{Type: model.Optional(stringType)}}, "must be String"},
		{"Acme::Strict", []CallArgument{{Type: model.Type{Kind: "Optional"}}}, "element type"},
	} {
		_, err := r.ResolveCall(tc.name, tc.args)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ResolveCall(%s) error = %v, want %q", tc.name, err, tc.want)
		}
	}
	var nilRegistry *Registry
	if _, err := nilRegistry.ResolveCall("ABS", []CallArgument{{Name: "value", Type: scalar("Int32")}}); err == nil || !strings.Contains(err.Error(), "does not support named") {
		t.Fatalf("nil registry named argument error = %v", err)
	}
}

func TestRegistryValidatesNestedConcreteSignatures(t *testing.T) {
	stringType := scalar("String")
	uint64Type := scalar("Uint64")
	valueType := scalar("Bool")
	valid := Signature{
		Name:      "Acme::Nested",
		Arguments: []Parameter{{Type: model.Type{Kind: "Dict", Key: &stringType, Elem: &uint64Type}}},
		Returns:   model.Type{Kind: "Struct", Fields: []model.StructField{{Name: "items", Type: model.Type{Kind: "List", Elem: &valueType}}}},
	}
	if _, err := NewRegistry([]Signature{valid}); err != nil {
		t.Fatal(err)
	}
	invalidKey := valid
	invalidKey.Arguments = []Parameter{{Type: model.Type{Kind: "Dict", Key: typePointer(scalar("Null")), Elem: &uint64Type}}}
	if _, err := NewRegistry([]Signature{invalidKey}); err == nil || !strings.Contains(err.Error(), "Null") {
		t.Fatalf("nested Dict Null error = %v", err)
	}
	invalidField := valid
	invalidField.Returns = model.Type{Kind: "Struct", Fields: []model.StructField{{Name: "bad", Type: scalar("Null")}}}
	if _, err := NewRegistry([]Signature{invalidField}); err == nil || !strings.Contains(err.Error(), "Null") {
		t.Fatalf("nested Struct Null error = %v", err)
	}
}

func TestRegistryRejectsInvalidDictionaryKeysRecursively(t *testing.T) {
	boolType := scalar("Bool")
	jsonType := scalar("Json")
	jsonDocumentType := scalar("JsonDocument")
	stringType := scalar("String")
	decimalType := model.Type{Kind: "Decimal", Precision: 22, Scale: 9}
	nestedOptional := model.Optional(model.Optional(stringType))
	for _, tc := range []struct {
		name      string
		typeValue model.Type
		want      string
	}{
		{"top-level Json key", model.Type{Kind: "Dict", Key: &jsonType, Elem: &boolType}, "dictionary key"},
		{"top-level JsonDocument key", model.Type{Kind: "Dict", Key: &jsonDocumentType, Elem: &boolType}, "dictionary key"},
		{"Dict nested in List", model.Type{Kind: "List", Elem: &model.Type{Kind: "Dict", Key: &jsonType, Elem: &boolType}}, "dictionary key"},
		{"Dict nested in Struct", model.Type{Kind: "Struct", Fields: []model.StructField{{Name: "lookup", Type: model.Type{Kind: "Dict", Key: &jsonType, Elem: &boolType}}}}, "dictionary key"},
		{"nested Optional key", model.Type{Kind: "Dict", Key: &nestedOptional, Elem: &boolType}, "nested Optional"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewRegistry([]Signature{{Name: "Acme::Lookup", Arguments: []Parameter{{Type: tc.typeValue}}, Returns: boolType}})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("NewRegistry() error = %v, want %q", err, tc.want)
			}
		})
	}
	optionalString := model.Optional(stringType)
	tupleKey := model.Type{Kind: "Tuple", Items: []model.Type{optionalString, scalar("Uint64")}}
	optionalTupleKey := model.Optional(tupleKey)
	nestedTupleKey := model.Type{Kind: "Tuple", Items: []model.Type{tupleKey, scalar("Uint64")}}
	for _, key := range []model.Type{optionalString, tupleKey, optionalTupleKey, nestedTupleKey, decimalType} {
		if _, err := NewRegistry([]Signature{{Name: "Acme::Lookup", Arguments: []Parameter{{Type: model.Type{Kind: "Dict", Key: &key, Elem: &boolType}}}, Returns: boolType}}); err != nil {
			t.Errorf("valid dictionary key %s rejected: %v", key.String(), err)
		}
	}
}

func TestResolveDigestSignatureGroups(t *testing.T) {
	for _, tc := range []struct {
		name string
		arg  model.Type
		want string
	}{
		{"Digest::Crc64", scalar("String"), "Uint64"},
		{"Digest::Fnv32", scalar("String"), "Uint32"},
		{"Digest::Crc32c", scalar("String"), "Uint32"},
		{"Digest::Md5Raw", scalar("String"), "String"},
		{"Digest::XXH3", scalar("String"), "Uint64"},
		{"Digest::IntHash64", scalar("Uint64"), "Uint64"},
	} {
		got, err := Resolve(tc.name, []model.Type{tc.arg})
		if err != nil || got.Kind != tc.want {
			t.Errorf("Resolve(%s) = %s, %v; want %s", tc.name, got.String(), err, tc.want)
		}
	}
}

func TestRegistryValidatesCustomSignatures(t *testing.T) {
	valid := Signature{Name: "Acme::Hash", Arguments: []Parameter{{Name: "value", Type: scalar("String")}}, Returns: scalar("Uint64")}
	for _, tc := range []struct {
		name string
		sigs []Signature
		want string
	}{
		{"unknown argument type", []Signature{{Name: "Acme::Hash", Arguments: []Parameter{{Type: scalar("Mystery")}}, Returns: scalar("Uint64")}}, "argument 1"},
		{"unknown return type", []Signature{{Name: "Acme::Hash", Returns: scalar("Mystery")}}, "return type"},
		{"null nested in argument", []Signature{{Name: "Acme::Hash", Arguments: []Parameter{{Type: model.Type{Kind: "List", Elem: typePointer(scalar("Null"))}}}, Returns: scalar("Uint64")}}, "Null"},
		{"null nested in result", []Signature{{Name: "Acme::Hash", Returns: model.Type{Kind: "Tuple", Items: []model.Type{scalar("Uint64"), scalar("Null")}}}}, "Null"},
		{"nested optional argument", []Signature{{Name: "Acme::Hash", Arguments: []Parameter{{Type: model.Optional(model.Optional(scalar("Uint64")))}}, Returns: scalar("Uint64")}}, "nested Optional"},
		{"nested optional result", []Signature{{Name: "Acme::Hash", Returns: model.Type{Kind: "List", Elem: typePointer(model.Optional(model.Optional(scalar("Uint64"))))}}}, "nested Optional"},
		{"empty Struct result", []Signature{{Name: "Acme::Hash", Returns: model.Type{Kind: "Struct"}}}, "at least one field"},
		{"unnamed Struct field", []Signature{{Name: "Acme::Hash", Returns: model.Type{Kind: "Struct", Fields: []model.StructField{{Type: scalar("String")}}}}}, "field name must not be empty"},
		{"duplicate Struct field", []Signature{{Name: "Acme::Hash", Returns: model.Type{Kind: "Struct", Fields: []model.StructField{{Name: "value", Type: scalar("String")}, {Name: "value", Type: scalar("Uint64")}}}}}, "duplicate Struct field"},
		{"malformed Struct result", []Signature{{Name: "Acme::Hash", Returns: model.Type{Kind: "Struct", Elem: typePointer(scalar("String")), Fields: []model.StructField{{Name: "value", Type: scalar("String")}}}}}, "unexpected type parameters"},
		{"invalid function name", []Signature{{Name: "Acme::bad-name", Returns: scalar("Uint64")}}, "function name"},
		{"invalid argument name", []Signature{{Name: "Acme::Hash", Arguments: []Parameter{{Name: "bad-name", Type: scalar("String")}}, Returns: scalar("Uint64")}}, "argument 1 name"},
		{"duplicate argument name", []Signature{{Name: "Acme::Hash", Arguments: []Parameter{{Name: "value", Type: scalar("String")}, {Name: "value", Type: scalar("String")}}, Returns: scalar("Uint64")}}, "duplicate argument"},
		{"required after optional", []Signature{{Name: "Acme::Hash", Arguments: []Parameter{{Type: scalar("String"), Optional: true}, {Type: scalar("Uint64")}}, Returns: scalar("Uint64")}}, "required argument"},
		{"automap optional parameter", []Signature{{Name: "Acme::Hash", Arguments: []Parameter{{Type: model.Optional(scalar("String")), AutoMap: true}}, Returns: scalar("Uint64")}}, "AutoMap"},
		{"duplicate overload", []Signature{valid, valid}, "ambiguous duplicate"},
		{"overlapping optional overload", []Signature{valid, {Name: "Acme::Hash", Arguments: []Parameter{{Name: "value", Type: scalar("String")}, {Name: "seed", Type: scalar("Uint64"), Optional: true}}, Returns: scalar("Uint64")}}, "ambiguous overloads"},
		{"known builtin conflict", []Signature{{Name: "Digest::CityHash", Arguments: []Parameter{{Type: scalar("String")}}, Returns: scalar("Uint64")}}, "built-in"},
		{"known core conflict is case insensitive", []Signature{{Name: "coalesce", Returns: scalar("Uint64")}}, "built-in"},
		{"known library conflict", []Signature{{Name: "Yson::ConvertToStringList", Returns: scalar("Uint64")}}, "built-in"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewRegistry(tc.sigs)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("NewRegistry() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestRegistryAllowsWrongCaseLibraryName(t *testing.T) {
	if _, err := NewRegistry([]Signature{{Name: "yson::ConvertToStringList", Returns: scalar("Uint64")}}); err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
}

func TestLegacyResolveIncludesDigestCatalog(t *testing.T) {
	got, err := Resolve("Digest::CityHash", []model.Type{scalar("String")})
	if err != nil || !got.Equal(scalar("Uint64")) {
		t.Fatalf("Resolve() = %s, %v", got.String(), err)
	}
}

func TestLegacyResolveDoesNotObserveCustomRegistry(t *testing.T) {
	r, err := NewRegistry([]Signature{{Name: "Acme::OnlyHere", Returns: scalar("Uint64")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ResolveCall("Acme::OnlyHere", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve("Acme::OnlyHere", nil); err == nil || !strings.Contains(err.Error(), "unsupported YQL function") {
		t.Fatalf("Resolve observed custom registry: %v", err)
	}
}
