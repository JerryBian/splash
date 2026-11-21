package api

import (
	"net/http"
	"strings"
)

// etagMatches reports whether an If-Match or If-None-Match header value selects
// the given entity tag.
//
// It implements the subset of RFC 9110 §13 that a JSON API actually needs:
// the "*" wildcard, comma-separated lists, optional quotes and the weak "W/"
// prefix. Comparison is by opaque tag, so a weak prefix does not affect the
// result — these tags identify revisions, not byte-for-byte representations.
func etagMatches(headerValue, etag string) bool {
	if strings.TrimSpace(headerValue) == "*" {
		return true
	}
	for _, candidate := range strings.Split(headerValue, ",") {
		if normalizeETag(strings.TrimSpace(candidate)) == normalizeETag(etag) {
			return true
		}
	}
	return false
}

func normalizeETag(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "W/")
	s = strings.TrimPrefix(s, "w/")
	return strings.Trim(s, `"`)
}

// preconditionFailed reports whether the request carried an If-Match header that
// the current entity tag does not satisfy. A request without If-Match always
// passes: conditional writes are opt-in, so a client that does not know about
// them is not punished.
func preconditionFailed(r *http.Request, currentETag string) bool {
	ifMatch := r.Header.Get("If-Match")
	if ifMatch == "" {
		return false
	}
	return !etagMatches(ifMatch, currentETag)
}
