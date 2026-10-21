// Package web serves the embedded Svelte SPA build.
package web

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"mbyte.ro/sofalog/httpx"
)

// The Vite production build lands in dist/. A committed stub index.html and
// .gitkeep keep the embed compiling on a fresh clone (build output itself is
// gitignored), and vite.config.js must set emptyOutDir: false so builds do
// not delete them.
//
//go:embed all:dist
var distFS embed.FS

// baseHref matches the <base href="..."> tag the SPA's index.html ships with.
var baseHref = regexp.MustCompile(`<base href="[^"]*"`)

// Handler serves the SPA. It is registered at "/" and therefore also sees
// unmatched /api/ paths — those get a JSON 404, never index.html, so API
// clients are not handed HTML to parse.
//
// basePath is the URL path the app is served under (config.BasePath()). Every
// URL in the bundle is relative, so index.html's <base href> is rewritten to it
// once here: otherwise those URLs resolve against the current route and a
// two-segment path like /video/history asks for /video/assets/... and
// /video/api/v1/... . Rewriting at startup — rather than baking a path in at
// build time — is what lets one binary serve any sub-path (BASE_URL is the only
// source of truth for it).
func Handler(basePath string) http.Handler {
	dist, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err) // impossible: "dist" is embedded above
	}
	fileServer := http.FileServerFS(dist)
	shell := indexShell(dist, basePath)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			httpx.WriteError(w, http.StatusNotFound, "not_found", "no such endpoint")
			return
		}

		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" || name == "index.html" {
			serveShell(w, r, shell)
			return
		}

		if !fs.ValidPath(name) {
			http.NotFound(w, r)
			return
		}

		if info, err := fs.Stat(dist, name); err == nil && !info.IsDir() {
			// Vite emits content-hashed filenames under assets/, safe to
			// cache forever; everything else stays revalidated.
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: an unknown extensionless path is a client-side route;
		// serve the shell so a refresh deep in the app still works.
		if path.Ext(name) == "" {
			serveShell(w, r, shell)
			return
		}

		http.NotFound(w, r)
	})
}

// indexShell reads index.html once and points its <base href> at basePath.
// The tag is inserted if the build did not ship one, so deep-route asset
// resolution cannot silently break by someone editing frontend/index.html.
func indexShell(dist fs.FS, basePath string) []byte {
	raw, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		panic(err) // impossible: a stub index.html is committed
	}
	tag := []byte(`<base href="` + basePath + `"`)
	if baseHref.Match(raw) {
		return baseHref.ReplaceAll(raw, tag)
	}
	if i := bytes.Index(raw, []byte("<head>")); i >= 0 {
		i += len("<head>")
		return append(append(append([]byte{}, raw[:i]...), append(tag, " />"...)...), raw[i:]...)
	}
	return raw
}

// startTime dates the shell so ServeContent can answer conditional requests;
// the bytes are fixed for the process lifetime.
var startTime = time.Now()

func serveShell(w http.ResponseWriter, r *http.Request, shell []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "index.html", startTime, bytes.NewReader(shell))
}
