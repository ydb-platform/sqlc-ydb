CREATE TABLE authors (id Uint64 NOT NULL, name Utf8 NOT NULL, PRIMARY KEY (id));
CREATE TABLE books (id Uint64 NOT NULL, author_id Uint64 NOT NULL, title Utf8 NOT NULL, PRIMARY KEY (id));
