using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Runtime.ExceptionServices;
using System.Threading;
using System.Threading.Tasks;
using LinqToDB.Data;
using LinqToDB.DataProvider.Ydb;
using Ydb.Sdk.Ado;
using AuthorsDapper = Authors.Dapper;
using AuthorsLinq2DB = Authors.Linq2DB;
using BatchDapper = Batch.Dapper;
using BatchLinq2DB = Batch.Linq2DB;
using BooktestDapper = Booktest.Dapper;
using BooktestLinq2DB = Booktest.Linq2DB;
using JetsDapper = Jets.Dapper;
using JetsLinq2DB = Jets.Linq2DB;
using OndeckDapper = Ondeck.Dapper;
using OndeckLinq2DB = Ondeck.Linq2DB;

internal static class Program
{
    private static readonly string[] AuthorsSchema = ["examples/authors/schema.sql"];
    private static readonly string[] BatchSchema = ["examples/batch/schema.sql"];
    private static readonly string[] BooktestSchema = ["examples/booktest/schema.sql"];
    private static readonly string[] JetsSchema = ["examples/jets/schema.sql"];
    private static readonly string[] OndeckSchema =
    [
        "examples/ondeck/schema/0001_city.sql",
        "examples/ondeck/schema/0002_venue.sql",
        "examples/ondeck/schema/0003_rename_venue.sql",
        "examples/ondeck/schema/0004_add_created_at.sql",
        "examples/ondeck/schema/0005_drop_column.sql",
    ];

    public static async Task<int> Main(string[] args)
    {
        if (args.Length != 1 || (args[0] != "dapper" && args[0] != "linq2db"))
        {
            Console.Error.WriteLine("usage: GeneratedProfiles <dapper|linq2db>");
            return 2;
        }
        var dsn = Environment.GetEnvironmentVariable("YDB_CONNECTION_STRING");
        if (string.IsNullOrWhiteSpace(dsn))
        {
            Console.Error.WriteLine("YDB_CONNECTION_STRING is required");
            return 2;
        }

        using var timeout = new CancellationTokenSource(TimeSpan.FromSeconds(120));
        await using var dataSource = new YdbDataSource(dsn);
        await using var connection = await dataSource.OpenConnectionAsync(timeout.Token);
        if (args[0] == "dapper")
            await ExerciseDapperAsync(connection, timeout.Token);
        else
            await ExerciseLinq2DBAsync(connection, timeout.Token);
        return 0;
    }

    private static async Task ExerciseDapperAsync(YdbConnection connection, CancellationToken cancellationToken)
    {
        await RunExampleAsync(connection, "authors", AuthorsSchema, token => ExerciseAuthorsDapperAsync(connection, token), cancellationToken);
        await RunExampleAsync(connection, "batch", BatchSchema, token => ExerciseBatchDapperAsync(connection, token), cancellationToken);
        await RunExampleAsync(connection, "booktest", BooktestSchema, token => ExerciseBooktestDapperAsync(connection, token), cancellationToken);
        await RunExampleAsync(connection, "jets", JetsSchema, token => ExerciseJetsDapperAsync(connection, token), cancellationToken);
        await RunExampleAsync(connection, "ondeck", OndeckSchema, token => ExerciseOndeckDapperAsync(connection, token), cancellationToken);
    }

    private static async Task ExerciseLinq2DBAsync(YdbConnection connection, CancellationToken cancellationToken)
    {
        await using var db = YdbTools.CreateDataConnection(connection);
        await RunExampleAsync(connection, "authors", AuthorsSchema, token => ExerciseAuthorsLinq2DBAsync(db, token), cancellationToken);
        await RunExampleAsync(connection, "batch", BatchSchema, token => ExerciseBatchLinq2DBAsync(db, token), cancellationToken);
        await RunExampleAsync(connection, "booktest", BooktestSchema, token => ExerciseBooktestLinq2DBAsync(db, token), cancellationToken);
        await RunExampleAsync(connection, "jets", JetsSchema, token => ExerciseJetsLinq2DBAsync(connection, db, token), cancellationToken);
        await RunExampleAsync(connection, "ondeck", OndeckSchema, token => ExerciseOndeckLinq2DBAsync(db, token), cancellationToken);
    }

