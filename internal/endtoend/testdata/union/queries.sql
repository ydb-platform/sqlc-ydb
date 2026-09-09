-- name: DistinctLabels :many
SELECT id, label FROM local_labels
UNION
SELECT id, label FROM imported_labels;

-- name: QualifiedMissing :many
SELECT a.id FROM local_labels AS a JOIN imported_labels AS b ON a.id = b.id
UNION ALL
SELECT b.id FROM local_labels AS a JOIN imported_labels AS b ON a.id = b.id;

-- name: QualifiedNames :many
SELECT a.id, b.id FROM local_labels AS a JOIN imported_labels AS b ON a.id = b.id
UNION ALL
SELECT a.id, b.id FROM local_labels AS a JOIN imported_labels AS b ON a.id = b.id;

-- name: AllLabels :many
SELECT id, label FROM local_labels
UNION ALL
SELECT id, label FROM imported_labels;
