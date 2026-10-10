package main

import (
	"database/sql"
	"os"
	"time"
)

// migrate reads the create-table SQL file and runs it on the open database.
// IF NOT EXISTS leaves an existing checks table unchanged on a later call.
func migrate(db *sql.DB) error {
	migrationSQL, err := os.ReadFile("db/migrations/0001_create_checks_table.sql")
	if err != nil {
		return err
	}

	_, err = db.Exec(string(migrationSQL))
	return err
}

// saveCheck inserts one pingResponse into checks using insert_row.sql.
// A nil Err is stored as an empty string because the error column holds text.
func saveCheck(db *sql.DB, r pingResponse) error {
	insertQuery, err := os.ReadFile("internal/store/checks/insert_row.sql")
	if err != nil {
		return err
	}

	errString := ""
	if r.Err != nil {
		errString = r.Err.Error()
	}

	_, err = db.Exec(
		string(insertQuery),
		r.CheckedAt,
		r.URL,
		r.Status,
		r.Duration.Milliseconds(),
		errString,
	)
	return err
}

// readCheck runs the SQL in read_row.sql and turns each database row into one dbResponse. 
// It returns those records as a slice, newest first, up to limit.
func readCheck(db *sql.DB, limit int) ([]dbResponse, error) {
	query, err := os.ReadFile("internal/store/checks/read_row.sql")
	if err != nil {
		return nil, err
	}

	var checks []dbResponse

	rows, err := db.Query(string(query), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var row dbResponse
		var responseMs int64
		rows.Scan(
		&row.ID,
		&row.CheckedAt,
		&row.URL,
		&row.Status,
		&responseMs,
		&row.Err,
		)
		row.Duration = time.Duration(responseMs) * time.Millisecond
		checks = append(checks, row)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return checks, nil
}
