package builtins

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

const roaringBitmapResource = "Resource<roaring_bitmap>"

func lookupSpecializedUDF(name string) functionResolver {
	switch name {
	case "Knn::ToBinaryStringFloat", "Knn::ToBinaryStringUint8", "Knn::ToBinaryStringInt8",
		"Knn::ToBinaryStringBit", "Knn::FloatFromBinaryString", "Knn::InnerProductSimilarity", "Knn::CosineSimilarity",
		"Knn::CosineDistance", "Knn::ManhattanDistance", "Knn::EuclideanDistance":
		return func(args []model.Type) (model.Type, error) { return resolveKnn(name, args) }
	case "Roaring::And", "Roaring::AndNot", "Roaring::AndNotWithBinary", "Roaring::AndWithBinary",
		"Roaring::Cardinality", "Roaring::Deserialize", "Roaring::FromUint32List", "Roaring::Or",
		"Roaring::OrWithBinary", "Roaring::RunOptimize", "Roaring::Serialize", "Roaring::Uint32List":
		return func(args []model.Type) (model.Type, error) { return resolveRoaring(name, args) }
	default:
		if signatures := histogramSignatures(name); len(signatures) != 0 {
			return func(args []model.Type) (model.Type, error) {
				callArgs := make([]CallArgument, len(args))
				for i := range args {
					callArgs[i].Type = args[i]
				}
				return resolveSignatures(name, callArgs, signatures)
			}
		}
		if regexResult(name).Kind != "" {
			return func(args []model.Type) (model.Type, error) { return resolveRegexConstructor(name, args) }
		}
		return nil
	}
}

func resolveSpecializedCall(name string, args []CallArgument) (model.Type, bool, error) {
	if signatures := histogramSignatures(name); len(signatures) != 0 {
		result, err := resolveSignatures(name, args, signatures)
		return result, true, err
	}
	if name == "Re2::Options" {
		result, err := resolveSignatures(name, args, re2OptionsSignatures(name))
		return result, true, err
	}
	if isMultiRegex(name) {
		result, err := resolveMultiRegex(name, args)
		return result, true, err
	}
	if name == "Re2::Capture" {
		result, err := resolveRe2Capture(args)
		return result, true, err
	}
	return model.Type{}, false, nil
}

func isSpecializedCallName(name string) bool {
	return name == "Re2::Options" || name == "Re2::Capture" || isMultiRegex(name) || len(histogramSignatures(name)) != 0
}

func histogramStructType() model.Type {
	doubleType := model.Type{Kind: "Double"}
	bin := model.Type{Kind: "Struct", Fields: []model.StructField{
		{Name: "Frequency", Type: doubleType},
		{Name: "Position", Type: doubleType},
	}}
	return model.Type{Kind: "Struct", Fields: []model.StructField{
		{Name: "Bins", Type: model.Type{Kind: "List", Elem: &bin}},
		{Name: "Kind", Type: model.Type{Kind: "String"}},
		{Name: "Max", Type: doubleType},
		{Name: "Min", Type: doubleType},
		{Name: "WeightsSum", Type: doubleType},
	}}
}

func histogramSignatures(name string) []Signature {
	structType := histogramStructType()
	doubleType := model.Type{Kind: "Double"}
	input := Parameter{Type: structType, AutoMap: true}
	output := doubleType
	var extra []Parameter
	switch name {
	case "Histogram::Print":
		extra = []Parameter{{Type: model.Optional(model.Type{Kind: "Uint8"}), Optional: true}}
		output = model.Type{Kind: "String"}
	case "Histogram::Normalize":
		extra = []Parameter{{Type: model.Optional(doubleType), Optional: true}, {Type: model.Optional(model.Type{Kind: "Bool"}), Optional: true}}
		output = structType
	case "Histogram::ToCumulativeDistributionFunction":
		output = structType
	case "Histogram::GetSumAboveBound", "Histogram::GetSumBelowBound", "Histogram::CalcUpperBound",
		"Histogram::CalcLowerBound", "Histogram::CalcUpperBoundSafe", "Histogram::CalcLowerBoundSafe":
		extra = []Parameter{{Type: doubleType}}
	case "Histogram::GetSumInRange":
		extra = []Parameter{{Type: doubleType}, {Type: doubleType}}
	default:
		return nil
	}
	return []Signature{{Name: name, Arguments: append([]Parameter{input}, extra...), Returns: output}}
}

