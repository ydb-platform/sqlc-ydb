// Package cpp renders the resolved YQL model as C++20 source for the native
// YDB C++ SDK or userver's YDB driver.
package cpp

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

type Options struct {
	Namespace string
	Runtime   string // ydb or userver
}

type scalarType struct {
	cpp       string
	builder   string
	parser    string
	reference bool
}

func Generate(in *model.AnalysisResult, options Options) ([]model.File, error) {
	if in == nil {
		return nil, fmt.Errorf("analysis result is nil")
	}
	if len(in.Diagnostics) != 0 {
		return nil, fmt.Errorf("cannot generate with diagnostics: %s", in.Diagnostics[0])
	}
	if options.Namespace == "" {
		options.Namespace = "db"
	}
	if err := validateNamespace(options.Namespace); err != nil {
		return nil, err
	}
	if options.Runtime == "" {
		options.Runtime = "ydb"
	}
	if options.Runtime != "ydb" && options.Runtime != "userver" {
		return nil, fmt.Errorf("unsupported C++ runtime %q", options.Runtime)
	}
	if err := validate(in, options); err != nil {
		return nil, err
	}

	models, err := renderModels(in, options)
	if err != nil {
		return nil, err
	}
	return []model.File{
		{Name: "models.hpp", Content: []byte(models)},
		{Name: "queries.hpp", Content: []byte(renderHeader(in, options))},
		{Name: "queries.cpp", Content: []byte(renderSource(in, options))},
	}, nil
}

func validate(in *model.AnalysisResult, options Options) error {
	rowTypes := make(map[string]string)
	for _, query := range in.Queries {
		if query.Command == model.One || query.Command == model.Many {
			rowTypes[query.Name+"Row"] = query.Name
		}
	}

	seenQueries := map[string]bool{}
	for _, query := range in.Queries {
		if err := validateIdent(query.Name); err != nil || query.Name == "Queries" || query.Name == "client_" {
			return fmt.Errorf("invalid C++ query name %q", query.Name)
		}
		if owner, exists := rowTypes[query.Name]; exists {
			return fmt.Errorf("query name %q collides with generated row type for %s", query.Name, owner)
		}
		if seenQueries[query.Name] {
			return fmt.Errorf("duplicate C++ query name %q", query.Name)
		}
		seenQueries[query.Name] = true
		switch query.Command {
		case model.One, model.Many, model.Exec:
		case model.ExecRows:
			return fmt.Errorf("%s: :execrows is unavailable for C++ runtime %s", query.Name, options.Runtime)
		default:
			return fmt.Errorf("%s: unsupported command %q", query.Name, query.Command)
		}
		if (query.Command == model.One || query.Command == model.Many) &&
			(len(query.ResultSets) != 1 || len(query.ResultSets[0].Columns) == 0) {
			return fmt.Errorf("%s: %s requires one non-empty result set", query.Name, query.Command)
		}

		seenParams := map[string]bool{}
		for _, parameter := range query.Parameters {
			if err := validateIdent(parameter.Name); err != nil {
				return fmt.Errorf("%s: invalid C++ parameter %q: %w", query.Name, parameter.Name, err)
			}
			if seenParams[parameter.Name] {
				return fmt.Errorf("%s: duplicate C++ parameter %q", query.Name, parameter.Name)
			}
			if (query.Command == model.One || query.Command == model.Many) && parameter.Name == query.Name+"Row" {
				return fmt.Errorf("%s: parameter %q collides with generated row type", query.Name, parameter.Name)
			}
			seenParams[parameter.Name] = true
			if _, err := typeInfo(parameter.Type, options.Runtime); err != nil {
				return fmt.Errorf("%s parameter %s: %w", query.Name, parameter.Name, err)
			}
		}
		for _, resultSet := range query.ResultSets {
			seenColumns := map[string]bool{}
			for _, column := range resultSet.Columns {
				if err := validateIdent(column.Name); err != nil {
					return fmt.Errorf("%s: invalid C++ result column %q: %w", query.Name, column.Name, err)
				}
				if seenColumns[column.Name] {
					return fmt.Errorf("%s: duplicate C++ result column %q", query.Name, column.Name)
				}
				if (query.Command == model.One || query.Command == model.Many) && column.Name == query.Name+"Row" {
					return fmt.Errorf("%s: result column %q collides with generated row type", query.Name, column.Name)
				}
				seenColumns[column.Name] = true
				if _, err := typeInfo(column.Type, options.Runtime); err != nil {
					return fmt.Errorf("%s column %s: %w", query.Name, column.Name, err)
				}
			}
		}
	}
	return nil
}

