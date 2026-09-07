CREATE TABLE venues (
    id Uint64 NOT NULL,
    dropped Utf8,
    status Utf8 NOT NULL,
    statuses Json,
    slug Utf8 NOT NULL,
    name Utf8 NOT NULL,
    city Utf8 NOT NULL,
    spotify_playlist Utf8 NOT NULL,
    songkick_id Utf8,
    tags Json,
    PRIMARY KEY (id)
);