    private static async Task RunExampleAsync(YdbConnection connection, string name, IReadOnlyList<string> schemaPaths, Func<CancellationToken, Task> exercise, CancellationToken cancellationToken)
    {
        var ownedTables = new List<string>();
        Exception? primaryFailure = null;
        try
        {
            await ApplySchemaAsync(connection, schemaPaths, ownedTables, cancellationToken);
            await exercise(cancellationToken);
            Console.WriteLine($"{name}: passed");
        }
        catch (Exception error)
        {
            primaryFailure = error;
        }

        var cleanupFailure = await DropOwnedTablesAsync(connection, ownedTables);
        if (primaryFailure is not null)
        {
            if (cleanupFailure is not null)
                Console.Error.WriteLine($"{name}: cleanup also failed: {cleanupFailure.Message}");
            ExceptionDispatchInfo.Capture(primaryFailure).Throw();
        }
        if (cleanupFailure is not null)
            ExceptionDispatchInfo.Capture(cleanupFailure).Throw();
    }

    private static async Task ApplySchemaAsync(YdbConnection connection, IReadOnlyList<string> schemaPaths, List<string> ownedTables, CancellationToken cancellationToken)
    {
        foreach (var path in schemaPaths)
        {
            var sql = await File.ReadAllTextAsync(path, cancellationToken);
            foreach (var statement in sql.Split(';', StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries))
            {
                await ExecuteAsync(connection, statement + ";", cancellationToken);
                TrackSchemaChange(statement, ownedTables);
            }
        }
    }

    private static void TrackSchemaChange(string statement, List<string> ownedTables)
    {
        var words = statement.Split((char[]?)null, StringSplitOptions.RemoveEmptyEntries);
        if (words.Length >= 3 && words[0].Equals("CREATE", StringComparison.OrdinalIgnoreCase) && words[1].Equals("TABLE", StringComparison.OrdinalIgnoreCase))
        {
            ownedTables.Add(words[2]);
            return;
        }
        if (words.Length >= 6 && words[0].Equals("ALTER", StringComparison.OrdinalIgnoreCase) && words[1].Equals("TABLE", StringComparison.OrdinalIgnoreCase) && words[3].Equals("RENAME", StringComparison.OrdinalIgnoreCase) && words[4].Equals("TO", StringComparison.OrdinalIgnoreCase))
        {
            var index = ownedTables.IndexOf(words[2]);
            if (index >= 0)
                ownedTables[index] = words[5];
        }
    }

    private static async Task<Exception?> DropOwnedTablesAsync(YdbConnection connection, List<string> ownedTables)
    {
        Exception? firstFailure = null;
        for (var index = ownedTables.Count - 1; index >= 0; index--)
        {
            try
            {
                await ExecuteAsync(connection, $"DROP TABLE {ownedTables[index]};", CancellationToken.None);
            }
            catch (Exception error)
            {
                firstFailure ??= error;
            }
        }
        return firstFailure;
    }

    private static async Task ExecuteAsync(YdbConnection connection, string sql, CancellationToken cancellationToken)
    {
        await using var command = new YdbCommand(sql, connection);
        await command.ExecuteNonQueryAsync(cancellationToken);
    }

    private static async Task ExerciseAuthorsDapperAsync(YdbConnection connection, CancellationToken cancellationToken)
    {
        var queries = new AuthorsDapper.Queries(connection);
        const ulong id = ulong.MaxValue;
        var created = await queries.CreateAuthorAsync(new AuthorsDapper.CreateAuthorParams(id, "Ada", null), cancellationToken);
        await queries.UpsertAuthorAsync(new AuthorsDapper.UpsertAuthorParams(id, "Ada Lovelace", "programmer"), cancellationToken);
        var fetched = await queries.GetAuthorAsync(id, cancellationToken);
        if (created.ID != id || created.Bio is not null || fetched.Name != "Ada Lovelace" || fetched.Bio != "programmer" || (await queries.ListAuthorsAsync(cancellationToken)).Single().ID != id)
            throw new InvalidOperationException("authors Dapper CRUD mapping changed");
        await queries.DeleteAuthorAsync(id, cancellationToken);
        await AssertMissingAsync(() => queries.GetAuthorAsync(id, cancellationToken));
    }

