"""Run CLI-generated Python code against a disposable YDB database.

Requires SQLC_YDB_TEST_DSN and no existing authors table. An existing table is
never dropped if CREATE TABLE fails. Run profiles sequentially with Go smoke.
"""

import os
from pathlib import Path
from urllib.parse import urlsplit

import sqlalchemy
import ydb
import ydb.sqlalchemy  # registers the YDB dialect
import ydb_dbapi

from python_ydb.queries import Querier as NativeQuerier
from python_dbapi.queries import Querier as DBAPIQuerier
from python_sqlalchemy.queries import Querier as SQLAlchemyQuerier


def check(querier):
    author_id = 2**64 - 1
    querier.upsert_author(author_id, "Автор", None)
    author = querier.get_author(author_id)
    assert author.id == author_id and author.name == "Автор" and author.bio is None
    assert querier.get_author_name(author_id).name == "Автор"
    querier.upsert_author(author_id, "Автор", "Биография")
    assert querier.get_author(author_id).bio == "Биография"
    assert len(list(querier.list_authors())) == 1
    querier.delete_author(author_id)
    assert querier.get_author(author_id) is None


def main():
    url = urlsplit(os.environ["SQLC_YDB_TEST_DSN"])
    config = ydb.DriverConfig(
        endpoint=f"{url.scheme}://{url.netloc}", database=url.path,
        credentials=ydb.AnonymousCredentials(),
    )
    with ydb.Driver(config) as driver:
        driver.wait(timeout=10, fail_fast=True)
        with ydb.QuerySessionPool(driver) as pool:
            pool.execute_with_retries(Path(__file__).with_name("schema.sql").read_text())
            try:
                check(NativeQuerier(pool))
                print("native YDB: passed", flush=True)
                connection = ydb_dbapi.connect(
                    host=url.hostname, port=url.port, database=url.path, protocol=url.scheme,
                )
                try:
                    check(DBAPIQuerier(connection))
                finally:
                    connection.close()
                print("DB-API: passed", flush=True)
                engine = sqlalchemy.create_engine(f"yql+ydb://{url.netloc}/{url.path.lstrip('/')}")
                try:
                    with engine.begin() as connection:
                        check(SQLAlchemyQuerier(connection))
                finally:
                    engine.dispose()
                print("SQLAlchemy: passed", flush=True)
            finally:
                pool.execute_with_retries("DROP TABLE authors;")


if __name__ == "__main__":
    main()
