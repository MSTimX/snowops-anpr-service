package utils

import (
	"testing"
)

func TestNormalizePlate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "with spaces",
			input:    "123 ABC 02",
			expected: "123ABC02",
		},
		{
			name:     "lowercase",
			input:    "123abc02",
			expected: "123ABC02",
		},
		{
			name:     "with dashes",
			input:    "123-ABC-02",
			expected: "123ABC02",
		},
		{
			name:     "mixed case with spaces",
			input:    "123 AbC 02",
			expected: "123ABC02",
		},
		{
			name:     "already normalized",
			input:    "123ABC02",
			expected: "123ABC02",
		},
		{
			name:     "with leading/trailing spaces",
			input:    "  123 ABC 02  ",
			expected: "123ABC02",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizePlate(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizePlate(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