    private static async Task ExerciseAuthorsLinq2DBAsync(DataConnection db, CancellationToken cancellationToken)
    {
        var queries = new AuthorsLinq2DB.Queries(db);
        const ulong id = ulong.MaxValue;
        var created = await queries.CreateAuthorAsync(new AuthorsLinq2DB.CreateAuthorParams(id, "Ada", null), cancellationToken);
        await queries.UpsertAuthorAsync(new AuthorsLinq2DB.UpsertAuthorParams(id, "Ada Lovelace", "programmer"), cancellationToken);
        var fetched = await queries.GetAuthorAsync(id, cancellationToken);
        if (created.ID != id || created.Bio is not null || fetched.Name != "Ada Lovelace" || fetched.Bio != "programmer" || (await queries.ListAuthorsAsync(cancellationToken)).Single().ID != id)
            throw new InvalidOperationException("authors linq2db CRUD mapping changed");
        await queries.DeleteAuthorAsync(id, cancellationToken);
        await AssertMissingAsync(() => queries.GetAuthorAsync(id, cancellationToken));
    }

    private static async Task ExerciseBatchDapperAsync(YdbConnection connection, CancellationToken cancellationToken)
    {
        var queries = new BatchDapper.Queries(connection);
        const ulong id = ulong.MaxValue;
        var at = TestTimestamp();
        await queries.CreateAuthorAsync(new BatchDapper.CreateAuthorParams(id, "Dapper", "{\"profile\":\"dapper\"}"), cancellationToken);
        await queries.CreateAuthorAsync(new BatchDapper.CreateAuthorParams(id - 1, "Dapper null", null), cancellationToken);
        var book = await queries.CreateBookAsync(new BatchDapper.CreateBookParams(id, id, "isbn-dapper", "paper", "Dapper", 2026, at, "[\"typed\"]"), cancellationToken);
        var rows = await queries.BooksByYearAsync(2026, cancellationToken);
        var biography = await queries.GetBiographyAsync(id, cancellationToken);
        var nullBiography = await queries.GetBiographyAsync(id - 1, cancellationToken);
        if (book.Available != at || book.Tags != "[\"typed\"]" || rows.Single().BookID != id || biography.Biography != "{\"profile\":\"dapper\"}" || nullBiography.Biography is not null)
            throw new InvalidOperationException("batch Dapper Json/Timestamp/result mapping changed");
    }

    private static async Task ExerciseBatchLinq2DBAsync(DataConnection db, CancellationToken cancellationToken)
    {
        var queries = new BatchLinq2DB.Queries(db);
        const ulong id = ulong.MaxValue;
        var at = TestTimestamp();
        await queries.CreateAuthorAsync(new BatchLinq2DB.CreateAuthorParams(id, "linq2db", "{\"profile\":\"linq2db\"}"), cancellationToken);
        await queries.CreateAuthorAsync(new BatchLinq2DB.CreateAuthorParams(id - 1, "linq2db null", null), cancellationToken);
        var book = await queries.CreateBookAsync(new BatchLinq2DB.CreateBookParams(id, id, "isbn-linq2db", "paper", "linq2db", 2026, at, "[\"typed\"]"), cancellationToken);
        var rows = await queries.BooksByYearAsync(2026, cancellationToken);
        var biography = await queries.GetBiographyAsync(id, cancellationToken);
        var nullBiography = await queries.GetBiographyAsync(id - 1, cancellationToken);
        if (book.Available != at || book.Tags != "[\"typed\"]" || rows.Single().BookID != id || biography.Biography != "{\"profile\":\"linq2db\"}" || nullBiography.Biography is not null)
            throw new InvalidOperationException("batch linq2db Json/Timestamp/result mapping changed");
    }

