package builtins

import (
	"math/big"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestSpecializedUDFTypes(t *testing.T) {
	resource := model.Type{Kind: roaringBitmapResource}
	stringType := model.Type{Kind: "String"}
	floatType := model.Type{Kind: "Float"}
	uint8Type := model.Type{Kind: "Uint8"}
	int8Type := model.Type{Kind: "Int8"}
	doubleType := model.Type{Kind: "Double"}
	uint32Type := model.Type{Kind: "Uint32"}
	uint32List := model.Type{Kind: "List", Elem: &uint32Type}
	for _, tc := range []struct {
		name string
		args []model.Type
		want string
	}{
		{"Knn::ToBinaryStringFloat", []model.Type{{Kind: "List", Elem: &floatType}}, `Tagged<String,"FloatVector">`},
		{"Knn::ToBinaryStringUint8", []model.Type{{Kind: "List", Elem: &uint8Type}}, `Tagged<String,"Uint8Vector">`},
		{"Knn::ToBinaryStringInt8", []model.Type{{Kind: "List", Elem: &int8Type}}, `Tagged<String,"Int8Vector">`},
		{"Knn::ToBinaryStringBit", []model.Type{{Kind: "List", Elem: &doubleType}}, `Tagged<String,"BitVector">`},
		{"Knn::ToBinaryStringBit", []model.Type{{Kind: "List", Elem: &floatType}}, `Tagged<String,"BitVector">`},
		{"Knn::ToBinaryStringBit", []model.Type{{Kind: "List", Elem: &uint8Type}}, `Tagged<String,"BitVector">`},
		{"Knn::ToBinaryStringBit", []model.Type{{Kind: "List", Elem: &int8Type}}, `Tagged<String,"BitVector">`},
		{"Knn::FloatFromBinaryString", []model.Type{stringType}, "Optional<List<Float>>"},
		{"Knn::FloatFromBinaryString", []model.Type{{Kind: "Tagged", Elem: &stringType, Tag: "FloatVector"}}, "List<Float>"},
		{"Knn::FloatFromBinaryString", []model.Type{{Kind: "Tagged", Elem: &stringType, Tag: "Int8Vector"}}, "List<Float>"},
		{"Knn::FloatFromBinaryString", []model.Type{{Kind: "Tagged", Elem: &stringType, Tag: "Uint8Vector"}}, "List<Float>"},
		{"Knn::FloatFromBinaryString", []model.Type{{Kind: "Tagged", Elem: &stringType, Tag: "BitVector"}}, "List<Float>"},
		{"Knn::InnerProductSimilarity", []model.Type{stringType, stringType}, "Optional<Float>"},
		{"Knn::CosineSimilarity", []model.Type{stringType, stringType}, "Optional<Float>"},
		{"Knn::CosineDistance", []model.Type{stringType, stringType}, "Optional<Float>"},
		{"Knn::ManhattanDistance", []model.Type{stringType, stringType}, "Optional<Float>"},
		{"Knn::EuclideanDistance", []model.Type{stringType, stringType}, "Optional<Float>"},
		{"Roaring::And", []model.Type{resource, resource}, roaringBitmapResource},
		{"Roaring::AndNot", []model.Type{resource, resource}, roaringBitmapResource},
		{"Roaring::AndNotWithBinary", []model.Type{resource, stringType}, roaringBitmapResource},
		{"Roaring::AndWithBinary", []model.Type{resource, stringType}, roaringBitmapResource},
		{"Roaring::Cardinality", []model.Type{resource}, "Uint32"},
		{"Roaring::Deserialize", []model.Type{stringType}, roaringBitmapResource},
		{"Roaring::FromUint32List", []model.Type{uint32List}, roaringBitmapResource},
		{"Roaring::Or", []model.Type{resource, resource}, roaringBitmapResource},
		{"Roaring::OrWithBinary", []model.Type{resource, stringType}, roaringBitmapResource},
		{"Roaring::RunOptimize", []model.Type{resource}, roaringBitmapResource},
		{"Roaring::Serialize", []model.Type{resource}, "String"},
		{"Roaring::Uint32List", []model.Type{resource}, "List<Uint32>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolve := lookupSpecializedUDF(tc.name)
			require.NotNil(t, resolve)
			got, err := resolve(tc.args)
			require.NoError(t, err)
			require.Equal(t, tc.want, got.String())
			{
				_, err := resolve(nil)
				require.ErrorContains(t, err, "expects")
			}
		})
	}
}

