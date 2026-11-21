package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// builtAssets stands in for a real Vite build so the SPA handler can be tested
// without anyone having run pnpm in this working copy.
func builtAssets() fstest.MapFS {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(`<!doctype html><html><body><div id="root"></div></body></html>`),
		},
		"assets/app-ABC123.js": &fstest.MapFile{
			Data: []byte(`console.log("bundle")`),
		},
		"assets/app-ABC123.css": &fstest.MapFile{
			Data: []byte(`body{margin:0}`),
		},
		"favicon.svg": &fstest.MapFile{
			Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`),
		},
		".gitkeep": &fstest.MapFile{
			Data: []byte("# placeholder\n"),
		},
	}
}

func TestSpaHandlerServesTheIndexAtTheRoot(t *testing.T) {
	handler := spaHandler(builtAssets(), []string{"/api/"})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `<div id="root">`) {
		t.Errorf("body = %s", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Errorf("Content-Type = %q", got)
	}
}

func TestSpaHandlerFallsBackToTheIndexForClientRoutes(t *testing.T) {
	handler := spaHandler(builtAssets(), []string{"/api/"})

	// A hard refresh on a client-side route must return the app shell, not a
	// 404, or the router never gets a chance to run.
	for _, path := range []string{"/items", "/items/123", "/settings/deep/route"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), `<div id="root">`) {
				t.Errorf("body = %s, want the app shell", rec.Body.String())
			}
			// The shell must be revalidated every time, or a deploy is invisible
			// to returning clients until their cache expires.
			if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
				t.Errorf("Cache-Control = %q, want no-cache", got)
			}
		})
	}
}

func TestSpaHandlerServesRealFiles(t *testing.T) {
	handler := spaHandler(builtAssets(), []string{"/api/"})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/favicon.svg", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<svg") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestSpaHandlerCachesHashedAssetsForever(t *testing.T) {
	handler := spaHandler(builtAssets(), []string{"/api/"})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app-ABC123.js", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "bundle") {
		t.Errorf("body = %s, want the real bundle", rec.Body.String())
	}
	// Vite's filenames carry a content hash, so a changed build produces a new
	// URL and an immutable cache is safe.
	want := "public, max-age=31536000, immutable"
	if got := rec.Header().Get("Cache-Control"); got != want {
		t.Errorf("Cache-Control = %q, want %q", got, want)
	}
}

func TestSpaHandlerRejectsAPIPaths(t *testing.T) {
	handler := spaHandler(builtAssets(), []string{"/api/"})

	for _, path := range []string{"/api/v1/unknown", "/api/v1/items/123", "/api"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", rec.Code)
			}
			// Serving index.html for an API path would make a client parse HTML
			// as JSON and report a baffling error.
			if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
				t.Errorf("Content-Type = %q, want JSON", got)
			}
			if !strings.Contains(rec.Body.String(), "not_found") {
				t.Errorf("body = %s", rec.Body.String())
			}
		})
	}
}

func TestIsAPIPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/api/v1/items", true},
		{"/api/", true},
		{"/api", true},
		{"/apiv1/items", false}, // a prefix match must respect the separator
		{"/", false},
		{"/assets/app.js", false},
		{"/apiary", false},
	}
	for _, tt := range tests {
		if got := isAPIPath(tt.path, []string{"/api/"}); got != tt.want {
			t.Errorf("isAPIPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestHandlerServesAPlaceholderWhenNothingIsEmbedded(t *testing.T) {
	if Built() {
		t.Skip("a frontend build is embedded in this working copy; the placeholder path is covered by TestPlaceholderHandler")
	}

	rec := httptest.NewRecorder()
	Handler(Options{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 so a fresh clone still gets an explanation", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"这个二进制里还没有前端", "pnpm --dir web", "scripts/build.mjs", "/api/v1/health"} {
		if !strings.Contains(body, want) {
			t.Errorf("the placeholder does not mention %q", want)
		}
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store so the placeholder is never cached", got)
	}
}

func TestPlaceholderHandlerInterpolatesTheReason(t *testing.T) {
	rec := httptest.NewRecorder()
	placeholderHandler("the build exploded").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if !strings.Contains(rec.Body.String(), "the build exploded") {
		t.Errorf("body = %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "{{REASON}}") {
		t.Error("the placeholder template was not substituted")
	}
}

func TestPlaceholderContainsNoInlineScript(t *testing.T) {
	// The server sends Content-Security-Policy: script-src 'self', so an inline
	// script in the placeholder would be blocked and the page would look broken
	// for a reason that has nothing to do with the missing frontend.
	if strings.Contains(placeholderHTML, "<script") {
		t.Error("the placeholder must not contain an inline script; the CSP blocks it")
	}
}

func TestSpaHandlerWithoutAnIndexServesThePlaceholder(t *testing.T) {
	empty := fstest.MapFS{
		".gitkeep": &fstest.MapFile{Data: []byte("")},
	}

	rec := httptest.NewRecorder()
	spaHandler(empty, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "index.html is missing") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestAssetsAndBuiltAgree(t *testing.T) {
	assets, err := Assets()
	if err != nil {
		t.Fatalf("Assets: %v", err)
	}
	_, statErr := fs.Stat(assets, indexFile)
	if Built() != (statErr == nil) {
		t.Errorf("Built() = %v but index.html stat returned %v", Built(), statErr)
	}
}

func TestAssetsExposesTheEmbeddedTree(t *testing.T) {
	assets, err := Assets()
	if err != nil {
		t.Fatalf("Assets: %v", err)
	}

	// The committed .gitkeep is what makes `go:embed all:dist` succeed on a
	// fresh clone; if it stops being embedded, the build breaks for everyone
	// who has not run the frontend build.
	if _, err := fs.Stat(assets, ".gitkeep"); err != nil {
		t.Errorf(".gitkeep is not embedded: %v", err)
	}
}

func TestSPAHandlerPinsManifestAndFontMIME(t *testing.T) {
	// Windows' MIME registry knows neither extension; the handler must pin
	// them so PWA install and font loading behave the same on every host OS.
	assets := fstest.MapFS{
		"index.html":           {Data: []byte(`<div id="root"></div>`)},
		"manifest.webmanifest": {Data: []byte(`{}`)},
		"fonts/x.woff2":        {Data: []byte("w")},
	}
	h := spaHandler(assets, []string{"/api/"})
	for _, tt := range []struct{ path, want string }{
		{"/manifest.webmanifest", "application/manifest+json"},
		{"/fonts/x.woff2", "font/woff2"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
		if got := rec.Header().Get("Content-Type"); got != tt.want {
			t.Errorf("%s Content-Type = %q, want %q", tt.path, got, tt.want)
		}
	}
}
