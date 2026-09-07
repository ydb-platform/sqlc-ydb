CREATE TABLE authors (
    author_id Uint64 NOT NULL,
    name Utf8 NOT NULL,
    biography Json,
    PRIMARY KEY (author_id)
);

CREATE TABLE books (
    book_id Uint64 NOT NULL,
    author_id Uint64 NOT NULL,
    isbn Utf8 NOT NULL,
    book_type Utf8 NOT NULL,
    title Utf8 NOT NULL,
    year Int32 NOT NULL,
    available Timestamp NOT NULL,
    tags Json NOT NULL,
    PRIMARY KEY (book_id)
);
