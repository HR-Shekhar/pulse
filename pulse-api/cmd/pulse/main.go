package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"time"
	"sync"  // WaitGroup lives in this package

	_ "modernc.org/sqlite"
)



// UP and DOWN are the only status strings a check result is allowed to carry.
const (
	UP   = "UP"
	DOWN = "DOWN"
)



// How long to wait between full check rounds.
// Use tens of seconds so you don't hammer public sites.
const checkInterval = 30 * time.Second



// pingResponse is the in-memory result of checking one URL.
// checkURL returns it, and saveCheck writes these fields into the checks table.
type pingResponse struct {
	CheckedAt time.Time
	URL       string
	Status    string
	Duration  time.Duration
	Err       error
}



// dbResponse is one checks-table row loaded back from SQLite.
// readCheck scans a query result into these fields.
type dbResponse struct {
	ID        int
	CheckedAt time.Time
	URL       string
	Status    string
	Duration  time.Duration
	Err       string
}



// urls is the fixed list of addresses this program checks on each run.
var urls = []string{
	"https://www.himanshu-shekhar.xyz",
	"https://www.himanshushekhar.dev",
	"https://www.sythra.ai",
}



// checkURL sends one GET and reports UP or DOWN plus how long it took.
// A transport failure is DOWN with Err set; an HTTP status of 400 or higher is DOWN with Err nil.
func checkURL(url string) pingResponse {
	start := time.Now()
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return pingResponse{
			CheckedAt: start,
			URL:       url,
			Status:    DOWN,
			Duration:  time.Since(start),
			Err:       err,
		}
	}
	defer resp.Body.Close()

	status := UP
	if resp.StatusCode >= 400 {
		status = DOWN
	}
	return pingResponse{
		CheckedAt: start,
		URL:       url,
		Status:    status,
		Duration:  time.Since(start),
	}
}


// main opens SQLite, creates the checks table, checks each URL, stores the result, and prints the saved row.
func main() {
	db, err := sql.Open("sqlite", "./db/pulse.db")
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		fmt.Println(err.Error())
		return
	}

	if err := migrate(db); err != nil {
		fmt.Println(err.Error())
		return
	}

	// Create a ticker that fires every checkInterval.
	// ticker.C is a channel; every N seconds a value arrives on it.
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()
	
	// wg is a counter: how many URL helpers are still running.
	var wg sync.WaitGroup
	// Remember when parallel work started (optional, for Step 2.3).
	start := time.Now()

	for _, url := range urls {
		// To tell how many goroutines have started
		wg.Add(1)

		go func(url string) {
			defer wg.Done()

			if err := saveCheck(db, checkURL(url)); err != nil {
				fmt.Println(err.Error())
				return
			}
		}(url)
	}
	wg.Wait()

	total_duration := time.Since(start)
	fmt.Println("total:", total_duration)

	limit := 10
	checks, err := readCheck(db, limit)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	for _, row := range checks {
		fmt.Printf("%d, %v, %s, %s, %v, %s\n",
			row.ID, row.CheckedAt, row.URL, row.Status, row.Duration, row.Err)
	}
}