func validateNamespace(namespace string) error {
	parts := strings.Split(namespace, "::")
	for _, part := range parts {
		if err := validateIdent(part); err != nil {
			return fmt.Errorf("invalid C++ namespace %q: %w", namespace, err)
		}
	}
	return nil
}

func validateIdent(name string) error {
	if name == "" {
		return fmt.Errorf("identifier is empty")
	}
	if strings.HasPrefix(name, "sqlc_") {
		return fmt.Errorf("reserved sqlc_ prefix")
	}
	if strings.HasPrefix(name, "_") {
		return fmt.Errorf("leading underscore is not supported")
	}
	if cppKeywords[name] {
		return fmt.Errorf("C++ keyword")
	}
	for index, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (index > 0 && r >= '0' && r <= '9') || (index > 0 && r == '_')) {
			return fmt.Errorf("only ASCII C++ identifiers are supported")
		}
	}
	return nil
}

var cppKeywords = map[string]bool{
	"alignas": true, "alignof": true, "and": true, "and_eq": true, "asm": true,
	"auto": true, "bitand": true, "bitor": true, "bool": true, "break": true,
	"case": true, "catch": true, "char": true, "char8_t": true, "char16_t": true,
	"char32_t": true, "class": true, "compl": true, "concept": true, "const": true,
	"consteval": true, "constexpr": true, "constinit": true, "const_cast": true, "continue": true,
	"co_await": true, "co_return": true, "co_yield": true, "decltype": true, "default": true,
	"delete": true, "do": true, "double": true, "dynamic_cast": true, "else": true,
	"enum": true, "explicit": true, "export": true, "extern": true, "false": true,
	"float": true, "for": true, "friend": true, "goto": true, "if": true,
	"inline": true, "int": true, "long": true, "mutable": true, "namespace": true,
	"new": true, "noexcept": true, "not": true, "not_eq": true, "nullptr": true,
	"operator": true, "or": true, "or_eq": true, "private": true, "protected": true,
	"public": true, "register": true, "reinterpret_cast": true, "requires": true, "return": true,
	"short": true, "signed": true, "sizeof": true, "static": true, "static_assert": true,
	"static_cast": true, "struct": true, "switch": true, "template": true, "this": true,
	"thread_local": true, "throw": true, "true": true, "try": true, "typedef": true,
	"typeid": true, "typename": true, "union": true, "unsigned": true, "using": true,
	"virtual": true, "void": true, "volatile": true, "wchar_t": true, "while": true,
	"xor": true, "xor_eq": true,
}

func typeInfo(yqlType model.Type, runtime string) (scalarType, error) {
	if yqlType.IsOptional() {
		if yqlType.Elem == nil {
			return scalarType{}, fmt.Errorf("Optional lacks element")
		}
		if yqlType.Elem.IsOptional() {
			return scalarType{}, fmt.Errorf("nested Optional is unsupported")
		}
		base, err := typeInfo(*yqlType.Elem, runtime)
		if err != nil {
			return scalarType{}, err
		}
		base.cpp = "std::optional<" + base.cpp + ">"
		base.builder = "Optional" + base.builder
		base.parser = "GetOptional" + strings.TrimPrefix(base.parser, "Get")
		base.reference = true
		return base, nil
	}

	kind := strings.ToLower(yqlType.Kind)
	if runtime == "userver" && kind == "float" {
		return scalarType{}, fmt.Errorf("unsupported YQL type %q for userver", yqlType.Kind)
	}
	var info scalarType
	switch kind {
	case "bool":
		info = scalarType{cpp: "bool", builder: "Bool", parser: "GetBool"}
	case "int8":
		info = scalarType{cpp: "std::int8_t", builder: "Int8", parser: "GetInt8"}
	case "uint8":
		info = scalarType{cpp: "std::uint8_t", builder: "Uint8", parser: "GetUint8"}
	case "int16":
		info = scalarType{cpp: "std::int16_t", builder: "Int16", parser: "GetInt16"}
	case "uint16":
		info = scalarType{cpp: "std::uint16_t", builder: "Uint16", parser: "GetUint16"}
	case "int32":
		info = scalarType{cpp: "std::int32_t", builder: "Int32", parser: "GetInt32"}
	case "uint32":
		info = scalarType{cpp: "std::uint32_t", builder: "Uint32", parser: "GetUint32"}
	case "int64":
		info = scalarType{cpp: "std::int64_t", builder: "Int64", parser: "GetInt64"}
	case "uint64":
		info = scalarType{cpp: "std::uint64_t", builder: "Uint64", parser: "GetUint64"}
	case "float":
		info = scalarType{cpp: "float", builder: "Float", parser: "GetFloat"}
	case "double":
		info = scalarType{cpp: "double", builder: "Double", parser: "GetDouble"}
	case "string":
		info = scalarType{cpp: "std::string", builder: "String", parser: "GetString", reference: true}
	case "utf8":
		cppType := "std::string"
		if runtime == "userver" {
			cppType = "::userver::ydb::Utf8"
		}
		info = scalarType{cpp: cppType, builder: "Utf8", parser: "GetUtf8", reference: true}
	default:
		return scalarType{}, fmt.Errorf("unsupported YQL type %q for %s", yqlType.Kind, runtime)
	}
	return info, nil
}

