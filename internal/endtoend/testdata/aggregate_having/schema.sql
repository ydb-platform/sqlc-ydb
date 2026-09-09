CREATE TABLE weather (
    city Utf8 NOT NULL,
    temperature Int32 NOT NULL,
    PRIMARY KEY (city, temperature)
);
