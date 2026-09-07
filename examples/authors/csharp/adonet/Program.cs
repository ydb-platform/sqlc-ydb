using System;
using System.Threading;
using Ydb.Sdk.Ado;

namespace Authors.AdoNet;

public static class Program
{
    public static async Task<int> Main()
    {
        var dsn = Environment.GetEnvironmentVariable("SQLC_YDB_TEST_DSN");
        if (string.IsNullOrWhiteSpace(dsn))
        {
            Console.Error.WriteLine("SQLC_YDB_TEST_DSN is required (for example Host=localhost;Port=2136;Database=/local)");
            return 2;
        }
        using var cancellationSource = new CancellationTokenSource(TimeSpan.FromSeconds(45));
        await using var dataSource = new YdbDataSource(dsn);
        await using var connection = await dataSource.OpenConnectionAsync(cancellationSource.Token);
        await using (var create = new YdbCommand(await File.ReadAllTextAsync("schema.sql", cancellationSource.Token), connection))
            await create.ExecuteNonQueryAsync(cancellationSource.Token);
        try
        {
            await Smoke.ExerciseAsync(connection, cancellationSource.Token);
        }
        finally
        {
            await using var drop = new YdbCommand("DROP TABLE authors;", connection);
            await drop.ExecuteNonQueryAsync(CancellationToken.None);
        }
        return 0;
    }
}
