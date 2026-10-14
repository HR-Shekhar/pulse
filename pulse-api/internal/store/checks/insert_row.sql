-- Insert one check row; bind checked_at, url, status, response_ms, error.
INSERT INTO checks (checked_at, url, status, response_ms, Error)
VALUES (
    ?,
    ?,
    ?,
    ?,
    ?
);