func IsHistogramAggregate(name string) bool {
	switch strings.ToUpper(name) {
	case "HISTOGRAM", "HIST", "HISTOGRAMCDF", "ADAPTIVEWARDHISTOGRAM", "ADAPTIVEWARDHISTOGRAMCDF",
		"ADAPTIVEWEIGHTHISTOGRAM", "ADAPTIVEWEIGHTHISTOGRAMCDF", "ADAPTIVEDISTANCEHISTOGRAM",
		"ADAPTIVEDISTANCEHISTOGRAMCDF", "BLOCKWARDHISTOGRAM", "BLOCKWARDHISTOGRAMCDF",
		"BLOCKWEIGHTHISTOGRAM", "BLOCKWEIGHTHISTOGRAMCDF", "LINEARHISTOGRAM", "LINEARHISTOGRAMCDF",
		"LOGARITHMICHISTOGRAM", "LOGARITHMICHISTOGRAMCDF",
		"LOGHISTOGRAM", "LOGHISTOGRAMCDF":
		return true
	default:
		return false
	}
}

func resolveHistogramAggregate(name string, args []model.Type) (model.Type, bool, error) {
	if !IsHistogramAggregate(name) {
		return model.Type{}, false, nil
	}
	upper := strings.ToUpper(name)
	isLinear := strings.HasPrefix(upper, "LINEAR") || strings.HasPrefix(upper, "LOG")
	maximum := 3
	if isLinear {
		maximum = 4
	}
	if len(args) < 1 || len(args) > maximum {
		return model.Type{}, true, fmt.Errorf("%s expects 1 to %d arguments, got %d", name, maximum, len(args))
	}
	for i, argument := range args {
		base, _, err := baseType(argument)
		if err != nil || !isPrimitiveNumber(base.Kind) {
			return model.Type{}, true, fmt.Errorf("%s argument %d must be numeric, got %s", name, i+1, argument.String())
		}
	}
	if !isLinear && len(args) == 3 && !isInteger(args[2].UnwrapOptional().Kind) {
		return model.Type{}, true, fmt.Errorf("%s argument 3 must be an integer bucket count", name)
	}
	return model.Optional(histogramStructType()), true, nil
}

func isMultiRegex(name string) bool {
	switch name {
	case "Hyperscan::MultiGrep", "Hyperscan::MultiMatch", "Pcre::MultiGrep", "Pcre::MultiMatch",
		"Pire::MultiGrep", "Pire::MultiMatch":
		return true
	}
	return false
}

func resolveMultiRegex(name string, args []CallArgument) (model.Type, error) {
	if len(args) != 1 {
		return model.Type{}, fmt.Errorf("%s expects 1 argument, got %d", name, len(args))
	}
	if args[0].Name != "" {
		return model.Type{}, fmt.Errorf("%s does not support named argument %q", name, args[0].Name)
	}
	if err := specializedRequiredArgument(name, []model.Type{args[0].Type}, 0, model.Type{Kind: "String"}); err != nil {
		return model.Type{}, err
	}
	if args[0].StringLiteral == nil {
		return model.Type{}, fmt.Errorf("%s requires a string literal pattern to determine the result tuple type", name)
	}
	items := make([]model.Type, strings.Count(*args[0].StringLiteral, "\n")+1)
	for i := range items {
		items[i] = model.Type{Kind: "Bool"}
	}
	result := model.Type{Kind: "Tuple", Items: items}
	return model.Type{Kind: "Callable", Items: []model.Type{model.Optional(model.Type{Kind: "String"})}, Elem: &result}, nil
}

