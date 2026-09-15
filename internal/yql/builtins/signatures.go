package builtins

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

// CallArgument is a resolved positional or named function argument.
type CallArgument struct {
	Name string
	Type model.Type
}

// Parameter is one concrete function parameter. Optional means the argument
// may be omitted; AutoMap propagates an optional input to the result.
type Parameter struct {
	Name     string
	Type     model.Type
	Optional bool
	AutoMap  bool
}

// Signature is an offline type contract for one function overload.
type Signature struct {
	Name      string
	Arguments []Parameter
	Returns   model.Type
}

// Registry combines the shipped, verified signatures with user-provided
// concrete contracts.
type Registry struct {
	custom map[string][]Signature
}

// defaultRegistry has no mutable custom catalog. ResolveCall only reads it, so
// package-level Resolve calls can safely share the instance.
var defaultRegistry = &Registry{}

// NewRegistry validates user-provided signatures before any query is analyzed.
func NewRegistry(custom []Signature) (*Registry, error) {
	r := &Registry{custom: make(map[string][]Signature)}
	seen := make(map[string]struct{})
	for i, signature := range custom {
		if isKnownFunction(signature.Name) {
			return nil, fmt.Errorf("function signature %d %q conflicts with a known built-in function", i+1, signature.Name)
		}
		if err := validateSignature(signature); err != nil {
			return nil, fmt.Errorf("function signature %d %q: %w", i+1, signature.Name, err)
		}
		key := signatureShape(signature)
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("function signature %d %q is an ambiguous duplicate overload", i+1, signature.Name)
		}
		for _, other := range r.custom[signature.Name] {
			if signaturesOverlap(other, signature) {
				return nil, fmt.Errorf("function signature %d %q creates ambiguous overloads", i+1, signature.Name)
			}
		}
		seen[key] = struct{}{}
		r.custom[signature.Name] = append(r.custom[signature.Name], signature)
	}
	return r, nil
}

// ResolveCall validates named/positional arguments and returns one concrete
// result type. Library and user function names are case-sensitive.
func (r *Registry) ResolveCall(name string, args []CallArgument) (model.Type, error) {
	if signatures := standardSignatures(name); len(signatures) != 0 {
		return resolveSignatures(name, args, signatures)
	}
	if r != nil {
		if signatures := r.custom[name]; len(signatures) != 0 {
			return resolveSignatures(name, args, signatures)
		}
	}
	plain := make([]model.Type, len(args))
	for i, argument := range args {
		if argument.Name != "" {
			return model.Type{}, fmt.Errorf("%s does not support named argument %q in the offline resolver", name, argument.Name)
		}
		plain[i] = argument.Type
	}
	return resolveLegacy(name, plain)
}

func validateSignature(signature Signature) error {
	if !IsFunctionIdentifier(signature.Name) {
		return fmt.Errorf("function name must be a non-empty YQL identifier")
	}
	if err := validateSignatureType(signature.Returns, false); err != nil {
		return fmt.Errorf("invalid return type %s: %w", signature.Returns.String(), err)
	}
	seenNames := make(map[string]struct{})
	optionalSeen := false
	for i, parameter := range signature.Arguments {
		if err := validateSignatureType(parameter.Type, false); err != nil {
			return fmt.Errorf("argument %d has invalid type %s: %w", i+1, parameter.Type.String(), err)
		}
		if parameter.Name != "" {
			if !IsParameterIdentifier(parameter.Name) {
				return fmt.Errorf("argument %d name must be a YQL identifier", i+1)
			}
			if _, exists := seenNames[parameter.Name]; exists {
				return fmt.Errorf("duplicate argument name %q", parameter.Name)
			}
			seenNames[parameter.Name] = struct{}{}
		}
		if parameter.AutoMap && parameter.Type.IsOptional() {
			return fmt.Errorf("argument %d cannot combine AutoMap with an Optional parameter type; declare the base type", i+1)
		}
		if optionalSeen && !parameter.Optional {
			return fmt.Errorf("required argument %d follows an optional argument", i+1)
		}
		optionalSeen = optionalSeen || parameter.Optional
	}
	return nil
}

