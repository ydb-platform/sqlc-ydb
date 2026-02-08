CREATE TABLE pilots (
    id Uint64 NOT NULL,
    name Text NOT NULL,
    PRIMARY KEY (id)
);

CREATE TABLE jets (
    id Uint64 NOT NULL,
    pilot_id Uint64 NOT NULL,
    age Uint64 NOT NULL,
    name Text NOT NULL,
    color Text NOT NULL,
    PRIMARY KEY (id)
);

CREATE TABLE languages (
    id Uint64 NOT NULL,
    language Text NOT NULL,
    PRIMARY KEY (id)
);

CREATE TABLE pilot_languages (
    pilot_id Uint64 NOT NULL,
    language_id Uint64 NOT NULL,
    PRIMARY KEY (pilot_id, language_id)
);
