// Command live25-buildings-snapshot reads every location returned for selected
// 25Live building codes using a locally stored user session. It never writes
// to 25Live.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"loriss-prototype/internal/live25"
)

func main() {
	var (
		account       string
		service       string
		baseURL       string
		buildingCodes string
		start         string
		pageSize      int
		timeout       time.Duration
	)
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		log.Fatal(err)
	}
	flag.StringVar(&account, "keychain-account", "", "macOS Keychain account containing the local 25Live session")
	flag.StringVar(&service, "keychain-service", live25.DefaultKeychainService, "macOS Keychain service name")
	flag.StringVar(&baseURL, "base-url", live25.DefaultBaseURL, "25Live JSON API base URL")
	flag.StringVar(&buildingCodes, "building-codes", "SN,FB", "comma-separated 25Live building codes to read")
	flag.StringVar(&start, "start", time.Now().In(newYork).Format("2006-01-02"), "availability start date in YYYY-MM-DD")
	flag.IntVar(&pageSize, "page-size", 100, "availability page size")
	flag.DurationVar(&timeout, "timeout", 30*time.Second, "HTTP request timeout")
	flag.Parse()

	if account == "" {
		fmt.Fprintln(os.Stderr, "-keychain-account is required")
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

	buildings, err := selectBuildings(context.Background(), reader, buildingCodes)
	if err != nil {
		log.Fatal(err)
	}
	snapshot, err := live25.SnapshotBuildings(context.Background(), reader, buildings, live25.RoomSnapshotRequest{
		Start: startAt, PageSize: pageSize,
	})
	if err != nil {
		log.Fatal(err)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(snapshot); err != nil {
		log.Fatal(fmt.Errorf("write building snapshot: %w", err))
	}
}

func selectBuildings(ctx context.Context, reader *live25.Client, rawCodes string) ([]live25.Building, error) {
	wanted := make(map[string]struct{})
	for _, code := range strings.Split(rawCodes, ",") {
		code = strings.ToUpper(strings.TrimSpace(code))
		if code != "" {
			wanted[code] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return nil, fmt.Errorf("at least one building code is required")
	}
	available, err := reader.Buildings(ctx)
	if err != nil {
		return nil, fmt.Errorf("list 25Live buildings: %w", err)
	}
	selected := make([]live25.Building, 0, len(wanted))
	for _, building := range available {
		code := strings.ToUpper(strings.TrimSpace(building.Code))
		if _, ok := wanted[code]; ok {
			selected = append(selected, building)
			delete(wanted, code)
		}
	}
	if len(wanted) > 0 {
		missing := make([]string, 0, len(wanted))
		for code := range wanted {
			missing = append(missing, code)
		}
		return nil, fmt.Errorf("25Live building codes not found: %s", strings.Join(missing, ", "))
	}
	return selected, nil
}