func validateSignatureType(value model.Type, parentOptional bool) error {
	if value.Kind == "Null" {
		return fmt.Errorf("Null is not allowed in a configured signature")
	}
	if value.Kind == "Optional" {
		if parentOptional {
			return fmt.Errorf("nested Optional is not allowed in a configured signature")
		}
		if value.Elem == nil {
			return fmt.Errorf("Optional type has no element type")
		}
		if err := validateSignatureType(*value.Elem, true); err != nil {
			return err
		}
		return validateConcreteOrNull(value)
	}
	if value.Kind == "Dict" && value.Key != nil {
		if err := validateDictionaryKey(*value.Key); err != nil {
			return err
		}
	}
	if value.Key != nil {
		if err := validateSignatureType(*value.Key, false); err != nil {
			return err
		}
	}
	if value.Elem != nil {
		if err := validateSignatureType(*value.Elem, false); err != nil {
			return err
		}
	}
	for _, item := range value.Items {
		if err := validateSignatureType(item, false); err != nil {
			return err
		}
	}
	for _, field := range value.Fields {
		if err := validateSignatureType(field.Type, false); err != nil {
			return err
		}
	}
	return validateConcreteOrNull(value)
}

var signatureIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(?:::[A-Za-z_][A-Za-z0-9_]*)*$`)
var parameterIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// IsFunctionIdentifier reports whether value is a qualified YQL function name.
func IsFunctionIdentifier(value string) bool { return signatureIdentifier.MatchString(value) }

// IsParameterIdentifier reports whether value is a YQL parameter name.
func IsParameterIdentifier(value string) bool { return parameterIdentifier.MatchString(value) }

func signaturesOverlap(left, right Signature) bool {
	maximum := len(left.Arguments)
	if len(right.Arguments) < maximum {
		maximum = len(right.Arguments)
	}
	// A call has a positional prefix followed by named arguments. Try every
	// possible prefix; optional suffix arguments can be omitted.
	for prefix := 0; prefix <= maximum; prefix++ {
		if prefix > 0 && !parametersOverlap(left.Arguments[prefix-1], right.Arguments[prefix-1]) {
			return false
		}
		if requiredNamesOverlap(left.Arguments, right.Arguments, prefix) && requiredNamesOverlap(right.Arguments, left.Arguments, prefix) {
			return true
		}
	}
	return false
}

func requiredNamesOverlap(left, right []Parameter, prefix int) bool {
	for _, parameter := range left[prefix:] {
		if parameter.Optional {
			continue
		}
		index := parameterIndex(right, parameter.Name)
		if parameter.Name == "" || index < prefix || !parametersOverlap(parameter, right[index]) {
			return false
		}
	}
	return true
}

func parametersOverlap(left, right Parameter) bool {
	leftBase, _, _ := baseType(left.Type)
	rightBase, _, _ := baseType(right.Type)
	return leftBase.Equal(rightBase)
}

func signatureShape(signature Signature) string {
	var b strings.Builder
	b.WriteString(signature.Name)
	for _, parameter := range signature.Arguments {
		fmt.Fprintf(&b, "|%s:%s:%t:%t", parameter.Name, parameter.Type.String(), parameter.Optional, parameter.AutoMap)
	}
	return b.String()
}

func resolveSignatures(name string, args []CallArgument, signatures []Signature) (model.Type, error) {
	var matches []model.Type
	var failures []string
	for _, signature := range signatures {
		result, err := matchSignature(args, signature)
		if err == nil {
			matches = append(matches, result)
		} else {
			failures = append(failures, err.Error())
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return model.Type{}, fmt.Errorf("%s call is ambiguous between %d configured overloads", name, len(matches))
	}
	if len(failures) == 1 {
		return model.Type{}, fmt.Errorf("%s: %s", name, failures[0])
	}
	return model.Type{}, fmt.Errorf("%s has no matching overload: %s", name, strings.Join(failures, "; "))
}

func matchSignature(args []CallArgument, signature Signature) (model.Type, error) {
	bound := make([]*CallArgument, len(signature.Arguments))
	positional := 0
	namedSeen := false
	for i := range args {
		argument := &args[i]
		if argument.Name == "" {
			if namedSeen {
				return model.Type{}, fmt.Errorf("positional argument %d follows a named argument", i+1)
			}
			if positional >= len(bound) {
				return model.Type{}, fmt.Errorf("expects at most %d arguments, got %d", len(bound), len(args))
			}
			bound[positional] = argument
			positional++
			continue
		}
		namedSeen = true
		index := parameterIndex(signature.Arguments, argument.Name)
		if index < 0 {
			return model.Type{}, fmt.Errorf("unknown named argument %q", argument.Name)
		}
		if bound[index] != nil {
			return model.Type{}, fmt.Errorf("argument %q is specified more than once", argument.Name)
		}
		bound[index] = argument
	}
	nullable := false
	for i, parameter := range signature.Arguments {
		if bound[i] == nil {
			if !parameter.Optional {
				return model.Type{}, fmt.Errorf("expects required argument %d%s", i+1, parameterLabel(parameter))
			}
			continue
		}
		propagate, err := matchParameter(bound[i].Type, parameter)
		if err != nil {
			return model.Type{}, fmt.Errorf("argument %d%s: %w", i+1, parameterLabel(parameter), err)
		}
		nullable = nullable || propagate
	}
	if nullable && !signature.Returns.IsOptional() {
		return model.Optional(signature.Returns), nil
	}
	return signature.Returns, nil
}

func matchParameter(actual model.Type, parameter Parameter) (bool, error) {
	expectedBase, expectedOptional, err := baseType(parameter.Type)
	if err != nil {
		return false, err
	}
	actualBase, actualOptional, err := baseType(actual)
	if err != nil {
		return false, err
	}
	if actualBase.Kind == "Null" && (parameter.AutoMap || expectedOptional) {
		return parameter.AutoMap, nil
	}
	if !actualBase.Equal(expectedBase) {
		return false, fmt.Errorf("must be %s, got %s", parameter.Type.String(), actual.String())
	}
	if actualOptional && !parameter.AutoMap && !expectedOptional {
		return false, fmt.Errorf("must be %s, got %s", parameter.Type.String(), actual.String())
	}
	return parameter.AutoMap && actualOptional, nil
}

func parameterIndex(parameters []Parameter, name string) int {
	for i, parameter := range parameters {
		if parameter.Name == name {
			return i
		}
	}
	return -1
}

func parameterLabel(parameter Parameter) string {
	if parameter.Name == "" {
		return ""
	}
	return " (" + parameter.Name + ")"
}

func standardSignatures(name string) []Signature {
	stringAutoMap := func(result string) Signature {
		return Signature{Name: name, Arguments: []Parameter{{Type: model.Type{Kind: "String"}, AutoMap: true}}, Returns: model.Type{Kind: result}}
	}
	seeded := func(seed, result string) Signature {
		return Signature{Name: name, Arguments: []Parameter{{Type: model.Type{Kind: "String"}, AutoMap: true}, {Name: "Init", Type: model.Optional(model.Type{Kind: seed}), Optional: true}}, Returns: model.Type{Kind: result}}
	}
	switch name {
	case "Digest::CityHash", "Digest::Crc64", "Digest::Fnv64", "Digest::MurMurHash", "Digest::MurMurHash2A":
		return []Signature{seeded("Uint64", "Uint64")}
	case "Digest::Fnv32", "Digest::MurMurHash32", "Digest::MurMurHash2A32":
		return []Signature{seeded("Uint32", "Uint32")}
	case "Digest::Crc32c", "Digest::FarmHashFingerprint32", "Digest::SuperFastHash":
		return []Signature{stringAutoMap("Uint32")}
	case "Digest::Md5Hex", "Digest::Md5Raw", "Digest::Sha1", "Digest::Sha256":
		return []Signature{stringAutoMap("String")}
	case "Digest::Md5HalfMix", "Digest::FarmHashFingerprint64", "Digest::XXH3":
		return []Signature{stringAutoMap("Uint64")}
	case "Digest::NumericHash", "Digest::FarmHashFingerprint", "Digest::IntHash64":
		return []Signature{{Name: name, Arguments: []Parameter{{Type: model.Type{Kind: "Uint64"}, AutoMap: true}}, Returns: model.Type{Kind: "Uint64"}}}
	default:
		return nil
	}
}

func isKnownFunction(name string) bool {
	return len(standardSignatures(name)) != 0 || lookupCore(name) != nil || lookupLibrary(name) != nil
}
