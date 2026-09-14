CREATE TABLE books (
    book_id Uint64 NOT NULL,
    title Utf8,
    tags Json NOT NULL,
    PRIMARY KEY (book_id)
);
