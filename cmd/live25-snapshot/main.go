// Command live25-snapshot reads a single room's 25Live availability and event
// detail records using a locally stored user session. It never writes to 25Live.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"loriss-prototype/internal/live25"
)

func main() {
	var (
		account  string
		service  string
		baseURL  string
		spaceID  int
		start    string
		pageSize int
		timeout  time.Duration
	)
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		log.Fatal(err)
	}
	flag.StringVar(&account, "keychain-account", "", "macOS Keychain account containing the local 25Live session")
	flag.StringVar(&service, "keychain-service", live25.DefaultKeychainService, "macOS Keychain service name")
	flag.StringVar(&baseURL, "base-url", live25.DefaultBaseURL, "25Live JSON API base URL")
	flag.IntVar(&spaceID, "space-id", 0, "25Live room ID to read")
	flag.StringVar(&start, "start", time.Now().In(newYork).Format("2006-01-02"), "availability start date in YYYY-MM-DD")
	flag.IntVar(&pageSize, "page-size", 100, "availability page size")
	flag.DurationVar(&timeout, "timeout", 30*time.Second, "HTTP request timeout")
	flag.Parse()

	if account == "" || spaceID <= 0 {
		fmt.Fprintln(os.Stderr, "-keychain-account and a positive -space-id are required")
		flag.Usage()
		os.Exit(2)
	}
	startAt, err := time.ParseInLocation("2006-01-02", start, newYork)
	if err != nil {
		log.Fatalf("parse -start: %v", err)
	}

	session, err := live25.NewKeychainSession(service, account)
	if err != nil {
		log.Fatal(err)
	}
	httpClient, err := live25.NewSessionHTTPClient(baseURL, session, &http.Client{Timeout: timeout})
	if err != nil {
		log.Fatal(err)
	}
	reader, err := live25.NewClient(baseURL, httpClient)
	if err != nil {
		log.Fatal(err)
	}
	snapshot, err := live25.Snapshot(context.Background(), reader, live25.RoomSnapshotRequest{
		SpaceID:  spaceID,
		Start:    startAt,
		PageSize: pageSize,
	})
	if err != nil {
		log.Fatal(err)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(snapshot); err != nil {
		log.Fatal(fmt.Errorf("write room snapshot: %w", err))
	}
}
