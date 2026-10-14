package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"sync/atomic"
)

func TestCheckURLReturnsUPForHTTP200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	result := checkURL(client, server.URL)

	if result.HTTPStatusCode != http.StatusOK {
		t.Errorf("expected statuscode %q, got %q", 200, result.HTTPStatusCode)
	}

	if result.Status != UP {
		t.Errorf("expected status %q, got %q", UP, result.Status)
	}

	if result.Err != nil {
		t.Errorf("expected no error, got %v", result.Err)
	}
}

func TestCheckURLReturnsDOWNForHTTP500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	result := checkURL(client, server.URL)

	if result.HTTPStatusCode != http.StatusInternalServerError {
		t.Errorf("expected statuscode %q, got %q", 500, result.HTTPStatusCode)
	}

	if result.Status != DOWN {
		t.Errorf("expected status %q, got %q", DOWN, result.Status)
	}

	if result.Err != nil {
		t.Errorf("expected no error, got %v", result.Err)
	}
}

func TestCheckURLReturnsDOWNForInvalidUrl(t *testing.T) {
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	result := checkURL(client, "://invalid-url")

	if result.HTTPStatusCode != 0 {
		t.Errorf("expected statuscode %q, got %q", 0, result.HTTPStatusCode)
	}

	if result.Status != DOWN {
		t.Errorf("expected status %q, got %q", DOWN, result.Status)
	}

	if result.Err == nil {
		t.Errorf("expected an error for invalid URL, got nil")
	}
}

func TestCheckURLReturnsDOWNOnTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &http.Client{
		Timeout: 20 * time.Millisecond,
	}

	result := checkURL(client, server.URL)

	if result.Status != DOWN {
		t.Errorf("expected status %q, got %q", DOWN, result.Status)
	}

	if result.Err == nil {
		t.Error("expected a timeout error, got nil")
	}

	if result.HTTPStatusCode != 0 {
		t.Errorf("expected HTTP status code 0, got %d", result.HTTPStatusCode)
	}
}

func TestCheckURLReturnsDOWNForConnectionFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	url := server.URL
	server.Close()

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	result := checkURL(client, url)

	if result.Status != DOWN {
		t.Errorf("expected status %q, got %q", DOWN, result.Status)
	}

	if result.Err == nil {
		t.Error("expected a connection error, got nil")
	}

	if result.HTTPStatusCode != 0 {
		t.Errorf("expected HTTP status code 0, got %d", result.HTTPStatusCode)
	}
}

func TestCheckURLsRespectsConcurrencyLimit(t *testing.T) {
	var active atomic.Int32
	var maxActive atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := active.Add(1)

		for {
			previous := maxActive.Load()
			if current <= previous || maxActive.CompareAndSwap(previous, current) {
				break
			}
		}

		time.Sleep(50 * time.Millisecond)
		active.Add(-1)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	urls := make([]string, 8)
	for i := range urls {
		urls[i] = server.URL
	}

	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	results := checkURLs(client, urls, 2)

	if len(results) != len(urls) {
		t.Fatalf("expected %d results, got %d", len(urls), len(results))
	}

	if got := maxActive.Load(); got > 2 {
		t.Errorf("expected at most 2 concurrent requests, got %d", got)
	}

	if got := maxActive.Load(); got < 2 {
		t.Errorf("expected concurrent requests to overlap, maximum was %d", got)
	}

	for _, result := range results {
		if result.Status != UP {
			t.Errorf("expected status %q, got %q", UP, result.Status)
		}
	}
}
