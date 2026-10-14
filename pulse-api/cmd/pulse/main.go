package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// UP and DOWN are allowed check status values.
const (
	UP   = "UP"
	DOWN = "DOWN"
)

const maxConcurrentChecks = 2

// checkInterval is the delay between full check rounds.
const checkInterval = 30 * time.Second

// pingResponse is the result of checking one URL.
type pingResponse struct {
	CheckedAt      time.Time
	URL            string
	Status         string
	Duration       time.Duration
	Err            error
	HTTPStatusCode int
}

// dbResponse is one row from the checks table.
type dbResponse struct {
	ID        int
	CheckedAt time.Time
	URL       string
	Status    string
	Duration  time.Duration
	Err       string
}

// urls is the fixed list of addresses checked each run.
var urls = []string{
	"https://www.himanshu-shekhar.xyz",
	"https://www.himanshushekhar.dev",
	"https://www.sythra.ai",
}

// checkURL GETs url and returns UP/DOWN, duration, and any error.
func checkURL(client *http.Client, url string) pingResponse {
	start := time.Now()
	resp, err := client.Get(url)
	if err != nil {
		return pingResponse{
			CheckedAt:      start,
			URL:            url,
			Status:         DOWN,
			Duration:       time.Since(start),
			Err:            err,
			HTTPStatusCode: 0,
		}
	}
	defer resp.Body.Close()

	status := UP
	if resp.StatusCode >= 400 {
		status = DOWN
	}
	return pingResponse{
		CheckedAt:      start,
		URL:            url,
		Status:         status,
		Duration:       time.Since(start),
		HTTPStatusCode: resp.StatusCode,
	}
}


func checkURLs(client *http.Client, urls []string, maxConcurrent int) []pingResponse {
	if len(urls) == 0 {
		return nil
	}

	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	if maxConcurrent > len(urls) {
		maxConcurrent = len(urls)
	}

	jobs := make(chan string)
	results := make(chan pingResponse)

	var wg sync.WaitGroup

	// Start a fixed number of workers.
	for i := 0; i < maxConcurrent; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for url := range jobs {
				results <- checkURL(client, url)
			}
		}()
	}

	// Feed URLs to the workers.
	go func() {
		for _, url := range urls {
			jobs <- url
		}
		close(jobs)
	}()

	// Close results only after every worker has finished.
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results in one goroutine.
	checks := make([]pingResponse, 0, len(urls))
	for result := range results {
		checks = append(checks, result)
	}

	return checks
}


// main opens SQLite, checks URLs in parallel, stores results, and prints recent history.
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

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()


	start := time.Now()

	results := checkURLs(client, urls, maxConcurrentChecks)
	
	for _, result := range results {
		if err := saveCheck(db, result); err != nil {
			fmt.Println(err.Error())
		}
	}
	
	totalDuration := time.Since(start)
	fmt.Println("total:", totalDuration)

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
