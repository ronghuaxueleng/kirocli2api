package Utils

import "strings"

func ensureNonEmptyContent(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "-"
	}
	return raw
}
