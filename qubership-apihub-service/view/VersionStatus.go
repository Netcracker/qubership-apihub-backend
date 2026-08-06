package view

import "fmt"

type VersionStatus string

const (
	Draft    VersionStatus = "draft"
	Release  VersionStatus = "release"
	Archived VersionStatus = "archived"
)

func (v VersionStatus) String() string {
	switch v {
	case Draft:
		return "draft"
	case Release:
		return "release"
	case Archived:
		return "archived"
	default:
		return ""
	}
}

func ParseVersionStatus(s string) (VersionStatus, error) {
	switch s {
	case "draft":
		return Draft, nil
	case "release":
		return Release, nil
	case "archived":
		return Archived, nil
	}
	return "", fmt.Errorf("unknown version status: %v", s)
}

// ParseVersionStatuses validates each comma-separated status value from a query parameter list.
// An empty list means no status filter (all statuses).
func ParseVersionStatuses(parts []string) ([]string, error) {
	if len(parts) == 0 {
		return nil, nil
	}
	statuses := make([]string, 0, len(parts))
	for _, part := range parts {
		if _, err := ParseVersionStatus(part); err != nil {
			return nil, err
		}
		statuses = append(statuses, part)
	}
	return statuses, nil
}
