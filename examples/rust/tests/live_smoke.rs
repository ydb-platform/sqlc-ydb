#![recursion_limit = "256"]

use std::time::{Duration, SystemTime};

use sqlc_ydb_rust_examples::{authors, batch, booktest, jets, ondeck};

async fn exec(client: &mut ydb::QueryClient, sql: &str) -> ydb::YdbResult<()> {
    client.exec(sql).await
}

async fn authors_smoke(client: &mut ydb::QueryClient) -> ydb::YdbResult<()> {
    exec(client, include_str!("../../authors/schema.sql")).await?;
    let result = async {
        let mut queries = authors::queries::Queries::new(client);
        let created = queries
            .create_author(u64::MAX, "Автор".into(), None)
            .await?;
        assert_eq!(created.name, "Автор");
        assert_eq!(created.bio, None);

        queries
            .upsert_author(u64::MAX, "Автор".into(), Some("Биография".into()))
            .await?;
        let author = queries.get_author(u64::MAX).await?;
        assert_eq!(author.bio.as_deref(), Some("Биография"));
        assert_eq!(queries.list_authors().await?.len(), 1);
        queries.delete_author(u64::MAX).await?;
        Ok::<(), ydb::YdbError>(())
    }
    .await;
    let cleanup = exec(client, "DROP TABLE authors;").await;
    result.and(cleanup)
}

async fn batch_smoke(client: &mut ydb::QueryClient) -> ydb::YdbResult<()> {
    exec(client, include_str!("../../batch/schema.sql")).await?;
    let result = async {
        let mut queries = batch::queries::Queries::new(client);
        let author = queries.create_author(1, "Ada".into(), None).await?;
        assert_eq!(author.biography, None);

        let available = SystemTime::UNIX_EPOCH
            + Duration::from_secs(1_700_000_000)
            + Duration::from_micros(123_456);
        let book = queries
            .create_book(
                1,
                1,
                "isbn-1".into(),
                "FICTION".into(),
                "Typed Rust".into(),
                2026,
                available,
                "[\"rust\"]".into(),
            )
            .await?;
        assert_eq!(book.book_id, 1);
        assert_eq!(book.tags, "[\"rust\"]");
        assert_eq!(book.available, available);
        assert_eq!(queries.books_by_year(2026).await?.len(), 1);
        queries
            .update_book("Updated".into(), "[\"ydb\"]".into(), 1)
            .await?;
        assert_eq!(queries.books_by_year(2026).await?[0].title, "Updated");
        Ok::<(), ydb::YdbError>(())
    }
    .await;
    let cleanup = exec(client, "DROP TABLE books; DROP TABLE authors;").await;
    result.and(cleanup)
}

async fn booktest_smoke(client: &mut ydb::QueryClient) -> ydb::YdbResult<()> {
    exec(client, include_str!("../../booktest/schema.sql")).await?;
    let result = async {
        let mut queries = booktest::queries::Queries::new(client);
        let available = SystemTime::UNIX_EPOCH + Duration::from_secs(1_710_000_000);
        queries
            .create_book(
                7,
                404,
                "isbn-7".into(),
                "REFERENCE".into(),
                "Orphaned Book".into(),
                2024,
                available,
                "[\"join\"]".into(),
            )
            .await?;
        let rows = queries.books_by_tags("[\"join\"]".into()).await?;
        assert_eq!(rows.len(), 1);
        assert_eq!(rows[0].name, None);
        assert_eq!(queries.say_hello("YDB".into()).await?.greeting, "hello YDB");
        queries
            .update_book_isbn("Updated".into(), "[]".into(), "isbn-new".into(), 7)
            .await?;
        assert_eq!(queries.get_book(7).await?.isbn, "isbn-new");
        Ok::<(), ydb::YdbError>(())
    }
    .await;
    let cleanup = exec(client, "DROP TABLE books; DROP TABLE authors;").await;
    result.and(cleanup)
}

