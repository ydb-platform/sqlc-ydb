CREATE TABLE city (
    slug Text NOT NULL,
    name Text NOT NULL,
    PRIMARY KEY (slug)
);

CREATE TABLE venue (
    id Uint64 NOT NULL,
    status Text NOT NULL,
    slug Text NOT NULL,
    name Text NOT NULL,
    city Text NOT NULL,
    spotify_playlist Text NOT NULL,
    songkick_id Text,
    tags Text,
    created_at Text,
    PRIMARY KEY (id)
);