func TestSpecializedUDFOptionalityAndDiagnostics(t *testing.T) {
	resource := model.Type{Kind: roaringBitmapResource}
	stringType := model.Type{Kind: "String"}
	floatType := model.Type{Kind: "Float"}
	floatVector := model.Type{Kind: "Tagged", Elem: &stringType, Tag: "FloatVector"}
	uint8Vector := model.Type{Kind: "Tagged", Elem: &stringType, Tag: "Uint8Vector"}
	for _, tc := range []struct {
		name string
		args []model.Type
		want string
	}{
		{"Knn::ToBinaryStringFloat", []model.Type{model.Optional(model.Type{Kind: "List", Elem: &floatType})}, `Optional<Tagged<String,"FloatVector">>`},
		{"Knn::FloatFromBinaryString", []model.Type{model.Optional(stringType)}, "Optional<List<Float>>"},
		{"Knn::FloatFromBinaryString", []model.Type{{Kind: "Null"}}, "Optional<List<Float>>"},
		{"Knn::FloatFromBinaryString", []model.Type{model.Optional(floatVector)}, "Optional<List<Float>>"},
		{"Knn::CosineSimilarity", []model.Type{stringType, model.Optional(stringType)}, "Optional<Float>"},
		{"Knn::CosineSimilarity", []model.Type{{Kind: "Null"}, floatVector}, "Optional<Float>"},
		{"Knn::CosineDistance", []model.Type{floatVector, {Kind: "Null"}}, "Optional<Float>"},
		{"Knn::CosineSimilarity", []model.Type{floatVector, stringType}, "Optional<Float>"},
		{"Knn::CosineSimilarity", []model.Type{floatVector, model.Optional(floatVector)}, "Optional<Float>"},
		{"Roaring::Deserialize", []model.Type{model.Optional(stringType)}, "Optional<" + roaringBitmapResource + ">"},
		{"Roaring::Serialize", []model.Type{model.Optional(resource)}, "Optional<String>"},
		{"Roaring::And", []model.Type{resource, model.Optional(resource)}, "Optional<" + roaringBitmapResource + ">"},
	} {
		got, err := lookupSpecializedUDF(tc.name)(tc.args)
		require.NoError(t, err)
		require.Equal(t, tc.want, got.String())
	}
	for _, tc := range []struct {
		name string
		args []model.Type
		want string
	}{
		{"Knn::ToBinaryStringFloat", []model.Type{{Kind: "List", Elem: &stringType}}, "unsupported element type String"},
		{"Knn::FloatFromBinaryString", []model.Type{{Kind: "Utf8"}}, "argument 1 must be String"},
		{"Knn::FloatFromBinaryString", []model.Type{{Kind: "Tagged", Elem: &stringType, Tag: "UnknownVector"}}, "supported Tagged<String, vector tag>"},
		{"Knn::CosineSimilarity", []model.Type{stringType, {Kind: "Uint64"}}, "argument 2 must be String"},
		{"Knn::CosineSimilarity", []model.Type{floatVector, uint8Vector}, "different vector tags"},
		{"Roaring::FromUint32List", []model.Type{{Kind: "List", Elem: &stringType}}, "List<Uint32>"},
		{"Roaring::Cardinality", []model.Type{model.Optional(model.Optional(resource))}, "invalid Optional"},
	} {
		_, err := lookupSpecializedUDF(tc.name)(tc.args)
		require.ErrorContains(t, err, tc.want)
	}
}

