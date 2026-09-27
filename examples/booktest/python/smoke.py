"""Run the generated declaration-only query through each Python YDB adapter."""

import os
from urllib.parse import urlsplit

import sqlalchemy
import ydb
import ydb.sqlalchemy
import ydb_dbapi

from .dbapi.queries import Querier as DBAPIQuerier
from .native.queries import Querier as NativeQuerier
from .sqlalchemy.queries import Querier as SQLAlchemyQuerier


def main():
    url = urlsplit(os.environ["YDB_CONNECTION_STRING"])
    config = ydb.DriverConfig(
        endpoint=f"{url.scheme}://{url.netloc}", database=url.path,
        credentials=ydb.AnonymousCredentials(),
    )
    with ydb.Driver(config) as driver:
        driver.wait(timeout=10, fail_fast=True)
        with ydb.QuerySessionPool(driver) as pool:
            NativeQuerier(pool).no_op_with_parameter(1)
    connection = ydb_dbapi.connect(
        host=url.hostname, port=url.port, database=url.path, protocol=url.scheme,
    )
    try:
        DBAPIQuerier(connection).no_op_with_parameter(2)
    finally:
        connection.close()
    engine = sqlalchemy.create_engine(f"yql+ydb://{url.netloc}/{url.path.lstrip('/')}")
    try:
        with engine.begin() as connection:
            SQLAlchemyQuerier(connection).no_op_with_parameter(3)
    finally:
        engine.dispose()


if __name__ == "__main__":
    main()
