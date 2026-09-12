using System;
using System.Data;
using Dapper;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Runtime.ExceptionServices;
using System.Threading;
using System.Threading.Tasks;
using Ydb.Sdk.Ado;
using AuthorsDapper = Authors.Dapper;
using BatchDapper = Batch.Dapper;
using BooktestDapper = Booktest.Dapper;
using JetsDapper = Jets.Dapper;
using OndeckDapper = Ondeck.Dapper;

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
        if (args.Length == 1 && args[0] == "contracts")
        {
            CheckTimestampContracts();
            CheckDapperMapping();
            return 0;
        }
        if (args.Length != 1 || args[0] != "dapper")
        {
            Console.Error.WriteLine("usage: GeneratedProfiles <dapper|contracts>");
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
        await ExerciseDapperAsync(connection, timeout.Token);
        return 0;
    }

    private static void CheckDapperMapping()
    {
        var underscoreSetting = DefaultTypeMap.MatchNamesWithUnderscores;
        System.Runtime.CompilerServices.RuntimeHelpers.RunClassConstructor(typeof(BatchDapper.Queries).TypeHandle);
        if (DefaultTypeMap.MatchNamesWithUnderscores != underscoreSetting)
            throw new InvalidOperationException("Generated mapping changed global Dapper settings");

        using var table = new DataTable();
        table.Columns.Add("book_id", typeof(ulong));
        table.Columns.Add("author_id", typeof(ulong));
        table.Columns.Add("isbn", typeof(string));
        table.Columns.Add("book_type", typeof(string));
        table.Columns.Add("title", typeof(string));
        table.Columns.Add("year", typeof(int));
        table.Columns.Add("available", typeof(DateTime));
        table.Columns.Add("tags", typeof(string));
        var timestamp = TestTimestamp();
        const string json = "{\"large\":9007199254740993}";
        table.Rows.Add(ulong.MaxValue, ulong.MaxValue - 1, "isbn", "paper", "title", 2026, timestamp, json);
        using var reader = table.CreateDataReader();
        var parse = reader.GetRowParser<BatchDapper.BooksByYearRow>();
        if (!reader.Read()) throw new InvalidOperationException("Missing mapping fixture");
        var row = parse(reader);
        if (row.BookID != ulong.MaxValue || row.AuthorID != ulong.MaxValue - 1 || row.BookType != "paper" || row.Available != timestamp || row.Tags != json)
            throw new InvalidOperationException("Dapper constructor mapping changed typed values");

        using var optional = new DataTable();
        optional.Columns.Add("author_id", typeof(ulong));
        optional.Columns.Add("name", typeof(string));
        optional.Columns.Add("biography", typeof(string));
        optional.Rows.Add(1UL, "author", DBNull.Value);
        using var optionalReader = optional.CreateDataReader();
        var parseOptional = optionalReader.GetRowParser<BatchDapper.GetAuthorRow>();
        optionalReader.Read();
        if (parseOptional(optionalReader).Biography is not null)
            throw new InvalidOperationException("Dapper nullable constructor mapping changed null");
        Console.WriteLine("Dapper record mapping contracts passed");
    }

    private static void CheckTimestampContracts()
    {
        var utc = new DateTime(2026, 9, 11, 12, 34, 56, DateTimeKind.Utc).AddTicks(123450);
        foreach (var type in new[] { typeof(BatchDapper.Queries) })
        {
            var normalize = type.GetMethod("NormalizeTimestamp",
                System.Reflection.BindingFlags.NonPublic | System.Reflection.BindingFlags.Static,
                null, new[] { typeof(DateTime) }, null)!;
            foreach (var input in new[] { utc, utc.ToLocalTime(), DateTime.SpecifyKind(utc, DateTimeKind.Unspecified) })
            {
                var actual = (DateTime)normalize.Invoke(null, new object[] { input })!;
                if (actual.Kind != DateTimeKind.Utc || actual.Ticks != utc.Ticks)
                    throw new InvalidOperationException($"{type}: Timestamp normalization changed the instant");
                var wire = Ydb.Sdk.Value.YdbValue.MakeTimestamp(actual).GetTimestamp();
                if (wire.Ticks != utc.Ticks)
                    throw new InvalidOperationException($"{type}: Timestamp wire conversion changed microseconds");
            }
            var optional = type.GetMethod("NormalizeTimestamp",
                System.Reflection.BindingFlags.NonPublic | System.Reflection.BindingFlags.Static,
                null, new[] { typeof(DateTime?) }, null)!;
            if (optional.Invoke(null, new object?[] { null }) is not null)
                throw new InvalidOperationException("Optional Timestamp lost null");
        }
        Console.WriteLine("Timestamp contracts passed");
    }

    private static async Task ExerciseDapperAsync(YdbConnection connection, CancellationToken cancellationToken)
    {
        await RunExampleAsync(connection, "authors", AuthorsSchema, token => ExerciseAuthorsDapperAsync(connection, token), cancellationToken);
        await RunExampleAsync(connection, "batch", BatchSchema, token => ExerciseBatchDapperAsync(connection, token), cancellationToken);
        await RunExampleAsync(connection, "booktest", BooktestSchema, token => ExerciseBooktestDapperAsync(connection, token), cancellationToken);
        await RunExampleAsync(connection, "jets", JetsSchema, token => ExerciseJetsDapperAsync(connection, token), cancellationToken);
        await RunExampleAsync(connection, "ondeck", OndeckSchema, token => ExerciseOndeckDapperAsync(connection, token), cancellationToken);
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
            var tableNameIndex = 2;
            if (words[2].Equals("IF", StringComparison.OrdinalIgnoreCase))
            {
                if (words.Length < 6 ||
                    !words[3].Equals("NOT", StringComparison.OrdinalIgnoreCase) ||
                    !words[4].Equals("EXISTS", StringComparison.OrdinalIgnoreCase))
                    return;
                tableNameIndex = 5;
            }
            ownedTables.Add(words[tableNameIndex]);
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
        var fetched = await queries.GetAuthorAsync(id, cancellationToken, commandTimeout: 10);
        if (created.ID != id || created.Bio is not null || fetched.Name != "Ada Lovelace" || fetched.Bio != "programmer" || (await queries.ListAuthorsAsync(cancellationToken)).Single().ID != id)
            throw new InvalidOperationException("authors Dapper CRUD mapping changed");
        await using (var transaction = (YdbTransaction)await connection.BeginTransactionAsync(cancellationToken))
        {
            await using var otherConnection = new YdbConnection(connection.ConnectionString);
            try
            {
                _ = new AuthorsDapper.Queries(otherConnection, transaction);
                throw new InvalidOperationException("foreign transaction was accepted");
            }
            catch (ArgumentException error) when (error.ParamName == "transaction")
            {
            }
            var transactional = queries.WithTransaction(transaction);
            await transactional.UpsertAuthorAsync(new AuthorsDapper.UpsertAuthorParams(id, "transaction", null), cancellationToken);
            if ((await transactional.GetAuthorAsync(id, cancellationToken)).Name != "transaction")
                throw new InvalidOperationException("Dapper transaction did not read its write");
            await transaction.RollbackAsync(cancellationToken);
        }
        if ((await queries.GetAuthorAsync(id, cancellationToken)).Name != "Ada Lovelace")
            throw new InvalidOperationException("Dapper rollback changed committed data");
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

    private static DateTime TestTimestamp() =>
        new DateTime(2026, 9, 9, 12, 34, 56, DateTimeKind.Utc).AddTicks(1_234_560);

    private static async Task AssertMissingAsync<T>(Func<Task<T>> operation)
    {
        try
        {
            await operation();
        }
        catch (InvalidOperationException)
        {
            return;
        }
        throw new InvalidOperationException("expected generated :one method to reject a missing row");
    }
}
