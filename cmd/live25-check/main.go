// Command live25-check verifies that a locally stored user session can read a
// single configured 25Live room. It never writes to 25Live.
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
		account string
		service string
		baseURL string
		spaceID int
		timeout time.Duration
	)
	flag.StringVar(&account, "keychain-account", "", "macOS Keychain account containing the local 25Live session")
	flag.StringVar(&service, "keychain-service", live25.DefaultKeychainService, "macOS Keychain service name")
	flag.StringVar(&baseURL, "base-url", live25.DefaultBaseURL, "25Live JSON API base URL")
	flag.IntVar(&spaceID, "space-id", 0, "25Live room ID to validate")
	flag.DurationVar(&timeout, "timeout", 30*time.Second, "HTTP request timeout")
	flag.Parse()

	if account == "" || spaceID <= 0 {
		fmt.Fprintln(os.Stderr, "-keychain-account and a positive -space-id are required")
		flag.Usage()
		os.Exit(2)
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

	location, err := reader.Location(context.Background(), spaceID)
	if err != nil {
		log.Fatal(fmt.Errorf("read 25Live location: %w", err))
	}
	if err := json.NewEncoder(os.Stdout).Encode(location); err != nil {
		log.Fatal(fmt.Errorf("write location response: %w", err))
	}
}
