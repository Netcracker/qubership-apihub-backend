package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func main() {
	baseRaw := os.Getenv("APIHUB_URL")
	token := os.Getenv("APIHUB_API_KEY")
	migrationID := os.Getenv("MIGRATION_ID")

	if baseRaw == "" || token == "" || migrationID == "" {
		die("APIHUB_URL, APIHUB_API_KEY, and MIGRATION_ID must be set")
	}
	if strings.ContainsAny(token, "\r\n") {
		die("API key contains a newline")
	}
	for _, c := range baseRaw {
		if c < 32 {
			die("APIHUB_URL contains a control character")
		}
	}

	parsed, err := url.Parse(strings.TrimRight(baseRaw, "/"))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		die("use an absolute HTTP(S) base URL without credentials, a query, or a fragment")
	}

	if !isUUID(migrationID) {
		die("MIGRATION_ID is not a valid UUID")
	}

	endpoint := parsed.String() + "/api/internal/migrate/operations/" + url.PathEscape(migrationID) + "?includeBuildSamples=true"

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       60 * time.Second,
	}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		die("cannot build request: " + err.Error())
	}
	req.Header.Set("X-API-Key", token)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	token = ""
	_ = os.Unsetenv("APIHUB_API_KEY")
	if err != nil {
		die("cannot retrieve migration report: " + err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "migration request returned HTTP %d; check the base URL and access\n", resp.StatusCode)
		os.Exit(1)
	}

	var report map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		die("response is not valid JSON: " + err.Error())
	}
	if report["status"] == nil {
		die("response is not a migration report: status is missing")
	}

	out := map[string]any{
		"collectedAt": time.Now().UTC().Format(time.RFC3339),
		"report":      report,
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		die("cannot encode output: " + err.Error())
	}
}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}

func die(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
