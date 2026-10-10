-- Creates the checks history table when it does not already exist.
-- Each row is one URL check: when it ran, the result, the latency, and any error text.
CREATE TABLE IF NOT EXISTS checks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  checked_at TIMESTAMP,
  url TEXT NOT NULL,
  status TEXT NOT NULL,
  response_ms INTEGER NOT NULL,
  error TEXT
);