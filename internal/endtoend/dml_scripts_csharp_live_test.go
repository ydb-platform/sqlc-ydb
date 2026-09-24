package endtoend

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/codegen/csharp"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestDMLScriptsCsharpBuildsAgainstPublishedSDK(t *testing.T) {
	runDMLScriptsCsharp(t, true)
}

func dmlScriptsCsharp(t *testing.T) {
	runDMLScriptsCsharp(t, false)
}

func runDMLScriptsCsharp(t *testing.T, compileOnly bool) {
	t.Helper()
	dotnet := os.Getenv("SQLC_YDB_CSHARP_DOTNET")
	if dotnet == "" {
		t.Skip("set SQLC_YDB_CSHARP_DOTNET for published C# SDK script checks")
	}
	for _, runtime := range []string{"adonet", "dapper"} {
		t.Run(runtime, func(t *testing.T) {
			table := fmt.Sprintf("sqlc_csharp_scripts_%d", time.Now().UnixNano())
			t.Logf("fixture tables: %s, %s_copies", table, table)
			replace := strings.NewReplacer("records", table, "copies", table+"_copies")
			schema := replace.Replace(dmlScriptsSchema)
			analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: schema}}, []model.Source{{Name: "queries.sql", Text: replace.Replace(dmlScriptsCsharpQueries)}})
			require.NoError(t, err)
			files, err := csharp.Generate(analysis, csharp.Options{Namespace: "Generated", Runtime: runtime})
			require.NoError(t, err)
			dir := t.TempDir()
			files = append(files, model.File{Name: "Program.cs", Content: []byte(strings.ReplaceAll(dmlScriptsCsharpProgram, "$SCHEMA", strconv.Quote(schema)))})
			files = append(files, model.File{Name: "scripts.csproj", Content: []byte(`<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><OutputType>Exe</OutputType><TargetFramework>net8.0</TargetFramework><ImplicitUsings>enable</ImplicitUsings><Nullable>enable</Nullable><TreatWarningsAsErrors>true</TreatWarningsAsErrors></PropertyGroup><ItemGroup><PackageReference Include="Ydb.Sdk" Version="0.35.0"/><PackageReference Include="Dapper" Version="2.1.79"/></ItemGroup></Project>`)})
			for _, file := range files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, file.Name), file.Content, 0600))
			}
			env := append(os.Environ(), "DOTNET_CLI_HOME="+filepath.Join(dir, ".dotnet"))
			if os.Getenv("NUGET_PACKAGES") == "" {
				env = append(env, "NUGET_PACKAGES="+filepath.Join(dir, ".nuget"))
			}
			commands := [][]string{{"build", "--nologo"}}
			if !compileOnly {
				commands = append(commands, []string{"run", "--no-build", "--nologo"})
			}
			for _, args := range commands {
				cmd := exec.Command(dotnet, args...)
				cmd.Dir, cmd.Env = dir, env
				out, err := cmd.CombinedOutput()
				require.NoError(t, err, "C# %s %s:\n%s", runtime, args[0], out)
			}
		})
	}
}

const dmlScriptsCsharpQueries = `-- name: Seed :exec
UPSERT INTO records(id,value,label) VALUES(1ul,10l,'first'u),(2ul,20l,'second'u),(3ul,30l,'third'u);
UPSERT INTO copies(id,value,label) VALUES(99ul,0l,'counter'u);

-- name: ReadAndMutate :one
SELECT id,value,label FROM records ORDER BY id;
UPDATE copies SET value=value+1 WHERE id=99ul;

-- name: ReadManyAndMutate :many
SELECT id,value,label FROM records ORDER BY id;
UPDATE copies SET value=value+1 WHERE id=99ul;

-- name: ReadEmptyAndMutate :one
SELECT id,value,label FROM records WHERE false;
UPDATE copies SET value=value+1 WHERE id=99ul;

-- name: ReadEmptyManyAndMutate :many
SELECT id,value,label FROM records WHERE false;
UPDATE copies SET value=value+1 WHERE id=99ul;

-- name: ReadAndFail :one
SELECT id,value,label FROM records ORDER BY id;
UPDATE copies SET value=value+1 WHERE id=99ul;
INSERT INTO copies(id,value,label) VALUES(99ul,999l,'duplicate'u);

-- name: ReadAndFailMany :many
DECLARE $minimum AS Int64;
SELECT id,value,label FROM records WHERE value >= $minimum ORDER BY id;
UPDATE copies SET value=value+1 WHERE id=99ul;
INSERT INTO copies(id,value,label) VALUES(99ul,999l,'duplicate'u);

-- name: GetCounter :one
SELECT value FROM copies WHERE id=99ul;

-- name: ListRecords :many
SELECT id,value,label FROM records ORDER BY id;
`