    private static async Task ExerciseBooktestDapperAsync(YdbConnection connection, CancellationToken cancellationToken)
    {
        var queries = new BooktestDapper.Queries(connection);
        const ulong id = ulong.MaxValue;
        var at = TestTimestamp();
        await queries.CreateAuthorAsync(new BooktestDapper.CreateAuthorParams(id, "Octavia"), cancellationToken);
        var book = await queries.CreateBookAsync(new BooktestDapper.CreateBookParams(id, id - 1, "orphan", "paper", "Parable", 2026, at, "[\"speculative\"]"), cancellationToken);
        var joined = (await queries.BooksByTagsAsync("[\"speculative\"]", cancellationToken)).Single();
        var hello = await queries.SayHelloAsync("YDB", cancellationToken);
        if (book.Available != at || book.Tags != "[\"speculative\"]" || joined.BookID != id || joined.Name is not null || hello.Greeting != "hello YDB")
            throw new InvalidOperationException("booktest Dapper JSON/Timestamp/LEFT JOIN mapping changed");
        await queries.DeleteBookAsync(id, cancellationToken);
        await AssertMissingAsync(() => queries.GetBookAsync(id, cancellationToken));
    }

    private static async Task ExerciseBooktestLinq2DBAsync(DataConnection db, CancellationToken cancellationToken)
    {
        var queries = new BooktestLinq2DB.Queries(db);
        const ulong id = ulong.MaxValue;
        var at = TestTimestamp();
        await queries.CreateAuthorAsync(new BooktestLinq2DB.CreateAuthorParams(id, "Octavia"), cancellationToken);
        var book = await queries.CreateBookAsync(new BooktestLinq2DB.CreateBookParams(id, id - 1, "orphan", "paper", "Parable", 2026, at, "[\"speculative\"]"), cancellationToken);
        var joined = (await queries.BooksByTagsAsync("[\"speculative\"]", cancellationToken)).Single();
        var hello = await queries.SayHelloAsync("YDB", cancellationToken);
        if (book.Available != at || book.Tags != "[\"speculative\"]" || joined.BookID != id || joined.Name is not null || hello.Greeting != "hello YDB")
            throw new InvalidOperationException("booktest linq2db JSON/Timestamp/LEFT JOIN mapping changed");
        await queries.DeleteBookAsync(id, cancellationToken);
        await AssertMissingAsync(() => queries.GetBookAsync(id, cancellationToken));
    }

    private static async Task ExerciseJetsDapperAsync(YdbConnection connection, CancellationToken cancellationToken)
    {
        await InsertPilotsAsync(connection, cancellationToken);
        var queries = new JetsDapper.Queries(connection);
        var pilots = await queries.ListPilotsAsync(cancellationToken);
        if ((await queries.CountPilotsAsync(cancellationToken)).PilotCount != 2 || !pilots.Select(row => row.Name).SequenceEqual(["Amelia", "Bessie"]))
            throw new InvalidOperationException("jets Dapper aggregate/list mapping changed");
        await queries.DeletePilotAsync(1, cancellationToken);
        if ((await queries.CountPilotsAsync(cancellationToken)).PilotCount != 1)
            throw new InvalidOperationException("jets Dapper delete changed");
    }

    private static async Task ExerciseJetsLinq2DBAsync(YdbConnection connection, DataConnection db, CancellationToken cancellationToken)
    {
        await InsertPilotsAsync(connection, cancellationToken);
        var queries = new JetsLinq2DB.Queries(db);
        var pilots = await queries.ListPilotsAsync(cancellationToken);
        if ((await queries.CountPilotsAsync(cancellationToken)).PilotCount != 2 || !pilots.Select(row => row.Name).SequenceEqual(["Amelia", "Bessie"]))
            throw new InvalidOperationException("jets linq2db aggregate/list mapping changed");
        await queries.DeletePilotAsync(1, cancellationToken);
        if ((await queries.CountPilotsAsync(cancellationToken)).PilotCount != 1)
            throw new InvalidOperationException("jets linq2db delete changed");
    }