func TestRegexConstructorSignatures(t *testing.T) {
	stringType := model.Type{Kind: "String"}
	for _, tc := range []struct {
		name    string
		result  model.Type
		replace bool
	}{
		{"Hyperscan::Grep", model.Type{Kind: "Bool"}, false},
		{"Hyperscan::Match", model.Type{Kind: "Bool"}, false},
		{"Hyperscan::BacktrackingGrep", model.Type{Kind: "Bool"}, false},
		{"Hyperscan::BacktrackingMatch", model.Type{Kind: "Bool"}, false},
		{"Hyperscan::Capture", model.Optional(stringType), false},
		{"Hyperscan::Replace", model.Optional(stringType), true},
		{"Pcre::Grep", model.Type{Kind: "Bool"}, false},
		{"Pcre::Match", model.Type{Kind: "Bool"}, false},
		{"Pcre::BacktrackingGrep", model.Type{Kind: "Bool"}, false},
		{"Pcre::BacktrackingMatch", model.Type{Kind: "Bool"}, false},
		{"Pcre::Capture", model.Optional(stringType), false},
		{"Pcre::Replace", model.Optional(stringType), true},
		{"Pire::Grep", model.Type{Kind: "Bool"}, false},
		{"Pire::Match", model.Type{Kind: "Bool"}, false},
		{"Pire::Capture", model.Optional(stringType), false},
		{"Pire::Replace", model.Optional(stringType), true},
		{"Re2::Grep", model.Type{Kind: "Bool"}, false},
		{"Re2::Match", model.Type{Kind: "Bool"}, false},
		{"Re2::Count", model.Type{Kind: "Uint32"}, false},
		{"Re2::FindAndConsume", model.Type{Kind: "List", Elem: &stringType}, false},
		{"Re2::Replace", model.Optional(stringType), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolve := lookupSpecializedUDF(tc.name)
			require.NotNil(t, resolve)
			got, err := resolve([]model.Type{stringType})
			require.NoError(t, err)
			require.Equal(t, "Callable", got.Kind)
			require.NotNil(t, got.Elem)
			require.True(t, got.Elem.Equal(tc.result))
			wantArgs := []model.Type{model.Optional(stringType)}
			if tc.replace {
				wantArgs = append(wantArgs, stringType)
			}
			require.Len(t, got.Items, len(wantArgs))
			for i := range wantArgs {
				require.True(t, got.Items[i].Equal(wantArgs[i]))
			}
			{
				_, err := resolve(nil)
				require.ErrorContains(t, err, "expects")
			}
			{
				_, err := resolve([]model.Type{model.Optional(stringType)})
				require.ErrorContains(t, err, "non-optional String")
			}
		})
	}
	for _, name := range []string{"Re2::Grep", "Re2::Match", "Re2::Count", "Re2::FindAndConsume", "Re2::Replace"} {
		got, err := lookupSpecializedUDF(name)([]model.Type{stringType, re2OptionsType()})
		require.NoError(t, err)
		require.Equal(t, "Callable", got.Kind)
	}
}

func TestRe2OptionsNamedSignature(t *testing.T) {
	signatures := re2OptionsSignatures("Re2::Options")
	require.Len(t, signatures, 1)
	require.Nil(t, re2OptionsSignatures("Re2::Missing"))
	for _, args := range [][]CallArgument{
		nil,
		{{Name: "CaseSensitive", Type: model.Type{Kind: "Bool"}}},
		{{Name: "CaseSensitive", Type: model.Optional(model.Type{Kind: "Bool"})}},
		{{Name: "CaseSensitive", Type: model.Type{Kind: "Null"}}},
		{{Name: "MaxMem", Type: model.Type{Kind: "Uint64"}}, {Name: "Utf8", Type: model.Type{Kind: "Bool"}}},
	} {
		got, err := resolveSignatures("Re2::Options", args, signatures)
		require.NoError(t, err)
		require.True(t, got.Equal(re2OptionsType()))
	}
	for _, tc := range []struct {
		args []CallArgument
		want string
	}{
		{[]CallArgument{{Name: "Unknown", Type: model.Type{Kind: "Bool"}}}, "unknown named argument"},
		{[]CallArgument{{Name: "MaxMem", Type: model.Type{Kind: "Bool"}}}, "must be Optional<Uint64>"},
		{[]CallArgument{{Name: "Utf8", Type: model.Type{Kind: "String"}}}, "must be Optional<Bool>"},
	} {
		_, err := resolveSignatures("Re2::Options", tc.args, signatures)
		require.ErrorContains(t, err, tc.want)
	}
}