func resolveRe2Capture(args []CallArgument) (model.Type, error) {
	const name = "Re2::Capture"
	if len(args) < 1 || len(args) > 2 {
		return model.Type{}, fmt.Errorf("%s expects 1 or 2 arguments, got %d", name, len(args))
	}
	types := make([]model.Type, len(args))
	for i, argument := range args {
		if argument.Name != "" {
			return model.Type{}, fmt.Errorf("%s does not support named argument %q", name, argument.Name)
		}
		types[i] = argument.Type
	}
	if err := specializedRequiredArgument(name, types, 0, model.Type{Kind: "String"}); err != nil {
		return model.Type{}, err
	}
	if len(args) == 2 {
		if _, err := specializedArgument(name, types, 1, re2OptionsType()); err != nil {
			return model.Type{}, err
		}
	}
	if args[0].StringLiteral == nil {
		return model.Type{}, fmt.Errorf("%s requires a string literal pattern to determine capturing group fields", name)
	}
	compiled, err := regexp.Compile(*args[0].StringLiteral)
	if err != nil {
		return model.Type{}, fmt.Errorf("%s invalid pattern: %w", name, err)
	}
	names := compiled.SubexpNames()
	fields := make([]model.StructField, len(names))
	seen := make(map[string]bool, len(names))
	unnamed := 0
	for i, groupName := range names {
		if groupName == "" {
			groupName = fmt.Sprintf("_%d", unnamed)
			unnamed++
		}
		if seen[groupName] {
			return model.Type{}, fmt.Errorf("%s pattern contains duplicate capturing group name %q", name, groupName)
		}
		seen[groupName] = true
		fields[i] = model.StructField{Name: groupName, Type: model.Optional(model.Type{Kind: "String"})}
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	result := model.Type{Kind: "Struct", Fields: fields}
	return model.Type{Kind: "Callable", Items: []model.Type{model.Optional(model.Type{Kind: "String"})}, Elem: &result}, nil
}

func regexResult(name string) model.Type {
	module, function, ok := strings.Cut(name, "::")
	if !ok {
		return model.Type{}
	}
	if module != "Hyperscan" && module != "Pcre" && module != "Pire" && module != "Re2" {
		return model.Type{}
	}
	switch function {
	case "Grep", "Match":
		return model.Type{Kind: "Bool"}
	case "BacktrackingGrep", "BacktrackingMatch":
		if module == "Hyperscan" || module == "Pcre" {
			return model.Type{Kind: "Bool"}
		}
	case "Capture":
		if module != "Re2" {
			return model.Optional(model.Type{Kind: "String"})
		}
	case "Replace":
		return model.Optional(model.Type{Kind: "String"})
	case "Count":
		if module == "Re2" {
			return model.Type{Kind: "Uint32"}
		}
	case "FindAndConsume":
		if module == "Re2" {
			item := model.Type{Kind: "String"}
			return model.Type{Kind: "List", Elem: &item}
		}
	}
	return model.Type{}
}

func resolveRegexConstructor(name string, args []model.Type) (model.Type, error) {
	module, function, _ := strings.Cut(name, "::")
	if module == "Re2" {
		if len(args) < 1 || len(args) > 2 {
			return model.Type{}, fmt.Errorf("%s expects 1 or 2 arguments, got %d", name, len(args))
		}
	} else if err := arity(name, args, 1); err != nil {
		return model.Type{}, err
	}
	if err := specializedRequiredArgument(name, args, 0, model.Type{Kind: "String"}); err != nil {
		return model.Type{}, err
	}
	if len(args) == 2 {
		if _, err := specializedArgument(name, args, 1, re2OptionsType()); err != nil {
			return model.Type{}, err
		}
	}
	parameters := []model.Type{model.Optional(model.Type{Kind: "String"})}
	if function == "Replace" {
		parameters = append(parameters, model.Type{Kind: "String"})
	}
	result := regexResult(name)
	return model.Type{Kind: "Callable", Items: parameters, Elem: &result}, nil
}

func re2OptionsType() model.Type {
	names := []string{"CaseSensitive", "DotNl", "Literal", "LogErrors", "LongestMatch", "MaxMem", "NeverCapture", "NeverNl", "OneLine", "PerlClasses", "PosixSyntax", "Utf8", "WordBoundary"}
	fields := make([]model.StructField, len(names))
	for i, name := range names {
		kind := "Bool"
		if name == "MaxMem" {
			kind = "Uint64"
		}
		fields[i] = model.StructField{Name: name, Type: model.Type{Kind: kind}}
	}
	return model.Type{Kind: "Struct", Fields: fields}
}

func re2OptionsSignatures(name string) []Signature {
	if name != "Re2::Options" {
		return nil
	}
	output := re2OptionsType()
	arguments := make([]Parameter, len(output.Fields))
	for i, field := range output.Fields {
		arguments[i] = Parameter{Name: field.Name, Type: model.Optional(field.Type), Optional: true}
	}
	return []Signature{{Name: name, Arguments: arguments, Returns: output}}
}

func resolveKnn(name string, args []model.Type) (model.Type, error) {
	if strings.HasPrefix(name, "Knn::ToBinaryString") {
		if err := arity(name, args, 1); err != nil {
			return model.Type{}, err
		}
		if err := validateConcreteOrNull(args[0]); err != nil {
			return model.Type{}, fmt.Errorf("%s argument 1: %w", name, err)
		}
		value := args[0]
		nullable := value.IsOptional()
		if nullable {
			value = value.UnwrapOptional()
		}
		if value.Kind != "List" || value.Elem == nil {
			return model.Type{}, fmt.Errorf("%s argument 1 must be a supported numeric List", name)
		}
		tag := ""
		switch name {
		case "Knn::ToBinaryStringFloat":
			if value.Elem.Kind == "Float" {
				tag = "FloatVector"
			}
		case "Knn::ToBinaryStringUint8":
			if value.Elem.Kind == "Uint8" {
				tag = "Uint8Vector"
			}
		case "Knn::ToBinaryStringInt8":
			if value.Elem.Kind == "Int8" {
				tag = "Int8Vector"
			}
		case "Knn::ToBinaryStringBit":
			switch value.Elem.Kind {
			case "Double", "Float", "Uint8", "Int8":
				tag = "BitVector"
			}
		}
		if tag == "" {
			return model.Type{}, fmt.Errorf("%s argument 1 has unsupported element type %s", name, value.Elem.String())
		}
		stringType := model.Type{Kind: "String"}
		return withOptional(model.Type{Kind: "Tagged", Elem: &stringType, Tag: tag}, nullable), nil
	}
	if name == "Knn::FloatFromBinaryString" {
		if err := arity(name, args, 1); err != nil {
			return model.Type{}, err
		}
		input, nullable, err := baseType(args[0])
		if err != nil {
			return model.Type{}, fmt.Errorf("%s argument 1: %w", name, err)
		}
		item := model.Type{Kind: "Float"}
		result := model.Type{Kind: "List", Elem: &item}
		if input.Kind == "String" || input.Kind == "Null" {
			return model.Optional(result), nil
		}
		if input.Kind == "Tagged" && input.Elem != nil && input.Elem.Kind == "String" {
			switch input.Tag {
			case "FloatVector", "Int8Vector", "Uint8Vector", "BitVector":
				return withOptional(result, nullable), nil
			}
		}
		return model.Type{}, fmt.Errorf("%s argument 1 must be String or a supported Tagged<String, vector tag>, got %s", name, args[0].String())
	}
	if err := arity(name, args, 2); err != nil {
		return model.Type{}, err
	}
	var tags [2]string
	for i := range args {
		var err error
		tags[i], err = knnSimilarityArgument(name, args, i)
		if err != nil {
			return model.Type{}, err
		}
	}
	if tags[0] != "" && tags[1] != "" && tags[0] != tags[1] {
		return model.Type{}, fmt.Errorf("%s arguments have different vector tags %q and %q", name, tags[0], tags[1])
	}
	return model.Optional(model.Type{Kind: "Float"}), nil
}

func knnSimilarityArgument(name string, args []model.Type, index int) (string, error) {
	value := args[index].UnwrapOptional()
	if value.Kind == "String" || value.Kind == "Null" {
		return "", nil
	}
	if value.Kind == "Tagged" && value.Elem != nil && value.Elem.Equal(model.Type{Kind: "String"}) {
		switch value.Tag {
		case "FloatVector", "Uint8Vector", "Int8Vector", "BitVector":
			return value.Tag, nil
		}
	}
	return "", fmt.Errorf("%s argument %d must be String or a supported Tagged<String, vector tag>, got %s", name, index+1, args[index].String())
}

func resolveRoaring(name string, args []model.Type) (model.Type, error) {
	resource := model.Type{Kind: roaringBitmapResource}
	stringType := model.Type{Kind: "String"}
	uint32Type := model.Type{Kind: "Uint32"}
	list := model.Type{Kind: "List", Elem: &uint32Type}
	var inputs []model.Type
	var output model.Type
	switch name {
	case "Roaring::And", "Roaring::AndNot", "Roaring::Or":
		inputs, output = []model.Type{resource, resource}, resource
	case "Roaring::AndNotWithBinary", "Roaring::AndWithBinary", "Roaring::OrWithBinary":
		inputs, output = []model.Type{resource, stringType}, resource
	case "Roaring::Cardinality":
		inputs, output = []model.Type{resource}, uint32Type
	case "Roaring::Deserialize":
		inputs, output = []model.Type{stringType}, resource
	case "Roaring::FromUint32List":
		inputs, output = []model.Type{list}, resource
	case "Roaring::RunOptimize":
		inputs, output = []model.Type{resource}, resource
	case "Roaring::Serialize":
		inputs, output = []model.Type{resource}, stringType
	case "Roaring::Uint32List":
		inputs, output = []model.Type{resource}, list
	}
	if err := arity(name, args, len(inputs)); err != nil {
		return model.Type{}, err
	}
	nullable := false
	for i, input := range inputs {
		optional, err := specializedArgument(name, args, i, input)
		if err != nil {
			return model.Type{}, err
		}
		nullable = nullable || optional
	}
	return withOptional(output, nullable), nil
}

func specializedArgument(name string, args []model.Type, index int, want model.Type) (bool, error) {
	got := args[index]
	optional := got.IsOptional()
	if optional {
		if got.Elem == nil || got.Elem.IsOptional() {
			return false, fmt.Errorf("%s argument %d has invalid Optional type", name, index+1)
		}
		got = *got.Elem
	}
	if !got.Equal(want) {
		return false, fmt.Errorf("%s argument %d must be %s or Optional<%s>, got %s", name, index+1, want.String(), want.String(), args[index].String())
	}
	return optional, nil
}

func specializedRequiredArgument(name string, args []model.Type, index int, want model.Type) error {
	optional, err := specializedArgument(name, args, index, want)
	if err != nil {
		return err
	}
	if optional {
		return fmt.Errorf("%s argument %d must be non-optional %s", name, index+1, want.String())
	}
	return nil
}
