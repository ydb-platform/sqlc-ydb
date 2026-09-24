package builtins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func checkDocumentedUDF(t *testing.T, name string, args []model.Type, want model.Type) {
	t.Helper()
	resolver := lookupDateTimeDigestIpMath(name)
	require.NotNil(t, resolver)
	got, err := resolver(args)
	require.NoError(t, err)
	assert.True(t, got.Equal(want))
}

func TestDateTimeDocumentedResourceOverloads(t *testing.T) {
	base, extended := udfKind(dateTimeTM), udfKind(dateTimeTM64)
	for _, input := range []string{"Date", "TzDate", "Datetime", "TzDatetime", "Timestamp", "TzTimestamp"} {
		checkDocumentedUDF(t, "DateTime::Split", []model.Type{udfKind(input)}, base)
	}
	for _, input := range []string{"Date32", "TzDate32", "Datetime64", "TzDatetime64", "Timestamp64", "TzTimestamp64"} {
		checkDocumentedUDF(t, "DateTime::Split", []model.Type{udfKind(input)}, extended)
	}
	for _, tc := range []struct {
		name, result, resource string
	}{
		{"MakeDate", "Date", dateTimeTM}, {"MakeDate32", "Date32", dateTimeTM64},
		{"MakeTzDate32", "TzDate32", dateTimeTM64}, {"MakeDatetime", "Datetime", dateTimeTM},
		{"MakeTzDatetime", "TzDatetime", dateTimeTM}, {"MakeDatetime64", "Datetime64", dateTimeTM64},
		{"MakeTzDatetime64", "TzDatetime64", dateTimeTM64}, {"MakeTimestamp", "Timestamp", dateTimeTM},
		{"MakeTzTimestamp", "TzTimestamp", dateTimeTM}, {"MakeTimestamp64", "Timestamp64", dateTimeTM64},
		{"MakeTzTimestamp64", "TzTimestamp64", dateTimeTM64},
	} {
		name := "DateTime::" + tc.name
		checkDocumentedUDF(t, name, []model.Type{udfKind(tc.resource)}, udfKind(tc.result))
		primitive := udfKind("Timestamp")
		if tc.resource == dateTimeTM64 {
			primitive = udfKind("Timestamp64")
		}
		checkDocumentedUDF(t, name, []model.Type{primitive}, udfKind(tc.result))
	}
	for _, tc := range []struct{ name, result string }{
		{"GetYear", "Uint16"}, {"GetDayOfYear", "Uint16"}, {"GetMonth", "Uint8"},
		{"GetMonthName", "String"}, {"GetWeekOfYear", "Uint8"}, {"GetWeekOfYearIso8601", "Uint8"},
		{"GetDayOfMonth", "Uint8"}, {"GetDayOfWeek", "Uint8"}, {"GetDayOfWeekName", "String"},
		{"GetHour", "Uint8"}, {"GetMinute", "Uint8"}, {"GetSecond", "Uint8"},
		{"GetMillisecondOfSecond", "Uint32"}, {"GetMicrosecondOfSecond", "Uint32"},
		{"GetTimezoneId", "Uint16"}, {"GetTimezoneName", "String"},
	} {
		name := "DateTime::" + tc.name
		checkDocumentedUDF(t, name, []model.Type{base}, udfKind(tc.result))
		wideResult := tc.result
		if tc.name == "GetYear" {
			wideResult = "Int32"
		}
		checkDocumentedUDF(t, name, []model.Type{extended}, udfKind(wideResult))
	}
	for _, resource := range []model.Type{base, extended} {
		checkDocumentedUDF(t, "DateTime::Update", []model.Type{resource}, model.Optional(resource))
		primitive := udfKind("Timestamp")
		if resource.Kind == dateTimeTM64 {
			primitive = udfKind("Timestamp64")
		}
		checkDocumentedUDF(t, "DateTime::Update", []model.Type{primitive}, model.Optional(resource))
		for _, name := range []string{"StartOfYear", "EndOfYear", "StartOfQuarter", "EndOfQuarter",
			"StartOfMonth", "EndOfMonth", "StartOfWeek", "EndOfWeek", "StartOfDay", "EndOfDay"} {
			checkDocumentedUDF(t, "DateTime::"+name, []model.Type{resource}, model.Optional(resource))
		}
		interval := udfKind("Interval")
		if resource.Kind == dateTimeTM64 {
			interval = udfKind("Interval64")
		}
		for _, name := range []string{"StartOf", "EndOf"} {
			checkDocumentedUDF(t, "DateTime::"+name, []model.Type{resource, interval}, model.Optional(resource))
		}
		checkDocumentedUDF(t, "DateTime::TimeOfDay", []model.Type{resource}, interval)
		for _, name := range []string{"ShiftYears", "ShiftQuarters", "ShiftMonths"} {
			checkDocumentedUDF(t, "DateTime::"+name, []model.Type{resource, udfKind("Int32")}, model.Optional(resource))
		}
	}
	formatResult := udfKind("String")
	format := model.Type{Kind: "Callable", Items: []model.Type{extended}, Elem: &formatResult}
	checkDocumentedUDF(t, "DateTime::Format", []model.Type{udfKind("String")}, format)
	for _, tc := range []struct {
		name     string
		resource model.Type
	}{{"Parse", base}, {"Parse64", extended}} {
		result := model.Optional(tc.resource)
		callable := model.Type{Kind: "Callable", Items: []model.Type{udfKind("String")}, Elem: &result}
		checkDocumentedUDF(t, "DateTime::"+tc.name, []model.Type{udfKind("String")}, callable)
	}
	for _, name := range []string{"ParseRfc822", "ParseIso8601", "ParseHttp", "ParseX509"} {
		checkDocumentedUDF(t, "DateTime::"+name, []model.Type{udfKind("String")}, model.Optional(base))
	}
}

