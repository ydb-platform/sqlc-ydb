// Package csharp renders the resolved YQL model as modern C# code.
package csharp

import (
	"bytes"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

// Options controls the generated namespace and runtime profile.
type Options struct {
	Namespace string
	Runtime   string
}

func Generate(in *model.AnalysisResult, o Options) ([]model.File, error) {
	if in == nil {
		return nil, fmt.Errorf("csharp generator: analysis result is nil")
	}
	if len(in.Diagnostics) != 0 {
		return nil, fmt.Errorf("csharp generator: cannot generate with diagnostics: %s", in.Diagnostics[0])
	}
	if o.Namespace == "" {
		o.Namespace = "Db"
	}
	if o.Runtime == "" {
		o.Runtime = "adonet"
	}
	switch o.Runtime {
	case "adonet", "dapper":
	default:
		return nil, fmt.Errorf("csharp generator: unsupported runtime %q (supported: adonet, dapper)", o.Runtime)
	}
	if !namespace(o.Namespace) {
		return nil, fmt.Errorf("csharp generator: invalid namespace %q", o.Namespace)
	}
	if err := validate(in, o.Runtime); err != nil {
		return nil, err
	}
	return []model.File{
		{Name: "Models.cs", Content: renderModels(in, o)},
		{Name: "Queries.cs", Content: renderQueries(in, o)},
	}, nil
}

func validate(in *model.AnalysisResult, runtime string) error {
	methodNames := map[string]string{}
	// Queries is emitted by this generator, so no record may reuse its name.
	modelNames := map[string]string{"Queries": "generated query class"}
	for _, n := range []string{"Guid", "DateTime", "DateTimeKind", "Task", "CancellationToken", "List", "IReadOnlyList", "DbType", "DBNull", "DbDataReader", "ArgumentException", "ArgumentNullException", "InvalidOperationException", "YdbConnection", "YdbTransaction", "YdbCommand", "YdbParameter", "YdbValue"} {
		modelNames[n] = "framework type"
	}
	if runtime == "dapper" {
		for _, n := range []string{"CommandDefinition", "SqlMapper", "DefaultTypeMap", "Type", "ConstructorInfo", "Dictionary", "IReadOnlyDictionary"} {
			modelNames[n] = "framework type"
		}
	}
	add := func(dst map[string]string, name, original, what string) error {
		if old, ok := dst[name]; ok {
			return fmt.Errorf("csharp generator: %s collision %q (%q and %q)", what, name, old, original)
		}
		dst[name] = original
		return nil
	}
	for _, table := range in.Catalog.Tables {
		modelName := csName(table.Name)
		if !csIdent(modelName) {
			return fmt.Errorf("csharp generator: invalid model name for table %q", table.Name)
		}
		if err := add(modelNames, modelName, "table:"+table.Name, "model name"); err != nil {
			return err
		}
		if err := fields("table "+table.Name, modelName, table.Columns); err != nil {
			return err
		}
	}
	for _, q := range in.Queries {
		generatedName := csName(q.Name)
		if !csIdent(q.Name) || !csIdent(generatedName) {
			return fmt.Errorf("csharp generator: invalid query name %q", q.Name)
		}
		if err := add(methodNames, generatedName+"Async", q.Name, "method name"); err != nil {
			return err
		}
		switch q.Command {
		case model.One, model.Many, model.Exec:
		default:
			return fmt.Errorf("csharp generator: query %q: unsupported command %q", q.Name, q.Command)
		}
		if (q.Command == model.One || q.Command == model.Many) && (len(q.ResultSets) != 1 || len(q.ResultSets[0].Columns) == 0) {
			return fmt.Errorf("csharp generator: query %q: %s requires one non-empty result set", q.Name, q.Command)
		}
		if !utf8.ValidString(q.SQL) {
			return fmt.Errorf("csharp generator: query %q: SQL is not valid UTF-8", q.Name)
		}
		seen := map[string]bool{}
		for _, p := range q.Parameters {
			n := csName(p.Name)
			if !csIdent(n) || seen[n] {
				return fmt.Errorf("csharp generator: query %q: parameter name collision at %q", q.Name, p.Name)
			}
			seen[n] = true
			if _, err := csType(p.Type); err != nil {
				return fmt.Errorf("csharp generator: query %q parameter %q: %w", q.Name, p.Name, err)
			}
		}
		if len(q.Parameters) > 1 {
			if err := add(modelNames, csName(q.Name)+"Params", "params:"+q.Name, "model name"); err != nil {
				return err
			}
			params := make([]model.Column, len(q.Parameters))
			for i, p := range q.Parameters {
				params[i] = model.Column{Name: p.Name, Type: p.Type}
			}
			if err := fields("query "+q.Name+" parameters", csName(q.Name)+"Params", params); err != nil {
				return err
			}
		}
		if q.Command == model.One || q.Command == model.Many {
			if err := add(modelNames, csName(q.Name)+"Row", "row:"+q.Name, "model name"); err != nil {
				return err
			}
			if err := fields("query "+q.Name, csName(q.Name)+"Row", q.ResultSets[0].Columns); err != nil {
				return err
			}
		}
	}
	return nil
}

var recordReservedMembers = map[string]bool{
	"Clone": true, "Deconstruct": true, "EqualityContract": true, "PrintMembers": true,
	"Equals": true, "GetHashCode": true, "ToString": true,
	"GetType": true, "MemberwiseClone": true, "Finalize": true, "ReferenceEquals": true,
}

func fields(where, record string, columns []model.Column) error {
	seen := map[string]bool{}
	for _, c := range columns {
		n := csName(c.Name)
		if !csIdent(n) || seen[n] {
			return fmt.Errorf("csharp generator: %s: column name collision at %q", where, c.Name)
		}
		if n == record {
			return fmt.Errorf("csharp generator: %s: record member %q collides with record name", where, c.Name)
		}
		if recordReservedMembers[n] {
			return fmt.Errorf("csharp generator: %s: record member %q collides with generated record member", where, c.Name)
		}
		seen[n] = true
		if _, err := csType(c.Type); err != nil {
			return fmt.Errorf("csharp generator: %s column %q: %w", where, c.Name, err)
		}
	}
	return nil
}

func csType(t model.Type) (string, error) {
	if t.IsOptional() {
		if t.Elem == nil {
			return "", fmt.Errorf("Optional lacks element")
		}
		if t.Elem.IsOptional() {
			return "", fmt.Errorf("nested Optional is unsupported")
		}
		e, err := csType(*t.Elem)
		if err != nil {
			return "", err
		}
		return e + "?", nil
	}
	switch strings.ToLower(t.Kind) {
	case "bool":
		return "bool", nil
	case "int8":
		return "sbyte", nil
	case "int16":
		return "short", nil
	case "int32":
		return "int", nil
	case "int64":
		return "long", nil
	case "uint8":
		return "byte", nil
	case "uint16":
		return "ushort", nil
	case "uint32":
		return "uint", nil
	case "uint64":
		return "ulong", nil
	case "float":
		return "float", nil
	case "double":
		return "double", nil
	case "utf8":
		return "string", nil
	case "string":
		return "byte[]", nil
	case "uuid":
		return "Guid", nil
	case "json":
		return "string", nil
	case "timestamp":
		return "DateTime", nil
	default:
		return "", fmt.Errorf("unsupported YQL type %q", t.Kind)
	}
}

func renderModels(in *model.AnalysisResult, o Options) []byte {
	var b bytes.Buffer
	b.WriteString(modelsHeader(o) + "\n")
	write := func(name string, cols []model.Column) {
		b.WriteString("public sealed record " + name + "(\n")
		for i, c := range cols {
			typ, _ := csType(c.Type)
			comma := ","
			if i == len(cols)-1 {
				comma = ""
			}
			fmt.Fprintf(&b, "    %s %s%s\n", typ, csName(c.Name), comma)
		}
		b.WriteString(");\n\n")
	}
	for _, t := range in.Catalog.Tables {
		write(csName(t.Name), t.Columns)
	}
	for _, q := range in.Queries {
		if len(q.Parameters) > 1 {
			ps := make([]model.Column, len(q.Parameters))
			for i, p := range q.Parameters {
				ps[i] = model.Column{Name: p.Name, Type: p.Type}
			}
			write(csName(q.Name)+"Params", ps)
		}
		if q.Command == model.One || q.Command == model.Many {
			write(csName(q.Name)+"Row", q.ResultSets[0].Columns)
		}
	}
	return []byte(strings.TrimRight(b.String(), "\n") + "\n")
}

func renderQueries(in *model.AnalysisResult, o Options) []byte {
	switch o.Runtime {
	case "dapper":
		return renderDapperQueries(in, o)
	}
	var b bytes.Buffer
	b.WriteString(queriesHeader(o))
	b.WriteString("\npublic sealed class Queries\n{\n")
	b.WriteString("    private readonly YdbConnection _connection;\n    private readonly YdbTransaction? _transaction;\n\n    public Queries(YdbConnection connection, YdbTransaction? transaction = null)\n    {\n        _connection = connection ?? throw new ArgumentNullException(nameof(connection));\n        if (transaction is not null && !ReferenceEquals(transaction.Connection, connection))\n        {\n            throw new ArgumentException(\"Transaction must belong to the supplied connection.\", nameof(transaction));\n        }\n        _transaction = transaction;\n    }\n\n    public Queries WithTransaction(YdbTransaction transaction) => new(_connection, transaction ?? throw new ArgumentNullException(nameof(transaction)));\n")
	writeTimestampHelpers(&b, in)
	for _, q := range in.Queries {
		writeMethod(&b, q)
	}
	b.WriteString("}\n")
	return b.Bytes()
}

func modelsHeader(o Options) string {
	return "// Code generated by sqlc-ydb. DO NOT EDIT.\n#nullable enable\nusing System;\n\nnamespace " + o.Namespace + ";"
}
func queriesHeader(o Options) string {
	return "// Code generated by sqlc-ydb. DO NOT EDIT.\n#nullable enable\nusing System;\nusing System.Collections.Generic;\nusing System.Data;\nusing System.Data.Common;\nusing System.Threading;\nusing System.Threading.Tasks;\nusing Ydb.Sdk.Ado;\nusing Ydb.Sdk.Value;\n\nnamespace " + o.Namespace + ";\n\n"
}

func renderDapperQueries(in *model.AnalysisResult, o Options) []byte {
	var b bytes.Buffer
	b.WriteString("// Code generated by sqlc-ydb. DO NOT EDIT.\n#nullable enable\nusing System;\nusing System.Collections.Generic;\nusing System.Data;\nusing System.Linq;\nusing System.Reflection;\nusing System.Threading;\nusing System.Threading.Tasks;\nusing Dapper;\nusing Ydb.Sdk.Ado;\nusing Ydb.Sdk.Value;\n\nnamespace " + o.Namespace + ";\n\n")
	b.WriteString("public sealed class Queries\n{\n")
	b.WriteString("    private readonly YdbConnection _connection;\n    private readonly YdbTransaction? _transaction;\n\n    public Queries(YdbConnection connection, YdbTransaction? transaction = null)\n    {\n        _connection = connection ?? throw new ArgumentNullException(nameof(connection));\n        if (transaction is not null && !ReferenceEquals(transaction.Connection, connection))\n        {\n            throw new ArgumentException(\"Transaction must belong to the supplied connection.\", nameof(transaction));\n        }\n        _transaction = transaction;\n    }\n\n    public Queries WithTransaction(YdbTransaction transaction) => new(_connection, transaction ?? throw new ArgumentNullException(nameof(transaction)));\n")
	writeDapperMaps(&b, in)
	writeTimestampHelpers(&b, in)
	for _, q := range in.Queries {
		writeDapperMethod(&b, q)
	}
	b.WriteString("\n    private sealed class YdbParameters : SqlMapper.IDynamicParameters\n    {\n        private readonly YdbParameter[] _parameters;\n\n        public YdbParameters(params YdbParameter[] parameters) => _parameters = parameters;\n\n        public void AddParameters(IDbCommand command, SqlMapper.Identity identity)\n        {\n            foreach (var parameter in _parameters)\n            {\n                command.Parameters.Add(parameter);\n            }\n        }\n    }\n")
	b.WriteString("}\n")
	return b.Bytes()
}

func writeDapperMaps(b *bytes.Buffer, in *model.AnalysisResult) {
	var mapped []model.AnalyzedQuery
	for _, q := range in.Queries {
		if q.Command != model.One && q.Command != model.Many {
			continue
		}
		for _, c := range q.ResultSets[0].Columns {
			if !strings.EqualFold(c.ResultName(), csName(c.Name)) {
				mapped = append(mapped, q)
				break
			}
		}
	}
	if len(mapped) == 0 {
		return
	}
	b.WriteString("\n    static Queries()\n    {\n")
	for _, q := range mapped {
		fmt.Fprintf(b, "        SqlMapper.SetTypeMap(typeof(%sRow), new ColumnTypeMap(typeof(%sRow), new Dictionary<string, string>\n        {\n", csName(q.Name), csName(q.Name))
		for _, c := range q.ResultSets[0].Columns {
			if !strings.EqualFold(c.ResultName(), csName(c.Name)) {
				fmt.Fprintf(b, "            [%s] = nameof(%sRow.%s),\n", csString(c.ResultName()), csName(q.Name), csName(c.Name))
			}
		}
		b.WriteString("        }));\n")
	}
	b.WriteString(`    }

    private sealed class ColumnTypeMap : SqlMapper.ITypeMap
    {
        private readonly DefaultTypeMap _default;
        private readonly IReadOnlyDictionary<string, string> _columns;

        public ColumnTypeMap(Type type, IReadOnlyDictionary<string, string> columns)
        {
            _default = new DefaultTypeMap(type);
            _columns = columns;
        }

        private string MemberName(string column) => _columns.TryGetValue(column, out var member) ? member : column;
        public ConstructorInfo? FindConstructor(string[] names, Type[] types) => _default.FindConstructor(names.Select(MemberName).ToArray(), types);
        public ConstructorInfo? FindExplicitConstructor() => _default.FindExplicitConstructor();
        public SqlMapper.IMemberMap? GetConstructorParameter(ConstructorInfo constructor, string columnName) => _default.GetConstructorParameter(constructor, MemberName(columnName));
        public SqlMapper.IMemberMap? GetMember(string columnName) => _default.GetMember(MemberName(columnName));
    }
`)
}

func writeDapperMethod(b *bytes.Buffer, q model.AnalyzedQuery) {
	name := csName(q.Name)
	ret := "Task"
	if q.Command == model.One {
		ret = "Task<" + name + "Row>"
	} else if q.Command == model.Many {
		ret = "Task<IReadOnlyList<" + name + "Row>>"
	}
	fmt.Fprintf(b, "\n    // %s\n    public async %s %sAsync(%sCancellationToken cancellationToken = default)\n    {\n", model.QueryAnnotation(q), ret, name, methodParameters(q))
	fmt.Fprintf(b, "        var command = new CommandDefinition(%s, %s, _transaction, cancellationToken: cancellationToken);\n", sqlLiteral(model.WithoutQueryAnnotation(q.SQL)), dapperParameters(q))
	switch q.Command {
	case model.Exec:
		b.WriteString("        await _connection.ExecuteAsync(command).ConfigureAwait(false);\n")
	case model.One:
		fmt.Fprintf(b, "        return await _connection.QueryFirstAsync<%sRow>(command).ConfigureAwait(false);\n", name)
	case model.Many:
		fmt.Fprintf(b, "        return (await _connection.QueryAsync<%sRow>(command).ConfigureAwait(false)).AsList();\n", name)
	}
	b.WriteString("    }\n")
}

func dapperParameters(q model.AnalyzedQuery) string {
	if len(q.Parameters) == 0 {
		return "null"
	}
	var b strings.Builder
	b.WriteString("new YdbParameters(")
	for i, p := range q.Parameters {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(ydbParameterExpression(q, p))
	}
	b.WriteByte(')')
	return b.String()
}

func ydbParameterExpression(q model.AnalyzedQuery, p model.Parameter) string {
	v := parameterRef(q, p)
	if strings.EqualFold(p.Type.UnwrapOptional().Kind, "timestamp") {
		v = "NormalizeTimestamp(" + v + ")"
	}
	if p.Type.IsOptional() {
		return fmt.Sprintf("new YdbParameter(%q, YdbValue.MakeOptional%s(%s))", "$"+p.Name, optionalFactory(p.Type), v)
	}
	if strings.EqualFold(p.Type.Kind, "json") || strings.EqualFold(p.Type.Kind, "timestamp") {
		return fmt.Sprintf("new YdbParameter(%q, YdbValue.Make%s(%s))", "$"+p.Name, csName(p.Type.Kind), v)
	}
	return fmt.Sprintf("new YdbParameter(%q, DbType.%s, %s)", "$"+p.Name, dbType(p.Type), v)
}

func writeRowMapper(b *bytes.Buffer, q model.AnalyzedQuery) {
	name := csName(q.Name)
	fmt.Fprintf(b, "\n    private static %sRow %sRowFrom(DbDataReader reader) => new(\n", name, name)
	for i, c := range q.ResultSets[0].Columns {
		comma := ","
		if i == len(q.ResultSets[0].Columns)-1 {
			comma = ""
		}
		fmt.Fprintf(b, "        %s%s\n", readValue(c, i), comma)
	}
	b.WriteString("    );\n")
}
func sqlLiteral(sql string) string {
	parts := strings.SplitAfter(sql, "\n")
	if len(parts) > 1 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	for i, p := range parts {
		parts[i] = csString(p)
	}
	return "\n            " + strings.Join(parts, " +\n            ")
}

func writeMethod(b *bytes.Buffer, q model.AnalyzedQuery) {
	name := csName(q.Name)
	ret := "Task"
	if q.Command == model.One {
		ret = "Task<" + name + "Row>"
	}
	if q.Command == model.Many {
		ret = "Task<IReadOnlyList<" + name + "Row>>"
	}
	fmt.Fprintf(b, "\n    // %s\n    public async %s %sAsync(%sCancellationToken cancellationToken = default)\n    {\n", model.QueryAnnotation(q), ret, name, methodParameters(q))
	fmt.Fprintf(b, "        await using var command = new YdbCommand(%s, _connection) { Transaction = _transaction };\n", sqlLiteral(model.WithoutQueryAnnotation(q.SQL)))
	for _, p := range q.Parameters {
		writeParameter(b, q, p)
	}
	switch q.Command {
	case model.Exec:
		b.WriteString("        await command.ExecuteNonQueryAsync(cancellationToken).ConfigureAwait(false);\n")
	case model.One:
		b.WriteString("        await using var reader = await command.ExecuteReaderAsync(cancellationToken).ConfigureAwait(false);\n        if (!await reader.ReadAsync(cancellationToken).ConfigureAwait(false))\n        {\n            throw new InvalidOperationException(\"query returned no rows\");\n        }\n        return " + name + "RowFrom(reader);\n")
	case model.Many:
		b.WriteString("        await using var reader = await command.ExecuteReaderAsync(cancellationToken).ConfigureAwait(false);\n        var rows = new List<" + name + "Row>();\n        while (await reader.ReadAsync(cancellationToken).ConfigureAwait(false))\n        {\n            rows.Add(" + name + "RowFrom(reader));\n        }\n        return rows;\n")
	}
	b.WriteString("    }\n")
	if q.Command == model.One || q.Command == model.Many {
		writeRowMapper(b, q)
	}
}

func methodParameters(q model.AnalyzedQuery) string {
	if len(q.Parameters) == 0 {
		return ""
	}
	if len(q.Parameters) > 1 {
		return csName(q.Name) + "Params args, "
	}
	typ, _ := csType(q.Parameters[0].Type)
	return typ + " " + localParameterName(q.Parameters[0].Name) + ", "
}
func parameterRef(q model.AnalyzedQuery, p model.Parameter) string {
	if len(q.Parameters) > 1 {
		return "args." + csName(p.Name)
	}
	return localParameterName(p.Name)
}
func writeTimestampHelpers(b *bytes.Buffer, in *model.AnalysisResult) {
	for _, q := range in.Queries {
		for _, p := range q.Parameters {
			if strings.EqualFold(p.Type.UnwrapOptional().Kind, "timestamp") {
				b.WriteString("\n    private static DateTime NormalizeTimestamp(DateTime value) =>\n        value.Kind == DateTimeKind.Local ? value.ToUniversalTime() : DateTime.SpecifyKind(value, DateTimeKind.Utc);\n\n    private static DateTime? NormalizeTimestamp(DateTime? value) =>\n        value.HasValue ? NormalizeTimestamp(value.Value) : null;\n")
				return
			}
		}
	}
}

func localParameterName(name string) string {
	n := csName(name)
	if n == "ID" {
		return "id"
	}
	if strings.HasSuffix(n, "ID") {
		n = strings.TrimSuffix(n, "ID") + "Id"
	}
	runes := []rune(n)
	if len(runes) != 0 {
		runes[0] = unicode.ToLower(runes[0])
	}
	n = string(runes)
	switch n {
	case "command", "reader", "rows", "row", "cancellationToken", "args":
		return n + "Value"
	}
	if csKeywords[n] {
		return "@" + n
	}
	return n
}

func writeParameter(b *bytes.Buffer, q model.AnalyzedQuery, p model.Parameter) {
	fmt.Fprintf(b, "        command.Parameters.Add(%s);\n", ydbParameterExpression(q, p))
}
func optionalFactory(t model.Type) string {
	return csName(t.UnwrapOptional().Kind)
}
func dbType(t model.Type) string {
	if t.IsOptional() {
		t = *t.Elem
	}
	switch strings.ToLower(t.Kind) {
	case "utf8":
		return "String"
	case "string":
		return "Binary"
	case "bool":
		return "Boolean"
	case "float":
		return "Single"
	case "double":
		return "Double"
	case "uuid":
		return "Guid"
	case "int8":
		return "SByte"
	case "int16":
		return "Int16"
	case "int32":
		return "Int32"
	case "int64":
		return "Int64"
	case "uint8":
		return "Byte"
	case "uint16":
		return "UInt16"
	case "uint32":
		return "UInt32"
	case "uint64":
		return "UInt64"
	default:
		return csName(t.Kind)
	}
}
func readValue(c model.Column, i int) string {
	typ, _ := csType(c.Type)
	bare := strings.TrimSuffix(typ, "?")
	get := fmt.Sprintf("reader.GetFieldValue<%s>(%d)", bare, i)
	if c.Type.IsOptional() {
		return fmt.Sprintf("reader.IsDBNull(%d) ? null : %s", i, get)
	}
	return get
}

func csString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString("\\\\")
		case '"':
			b.WriteString("\\\"")
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case '\t':
			b.WriteString("\\t")
		case '\b':
			b.WriteString("\\b")
		case '\f':
			b.WriteString("\\f")
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, "\\u%04X", r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func csName(s string) string {
	var b strings.Builder
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if strings.EqualFold(p, "id") {
			b.WriteString("ID")
			continue
		}
		for i, r := range p {
			if i == 0 {
				b.WriteRune(unicode.ToUpper(r))
			} else {
				b.WriteRune(r)
			}
		}
	}
	n := b.String()
	if n == "" {
		return ""
	}
	if unicode.IsDigit([]rune(n)[0]) {
		return "Value" + n
	}
	return n
}
func csIdent(s string) bool {
	if s == "" || csKeywords[s] {
		return false
	}
	for i, r := range s {
		if !(r == '_' || unicode.IsLetter(r) || (i > 0 && unicode.IsDigit(r))) {
			return false
		}
	}
	return true
}
func namespace(s string) bool {
	for _, p := range strings.Split(s, ".") {
		if !csIdent(p) {
			return false
		}
	}
	return true
}

var csKeywords = map[string]bool{"abstract": true, "as": true, "base": true, "bool": true, "break": true, "byte": true, "case": true, "catch": true, "char": true, "checked": true, "class": true, "const": true, "continue": true, "decimal": true, "default": true, "delegate": true, "do": true, "double": true, "else": true, "enum": true, "event": true, "explicit": true, "extern": true, "false": true, "finally": true, "fixed": true, "float": true, "for": true, "foreach": true, "goto": true, "if": true, "implicit": true, "in": true, "int": true, "interface": true, "internal": true, "is": true, "lock": true, "long": true, "namespace": true, "new": true, "null": true, "object": true, "operator": true, "out": true, "override": true, "params": true, "private": true, "protected": true, "public": true, "readonly": true, "ref": true, "return": true, "sbyte": true, "sealed": true, "short": true, "sizeof": true, "stackalloc": true, "static": true, "string": true, "struct": true, "switch": true, "this": true, "throw": true, "true": true, "try": true, "typeof": true, "uint": true, "ulong": true, "unchecked": true, "unsafe": true, "ushort": true, "using": true, "virtual": true, "void": true, "volatile": true, "while": true}