func TestLiteralDependentRegexTypes(t *testing.T) {
	stringType := model.Type{Kind: "String"}
	for _, name := range []string{
		"Hyperscan::MultiGrep", "Hyperscan::MultiMatch", "Pcre::MultiGrep", "Pcre::MultiMatch",
		"Pire::MultiGrep", "Pire::MultiMatch",
	} {
		for _, tc := range []struct {
			pattern string
			want    string
		}{
			{"a", "Callable<(Optional<String>)->Tuple<Bool>>"},
			{"a\nb", "Callable<(Optional<String>)->Tuple<Bool,Bool>>"},
			{"a\nb\n", "Callable<(Optional<String>)->Tuple<Bool,Bool,Bool>>"},
		} {
			got, handled, err := resolveSpecializedCall(name, []CallArgument{{Type: stringType, StringLiteral: &tc.pattern}})
			assert.False(t, !handled || err != nil || got.String() != tc.want)
		}
		_, handled, err := resolveSpecializedCall(name, []CallArgument{{Type: stringType}})
		assert.False(t, !handled || err == nil || !strings.Contains(err.Error(), "requires a string literal"))
	}
	for _, tc := range []struct {
		pattern string
		want    string
	}{
		{"abc", "Callable<(Optional<String>)->Struct<`_0`:Optional<String>>>"},
		{"(?P<foo>x)(a)", "Callable<(Optional<String>)->Struct<`_0`:Optional<String>,`_1`:Optional<String>,`foo`:Optional<String>>>"},
		{"(?P<foo>x)(?<bar>a)", "Callable<(Optional<String>)->Struct<`_0`:Optional<String>,`bar`:Optional<String>,`foo`:Optional<String>>>"},
	} {
		got, handled, err := resolveSpecializedCall("Re2::Capture", []CallArgument{{Type: stringType, StringLiteral: &tc.pattern}})
		assert.False(t, !handled || err != nil || got.String() != tc.want)
	}
	pattern := "(?P<foo>x)(a)"
	got, handled, err := resolveSpecializedCall("Re2::Capture", []CallArgument{{Type: stringType, StringLiteral: &pattern}, {Type: model.Optional(re2OptionsType())}})
	assert.False(t, !handled || err != nil || got.Kind != "Callable")
	for _, tc := range []struct {
		pattern *string
		want    string
	}{
		{nil, "requires a string literal"},
		{func() *string { s := "["; return &s }(), "invalid pattern"},
		{func() *string { s := "(?P<foo>x)(?P<foo>a)"; return &s }(), "duplicate capturing group name"},
	} {
		_, handled, err := resolveSpecializedCall("Re2::Capture", []CallArgument{{Type: stringType, StringLiteral: tc.pattern}})
		assert.False(t, !handled || err == nil || !strings.Contains(err.Error(), tc.want))
	}
}

