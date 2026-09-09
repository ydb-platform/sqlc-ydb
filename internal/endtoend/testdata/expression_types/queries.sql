-- name: NormalizeProfiles :many
DECLARE $fallback AS Utf8;
DECLARE $minimum_score AS Int32;
DECLARE $use_nickname AS Bool;
SELECT
    CASE WHEN $use_nickname THEN nickname ELSE $fallback END AS display_name,
    CAST(score AS Int64) AS score64,
    COALESCE(nickname, $fallback) AS normalized_name,
    LENGTH(COALESCE(nickname, $fallback)) AS normalized_length,
    ABS(score) AS absolute_score
FROM profiles
WHERE score >= $minimum_score;
