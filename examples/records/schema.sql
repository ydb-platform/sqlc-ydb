CREATE TABLE records (
    owner_hash Uint64 NOT NULL,
    owner_id Uint64 NOT NULL,
    record_id Utf8 NOT NULL,
    group_id Utf8 NOT NULL,
    payload Bytes NOT NULL,
    attributes Json NOT NULL,
    created_at Timestamp NOT NULL,
    updated_at Timestamp NOT NULL,
    PRIMARY KEY (owner_hash, record_id)
);
