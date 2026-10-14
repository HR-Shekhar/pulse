-- Create the checks history table if missing.
CREATE TABLE IF NOT EXISTS checks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  checked_at TIMESTAMP,
  url TEXT NOT NULL,
  status TEXT NOT NULL,
  response_ms INTEGER NOT NULL,
  error TEXT
);
