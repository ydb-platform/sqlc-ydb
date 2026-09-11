"""Run CLI-generated Python code against a disposable YDB database.

From examples/authors, run: python -m python.smoke

Requires YDB_CONNECTION_STRING and no existing authors table. An existing table is
never dropped if CREATE TABLE fails. Run profiles sequentially with Go smoke.
"""

import os
from contextlib import contextmanager
from threading import Event
from pathlib import Path
from urllib.parse import urlsplit

import sqlalchemy
import ydb
import ydb.sqlalchemy  # registers the YDB dialect
import ydb_dbapi

from .native.queries import Querier as NativeQuerier
from .dbapi.queries import Querier as DBAPIQuerier
from .sqlalchemy.queries import Querier as SQLAlchemyQuerier


def check(querier):
    author_id = 2**64 - 1
    created = querier.create_author(author_id, "Автор", None)
    assert created is not None and created.id == author_id
    assert created.name == "Автор" and created.bio is None
    author = querier.get_author(author_id)
    assert author.id == author_id and author.name == "Автор" and author.bio is None
    assert querier.get_author_name(author_id).name == "Автор"
    querier.upsert_author(author_id, "Автор", "Биография")
    assert querier.get_author(author_id).bio == "Биография"
    authors = querier.list_authors()
    assert isinstance(authors, list) and len(authors) == 1
    querier.delete_author(author_id)
    assert querier.get_author(author_id) is None


def check_native_transaction(pool):
    author_id = 2**64 - 2

    def rollback(transaction):
        querier = NativeQuerier(transaction)
        querier.upsert_author(author_id, "rolled back", None)
        assert querier.get_author(author_id).name == "rolled back"
        transaction.rollback()

    pool.retry_tx_sync(rollback)

    assert NativeQuerier(pool).get_author(author_id) is None


def check_retry_policy():
    class FailingPool(ydb.QuerySessionPool):
        def __init__(self):
            self._should_stop = Event()
            self.attempts = 0

        @contextmanager
        def checkout(self, timeout=None):
            yield self

        def execute(self, query, parameters):
            self.attempts += 1
            if self.attempts == 1:
                raise ydb.ConnectionLost("response lost after possible commit")
            return iter([])

    pool = FailingPool()
    try:
        NativeQuerier(pool).delete_author(1)
    except ydb.ConnectionLost:
        pass
    else:
        raise AssertionError("ambiguous write was automatically replayed")
    assert pool.attempts == 1

    pool = FailingPool()
    settings = ydb.RetrySettings(
        max_retries=1, idempotent=True,
        slow_backoff_settings=ydb.BackoffSettings(0, 0),
    )
    NativeQuerier(pool, retry_settings=settings).delete_author(1)
    assert pool.attempts == 2


def main():
    check_retry_policy()
    url = urlsplit(os.environ["YDB_CONNECTION_STRING"])
    config = ydb.DriverConfig(
        endpoint=f"{url.scheme}://{url.netloc}", database=url.path,
        credentials=ydb.AnonymousCredentials(),
    )
    with ydb.Driver(config) as driver:
        driver.wait(timeout=10, fail_fast=True)
        with ydb.QuerySessionPool(driver) as pool:
            pool.execute_with_retries((Path(__file__).resolve().parent.parent / "schema.sql").read_text())
            try:
                check(NativeQuerier(pool))
                check_native_transaction(pool)
                print("native YDB: passed", flush=True)
                connection = ydb_dbapi.connect(
                    host=url.hostname, port=url.port, database=url.path, protocol=url.scheme,
                )
                try:
                    check(DBAPIQuerier(connection))
                    connection.set_isolation_level(ydb_dbapi.IsolationLevel.SERIALIZABLE)
                    connection.begin()
                    transactional = DBAPIQuerier(connection)
                    transactional.upsert_author(42, "transaction", None)
                    assert transactional.get_author(42).name == "transaction"
                    connection.rollback()
                    assert transactional.get_author(42) is None
                finally:
                    connection.close()
                print("DB-API: passed", flush=True)
                engine = sqlalchemy.create_engine(f"yql+ydb://{url.netloc}/{url.path.lstrip('/')}")
                try:
                    with engine.begin() as connection:
                        check(SQLAlchemyQuerier(connection))
                    with engine.connect().execution_options(isolation_level="SERIALIZABLE") as connection:
                        transaction = connection.begin()
                        transactional = SQLAlchemyQuerier(connection)
                        transactional.upsert_author(42, "transaction", None)
                        assert transactional.get_author(42).name == "transaction"
                        transaction.rollback()
                        assert transactional.get_author(42) is None
                finally:
                    engine.dispose()
                print("SQLAlchemy: passed", flush=True)
            finally:
                pool.execute_with_retries("DROP TABLE authors;")


if __name__ == "__main__":
    main()
