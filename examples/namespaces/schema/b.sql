PRAGMA TablePathPrefix("/local/sqlc_namespaces/b");

CREATE TABLE users (
    id Uint64 NOT NULL,
    name Utf8 NOT NULL,
    PRIMARY KEY (id)
);