const dmlScriptsCsharpProgram = `using Generated;
using Ydb.Sdk;
using Ydb.Sdk.Ado;

using var timeout=new CancellationTokenSource(TimeSpan.FromSeconds(90));
var cancellationToken=timeout.Token;
var source=Environment.GetEnvironmentVariable("YDB_CONNECTION_STRING") ?? throw new InvalidOperationException("YDB_CONNECTION_STRING is required");
var endpoint=new Uri(source);
var dsn=$"Host={endpoint.Host};Port={endpoint.Port};Database={endpoint.AbsolutePath}";
await using var dataSource=new YdbDataSource(dsn);
await using var connection=await dataSource.OpenConnectionAsync(cancellationToken);
var created=new List<string>();
try {
    foreach(var ddl in $SCHEMA.Split(';',StringSplitOptions.RemoveEmptyEntries)) {
        await using var command=new YdbCommand(ddl,connection);
        await command.ExecuteNonQueryAsync(cancellationToken);
        created.Add(ddl.Split((char[]?)null,StringSplitOptions.RemoveEmptyEntries)[2]);
    }
    var queries=new Queries(connection);
    await queries.SeedAsync(cancellationToken);
    async Task CheckCounter(long expected) {
        if((await queries.GetCounterAsync(cancellationToken)).Value!=expected) throw new Exception($"counter expected {expected}");
    }
    await using(var transaction=(YdbTransaction)await connection.BeginTransactionAsync(cancellationToken)) {
        var tx=queries.WithTransaction(transaction);
        var row=await tx.ReadAndMutateAsync(cancellationToken);
        if(row.ID!=1 || row.Value!=10 || row.Label!="first") throw new Exception("first-row result changed");
        if((await tx.GetCounterAsync(cancellationToken)).Value!=1 || (await tx.ListRecordsAsync(cancellationToken)).Count!=3) throw new Exception("stream incomplete or transaction unusable");
        await transaction.RollbackAsync(cancellationToken);
    }
    await CheckCounter(0);
    await using(var transaction=(YdbTransaction)await connection.BeginTransactionAsync(cancellationToken)) {
        var tx=queries.WithTransaction(transaction);
        if((await tx.ReadAndMutateAsync(cancellationToken)).ID!=1) throw new Exception("first row changed");
        if((await tx.GetCounterAsync(cancellationToken)).Value!=1) throw new Exception("pending mutation missing");
        await transaction.CommitAsync(cancellationToken);
    }
    await CheckCounter(1);
    if((await queries.ReadAndMutateAsync(cancellationToken)).ID!=1) throw new Exception("default first row changed");
    await CheckCounter(2);
    if((await queries.ReadManyAndMutateAsync(cancellationToken)).Count!=3) throw new Exception("many lost rows");
    await CheckCounter(3);
    bool emptyFailed=false;
    try {await queries.ReadEmptyAndMutateAsync(cancellationToken);} catch(InvalidOperationException) {emptyFailed=true;}
    if(!emptyFailed) throw new Exception("empty :one succeeded");
    await CheckCounter(4);
    if((await queries.ReadEmptyManyAndMutateAsync(cancellationToken)).Count!=0) throw new Exception("empty :many returned rows");
    await CheckCounter(5);
    bool failed=false;
    try {await queries.ReadAndFailAsync(cancellationToken);} catch(YdbException error) when(error.Code==StatusCode.PreconditionFailed) {failed=true;}
    if(!failed) throw new Exception("late script error was lost");
    await CheckCounter(5);
    await using(var transaction=(YdbTransaction)await connection.BeginTransactionAsync(cancellationToken)) {
        var tx=queries.WithTransaction(transaction);
        failed=false;
        try {await tx.ReadAndFailAsync(cancellationToken);} catch(YdbException error) when(error.Code==StatusCode.PreconditionFailed) {failed=true;}
        if(!failed) throw new Exception("late transaction error was lost");
        await transaction.RollbackAsync(cancellationToken);
    }
    await CheckCounter(5);
    foreach(var minimum in new long[]{0,1000}) {
        failed=false;
        try {await queries.ReadAndFailManyAsync(minimum,cancellationToken);} catch(YdbException error) when(error.Code==StatusCode.PreconditionFailed) {failed=true;}
        if(!failed) throw new Exception($"late :many script error was lost for minimum {minimum}");
        await CheckCounter(5);
        await using(var transaction=(YdbTransaction)await connection.BeginTransactionAsync(cancellationToken)) {
            var tx=queries.WithTransaction(transaction);
            failed=false;
            try {await tx.ReadAndFailManyAsync(minimum,cancellationToken);} catch(YdbException error) when(error.Code==StatusCode.PreconditionFailed) {failed=true;}
            if(!failed) throw new Exception($"late :many transaction error was lost for minimum {minimum}");
            await transaction.RollbackAsync(cancellationToken);
        }
        await CheckCounter(5);
    }
    await queries.ReadAndMutateAsync(cancellationToken);
    await CheckCounter(6);
} finally {
    await using var cleanup=await dataSource.OpenConnectionAsync(CancellationToken.None);
    for(var i=created.Count-1;i>=0;i--) {
        await using var command=new YdbCommand("DROP TABLE "+created[i],cleanup);
        await command.ExecuteNonQueryAsync(CancellationToken.None);
    }
}
`
