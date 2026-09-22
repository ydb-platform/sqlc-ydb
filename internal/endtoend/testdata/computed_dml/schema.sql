CREATE TABLE counters (
    id Utf8 NOT NULL,
    value Int64 NOT NULL,
    optional_value Int64,
    label Utf8,
    enabled Bool NOT NULL,
    PRIMARY KEY (id)
);