func renderModels(in *model.AnalysisResult, options Options) (string, error) {
	var out strings.Builder
	out.WriteString("// Code generated by sqlc-ydb. DO NOT EDIT.\n#pragma once\n\n#include <cstdint>\n#include <optional>\n#include <string>\n")
	if options.Runtime == "userver" {
		out.WriteString("\n#include <userver/ydb/types.hpp>\n")
	}
	out.WriteString("\nnamespace " + options.Namespace + " {\n\n")
	for _, query := range in.Queries {
		if query.Command != model.One && query.Command != model.Many {
			continue
		}
		out.WriteString("struct " + query.Name + "Row final {\n")
		for _, column := range query.ResultSets[0].Columns {
			info, err := typeInfo(column.Type, options.Runtime)
			if err != nil {
				return "", err
			}
			out.WriteString("    " + info.cpp + " " + column.Name + ";\n")
		}
		out.WriteString("};\n\n")
	}
	out.WriteString("}  // namespace " + options.Namespace + "\n")
	return out.String(), nil
}

func renderHeader(in *model.AnalysisResult, options Options) string {
	var out strings.Builder
	out.WriteString("// Code generated by sqlc-ydb. DO NOT EDIT.\n#pragma once\n\n#include \"models.hpp\"\n\n#include <cstdint>\n#include <optional>\n#include <string>\n#include <vector>\n")
	clientType := "NYdb::NQuery::TQueryClient"
	if options.Runtime == "ydb" {
		out.WriteString("\n#include <ydb-cpp-sdk/client/query/client.h>\n")
	} else {
		clientType = "::userver::ydb::TableClient"
		out.WriteString("\n#include <userver/ydb/table.hpp>\n")
	}
	out.WriteString("\nnamespace " + options.Namespace + " {\n\nclass Queries final {\npublic:\n")
	out.WriteString("    explicit Queries(" + clientType + "& client) noexcept : client_(client) {}\n\n")
	for _, query := range in.Queries {
		returnType := "void"
		if query.Command == model.One {
			returnType = "std::optional<" + query.Name + "Row>"
		} else if query.Command == model.Many {
			returnType = "std::vector<" + query.Name + "Row>"
		}
		out.WriteString("    " + returnType + " " + query.Name + "(" + methodParameters(query, options.Runtime) + ") const;\n")
	}
	out.WriteString("\nprivate:\n    " + clientType + "& client_;\n};\n\n}  // namespace " + options.Namespace + "\n")
	return out.String()
}

func methodParameters(query model.AnalyzedQuery, runtime string) string {
	parts := make([]string, 0, len(query.Parameters))
	for _, parameter := range query.Parameters {
		info, _ := typeInfo(parameter.Type, runtime)
		declaration := info.cpp + " " + parameter.Name
		if info.reference {
			declaration = "const " + info.cpp + "& " + parameter.Name
		}
		parts = append(parts, declaration)
	}
	return strings.Join(parts, ", ")
}

