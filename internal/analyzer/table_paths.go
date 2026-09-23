package analyzer

import (
	"fmt"
	"path"
	"strconv"
	"strings"
	"unicode"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	parser "github.com/ydb-platform/yql-parsers/go"
)

// A single leading absolute prefix is the supported compiler contract.
// Its scope is one schema source or one named query, never another file.
func tablePathPrefix(file string, lineOffset int, root parser.ISql_queryContext) (string, []model.Diagnostic) {
	var prefix string
	var diagnostics []model.Diagnostic
	if root.Sql_stmt_list() == nil {
		return prefix, diagnostics
	}
	started := false
	for _, statement := range root.Sql_stmt_list().AllSql_stmt() {
		core := statement.Sql_stmt_core()
		pragma := core.Pragma_stmt()
		if pragma == nil {
			if core.Declare_stmt() == nil {
				started = true
			}
			continue
		}
		message := ""
		switch {
		case pragma.Opt_id_prefix_or_type().GetText() != "" || !strings.EqualFold(identifier(pragma.An_id().GetText()), "TablePathPrefix"):
			message = "unsupported PRAGMA; only static TablePathPrefix is supported"
		case started:
			message = "PRAGMA TablePathPrefix must precede local bindings and data or schema statements"
		case len(pragma.AllPragma_value()) != 1 || pragma.Pragma_value(0).STRING_VALUE() == nil:
			message = "PRAGMA TablePathPrefix requires one static quoted absolute path, for example '/database/folder'; dynamic values and reset forms are unsupported"
		default:
			value, err := tablePathPrefixLiteral(pragma.Pragma_value(0).STRING_VALUE().GetText())
			if err != nil {
				message = err.Error()
			} else if prefix != "" && prefix != value {
				message = "conflicting PRAGMA TablePathPrefix values are unsupported; use one prefix per named query or schema file"
			} else {
				prefix = value
			}
		}
		if message != "" {
			diagnostics = append(diagnostics, diagnosticAt(file, lineOffset, pragma, message))
		}
	}
	return prefix, diagnostics
}

func tablePathPrefixLiteral(literal string) (string, error) {
	invalid := func() (string, error) {
		return "", fmt.Errorf("PRAGMA TablePathPrefix requires a nonempty absolute path in an ordinary quoted string, for example '/database/folder'")
	}
	if len(literal) < 2 || (literal[0] != '\'' && literal[0] != '"') || literal[len(literal)-1] != literal[0] {
		return invalid()
	}
	var value strings.Builder
	for rest := literal[1 : len(literal)-1]; rest != ""; {
		if len(rest) >= 2 && rest[0] == '\\' && (rest[1] == '\'' || rest[1] == '"') {
			value.WriteByte(rest[1])
			rest = rest[2:]
			continue
		}
		character, multibyte, tail, err := strconv.UnquoteChar(rest, literal[0])
		if err != nil || unicode.IsControl(character) {
			return "", fmt.Errorf("unsupported escape or control character in PRAGMA TablePathPrefix; use a quoted literal path")
		}
		if multibyte {
			value.WriteRune(character)
		} else {
			value.WriteByte(byte(character))
		}
		rest = tail
	}
	if !path.IsAbs(value.String()) {
		return invalid()
	}
	return path.Clean(value.String()), nil
}

func resolveTablePath(prefix, name string) string {
	if path.IsAbs(name) {
		return path.Clean(name)
	}
	if prefix == "" || name == "" {
		return name
	}
	return path.Join(prefix, name)
}

func resolvedTableReferences(root antlr.Tree, prefix string) map[int]string {
	tables := map[int]string{}
	descendants(root, func(node antlr.Tree) {
		switch ref := node.(type) {
		case *parser.Table_keyContext:
			tables[ref.Id_table_or_type().GetStart().GetTokenIndex()] = resolveTablePath(prefix, tableKeyName(ref))
		case *parser.Simple_table_refContext:
			if core := ref.Simple_table_ref_core(); core != nil {
				tables[core.GetStart().GetTokenIndex()] = resolveTablePath(prefix, simpleTableName(ref))
			}
		}
	})
	return tables
}
