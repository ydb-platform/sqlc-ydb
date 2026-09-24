package builtins

import (
	"fmt"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

const (
	dateTimeTM   = "Resource<'DateTime2.TM'>"
	dateTimeTM64 = "Resource<'DateTime2.TM64'>"
)

func udfKind(kind string) model.Type { return model.Type{Kind: kind} }

func udfAuto(kind string) Parameter { return Parameter{Type: udfKind(kind), AutoMap: true} }

func udfRequired(kind string) Parameter { return Parameter{Type: udfKind(kind)} }

func udfOptional(kind string) Parameter {
	return Parameter{Type: model.Optional(udfKind(kind)), Optional: true}
}

func udfResolver(name string, signatures ...Signature) functionResolver {
	return func(args []model.Type) (model.Type, error) {
		calls := make([]CallArgument, len(args))
		for i := range args {
			calls[i].Type = args[i]
		}
		return resolveSignatures(name, calls, signatures)
	}
}

func udfSignature(name string, result model.Type, parameters ...Parameter) Signature {
	return Signature{Name: name, Arguments: parameters, Returns: result}
}

func lookupDateTimeDigestIpMath(name string) functionResolver {
	if resolver := lookupDateTimeUDF(name); resolver != nil {
		return resolver
	}
	if resolver := lookupDigestUDF(name); resolver != nil {
		return resolver
	}
	if resolver := lookupIpUDF(name); resolver != nil {
		return resolver
	}
	return lookupMathUDF(name)
}

func lookupDateTimeUDF(name string) functionResolver {
	switch name {
	case "DateTime::Split":
		return func(args []model.Type) (model.Type, error) {
			if err := arity(name, args, 1); err != nil {
				return model.Type{}, err
			}
			base, nullable, err := baseType(args[0])
			if err != nil {
				return model.Type{}, err
			}
			switch {
			case isBasicDateTime(base.Kind):
				return withOptional(udfKind(dateTimeTM), nullable), nil
			case isExtendedDateTime(base.Kind):
				return withOptional(udfKind(dateTimeTM64), nullable), nil
			default:
				return model.Type{}, fmt.Errorf("%s argument 1 must be a primitive date/time value", name)
			}
		}
	case "DateTime::GetYear", "DateTime::GetDayOfYear", "DateTime::GetMonth", "DateTime::GetMonthName",
		"DateTime::GetWeekOfYear", "DateTime::GetWeekOfYearIso8601", "DateTime::GetDayOfMonth",
		"DateTime::GetDayOfWeek", "DateTime::GetDayOfWeekName", "DateTime::GetHour",
		"DateTime::GetMinute", "DateTime::GetSecond", "DateTime::GetMillisecondOfSecond",
		"DateTime::GetMicrosecondOfSecond", "DateTime::GetTimezoneId", "DateTime::GetTimezoneName":
		return func(args []model.Type) (model.Type, error) { return resolveDateTimeGet(name, args) }
	case "DateTime::MakeDate", "DateTime::MakeDate32", "DateTime::MakeTzDate32",
		"DateTime::MakeDatetime", "DateTime::MakeTzDatetime", "DateTime::MakeDatetime64",
		"DateTime::MakeTzDatetime64", "DateTime::MakeTimestamp", "DateTime::MakeTzTimestamp",
		"DateTime::MakeTimestamp64", "DateTime::MakeTzTimestamp64":
		return func(args []model.Type) (model.Type, error) { return resolveDateTimeMake(name, args) }
	case "DateTime::FromSeconds", "DateTime::FromSeconds64", "DateTime::FromMilliseconds",
		"DateTime::FromMilliseconds64", "DateTime::FromMicroseconds", "DateTime::FromMicroseconds64",
		"DateTime::IntervalFromDays", "DateTime::Interval64FromDays", "DateTime::IntervalFromHours",
		"DateTime::Interval64FromHours", "DateTime::IntervalFromMinutes", "DateTime::Interval64FromMinutes",
		"DateTime::IntervalFromSeconds", "DateTime::Interval64FromSeconds",
		"DateTime::IntervalFromMilliseconds", "DateTime::Interval64FromMilliseconds",
		"DateTime::IntervalFromMicroseconds", "DateTime::Interval64FromMicroseconds":
		return func(args []model.Type) (model.Type, error) { return resolveDateTimeFrom(name, args) }
	case "DateTime::ToDays", "DateTime::ToHours", "DateTime::ToMinutes", "DateTime::ToSeconds",
		"DateTime::ToMilliseconds", "DateTime::ToMicroseconds":
		return func(args []model.Type) (model.Type, error) { return resolveDateTimeTo(name, args) }
	case "DateTime::Update":
		return udfResolver(name, dateTimeUpdateSignatures()...)
	case "DateTime::StartOfYear", "DateTime::EndOfYear", "DateTime::StartOfQuarter", "DateTime::EndOfQuarter",
		"DateTime::StartOfMonth", "DateTime::EndOfMonth", "DateTime::StartOfWeek", "DateTime::EndOfWeek",
		"DateTime::StartOfDay", "DateTime::EndOfDay", "DateTime::TimeOfDay":
		return func(args []model.Type) (model.Type, error) { return resolveDateTimeBoundary(name, args) }
	case "DateTime::StartOf", "DateTime::EndOf":
		return func(args []model.Type) (model.Type, error) { return resolveDateTimeBoundaryInterval(name, args) }
	case "DateTime::ShiftYears", "DateTime::ShiftQuarters", "DateTime::ShiftMonths":
		return func(args []model.Type) (model.Type, error) { return resolveDateTimeShift(name, args) }
	case "DateTime::Format":
		return udfResolver(name, dateTimeFormatSignature(name))
	case "DateTime::Parse", "DateTime::Parse64":
		resource := udfKind(dateTimeTM)
		if name == "DateTime::Parse64" {
			resource = udfKind(dateTimeTM64)
		}
		result := model.Optional(resource)
		callable := model.Type{Kind: "Callable", Items: []model.Type{udfKind("String")}, Elem: &result}
		return udfResolver(name, udfSignature(name, callable, udfRequired("String")))
	case "DateTime::ParseRfc822", "DateTime::ParseIso8601", "DateTime::ParseHttp", "DateTime::ParseX509":
		return udfResolver(name, udfSignature(name, model.Optional(udfKind(dateTimeTM)), udfAuto("String")))
	default:
		return nil
	}
}

func dateTimeResourceArgument(name string, args []model.Type) (string, bool, error) {
	if err := arity(name, args, 1); err != nil {
		return "", false, err
	}
	base, nullable, err := baseType(args[0])
	if err != nil {
		return "", false, err
	}
	switch {
	case base.Kind == dateTimeTM || isBasicDateTime(base.Kind):
		return dateTimeTM, nullable, nil
	case base.Kind == dateTimeTM64 || isExtendedDateTime(base.Kind):
		return dateTimeTM64, nullable, nil
	default:
		return "", false, fmt.Errorf("%s argument 1 must be a DateTime resource or primitive date/time value", name)
	}
}

func resolveDateTimeGet(name string, args []model.Type) (model.Type, error) {
	resource, nullable, err := dateTimeResourceArgument(name, args)
	if err != nil {
		return model.Type{}, err
	}
	result := "Uint8"
	switch name {
	case "DateTime::GetYear":
		result = "Uint16"
		if resource == dateTimeTM64 {
			result = "Int32"
		}
	case "DateTime::GetDayOfYear", "DateTime::GetTimezoneId":
		result = "Uint16"
	case "DateTime::GetMillisecondOfSecond", "DateTime::GetMicrosecondOfSecond":
		result = "Uint32"
	case "DateTime::GetMonthName", "DateTime::GetDayOfWeekName", "DateTime::GetTimezoneName":
		result = "String"
	}
	return withOptional(udfKind(result), nullable), nil
}

func resolveDateTimeMake(name string, args []model.Type) (model.Type, error) {
	resource, nullable, err := dateTimeResourceArgument(name, args)
	if err != nil {
		return model.Type{}, err
	}
	result := name[len("DateTime::Make"):]
	wantsWide := result == "Date32" || result == "TzDate32" || result == "Datetime64" ||
		result == "TzDatetime64" || result == "Timestamp64" || result == "TzTimestamp64"
	required := dateTimeTM
	if wantsWide {
		required = dateTimeTM64
	}
	if resource != required {
		return model.Type{}, fmt.Errorf("%s argument 1 requires %s", name, required)
	}
	return withOptional(udfKind(result), nullable), nil
}

func resolveDateTimeFrom(name string, args []model.Type) (model.Type, error) {
	input, output := "", ""
	switch name {
	case "DateTime::FromSeconds":
		input, output = "Uint32", "Timestamp"
	case "DateTime::FromMilliseconds", "DateTime::FromMicroseconds":
		input, output = "Uint64", "Timestamp"
	case "DateTime::FromSeconds64", "DateTime::FromMilliseconds64", "DateTime::FromMicroseconds64":
		input, output = "Int64", "Timestamp64"
	case "DateTime::IntervalFromDays", "DateTime::IntervalFromHours", "DateTime::IntervalFromMinutes":
		input, output = "Int32", "Interval"
	case "DateTime::IntervalFromSeconds", "DateTime::IntervalFromMilliseconds", "DateTime::IntervalFromMicroseconds":
		input, output = "Int64", "Interval"
	case "DateTime::Interval64FromDays":
		input, output = "Int32", "Interval64"
	case "DateTime::Interval64FromHours", "DateTime::Interval64FromMinutes", "DateTime::Interval64FromSeconds",
		"DateTime::Interval64FromMilliseconds", "DateTime::Interval64FromMicroseconds":
		input, output = "Int64", "Interval64"
	}
	return udfResolver(name, udfSignature(name, model.Optional(udfKind(output)), udfAuto(input)))(args)
}

func resolveDateTimeTo(name string, args []model.Type) (model.Type, error) {
	if err := arity(name, args, 1); err != nil {
		return model.Type{}, err
	}
	base, nullable, err := baseType(args[0])
	if err != nil {
		return model.Type{}, err
	}
	result := ""
	switch base.Kind {
	case "Interval", "Interval64":
		switch name {
		case "DateTime::ToDays":
			result = "Int32"
		case "DateTime::ToHours", "DateTime::ToMinutes":
			result = "Int32"
			if base.Kind == "Interval64" {
				result = "Int64"
			}
		default:
			result = "Int64"
		}
	case "Date", "Datetime", "Timestamp", "TzDate", "TzDatetime", "TzTimestamp":
		if name == "DateTime::ToSeconds" {
			result = "Uint32"
		} else if name != "DateTime::ToDays" && name != "DateTime::ToHours" && name != "DateTime::ToMinutes" {
			result = "Uint64"
		}
	case "Date32", "Datetime64", "Timestamp64", "TzDate32", "TzDatetime64", "TzTimestamp64":
		if name != "DateTime::ToDays" && name != "DateTime::ToHours" && name != "DateTime::ToMinutes" {
			result = "Int64"
		}
	}
	if result == "" {
		return model.Type{}, fmt.Errorf("%s argument 1 does not accept %s", name, args[0].String())
	}
	return withOptional(udfKind(result), nullable), nil
}

func dateTimeUpdateSignatures() []Signature {
	var signatures []Signature
	for _, input := range []string{
		dateTimeTM, "Date", "TzDate", "Datetime", "TzDatetime", "Timestamp", "TzTimestamp",
		dateTimeTM64, "Date32", "TzDate32", "Datetime64", "TzDatetime64", "Timestamp64", "TzTimestamp64",
	} {
		resource := dateTimeTM
		year := "Uint16"
		if input == dateTimeTM64 || isExtendedDateTime(input) {
			resource = dateTimeTM64
			year = "Int32"
		}
		parameters := []Parameter{udfAuto(input)}
		for _, field := range []struct{ name, kind string }{
			{"Year", year}, {"Month", "Uint8"}, {"Day", "Uint8"}, {"Hour", "Uint8"},
			{"Minute", "Uint8"}, {"Second", "Uint8"}, {"Microsecond", "Uint32"}, {"TimezoneId", "Uint16"},
		} {
			parameter := udfOptional(field.kind)
			parameter.Name = field.name
			parameters = append(parameters, parameter)
		}
		signatures = append(signatures, udfSignature("DateTime::Update", model.Optional(udfKind(resource)), parameters...))
	}
	return signatures
}

func resolveDateTimeBoundary(name string, args []model.Type) (model.Type, error) {
	resource, nullable, err := dateTimeResourceArgument(name, args)
	if err != nil {
		return model.Type{}, err
	}
	if name == "DateTime::TimeOfDay" {
		if resource == dateTimeTM64 {
			return withOptional(udfKind("Interval64"), nullable), nil
		}
		return withOptional(udfKind("Interval"), nullable), nil
	}
	return model.Optional(udfKind(resource)), nil
}

func resolveDateTimeBoundaryInterval(name string, args []model.Type) (model.Type, error) {
	if len(args) != 2 {
		return model.Type{}, fmt.Errorf("%s expects 2 arguments, got %d", name, len(args))
	}
	resource, _, err := dateTimeResourceArgument(name, args[:1])
	if err != nil {
		return model.Type{}, err
	}
	interval := "Interval"
	if resource == dateTimeTM64 {
		interval = "Interval64"
	}
	if err := requireOptionalScalar(name, args, 1, interval); err != nil {
		return model.Type{}, err
	}
	return model.Optional(udfKind(resource)), nil
}

func resolveDateTimeShift(name string, args []model.Type) (model.Type, error) {
	if len(args) != 2 {
		return model.Type{}, fmt.Errorf("%s expects 2 arguments, got %d", name, len(args))
	}
	resource, _, err := dateTimeResourceArgument(name, args[:1])
	if err != nil {
		return model.Type{}, err
	}
	if err := requireExact(name, args, 1, "Int32"); err != nil {
		return model.Type{}, err
	}
	return model.Optional(udfKind(resource)), nil
}

func dateTimeFormatSignature(name string) Signature {
	result := udfKind("String")
	callable := model.Type{Kind: "Callable", Items: []model.Type{udfKind(dateTimeTM64)}, Elem: &result}
	second := udfOptional("Bool")
	second.Name = "AlwaysWriteFractionalSeconds"
	third := udfOptional("Bool")
	third.Name = "WriteOffsetWithColon"
	return udfSignature(name, callable, udfRequired("String"), second, third)
}

func lookupDigestUDF(name string) functionResolver {
	stringAuto := udfAuto("String")
	switch name {
	case "Digest::CityHash128", "Digest::FarmHashFingerprint128", "Digest::XXH3_128":
		result := model.Type{Kind: "Tuple", Items: []model.Type{udfKind("Uint64"), udfKind("Uint64")}}
		return udfResolver(name, udfSignature(name, result, stringAuto))
	case "Digest::Argon2":
		return udfResolver(name, udfSignature(name, udfKind("String"), stringAuto, stringAuto))
	case "Digest::Blake2B":
		return udfResolver(name, udfSignature(name, udfKind("String"), stringAuto, udfOptional("String")))
	case "Digest::SipHash":
		return udfResolver(name, udfSignature(name, udfKind("Uint64"), udfRequired("Uint64"), udfRequired("Uint64"), stringAuto))
	case "Digest::HighwayHash":
		return udfResolver(name, udfSignature(name, udfKind("Uint64"),
			udfRequired("Uint64"), udfRequired("Uint64"), udfRequired("Uint64"), udfRequired("Uint64"), stringAuto))
	case "Digest::FarmHashFingerprint2":
		return udfResolver(name, udfSignature(name, udfKind("Uint64"), udfAuto("Uint64"), udfAuto("Uint64")))
	default:
		return nil
	}
}

func lookupIpUDF(name string) functionResolver {
	stringAuto := udfAuto("String")
	switch name {
	case "Ip::FromString", "Ip::SubnetFromString":
		return udfResolver(name, udfSignature(name, model.Optional(udfKind("String")), stringAuto))
	case "Ip::ToString", "Ip::SubnetToString", "Ip::ConvertToIPv6":
		return udfResolver(name, udfSignature(name, udfKind("String"), stringAuto))
	case "Ip::IsIPv4", "Ip::IsIPv6", "Ip::IsEmbeddedIPv4":
		return udfResolver(name, udfSignature(name, udfKind("Bool"), Parameter{Type: model.Optional(udfKind("String"))}))
	case "Ip::GetSubnet":
		return udfResolver(name, udfSignature(name, udfKind("String"), stringAuto, udfOptional("Uint8")))
	case "Ip::GetSubnetByMask":
		return udfResolver(name, udfSignature(name, udfKind("String"), stringAuto, stringAuto))
	case "Ip::SubnetMatch":
		return udfResolver(name, udfSignature(name, udfKind("Bool"), stringAuto, stringAuto))
	default:
		return nil
	}
}

func lookupMathUDF(name string) functionResolver {
	doubleAuto := udfAuto("Double")
	switch name {
	case "Math::Pi", "Math::E", "Math::Eps":
		return udfResolver(name, udfSignature(name, udfKind("Double")))
	case "Math::IsInf", "Math::IsNaN", "Math::IsFinite":
		return mathResolver(name, udfSignature(name, udfKind("Bool"), doubleAuto))
	case "Math::Abs", "Math::Acos", "Math::Asin", "Math::Asinh", "Math::Atan", "Math::Cbrt",
		"Math::Ceil", "Math::Cos", "Math::Cosh", "Math::Erf", "Math::ErfInv", "Math::ErfcInv",
		"Math::Exp", "Math::Exp2", "Math::Fabs", "Math::Floor", "Math::Lgamma", "Math::Rint",
		"Math::Sigmoid", "Math::Sin", "Math::Sinh", "Math::Sqrt", "Math::Tan", "Math::Tanh",
		"Math::Tgamma", "Math::Trunc", "Math::Log", "Math::Log2", "Math::Log10":
		return mathResolver(name, udfSignature(name, udfKind("Double"), doubleAuto))
	case "Math::Atan2", "Math::Fmod", "Math::Hypot", "Math::Pow", "Math::Remainder":
		return mathResolver(name, udfSignature(name, udfKind("Double"), doubleAuto, doubleAuto))
	case "Math::Ldexp":
		return mathResolver(name, udfSignature(name, udfKind("Double"), doubleAuto, udfAuto("Int32")))
	case "Math::Round":
		return mathResolver(name, udfSignature(name, udfKind("Double"), doubleAuto, udfOptional("Int32")))
	case "Math::FuzzyEquals":
		return mathResolver(name, udfSignature(name, udfKind("Bool"), doubleAuto, doubleAuto, udfOptional("Double")))
	case "Math::Mod", "Math::Rem":
		return udfResolver(name, udfSignature(name, model.Optional(udfKind("Int64")), udfAuto("Int64"), udfRequired("Int64")))
	case "Math::RoundDownward", "Math::RoundToNearest", "Math::RoundTowardZero", "Math::RoundUpward":
		return udfResolver(name, udfSignature(name, mathRoundingMode()))
	case "Math::NearbyInt":
		return mathResolver(name, udfSignature(name, model.Optional(udfKind("Int64")), doubleAuto, Parameter{Type: mathRoundingMode()}))
	default:
		return nil
	}
}

func mathResolver(name string, signature Signature) functionResolver {
	resolve := udfResolver(name, signature)
	return func(args []model.Type) (model.Type, error) {
		converted := append([]model.Type(nil), args...)
		for i := range converted {
			if i >= len(signature.Arguments) {
				break
			}
			expected, _, err := baseType(signature.Arguments[i].Type)
			if err != nil || expected.Kind != "Double" {
				continue
			}
			actual, nullable, err := baseType(converted[i])
			if err != nil || !mathNumericInput(actual.Kind) {
				continue
			}
			converted[i] = withOptional(udfKind("Double"), nullable)
		}
		return resolve(converted)
	}
}

func mathNumericInput(kind string) bool {
	switch kind {
	case "Uint8", "Int8", "Uint16", "Int16", "Uint32", "Int32", "Uint64", "Int64", "Float", "Double":
		return true
	default:
		return false
	}
}

func mathRoundingMode() model.Type {
	value := udfKind("Uint32")
	return model.Type{Kind: "Tagged", Elem: &value, Tag: "MathRoundingMode"}
}
