package apiutil

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"time"
)

const (
	migrationKeyEnv = "APIHUB_API_KEY"
	readOnlyKeyEnv  = "APIHUB_READ_ONLY_API_KEY"
	requestTimeout  = 240 * time.Second
)

var migrationReadPath = regexp.MustCompile(`^/api/internal/migrate/operations/[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}(/suspiciousBuilds)?$`)

// Get uses the migration key only for the two migration diagnostics endpoints.
// All other paths require a separate read-only key; requests never follow redirects.
func Get(endpoint string) (*http.Response, error) {
	migrationKey := os.Getenv(migrationKeyEnv)
	readOnlyKey := os.Getenv(readOnlyKeyEnv)
	for _, name := range []string{migrationKeyEnv, readOnlyKeyEnv} {
		if err := os.Unsetenv(name); err != nil {
			return nil, fmt.Errorf("cannot clear %s: %w", name, err)
		}
	}
	return get(os.Getenv("APIHUB_URL"), endpoint, migrationKey, readOnlyKey)
}

func get(baseRaw, endpoint, migrationKey, readOnlyKey string) (*http.Response, error) {
	base, err := url.Parse(strings.TrimRight(baseRaw, "/"))
	if err != nil || base == nil || (base.Scheme != "http" && base.Scheme != "https") ||
		base.Hostname() == "" || base.User != nil || base.RawQuery != "" || base.ForceQuery || base.Fragment != "" ||
		(base.Path != "" && !canonicalPath(base)) {
		return nil, fmt.Errorf("APIHUB_URL must be an absolute HTTP(S) URL without credentials, a query, a fragment, or path traversal")
	}
	relative, err := url.Parse(endpoint)
	if err != nil || relative == nil || relative.IsAbs() || relative.Host != "" || relative.User != nil ||
		relative.Fragment != "" || !strings.HasPrefix(relative.Path, "/api/") || !canonicalPath(relative) {
		return nil, fmt.Errorf("endpoint must be a canonical /api/ path with optional query parameters, without a host or fragment")
	}

	keyName, key := readOnlyKeyEnv, readOnlyKey
	if migrationReadPath.MatchString(relative.Path) {
		keyName, key = migrationKeyEnv, migrationKey
	} else if key != "" && key == migrationKey {
		return nil, fmt.Errorf("%s must differ from %s", readOnlyKeyEnv, migrationKeyEnv)
	}
	if key == "" {
		return nil, fmt.Errorf("%s must be set for this endpoint", keyName)
	}
	if strings.ContainsAny(key, "\r\n") {
		return nil, fmt.Errorf("%s contains a newline", keyName)
	}

	req, err := http.NewRequest(http.MethodGet, base.String()+relative.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("cannot build APIHub request: %w", err)
	}
	req.Header.Set("api-key", key)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       requestTimeout,
	}
	return client.Do(req)
}

func canonicalPath(u *url.URL) bool {
	return u.RawPath == "" && path.Clean(u.Path) == u.Path && !strings.ContainsAny(u.Path, "\\%")
}
