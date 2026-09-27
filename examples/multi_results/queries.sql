-- name: FetchSummary :multi
DECLARE $id AS Uint64;
-- result: Item
SELECT $id AS id FROM (SELECT 1 AS x) AS source WHERE false;
-- result: Flags
SELECT true AS enabled LIMIT 1;
SELECT "ready"u AS status;

-- name: BareLiterals :multi
SELECT 1;
SELECT "2"u;
SELECT false;