func TestDateTimeDocumentedScalarOverloads(t *testing.T) {
	for _, tc := range []struct{ name, input, result string }{
		{"FromSeconds", "Uint32", "Timestamp"}, {"FromSeconds64", "Int64", "Timestamp64"},
		{"FromMilliseconds", "Uint64", "Timestamp"}, {"FromMilliseconds64", "Int64", "Timestamp64"},
		{"FromMicroseconds", "Uint64", "Timestamp"}, {"FromMicroseconds64", "Int64", "Timestamp64"},
		{"IntervalFromDays", "Int32", "Interval"}, {"Interval64FromDays", "Int32", "Interval64"},
		{"IntervalFromHours", "Int32", "Interval"}, {"Interval64FromHours", "Int64", "Interval64"},
		{"IntervalFromMinutes", "Int32", "Interval"}, {"Interval64FromMinutes", "Int64", "Interval64"},
		{"IntervalFromSeconds", "Int64", "Interval"}, {"Interval64FromSeconds", "Int64", "Interval64"},
		{"IntervalFromMilliseconds", "Int64", "Interval"}, {"Interval64FromMilliseconds", "Int64", "Interval64"},
		{"IntervalFromMicroseconds", "Int64", "Interval"}, {"Interval64FromMicroseconds", "Int64", "Interval64"},
	} {
		checkDocumentedUDF(t, "DateTime::"+tc.name, []model.Type{udfKind(tc.input)}, model.Optional(udfKind(tc.result)))
	}
	for _, tc := range []struct{ name, input, result string }{
		{"ToDays", "Interval", "Int32"}, {"ToDays", "Interval64", "Int32"},
		{"ToHours", "Interval", "Int32"}, {"ToHours", "Interval64", "Int64"},
		{"ToMinutes", "Interval", "Int32"}, {"ToMinutes", "Interval64", "Int64"},
		{"ToSeconds", "Interval", "Int64"}, {"ToSeconds", "Interval64", "Int64"},
		{"ToMilliseconds", "Interval", "Int64"}, {"ToMilliseconds", "Interval64", "Int64"},
		{"ToMicroseconds", "Interval", "Int64"}, {"ToMicroseconds", "Interval64", "Int64"},
		{"ToSeconds", "Timestamp", "Uint32"}, {"ToSeconds", "Timestamp64", "Int64"},
		{"ToMilliseconds", "Timestamp", "Uint64"}, {"ToMilliseconds", "Timestamp64", "Int64"},
		{"ToMicroseconds", "Timestamp", "Uint64"}, {"ToMicroseconds", "Timestamp64", "Int64"},
	} {
		checkDocumentedUDF(t, "DateTime::"+tc.name, []model.Type{udfKind(tc.input)}, udfKind(tc.result))
	}
}

