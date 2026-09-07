-- +goose Up
CREATE TABLE authors (
    id Uint64 NOT NULL,
    old_bio Utf8,
    PRIMARY KEY (id)
);
CREATE TABLE obsolete (id Uint64 NOT NULL, PRIMARY KEY (id));

-- +goose Down
DROP TABLE authors;
DROP TABLE obsolete;