func renderSource(in *model.AnalysisResult, options Options) string {
	var out strings.Builder
	out.WriteString("// Code generated by sqlc-ydb. DO NOT EDIT.\n#include \"queries.hpp\"\n")
	if options.Runtime == "ydb" {
		out.WriteString("\n#include <ydb-cpp-sdk/client/params/params.h>\n#include <ydb-cpp-sdk/client/result/result.h>\n#include <ydb-cpp-sdk/client/types/status/status.h>\n")
	}
	out.WriteString("\n#include <stdexcept>\n#include <utility>\n\nnamespace " + options.Namespace + " {\n\n")
	for _, query := range in.Queries {
		if options.Runtime == "ydb" {
			renderNativeMethod(&out, query, options)
		} else {
			renderUserverMethod(&out, query, options)
		}
	}
	out.WriteString("}  // namespace " + options.Namespace + "\n")
	return out.String()
}

func renderNativeMethod(out *strings.Builder, query model.AnalyzedQuery, options Options) {
	returnType := "void"
	if query.Command == model.One {
		returnType = "std::optional<" + query.Name + "Row>"
	} else if query.Command == model.Many {
		returnType = "std::vector<" + query.Name + "Row>"
	}
	out.WriteString(returnType + " Queries::" + query.Name + "(" + methodParameters(query, options.Runtime) + ") const {\n")
	if query.Command == model.One || query.Command == model.Many {
		out.WriteString("    std::optional<NYdb::TResultSet> sqlc_result_set;\n")
	}
	out.WriteString("    const auto sqlc_status = this->client_.RetryQuerySync([&](NYdb::NQuery::TSession sqlc_session) -> NYdb::TStatus {\n")
	if len(query.Parameters) != 0 {
		out.WriteString("        auto sqlc_params = NYdb::TParamsBuilder()")
		for _, parameter := range query.Parameters {
			info, _ := typeInfo(parameter.Type, options.Runtime)
			out.WriteString("\n            .AddParam(" + strconv.Quote("$"+parameter.Name) + ")." + info.builder + "(" + parameter.Name + ").Build()")
		}
		out.WriteString("\n            .Build();\n")
	}
	out.WriteString("        auto sqlc_result = sqlc_session.ExecuteQuery(\n            " + sqlLiteral(query.SQL) + ",\n            NYdb::NQuery::TTxControl::BeginTx(NYdb::NQuery::TTxSettings::SerializableRW()).CommitTx()")
	if len(query.Parameters) != 0 {
		out.WriteString(",\n            sqlc_params")
	}
	out.WriteString("\n        ).GetValueSync();\n")
	if query.Command == model.One || query.Command == model.Many {
		out.WriteString("        if (sqlc_result.IsSuccess() && !sqlc_result.GetResultSets().empty()) {\n            sqlc_result_set = sqlc_result.GetResultSet(0);\n        }\n")
	}
	out.WriteString("        return sqlc_result;\n    });\n    NYdb::NStatusHelpers::ThrowOnError(sqlc_status);\n")
	if query.Command == model.Exec {
		out.WriteString("}\n\n")
		return
	}
	out.WriteString("    if (!sqlc_result_set) {\n        throw std::runtime_error(" + strconv.Quote(query.Name+": successful query returned no result set") + ");\n    }\n    NYdb::TResultSetParser sqlc_parser(*sqlc_result_set);\n")
	if query.Command == model.One {
		out.WriteString("    if (!sqlc_parser.TryNextRow()) {\n        return std::nullopt;\n    }\n    " + query.Name + "Row sqlc_row{\n")
		writeNativeRow(out, query.ResultSets[0], options.Runtime, "        ")
		out.WriteString("    };\n    return sqlc_row;\n}\n\n")
		return
	}
	out.WriteString("    std::vector<" + query.Name + "Row> sqlc_rows;\n    sqlc_rows.reserve(sqlc_result_set->RowsCount());\n    while (sqlc_parser.TryNextRow()) {\n        sqlc_rows.push_back(" + query.Name + "Row{\n")
	writeNativeRow(out, query.ResultSets[0], options.Runtime, "            ")
	out.WriteString("        });\n    }\n    return sqlc_rows;\n}\n\n")
}

func writeNativeRow(out *strings.Builder, resultSet model.ResultSet, runtime, indent string) {
	for _, column := range resultSet.Columns {
		info, _ := typeInfo(column.Type, runtime)
		out.WriteString(indent + "sqlc_parser.ColumnParser(" + strconv.Quote(column.ResultName()) + ")." + info.parser + "(),\n")
	}
}

