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

linq2db integration targets `6.4.0`, commit
`82fbf0f91399cc8c9cea22d09dcae20e4d7568c6`. This release promotes its YDB
provider to supported status and exposes `YdbTools.CreateDataConnection` for a
connection or transaction. Its own build pins `Ydb.Sdk` `0.35.0`.

The shared example compile project targets `net8.0` and pins `Ydb.Sdk`
`0.35.0`, Dapper `2.1.79`, and linq2db `6.4.0`. Runtime packages belong to
generated projects, never to sqlc-ydb's Go module.
