package remote

import (
	"testing"
)

func TestIsSubsequence(t *testing.T) {
	tests := []struct {
		name     string
		search   string
		target   string
		expected bool
	}{
		{
			name:     "exact match",
			search:   "gcloud",
			target:   "gcloud",
			expected: true,
		},
		{
			name:     "case insensitive match",
			search:   "DoCtl",
			target:   "doctl",
			expected: true,
		},
		{
			name:     "subsequence match with gaps",
			search:   "fbse",
			target:   "firebase",
			expected: true,
		},
		{
			name:     "subsequence match with special chars",
			search:   "bash",
			target:   "git-bash-windows",
			expected: true,
		},
		{
			name:     "not a subsequence (wrong order)",
			search:   "edoc",
			target:   "opencode",
			expected: false,
		},
		{
			name:     "not a subsequence (missing characters)",
			search:   "claudee",
			target:   "claude",
			expected: false,
		},
		{
			name:     "empty search string",
			search:   "",
			target:   "gcloud",
			expected: true,
		},
		{
			name:     "empty target string",
			search:   "doctl",
			target:   "",
			expected: false,
		},
		{
			name:     "both empty",
			search:   "",
			target:   "",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSubsequence(tt.search, tt.target)
			if result != tt.expected {
				t.Errorf("isSubsequence(%q, %q) = %v; want %v", tt.search, tt.target, result, tt.expected)
			}
		})
	}
}

func TestFuzzyCompare(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected bool
	}{
		{
			name:     "exact match",
			a:        "firebase",
			b:        "firebase",
			expected: true,
		},
		{
			name:     "a is shorter, a is subsequence of b",
			a:        "opcd",
			b:        "opencode",
			expected: true,
		},
		{
			name:     "b is shorter, b is subsequence of a",
			a:        "claude",
			b:        "cld",
			expected: true,
		},
		{
			name:     "case insensitivity",
			a:        "GCloud",
			b:        "gcloud",
			expected: true,
		},
		{
			name:     "a is shorter, not a subsequence",
			a:        "dcot",
			b:        "doctl",
			expected: false,
		},
		{
			name:     "b is shorter, not a subsequence",
			a:        "bash",
			b:        "hsb",
			expected: false,
		},
		{
			name:     "empty vs non-empty",
			a:        "",
			b:        "opencode",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fuzzyCompare(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("fuzzyCompare(%q, %q) = %v; want %v", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}
