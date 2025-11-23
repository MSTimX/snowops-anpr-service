package utils

import (
	"strings"
)

func NormalizePlate(raw string) string {
	normalized := strings.TrimSpace(raw)
	normalized = strings.ReplaceAll(normalized, " ", "")
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ToUpper(normalized)
	return normalized
}

