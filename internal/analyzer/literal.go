package analyzer

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	parser "github.com/ydb-platform/yql-parsers/go"
)

// Preserve only a direct literal's value for value-dependent builtin coercion.
// Arbitrary arithmetic and local bindings require their own static type rules.
func integerLiteralValue(root antlr.ParserRuleContext) *big.Int {
	for inner := parenthesizedExpression(root); inner != nil; inner = parenthesizedExpression(root) {
		root = inner
	}
	integer, ok := coveringExpressionContext(root).(*parser.IntegerContext)
	if !ok {
		return nil
	}
	text := strings.ToLower(integer.GetText())
	for _, suffix := range []string{"ul", "us", "ut", "l", "s", "t", "u"} {
		if strings.HasSuffix(text, suffix) {
			text = strings.TrimSuffix(text, suffix)
			break
		}
	}
	number, base := integerDigits(text)
	value, _ := new(big.Int).SetString(number, base)
	return value
}

func stringLiteralValue(root antlr.ParserRuleContext) *string {
	for inner := parenthesizedExpression(root); inner != nil; inner = parenthesizedExpression(root) {
		root = inner
	}
	var literal parser.ILiteral_valueContext
	descendants(root, func(node antlr.Tree) {
		if ctx, ok := node.(parser.ILiteral_valueContext); ok && sameSpan(root, ctx) && ctx.STRING_VALUE() != nil {
			literal = ctx
		}
	})
	if literal == nil {
		return nil
	}
	text := literal.GetText()
	if len(text) > 2 && (text[len(text)-1] == 's' || text[len(text)-1] == 'S') {
		text = text[:len(text)-1]
	}
	if strings.HasPrefix(text, "@@") && strings.HasSuffix(text, "@@") && len(text) >= 4 {
		value := text[2 : len(text)-2]
		return &value
	}
	if len(text) < 2 || text[0] != text[len(text)-1] || text[0] != '\'' && text[0] != '"' {
		return nil
	}
	var value strings.Builder
	for rest := text[1 : len(text)-1]; rest != ""; {
		character, multibyte, tail, err := strconv.UnquoteChar(rest, text[0])
		if err != nil {
			return nil
		}
		if multibyte {
			value.WriteRune(character)
		} else {
			value.WriteByte(byte(character))
		}
		rest = tail
	}
	decoded := value.String()
	return &decoded
}

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
	typeValue, err := literalValueType(literal)
	return typeValue, true, err
}

func literalValueType(literal parser.ILiteral_valueContext) (model.Type, error) {
	switch {
	case literal.NULL() != nil:
		return model.Type{Kind: "Null"}, nil
	case literal.Bool_value() != nil:
		return model.Type{Kind: "Bool"}, nil
	case literal.STRING_VALUE() != nil:
		return stringLiteralType(literal.GetText())
	case literal.Integer() != nil:
		return integerLiteralType(literal.GetText(), literal.Integer().INTEGER_VALUE() != nil)
	case literal.Real_() != nil:
		return realLiteralType(literal.GetText())
	default:
		return model.Type{}, fmt.Errorf("unsupported literal %q", literal.GetText())
	}
}

func stringLiteralType(text string) (model.Type, error) {
	if len(text) < 2 || strings.HasSuffix(text, "'") || strings.HasSuffix(text, "\"") || strings.HasSuffix(text, "@@") {
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
