// Package web serves the compiled React frontend from inside the Go binary.
//
// The build pipeline runs `pnpm --dir web build` and copies the result into
// internal/web/dist, which the go:embed directive below bakes into the
// executable. A release artifact is therefore one static binary with no runtime
// dependency on the frontend source tree — nothing to serve from disk, nothing
// to get out of sync.
//
// A fresh clone has no built frontend, so dist holds only a .gitkeep. That has
// to keep compiling, because go:embed fails at build time when the directory is
// missing, and it does: [Handler] notices the absent index.html and serves a
// placeholder page that says how to build the real thing.
package web

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"strings"

	"github.com/JerryBian/splash/internal/httpx"
)

// distFS is the frontend build output. The all: prefix includes files whose
// names begin with . or _, which keeps the committed .gitkeep part of the
// embed and therefore keeps `go build` working on a clean checkout.
//
//go:embed all:dist
var distFS embed.FS

// indexFile is the SPA entry point; its presence is what distinguishes a built
// frontend from an empty dist directory.
const indexFile = "index.html"

// Options configures [Handler].
type Options struct {
	// APIPrefixes are path prefixes that must never be answered with
	// index.html. Defaults to ["/api/"].
	//
	// The server also registers an explicit /api/ catch-all, so this is a
	// second line of defence for anyone who mounts the frontend handler on its
	// own mux.
	APIPrefixes []string
}

// Assets returns the embedded build rooted at the directory that contains
// index.html.
func Assets() (fs.FS, error) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, fmt.Errorf("web: read embedded assets: %w", err)
	}
	return sub, nil
}

// Built reports whether the binary contains a frontend build. main logs a hint
// when it does not, which saves a confusing "why is the UI a placeholder"
// moment on a fresh clone.
func Built() bool {
	assets, err := Assets()
	if err != nil {
		return false
	}
	_, err = fs.Stat(assets, indexFile)
	return err == nil
}

// Handler returns the SPA handler: real files when they exist, index.html for
// everything else so client-side routing works on a hard refresh.
//
// It never returns an error. A missing build is a supported state, not a
// failure, and is rendered as a page that explains itself.
func Handler(opts Options) http.Handler {
	prefixes := opts.APIPrefixes
	if len(prefixes) == 0 {
		prefixes = []string{"/api/"}
	}

	assets, err := Assets()
	if err != nil {
		return placeholderHandler(err.Error())
	}
	if _, err := fs.Stat(assets, indexFile); err != nil {
		return placeholderHandler("the frontend has not been built into this binary")
	}
	return spaHandler(assets, prefixes)
}

// spaHandler serves a built frontend out of assets. Split out from [Handler] so
// tests can drive it with an fstest.MapFS instead of depending on whether
// someone happened to run the frontend build in this working copy.
func spaHandler(assets fs.FS, prefixes []string) http.Handler {
	index, err := fs.ReadFile(assets, indexFile)
	if err != nil {
		return placeholderHandler("index.html is missing from the embedded frontend")
	}

	fileServer := http.FileServerFS(assets)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		if isAPIPath(path, prefixes) {
			httpx.WriteError(w, r, httpx.Errorf(http.StatusNotFound, httpx.CodeNotFound,
				"no API route for %s %s", r.Method, path))
			return
		}

		// Serve the file itself when there is one. Directories and unknown
		// paths fall through to index.html below.
		if name := strings.TrimPrefix(path, "/"); name != "" {
			if info, statErr := fs.Stat(assets, name); statErr == nil && !info.IsDir() {
				// Vite names built assets with a content hash, so they can be
				// cached forever. index.html must not be, or a deploy would be
				// invisible to returning clients.
				if strings.HasPrefix(path, "/assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				// Windows' MIME registry knows neither extension; serveContent
				// only guesses when the header is unset, so pin them here.
				switch {
				case strings.HasSuffix(path, ".webmanifest"):
					w.Header().Set("Content-Type", "application/manifest+json")
				case strings.HasSuffix(path, ".woff2"):
					w.Header().Set("Content-Type", "font/woff2")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		header := w.Header()
		header.Set("Content-Type", httpx.ContentTypeHTML)
		header.Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(index)
	})
}

// isAPIPath reports whether path belongs to the API rather than the frontend.
// Both /api and /api/... count, so a request for the bare prefix is not turned
// into an HTML page either.
func isAPIPath(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		trimmed := strings.TrimSuffix(prefix, "/")
		if path == trimmed || strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// placeholderHandler answers with a page that explains the frontend is not in
// this binary and how to build it. It exists so `go run ./cmd/server` on a
// fresh clone gives a useful answer instead of a blank 404.
func placeholderHandler(reason string) http.Handler {
	body := []byte(strings.ReplaceAll(placeholderHTML, "{{REASON}}", reason))
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		httpx.Write(w, http.StatusOK, httpx.ContentTypeHTML, body)
	})
}

const placeholderHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>水花打卡 — 前端未构建</title>
<style>
  :root { color-scheme: light dark; --fg: #16181d; --bg: #fbfbfd; --mut: #5c6373; --acc: #2f5bea; --bd: #e2e4ea; }
  @media (prefers-color-scheme: dark) {
    :root { --fg: #e8eaf0; --bg: #101218; --mut: #99a1b3; --acc: #7aa2ff; --bd: #232733; }
  }
  * { box-sizing: border-box; }
  body { margin: 0; background: var(--bg); color: var(--fg);
         font: 16px/1.6 ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif; }
  main { max-width: 44rem; margin: 0 auto; padding: 5rem 1.25rem; }
  h1 { font-size: 1.6rem; letter-spacing: -0.01em; margin: 0 0 .5rem; }
  p { color: var(--mut); margin: 0 0 1.5rem; }
  code, pre { font-family: ui-monospace, SFMono-Regular, "Cascadia Mono", Menlo, monospace; font-size: .875rem; }
  pre { background: color-mix(in srgb, var(--fg) 5%, var(--bg)); border: 1px solid var(--bd);
        border-radius: 10px; padding: 1rem; overflow-x: auto; color: var(--fg); }
  ul { color: var(--mut); padding-left: 1.2rem; margin: 0 0 1.5rem; }
  a { color: var(--acc); text-decoration: none; border-bottom: 1px solid color-mix(in srgb, var(--acc) 35%, transparent); }
  a:hover { border-bottom-color: var(--acc); }
  .tag { display: inline-block; font-size: .75rem; text-transform: uppercase; letter-spacing: .08em;
         color: var(--acc); border: 1px solid color-mix(in srgb, var(--acc) 30%, transparent);
         border-radius: 999px; padding: .15rem .6rem; margin-bottom: 1rem; }
</style>
</head>
<body>
<main>
  <span class="tag">Go 服务正在运行</span>
  <h1>这个二进制里还没有前端</h1>
  <p>{{REASON}}。构建前端，或启动带热重载的 Vite 开发服务：</p>
  <pre>pnpm --dir web install
pnpm --dir web dev      # http://localhost:5173，/api 代理到 :8080</pre>
  <p>把前端编译进二进制：</p>
  <pre>node scripts/build.mjs  # 或 make build；Windows 一键：pwsh -File scripts/run.ps1</pre>
  <p>无论如何，API 都已可用：</p>
  <ul>
    <li><a href="/api/v1/health">/api/v1/health</a></li>
    <li><a href="/api/v1/version">/api/v1/version</a></li>
    <li><a href="/api/v1/projects">/api/v1/projects</a></li>
  </ul>
</main>
</body>
</html>
`