    private static Task InsertPilotsAsync(YdbConnection connection, CancellationToken cancellationToken) =>
        ExecuteAsync(connection, "UPSERT INTO pilots (id, name) VALUES (1, \"Amelia\"u), (2, \"Bessie\"u);", cancellationToken);

    private static async Task ExerciseOndeckDapperAsync(YdbConnection connection, CancellationToken cancellationToken)
    {
        var queries = new OndeckDapper.Queries(connection);
        const ulong id = ulong.MaxValue;
        var at = TestTimestamp();
        await queries.CreateCityAsync(new OndeckDapper.CreateCityParams("London", "london"), cancellationToken);
        await queries.CreateVenueAsync(new OndeckDapper.CreateVenueParams(id, "roundhouse", "Roundhouse", "london", at, "playlist", "open", "[\"open\"]", null), cancellationToken);
        await queries.CreateVenueAsync(new OndeckDapper.CreateVenueParams(id - 1, "forum", "Forum", "london", null, "playlist", "open", null, "[\"rock\"]"), cancellationToken);
        var venue = await queries.GetVenueAsync(new OndeckDapper.GetVenueParams("roundhouse", "london"), cancellationToken);
        var count = (await queries.VenueCountByCityAsync(cancellationToken)).Single();
        var updated = await queries.UpdateVenueNameAsync(new OndeckDapper.UpdateVenueNameParams("The Roundhouse", "roundhouse"), cancellationToken);
        if (venue.ID != id || venue.CreatedAt != at || venue.Statuses != "[\"open\"]" || venue.Tags is not null || count.VenueCount != 2 || updated.ID != id)
            throw new InvalidOperationException("ondeck Dapper migration/optional/aggregate mapping changed");
        await queries.DeleteVenueAsync("roundhouse", cancellationToken);
        await AssertMissingAsync(() => queries.GetVenueAsync(new OndeckDapper.GetVenueParams("roundhouse", "london"), cancellationToken));
    }

    private static async Task ExerciseOndeckLinq2DBAsync(DataConnection db, CancellationToken cancellationToken)
    {
        var queries = new OndeckLinq2DB.Queries(db);
        const ulong id = ulong.MaxValue;
        var at = TestTimestamp();
        await queries.CreateCityAsync(new OndeckLinq2DB.CreateCityParams("London", "london"), cancellationToken);
        await queries.CreateVenueAsync(new OndeckLinq2DB.CreateVenueParams(id, "roundhouse", "Roundhouse", "london", at, "playlist", "open", "[\"open\"]", null), cancellationToken);
        await queries.CreateVenueAsync(new OndeckLinq2DB.CreateVenueParams(id - 1, "forum", "Forum", "london", null, "playlist", "open", null, "[\"rock\"]"), cancellationToken);
        var venue = await queries.GetVenueAsync(new OndeckLinq2DB.GetVenueParams("roundhouse", "london"), cancellationToken);
        var count = (await queries.VenueCountByCityAsync(cancellationToken)).Single();
        var updated = await queries.UpdateVenueNameAsync(new OndeckLinq2DB.UpdateVenueNameParams("The Roundhouse", "roundhouse"), cancellationToken);
        if (venue.ID != id || venue.CreatedAt != at || venue.Statuses != "[\"open\"]" || venue.Tags is not null || count.VenueCount != 2 || updated.ID != id)
            throw new InvalidOperationException("ondeck linq2db migration/optional/aggregate mapping changed");
        await queries.DeleteVenueAsync("roundhouse", cancellationToken);
        await AssertMissingAsync(() => queries.GetVenueAsync(new OndeckLinq2DB.GetVenueParams("roundhouse", "london"), cancellationToken));
    }

    private static DateTime TestTimestamp() =>
        new DateTime(2026, 9, 9, 12, 34, 56, DateTimeKind.Utc).AddTicks(1_234_560);

    private static async Task AssertMissingAsync<T>(Func<Task<T>> operation)
    {
        try
        {
            await operation();
        }
        catch (InvalidOperationException error) when (error.Message == "query returned no rows")
        {
            return;
        }
        throw new InvalidOperationException("expected generated :one method to reject a missing row");
    }
}
