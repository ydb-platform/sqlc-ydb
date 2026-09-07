using System;
using System.Collections.Generic;
using System.Linq;
using System.Threading;
using System.Threading.Tasks;
using Ydb.Sdk.Ado;

namespace Authors.AdoNet;

// Exercises every generated command. The caller owns YdbDataSource and the
// connection lifetime; this method neither creates nor disposes either.
public static class Smoke
{
    public static async Task ExerciseAsync(YdbConnection connection, CancellationToken cancellationToken = default)
    {
        var queries = new Queries(connection);
        const ulong id = ulong.MaxValue;
        await queries.UpsertAuthorAsync(new UpsertAuthorParams(id, "sqlc-ydb C# smoke", null), cancellationToken);
        GetAuthorRow author = await queries.GetAuthorAsync(id, cancellationToken);
        if (author.ID != id || author.Name != "sqlc-ydb C# smoke" || author.Bio is not null)
            throw new InvalidOperationException("optional Utf8 null or Uint64 binding changed");

        await queries.UpsertAuthorAsync(new UpsertAuthorParams(id, "sqlc-ydb C# smoke", "present"), cancellationToken);
        author = await queries.GetAuthorAsync(id, cancellationToken);
        if (author.Bio != "present")
            throw new InvalidOperationException("optional Utf8 value binding changed");

        GetAuthorNameRow name = await queries.GetAuthorNameAsync(id, cancellationToken);
        if (name.Name != author.Name)
            throw new InvalidOperationException("single-column :one mapping changed");
        IReadOnlyList<ListAuthorsRow> authors = await queries.ListAuthorsAsync(cancellationToken);
        if (!authors.Any(row => row.ID == id && row.Bio == "present"))
            throw new InvalidOperationException(":many mapping changed");
        await queries.DeleteAuthorAsync(id, cancellationToken);
        try
        {
            await queries.GetAuthorAsync(id, cancellationToken);
            throw new Exception("missing-row query unexpectedly succeeded");
        }
        catch (InvalidOperationException error) when (error.Message == "query returned no rows")
        {
            // :one reports an absent row without inventing a default record.
        }
    }
}
