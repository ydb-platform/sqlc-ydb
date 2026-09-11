# C# SDK investigation

The public generated API and type contract is in [C# generation](../docs/csharp.md).


The ADO.NET type contract was checked in the current official YDB documentation
and `ydb-platform/ydb-dotnet-sdk` main at
`236bfa176940feafbf61f11c1cb9fc000572b237`. The SDK exposes
`YdbParameter`, `YdbCommand.ExecuteReaderAsync`, `GetFieldValue<T>`, and typed
`YdbValue` builders for `Json`, `Timestamp`, and their optional forms.

Dapper integration was checked against Dapper main at
`6d48ef664acc7298c649e2d449d903b3360d5a90`; its public
`SqlMapper.IDynamicParameters` contract accepts provider-specific parameters,
and `CommandDefinition` carries the transaction and cancellation token.

The generated Dapper profile uses QueryFirstAsync<T> and QueryAsync<T> with
CommandDefinition carrying transaction and cancellation. Positional records are
materialized by Dapper. A type-specific ITypeMap forwards exact column/member
name translations to DefaultTypeMap, including constructor parameter lookup.
No global underscore setting is changed. YDB-specific parameter values still
use IDynamicParameters with concrete YdbParameter objects.

The shared example compile project targets net8.0 and pins Ydb.Sdk 0.35.0 and
Dapper 2.1.79. Runtime packages belong to generated projects.

The linq2db profile was removed: the SQL-first wrapper added no useful LINQ API.
Timestamp normalization remains Local -> UTC and Unspecified -> UTC before
constructing typed YdbValue values.
