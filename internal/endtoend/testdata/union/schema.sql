CREATE TABLE local_labels (
    id Int64 NOT NULL,
    label Utf8,
    PRIMARY KEY (id)
);

CREATE TABLE imported_labels (
    id Int64 NOT NULL,
    label Utf8 NOT NULL,
    PRIMARY KEY (id)
);
