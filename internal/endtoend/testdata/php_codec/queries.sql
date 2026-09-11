-- name: BindValues :one
INSERT INTO values_test (id, float, float_optional, double, double_optional, uint64, uint64_optional, timestamp, timestamp_optional, json, json_optional)
VALUES ($id, $float, $float_optional, $double, $double_optional, $uint64, $uint64_optional, $timestamp, $timestamp_optional, $json, $json_optional)
RETURNING id, float, float_optional, double, double_optional, uint64, uint64_optional, timestamp, timestamp_optional, json, json_optional;
