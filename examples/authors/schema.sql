CREATE TABLE IF NOT EXISTS authors (
   id Uint64 NOT NULL,
   name Utf8 NOT NULL,
   bio Utf8,
   INDEX by_name GLOBAL SYNC ON (name),
   INDEX by_name_covering GLOBAL SYNC ON (name) COVER (bio),
   PRIMARY KEY (id)
);