func TestHistogramUDFSignatures(t *testing.T) {
	histogram := histogramStructType()
	doubleType := model.Type{Kind: "Double"}
	require.Len(t, histogram.Fields, 5)
	require.Equal(t, "Bins", histogram.Fields[0].Name)
	require.Equal(t, "WeightsSum", histogram.Fields[4].Name)
	for _, tc := range []struct {
		name string
		args []model.Type
		want model.Type
	}{
		{"Histogram::Print", []model.Type{histogram}, model.Type{Kind: "String"}},
		{"Histogram::Print", []model.Type{histogram, model.Optional(model.Type{Kind: "Uint8"})}, model.Type{Kind: "String"}},
		{"Histogram::Normalize", []model.Type{histogram}, histogram},
		{"Histogram::Normalize", []model.Type{histogram, model.Optional(doubleType)}, histogram},
		{"Histogram::Normalize", []model.Type{histogram, model.Optional(doubleType), model.Optional(model.Type{Kind: "Bool"})}, histogram},
		{"Histogram::ToCumulativeDistributionFunction", []model.Type{histogram}, histogram},
		{"Histogram::GetSumAboveBound", []model.Type{histogram, doubleType}, doubleType},
		{"Histogram::GetSumBelowBound", []model.Type{histogram, doubleType}, doubleType},
		{"Histogram::GetSumInRange", []model.Type{histogram, doubleType, doubleType}, doubleType},
		{"Histogram::CalcUpperBound", []model.Type{histogram, doubleType}, doubleType},
		{"Histogram::CalcLowerBound", []model.Type{histogram, doubleType}, doubleType},
		{"Histogram::CalcUpperBoundSafe", []model.Type{histogram, doubleType}, doubleType},
		{"Histogram::CalcLowerBoundSafe", []model.Type{histogram, doubleType}, doubleType},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolve := lookupSpecializedUDF(tc.name)
			require.NotNil(t, resolve)
			got, err := resolve(tc.args)
			require.NoError(t, err)
			require.True(t, got.Equal(tc.want))
			{
				_, err := resolve(nil)
				require.Error(t, err)
			}
			optionalArgs := append([]model.Type(nil), tc.args...)
			optionalArgs[0] = model.Optional(histogram)
			optionalResult, err := resolve(optionalArgs)
			require.NoError(t, err)
			require.True(t, optionalResult.Equal(model.Optional(tc.want)))
		})
	}
	barCount := model.Type{Kind: "Int32"}
	count := big.NewInt(50)
	got, handled, err := resolveSpecializedCall("Histogram::Print", []CallArgument{{Type: histogram}, {Type: barCount, IntegerLiteral: count}})
	require.True(t, handled)
	require.NoError(t, err)
	require.Equal(t, "String", got.Kind)
	count = big.NewInt(256)
	_, handled, err = resolveSpecializedCall("Histogram::Print", []CallArgument{{Type: histogram}, {Type: barCount, IntegerLiteral: count}})
	require.True(t, handled)
	require.Error(t, err)
}

func TestHistogramAggregateResults(t *testing.T) {
	for _, name := range []string{
		"HISTOGRAM", "HIST", "HISTOGRAMCDF", "AdaptiveWardHistogram", "AdaptiveWeightHistogram",
		"AdaptiveDistanceHistogram", "BlockWardHistogram", "BlockWeightHistogram", "AdaptiveWardHistogramCDF",
		"AdaptiveWeightHistogramCDF", "AdaptiveDistanceHistogramCDF", "BlockWardHistogramCDF", "BlockWeightHistogramCDF",
		"LinearHistogram", "LinearHistogramCDF", "LogarithmicHistogram", "LogarithmicHistogramCDF",
		"LogHistogram", "LogHistogramCDF",
	} {
		got, handled, err := resolveHistogramAggregate(name, []model.Type{{Kind: "Double"}})
		assert.False(t, !handled || err != nil || !got.Equal(model.Optional(histogramStructType())))
	}
	{
		_, handled, err := resolveHistogramAggregate("AVG", nil)
		assert.False(t, handled || err != nil)
	}
	for _, tc := range []struct {
		name string
		args []model.Type
		want string
	}{
		{"HISTOGRAM", nil, "expects 1 to 3 arguments"},
		{"HISTOGRAM", []model.Type{{Kind: "String"}}, "argument 1 must be numeric"},
		{"HISTOGRAM", []model.Type{{Kind: "Double"}, {Kind: "Double"}, {Kind: "Double"}}, "argument 3 must be an integer bucket count"},
		{"LinearHistogram", []model.Type{{Kind: "Double"}, {Kind: "Double"}, {Kind: "Double"}, {Kind: "Double"}, {Kind: "Double"}}, "expects 1 to 4 arguments"},
	} {
		_, handled, err := resolveHistogramAggregate(tc.name, tc.args)
		assert.False(t, !handled || err == nil || !strings.Contains(err.Error(), tc.want))
	}
}