func renderUserverMethod(out *strings.Builder, query model.AnalyzedQuery, options Options) {
	returnType := "void"
	if query.Command == model.One {
		returnType = "std::optional<" + query.Name + "Row>"
	} else if query.Command == model.Many {
		returnType = "std::vector<" + query.Name + "Row>"
	}
	out.WriteString(returnType + " Queries::" + query.Name + "(" + methodParameters(query, options.Runtime) + ") const {\n")
	call := "this->client_.ExecuteQuery(\n        ::userver::ydb::Query{\n            " + sqlLiteral(query.SQL) + ",\n            ::userver::ydb::Query::NameLiteral{" + strconv.Quote(query.Name) + "},\n            ::userver::ydb::Query::LogMode::kNameOnly,\n        }"
	for _, parameter := range query.Parameters {
		call += ", " + strconv.Quote("$"+parameter.Name) + ", " + parameter.Name
	}
	call += ")"
	if query.Command == model.Exec {
		out.WriteString("    static_cast<void>(" + call + ");\n}\n\n")
		return
	}
	out.WriteString("    auto sqlc_response = " + call + ";\n    auto sqlc_cursor = sqlc_response.GetSingleCursor();\n")
	if query.Command == model.One {
		out.WriteString("    if (sqlc_cursor.empty()) {\n        return std::nullopt;\n    }\n    auto sqlc_row = sqlc_cursor.GetFirstRow();\n    return " + query.Name + "Row{\n")
		writeUserverRow(out, query.ResultSets[0], options.Runtime, "        ")
		out.WriteString("    };\n}\n\n")
		return
	}
	out.WriteString("    std::vector<" + query.Name + "Row> sqlc_rows;\n    sqlc_rows.reserve(sqlc_cursor.size());\n    for (auto sqlc_row : sqlc_cursor) {\n        sqlc_rows.push_back(" + query.Name + "Row{\n")
	writeUserverRow(out, query.ResultSets[0], options.Runtime, "            ")
	out.WriteString("        });\n    }\n    return sqlc_rows;\n}\n\n")
}

func writeUserverRow(out *strings.Builder, resultSet model.ResultSet, runtime, indent string) {
	for _, column := range resultSet.Columns {
		info, _ := typeInfo(column.Type, runtime)
		out.WriteString(indent + "sqlc_row.Get<" + info.cpp + ">(" + strconv.Quote(column.ResultName()) + "),\n")
	}
}

// sqlLiteral prefers readable raw strings. Bytes that C++ translation phases
// may rewrite or reject are emitted as isolated hexadecimal string fragments.
func sqlLiteral(sql string) string {
	delimiter := "sqlc"
	for suffix := 0; strings.Contains(sql, ")"+delimiter+"\""); suffix++ {
		delimiter = "sqlc" + strconv.Itoa(suffix+1)
	}

	var parts []string
	var raw bytes.Buffer
	flushRaw := func() {
		if raw.Len() == 0 {
			return
		}
		parts = append(parts, "R\""+delimiter+"("+raw.String()+")"+delimiter+"\"")
		raw.Reset()
	}
	for index := 0; index < len(sql); {
		r, size := utf8.DecodeRuneInString(sql[index:])
		if r == utf8.RuneError && size == 1 {
			flushRaw()
			parts = append(parts, fmt.Sprintf("\"\\x%02X\"", sql[index]))
			index++
			continue
		}
		unsafe := r == '\r' || r == 0x7f || r == 0xfeff || (r < 0x20 && r != '\n' && r != '\t')
		if unsafe {
			flushRaw()
			for _, value := range []byte(sql[index : index+size]) {
				parts = append(parts, fmt.Sprintf("\"\\x%02X\"", value))
			}
		} else if r == '\n' {
			flushRaw()
			parts = append(parts, `"\n"`)
		} else {
			raw.WriteString(sql[index : index+size])
		}
		index += size
	}
	flushRaw()
	if len(parts) == 0 {
		return "std::string{}"
	}
	if len(parts) == 1 && strings.HasPrefix(parts[0], "R\"") {
		return parts[0]
	}
	return "std::string{\n                " + strings.Join(parts, "\n                ") + ",\n                " + strconv.Itoa(len(sql)) + "\n            }"
}