func TestDigestIpMathDocumentedSignatures(t *testing.T) {
	stringType, uint64Type, doubleType := udfKind("String"), udfKind("Uint64"), udfKind("Double")
	tuple := model.Type{Kind: "Tuple", Items: []model.Type{uint64Type, uint64Type}}
	for _, name := range []string{"CityHash128", "FarmHashFingerprint128", "XXH3_128"} {
		checkDocumentedUDF(t, "Digest::"+name, []model.Type{stringType}, tuple)
	}
	checkDocumentedUDF(t, "Digest::Argon2", []model.Type{stringType, stringType}, stringType)
	checkDocumentedUDF(t, "Digest::Blake2B", []model.Type{stringType}, stringType)
	checkDocumentedUDF(t, "Digest::Blake2B", []model.Type{stringType, model.Optional(stringType)}, stringType)
	checkDocumentedUDF(t, "Digest::SipHash", []model.Type{uint64Type, uint64Type, stringType}, uint64Type)
	checkDocumentedUDF(t, "Digest::HighwayHash", []model.Type{uint64Type, uint64Type, uint64Type, uint64Type, stringType}, uint64Type)
	checkDocumentedUDF(t, "Digest::FarmHashFingerprint2", []model.Type{uint64Type, uint64Type}, uint64Type)
	for _, name := range []string{"FromString", "SubnetFromString"} {
		checkDocumentedUDF(t, "Ip::"+name, []model.Type{stringType}, model.Optional(stringType))
	}
	for _, name := range []string{"ToString", "SubnetToString", "ConvertToIPv6"} {
		checkDocumentedUDF(t, "Ip::"+name, []model.Type{stringType}, stringType)
	}
	for _, name := range []string{"IsIPv4", "IsIPv6", "IsEmbeddedIPv4"} {
		checkDocumentedUDF(t, "Ip::"+name, []model.Type{model.Optional(stringType)}, udfKind("Bool"))
	}
	checkDocumentedUDF(t, "Ip::GetSubnet", []model.Type{stringType}, stringType)
	checkDocumentedUDF(t, "Ip::GetSubnet", []model.Type{stringType, model.Optional(udfKind("Uint8"))}, stringType)
	checkDocumentedUDF(t, "Ip::GetSubnetByMask", []model.Type{stringType, stringType}, stringType)
	checkDocumentedUDF(t, "Ip::SubnetMatch", []model.Type{stringType, stringType}, udfKind("Bool"))
	for _, name := range []string{"Pi", "E", "Eps"} {
		checkDocumentedUDF(t, "Math::"+name, nil, doubleType)
	}
	for _, name := range []string{"IsInf", "IsNaN", "IsFinite"} {
		checkDocumentedUDF(t, "Math::"+name, []model.Type{doubleType}, udfKind("Bool"))
	}
	for _, name := range []string{"Abs", "Acos", "Asin", "Asinh", "Atan", "Cbrt", "Ceil", "Cos", "Cosh", "Erf", "ErfInv", "ErfcInv", "Exp", "Exp2", "Fabs", "Floor", "Lgamma", "Rint", "Sigmoid", "Sin", "Sinh", "Sqrt", "Tan", "Tanh", "Tgamma", "Trunc", "Log", "Log2", "Log10"} {
		checkDocumentedUDF(t, "Math::"+name, []model.Type{doubleType}, doubleType)
	}
	for _, name := range []string{"Atan2", "Fmod", "Hypot", "Pow", "Remainder"} {
		checkDocumentedUDF(t, "Math::"+name, []model.Type{doubleType, doubleType}, doubleType)
	}
	checkDocumentedUDF(t, "Math::Ldexp", []model.Type{doubleType, udfKind("Int32")}, doubleType)
	checkDocumentedUDF(t, "Math::Round", []model.Type{doubleType}, doubleType)
	checkDocumentedUDF(t, "Math::Round", []model.Type{doubleType, model.Optional(udfKind("Int32"))}, doubleType)
	checkDocumentedUDF(t, "Math::FuzzyEquals", []model.Type{doubleType, doubleType}, udfKind("Bool"))
	checkDocumentedUDF(t, "Math::FuzzyEquals", []model.Type{doubleType, doubleType, model.Optional(doubleType)}, udfKind("Bool"))
	for _, name := range []string{"Mod", "Rem"} {
		checkDocumentedUDF(t, "Math::"+name, []model.Type{udfKind("Int64"), udfKind("Int64")}, model.Optional(udfKind("Int64")))
	}
	for _, name := range []string{"RoundDownward", "RoundToNearest", "RoundTowardZero", "RoundUpward"} {
		checkDocumentedUDF(t, "Math::"+name, nil, mathRoundingMode())
	}
	checkDocumentedUDF(t, "Math::NearbyInt", []model.Type{doubleType, mathRoundingMode()}, model.Optional(udfKind("Int64")))
}

func TestMathNumericArguments(t *testing.T) {
	for _, kind := range []string{"Uint8", "Int8", "Uint16", "Int16", "Uint32", "Int32", "Uint64", "Int64", "Float", "Double"} {
		checkDocumentedUDF(t, "Math::Sqrt", []model.Type{udfKind(kind)}, udfKind("Double"))
	}
	checkDocumentedUDF(t, "Math::Sqrt", []model.Type{model.Optional(udfKind("Int32"))}, model.Optional(udfKind("Double")))
	checkDocumentedUDF(t, "Math::Pow", []model.Type{udfKind("Uint64"), udfKind("Float")}, udfKind("Double"))
	checkDocumentedUDF(t, "Math::NearbyInt", []model.Type{udfKind("Float"), mathRoundingMode()}, model.Optional(udfKind("Int64")))
}