async fn jets_smoke(client: &mut ydb::QueryClient) -> ydb::YdbResult<()> {
    exec(client, include_str!("../../jets/schema.sql")).await?;
    exec(
        client,
        "UPSERT INTO pilots (id, name) VALUES (1, \"Maverick\"u), (2, \"Iceman\"u);",
    )
    .await?;
    let result = async {
        let mut queries = jets::queries::Queries::new(client);
        assert_eq!(queries.count_pilots().await?.pilot_count, 2);
        assert_eq!(queries.list_pilots().await?.len(), 2);
        queries.delete_pilot(1).await?;
        assert_eq!(queries.count_pilots().await?.pilot_count, 1);
        Ok::<(), ydb::YdbError>(())
    }
    .await;
    let cleanup = exec(
        client,
        "DROP TABLE pilot_languages; DROP TABLE languages; DROP TABLE jets; DROP TABLE pilots;",
    )
    .await;
    result.and(cleanup)
}

async fn ondeck_smoke(client: &mut ydb::QueryClient) -> ydb::YdbResult<()> {
    for migration in [
        include_str!("../../ondeck/schema/0001_city.sql"),
        include_str!("../../ondeck/schema/0002_venue.sql"),
        include_str!("../../ondeck/schema/0003_rename_venue.sql"),
        include_str!("../../ondeck/schema/0004_add_created_at.sql"),
        include_str!("../../ondeck/schema/0005_drop_column.sql"),
    ] {
        exec(client, migration).await?;
    }
    let result = async {
        let mut queries = ondeck::queries::Queries::new(client);
        let city = queries
            .create_city("Moscow".into(), "moscow".into())
            .await?;
        assert_eq!(city.slug, "moscow");
        let created_at = SystemTime::UNIX_EPOCH
            + Duration::from_secs(1_720_000_000)
            + Duration::from_micros(654_321);
        let venue = queries
            .create_venue(
                1,
                "club".into(),
                "Club".into(),
                "moscow".into(),
                Some(created_at),
                "playlist".into(),
                "open".into(),
                None,
                Some("[\"music\"]".into()),
            )
            .await?;
        assert_eq!(venue.id, 1);
        let loaded = queries.get_venue("club".into(), "moscow".into()).await?;
        assert_eq!(loaded.created_at, Some(created_at));
        assert_eq!(loaded.statuses, None);
        assert_eq!(loaded.tags.as_deref(), Some("[\"music\"]"));
        assert_eq!(queries.venue_count_by_city().await?[0].venue_count, 1);
        assert_eq!(
            queries
                .update_venue_name("Renamed".into(), "club".into())
                .await?
                .id,
            1
        );
        queries.delete_venue("club".into()).await?;
        Ok::<(), ydb::YdbError>(())
    }
    .await;
    let cleanup = exec(client, "DROP TABLE venue; DROP TABLE city;").await;
    result.and(cleanup)
}

#[tokio::test(flavor = "multi_thread")]
async fn generated_queries_execute_all_five_example_families() -> ydb::YdbResult<()> {
    let connection_string = match std::env::var("YDB_CONNECTION_STRING") {
        Ok(value) => value,
        Err(_) => {
            eprintln!("set YDB_CONNECTION_STRING for live Rust acceptance");
            return Ok(());
        }
    };
    let client = ydb::ClientBuilder::new_from_connection_string(connection_string)?
        .build()
        .await?;
    let mut query_client = client.query_client();

    authors_smoke(&mut query_client)
        .await
        .map_err(|err| ydb::YdbError::Custom(format!("authors: {err}")))?;
    batch_smoke(&mut query_client)
        .await
        .map_err(|err| ydb::YdbError::Custom(format!("batch: {err}")))?;
    booktest_smoke(&mut query_client)
        .await
        .map_err(|err| ydb::YdbError::Custom(format!("booktest: {err}")))?;
    jets_smoke(&mut query_client)
        .await
        .map_err(|err| ydb::YdbError::Custom(format!("jets: {err}")))?;
    ondeck_smoke(&mut query_client)
        .await
        .map_err(|err| ydb::YdbError::Custom(format!("ondeck: {err}")))?;
    Ok(())
}
