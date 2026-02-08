CREATE TABLE authors (
    id Uint64 NOT NULL,
    name Text NOT NULL,
    PRIMARY KEY (id)
);

CREATE TABLE albums (
    id Uint64 NOT NULL,
    title Text NOT NULL,
    author_id Uint64 NOT NULL,
    PRIMARY KEY (id)
);
