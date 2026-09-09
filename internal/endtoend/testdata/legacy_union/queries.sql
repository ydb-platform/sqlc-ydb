-- name: DistinctLabels :many
SELECT id, label FROM local_labels
UNION
SELECT id, label FROM imported_labels;

-- name: AllLabels :many
SELECT id, label FROM local_labels
UNION ALL
SELECT id, label FROM imported_labels;
