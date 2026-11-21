package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestETagMatches(t *testing.T) {
	tests := []struct {
		name   string
		header string
		etag   string
		want   bool
	}{
		{name: "exact", header: `"v3"`, etag: `"v3"`, want: true},
		{name: "different revision", header: `"v2"`, etag: `"v3"`, want: false},
		{name: "star matches anything", header: `*`, etag: `"v3"`, want: true},
		{name: "star with surrounding space", header: "  *  ", etag: `"v3"`, want: true},
		{name: "weak prefix is equivalent", header: `W/"v3"`, etag: `"v3"`, want: true},
		{name: "weak on both sides", header: `W/"v3"`, etag: `W/"v3"`, want: true},
		{name: "unquoted header value", header: `v3`, etag: `"v3"`, want: true},
		{
			name:   "list with a match last",
			header: `"v1", "v2", "v3"`,
			etag:   `"v3"`,
			want:   true,
		},
		{
			name:   "list without a match",
			header: `"v1", "v2"`,
			etag:   `"v3"`,
			want:   false,
		},
		{name: "empty header", header: ``, etag: `"v3"`, want: false},
		{name: "empty etag does not match a star-less header", header: `"v3"`, etag: ``, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := etagMatches(tt.header, tt.etag); got != tt.want {
				t.Errorf("etagMatches(%q, %q) = %v, want %v", tt.header, tt.etag, got, tt.want)
			}
		})
	}
}

func TestNormalizeETag(t *testing.T) {
	tests := []struct{ in, want string }{
		{`"v1"`, `v1`},
		{`W/"v1"`, `v1`},
		{`w/"v1"`, `v1`},
		{`  "v1"  `, `v1`},
		{`v1`, `v1`},
		{``, ``},
	}
	for _, tt := range tests {
		if got := normalizeETag(tt.in); got != tt.want {
			t.Errorf("normalizeETag(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPreconditionFailed(t *testing.T) {
	tests := []struct {
		name    string
		ifMatch string
		current string
		want    bool
	}{
		{name: "no header means no precondition", ifMatch: "", current: `"v3"`, want: false},
		{name: "matching tag", ifMatch: `"v3"`, current: `"v3"`, want: false},
		{name: "stale tag", ifMatch: `"v2"`, current: `"v3"`, want: true},
		{name: "star always passes", ifMatch: `*`, current: `"v3"`, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPatch, "/api/v1/items/x", nil)
			if tt.ifMatch != "" {
				req.Header.Set("If-Match", tt.ifMatch)
			}
			if got := preconditionFailed(req, tt.current); got != tt.want {
				t.Errorf("preconditionFailed(If-Match=%q, current=%s) = %v, want %v",
					tt.ifMatch, tt.current, got, tt.want)
			}
		})
	}
}
