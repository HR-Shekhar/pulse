-- Inserts one check. saveCheck fills each ? in this column order.
-- The Error name is the table's error column; SQLite matches them case-insensitively.
INSERT INTO checks (checked_at, url, status, response_ms, Error)
VALUES (
    ?,
    ?,
    ?,
    ?,
    ?
);