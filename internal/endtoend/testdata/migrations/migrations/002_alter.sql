-- +goose Up
ALTER TABLE authors ADD COLUMN name Utf8, DROP COLUMN old_bio;
DROP TABLE obsolete;

-- +goose Down
ALTER TABLE authors DROP COLUMN name;
