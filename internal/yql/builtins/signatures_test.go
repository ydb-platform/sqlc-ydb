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

func TestRegistryValidatesCustomSignatures(t *testing.T) {
	valid := Signature{Name: "Acme::Hash", Arguments: []Parameter{{Name: "value", Type: scalar("String")}}, Returns: scalar("Uint64")}
	for _, tc := range []struct {
		name string
		sigs []Signature
		want string
	}{
		{"unknown argument type", []Signature{{Name: "Acme::Hash", Arguments: []Parameter{{Type: scalar("Mystery")}}, Returns: scalar("Uint64")}}, "argument 1"},
		{"unknown return type", []Signature{{Name: "Acme::Hash", Returns: scalar("Mystery")}}, "return type"},
		{"invalid function name", []Signature{{Name: "Acme::bad-name", Returns: scalar("Uint64")}}, "function name"},
		{"invalid argument name", []Signature{{Name: "Acme::Hash", Arguments: []Parameter{{Name: "bad-name", Type: scalar("String")}}, Returns: scalar("Uint64")}}, "argument 1 name"},
		{"duplicate argument name", []Signature{{Name: "Acme::Hash", Arguments: []Parameter{{Name: "value", Type: scalar("String")}, {Name: "value", Type: scalar("String")}}, Returns: scalar("Uint64")}}, "duplicate argument"},
		{"required after optional", []Signature{{Name: "Acme::Hash", Arguments: []Parameter{{Type: scalar("String"), Optional: true}, {Type: scalar("Uint64")}}, Returns: scalar("Uint64")}}, "required argument"},
		{"automap optional parameter", []Signature{{Name: "Acme::Hash", Arguments: []Parameter{{Type: model.Optional(scalar("String")), AutoMap: true}}, Returns: scalar("Uint64")}}, "AutoMap"},
		{"duplicate overload", []Signature{valid, valid}, "ambiguous duplicate"},
		{"overlapping optional overload", []Signature{valid, {Name: "Acme::Hash", Arguments: []Parameter{{Name: "value", Type: scalar("String")}, {Name: "seed", Type: scalar("Uint64"), Optional: true}}, Returns: scalar("Uint64")}}, "ambiguous overloads"},
		{"known builtin conflict", []Signature{{Name: "Digest::CityHash", Arguments: []Parameter{{Type: scalar("String")}}, Returns: scalar("Uint64")}}, "built-in"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewRegistry(tc.sigs)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("NewRegistry() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestLegacyResolveIncludesDigestCatalog(t *testing.T) {
	got, err := Resolve("Digest::CityHash", []model.Type{scalar("String")})
	if err != nil || !got.Equal(scalar("Uint64")) {
		t.Fatalf("Resolve() = %s, %v", got.String(), err)
	}
}
