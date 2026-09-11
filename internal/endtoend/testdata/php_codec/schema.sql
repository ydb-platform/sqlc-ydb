CREATE TABLE values_test (
  id Uint64 NOT NULL,
  float Float NOT NULL,
  float_optional Float,
  double Double NOT NULL,
  double_optional Double,
  uint64 Uint64 NOT NULL,
  uint64_optional Uint64,
  timestamp Timestamp NOT NULL,
  timestamp_optional Timestamp,
  json Json NOT NULL,
  json_optional Json,
  PRIMARY KEY (id)
);
