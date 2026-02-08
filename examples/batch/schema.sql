CREATE TABLE authors (
    author_id Uint64 NOT NULL,
    name Text NOT NULL,
    biography Text,
    PRIMARY KEY (author_id)
);

CREATE TABLE books (
    book_id Uint64 NOT NULL,
    author_id Uint64 NOT NULL,
    isbn Text NOT NULL,
    book_type Text NOT NULL,
    title Text NOT NULL,
    year Uint64 NOT NULL,
    available Text,
    tags Text,
    PRIMARY KEY (book_id)
);
