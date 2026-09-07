package analyzer

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-engine-ydb/internal/model"
	parser "github.com/ydb-platform/yql-parsers/go"
)

func literalType(expr parser.IExprContext) (model.Type, bool, error) {
	var literals []parser.ILiteral_valueContext
	descendants(expr, func(node antlr.Tree) {
		if literal, ok := node.(parser.ILiteral_valueContext); ok {
			literals = append(literals, literal)
		}
	})
	if len(literals) != 1 {
		return model.Type{}, false, nil
	}
	literal := literals[0]
	expressionText := expr.GetText()
	literalText := literal.GetText()
	if expressionText != literalText {
		return model.Type{}, false, nil
	}
	switch {
	case literal.Bool_value() != nil:
		return model.Type{Kind: "Bool"}, true, nil
	case literal.STRING_VALUE() != nil:
		typeValue, err := stringLiteralType(literal.GetText())
		return typeValue, true, err
	case literal.Integer() != nil:
		typeValue, err := integerLiteralType(expressionText, literal.Integer().INTEGER_VALUE() != nil)
		return typeValue, true, err
	case literal.Real_() != nil:
		typeValue, err := realLiteralType(expressionText)
		return typeValue, true, err
	default:
		return model.Type{}, false, nil
	}
}

func stringLiteralType(text string) (model.Type, error) {
	if len(text) < 2 {
		return model.Type{Kind: "String"}, nil
	}
	prefix, suffix := text[:len(text)-1], text[len(text)-1]
	if !(strings.HasSuffix(prefix, "'") || strings.HasSuffix(prefix, "\"") || strings.HasSuffix(prefix, "@@")) {
		return model.Type{Kind: "String"}, nil
	}
	switch strings.ToLower(string(suffix)) {
	case "s":
		return model.Type{Kind: "String"}, nil
	case "u":
		return model.Type{Kind: "Utf8"}, nil
	case "y":
		return model.Type{Kind: "Yson"}, nil
	case "j":
		return model.Type{Kind: "Json"}, nil
	default:
		return model.Type{}, fmt.Errorf("unsupported string literal suffix %q", string(text[len(text)-1]))
	}
}

func integerLiteralType(text string, hasSuffix bool) (model.Type, error) {
	lower := strings.ToLower(text)
	type suffixType struct {
		suffix   string
		kind     string
		bits     int
		unsigned bool
	}
	types := []suffixType{
		{suffix: "ul", kind: "Uint64", bits: 64, unsigned: true},
		{suffix: "us", kind: "Uint16", bits: 16, unsigned: true},
		{suffix: "ut", kind: "Uint8", bits: 8, unsigned: true},
		{suffix: "l", kind: "Int64", bits: 64},
		{suffix: "s", kind: "Int16", bits: 16},
		{suffix: "t", kind: "Int8", bits: 8},
		{suffix: "u", kind: "Uint32", bits: 32, unsigned: true},
	}
	for _, candidate := range types {
		if !strings.HasSuffix(lower, candidate.suffix) {
			continue
		}
		number, base := integerDigits(text[:len(text)-len(candidate.suffix)])
		var err error
		if candidate.unsigned {
			_, err = strconv.ParseUint(number, base, candidate.bits)
		} else {
			_, err = strconv.ParseInt(number, base, candidate.bits)
		}
		if err != nil {
			return model.Type{}, fmt.Errorf("integer literal %q is out of range for %s", text, candidate.kind)
		}
		return model.Type{Kind: candidate.kind}, nil
	}
	if hasSuffix {
		return model.Type{}, fmt.Errorf("unsupported integer literal suffix in %q", text)
	}
	number, base := integerDigits(text)
	value, err := strconv.ParseInt(number, base, 64)
	if err != nil {
		return model.Type{}, fmt.Errorf("integer literal %q is out of range for Int64", text)
	}
	if value >= -1<<31 && value <= 1<<31-1 {
		return model.Type{Kind: "Int32"}, nil
	}
	return model.Type{Kind: "Int64"}, nil
}

func integerDigits(text string) (string, int) {
	lower := strings.ToLower(text)
	switch {
	case strings.HasPrefix(lower, "0x"):
		return text[2:], 16
	case strings.HasPrefix(lower, "0o"):
		return text[2:], 8
	case strings.HasPrefix(lower, "0b"):
		return text[2:], 2
	default:
		return text, 10
	}
}

func realLiteralType(text string) (model.Type, error) {
	kind, bits, number := "Double", 64, text
	if strings.HasSuffix(strings.ToLower(text), "f") {
		kind, bits, number = "Float", 32, text[:len(text)-1]
	}
	if _, err := strconv.ParseFloat(number, bits); err != nil {
		return model.Type{}, fmt.Errorf("floating-point literal %q is out of range for %s", text, kind)
	}
	return model.Type{Kind: kind}, nil
}