func TestDocumentedUDFArgumentErrorsAndNullability(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []model.Type
		want string
	}{
		{"DateTime::Split", []model.Type{udfKind("String")}, "primitive date/time"},
		{"DateTime::Split", nil, "expects 1 argument"},
		{"DateTime::Split", []model.Type{model.Optional(model.Optional(udfKind("Date")))}, "nested Optional"},
		{"DateTime::GetYear", nil, "expects 1 argument"},
		{"DateTime::GetYear", []model.Type{model.Optional(model.Optional(udfKind(dateTimeTM)))}, "nested Optional"},
		{"DateTime::MakeDate", nil, "expects 1 argument"},
		{"DateTime::MakeDate", []model.Type{udfKind(dateTimeTM64)}, "requires"},
		{"DateTime::ToSeconds", nil, "expects 1 argument"},
		{"DateTime::ToSeconds", []model.Type{model.Optional(model.Optional(udfKind("Timestamp")))}, "nested Optional"},
		{"DateTime::ToDays", []model.Type{udfKind("Timestamp")}, "does not accept"},
		{"DateTime::FromSeconds", []model.Type{udfKind("Int64")}, "must be"},
		{"DateTime::StartOfYear", nil, "expects 1 argument"},
		{"DateTime::StartOf", nil, "expects 2 arguments"},
		{"DateTime::StartOf", []model.Type{udfKind("String"), udfKind("Interval")}, "DateTime resource"},
		{"DateTime::StartOf", []model.Type{udfKind(dateTimeTM), udfKind("Interval64")}, "must be"},
		{"DateTime::ShiftMonths", nil, "expects 2 arguments"},
		{"DateTime::ShiftMonths", []model.Type{udfKind("String"), udfKind("Int32")}, "DateTime resource"},
		{"DateTime::ShiftMonths", []model.Type{udfKind(dateTimeTM), udfKind("Int64")}, "must be"},
		{"Digest::Argon2", []model.Type{udfKind("Utf8"), udfKind("String")}, "must be"},
		{"Digest::SipHash", []model.Type{udfKind("Uint32"), udfKind("Uint64"), udfKind("String")}, "must be"},
		{"Ip::GetSubnet", []model.Type{udfKind("String"), udfKind("Int32")}, "must be"},
		{"Math::Sqrt", []model.Type{udfKind("String")}, "must be"},
		{"Math::NearbyInt", []model.Type{udfKind("Double"), udfKind("Uint32")}, "must be"},
	} {
		resolver := lookupDateTimeDigestIpMath(tc.name)
		require.NotNil(t, resolver)
		_, err := resolver(tc.args)
		assert.ErrorContains(t, err, tc.want)
	}
	checkDocumentedUDF(t, "DateTime::GetYear", []model.Type{model.Optional(udfKind(dateTimeTM64))}, model.Optional(udfKind("Int32")))
	checkDocumentedUDF(t, "Digest::Argon2", []model.Type{model.Optional(udfKind("String")), udfKind("String")}, model.Optional(udfKind("String")))
	checkDocumentedUDF(t, "Ip::IsIPv4", []model.Type{udfKind("Null")}, udfKind("Bool"))
	checkDocumentedUDF(t, "Math::Pow", []model.Type{model.Optional(udfKind("Double")), udfKind("Double")}, model.Optional(udfKind("Double")))
}

func TestDateTimeNamedOptions(t *testing.T) {
	resource := udfKind(dateTimeTM)
	result, err := defaultRegistry.ResolveCall("DateTime::Update", []CallArgument{
		{Type: resource}, {Name: "Month", Type: udfKind("Uint8")}, {Name: "TimezoneId", Type: udfKind("Uint16")},
	})
	require.NoError(t, err)
	require.True(t, result.Equal(model.Optional(resource)))
	_, err = defaultRegistry.ResolveCall("DateTime::Update", []CallArgument{{Type: resource}, {Name: "Timezone", Type: udfKind("String")}})
	require.ErrorContains(t, err, "unknown named argument")
	result, err = defaultRegistry.ResolveCall("DateTime::Format", []CallArgument{
		{Type: udfKind("String")}, {Name: "AlwaysWriteFractionalSeconds", Type: udfKind("Bool")},
	})
	require.NoError(t, err)
	require.Equal(t, "Callable", result.Kind)
	result, err = defaultRegistry.ResolveCall("DateTime::Format", []CallArgument{
		{Type: udfKind("String")}, {Name: "WriteOffsetWithColon", Type: udfKind("Bool")},
	})
	require.NoError(t, err)
	require.Equal(t, "Callable", result.Kind)
}
