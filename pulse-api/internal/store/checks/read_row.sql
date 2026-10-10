-- Returns every saved check whose url equals the bound parameter.
SELECT id, checked_at, url, status, response_ms, error
FROM checks
ORDER BY id DESC
LIMIT ?;