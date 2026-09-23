PRAGMA TablePathPrefix("/local/sqlc_namespaces/a");

CREATE TABLE users (
    id Uint64 NOT NULL,
    name Utf8 NOT NULL,
    PRIMARY KEY (id),
    INDEX by_name GLOBAL SYNC ON (name)
);
