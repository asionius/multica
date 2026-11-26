package handler

import (
	"net/http/httptest"
	"testing"
)

func TestParseBoundedPagination(t *testing.T) {
	cases := []struct {
		name      string
		query     string
		wantLimit int32
		wantOff   int32
	}{
		{"defaults", "", 20, 0},
		{"explicit", "?limit=5&offset=10", 5, 10},
		{"clamps above max", "?limit=1000", 100, 0},
		{"ignores non numeric", "?limit=abc&offset=-3", 20, 0},
		{"zero falls back", "?limit=0", 20, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/dev-reports"+tc.query, nil)
			limit, offset := parseBoundedPagination(req, 20, 100)
			if limit != tc.wantLimit || offset != tc.wantOff {
				t.Fatalf("got limit=%d offset=%d, want limit=%d offset=%d", limit, offset, tc.wantLimit, tc.wantOff)
			}
		})
	}
}

func TestSanitizeFilenamePart(t *testing.T) {
	cases := map[string]string{
		"3.34.0":           "3.34.0",
		"3.34.0 rc/1":      "3.34.0-rc-1",
		"../../etc/passwd": "etc-passwd",
		"":                 "report",
		"版本 3.34":          "3.34",
	}
	for input, want := range cases {
		if got := sanitizeFilenamePart(input); got != want {
			t.Fatalf("sanitizeFilenamePart(%q)=%q, want %q", input, got, want)
		}
	}
}
