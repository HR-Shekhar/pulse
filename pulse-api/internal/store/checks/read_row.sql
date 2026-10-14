-- Newest checks first, up to the bound LIMIT.
SELECT id, checked_at, url, status, response_ms, error
FROM checks
ORDER BY id DESC
LIMIT ?;
