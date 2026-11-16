// Package webassets serves the compiled WebApp.
//
// The WebApp is a Vite build in web/dist. The directory is git-ignored, so a
// tracked placeholder keeps the embed directive valid on a fresh checkout; in
// that case a clear stub page is served instead of failing to compile or
// panicking at runtime.
package webassets

import (
	"embed"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strings"
)

// distFS is the compiled WebApp. `all:` makes the embed directive include
// dotfiles such as the placeholder, which keeps `go build` working before the
// first `make build`.
//
//go:embed all:dist
var distFS embed.FS

func init() {
	// A minimal container has no mime.types, and Go's built-in table lacks
	// woff2; without it the font would be sniffed as octet-stream.
	_ = mime.AddExtensionType(".woff2", "font/woff2")
}

// indexFile is the entry point of the single page application.
const indexFile = "index.html"

// Handler returns the WebApp handler rooted at "/". Mount it at /app/ with
// http.StripPrefix("/app", webassets.Handler()).
//
// When no build is present the handler serves a stub page that explains how to
// build the WebApp.
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		slog.Warn("webapp assets are not embedded, serving stub page", slog.String("error", err.Error()))
		return stubHandler()
	}
	return newHandler(sub)
}

// newHandler builds the SPA handler for fsys. Tests use it with an in-memory
// filesystem.
func newHandler(fsys fs.FS) http.Handler {
	if _, err := fs.Stat(fsys, indexFile); err != nil {
		return stubHandler()
	}
	return &spaHandler{fsys: fsys, files: http.FileServer(http.FS(fsys))}
}

func stubHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(stubPage))
	})
}

// spaHandler serves files from the build directory and falls back to
// index.html so client side routes survive a page reload.
type spaHandler struct {
	fsys  fs.FS
	files http.Handler
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")

	if name != "" {
		if info, err := fs.Stat(h.fsys, name); err == nil && !info.IsDir() {
			switch {
			case name == indexFile:
				w.Header().Set("Cache-Control", "no-cache")
			case strings.HasPrefix(name, "fonts/"):
				// Copied from public/ as is, so the names are not fingerprinted.
				w.Header().Set("Cache-Control", "public, max-age=604800")
			default:
				// Vite fingerprints asset names, so they never change.
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			h.files.ServeHTTP(w, r)
			return
		}
	}

	h.serveIndex(w)
}

func (h *spaHandler) serveIndex(w http.ResponseWriter) {
	content, err := fs.ReadFile(h.fsys, indexFile)
	if err != nil {
		http.Error(w, "webapp index is missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

// stubPage is shown when the WebApp was not built, for example when the Go
// binary runs straight from a source checkout.
const stubPage = `<!doctype html>
<html lang="ru">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Cublyverse — WebApp не собран</title>
  <style>
    body { font-family: system-ui, sans-serif; margin: 0; padding: 2rem; line-height: 1.5; color: #1f2937; background: #fafafa; }
    main { max-width: 42rem; margin: 0 auto; }
    h1 { font-size: 1.5rem; }
    code, pre { background: #111827; color: #f9fafb; border-radius: .375rem; }
    code { padding: .1rem .35rem; }
    pre { padding: .75rem 1rem; overflow-x: auto; }
    a { color: #2563eb; }
  </style>
</head>
<body>
  <main>
    <h1>WebApp не собран</h1>
    <p>Сервер работает, но каталог <code>web/dist</code> отсутствует или пуст,
       поэтому показать приложение нельзя.</p>
    <p>Соберите проект целиком:</p>
    <pre>make build</pre>
    <p>Или запустите режим разработки WebApp:</p>
    <pre>cd web &amp;&amp; pnpm install &amp;&amp; pnpm dev</pre>
    <p>API работает: <a href="/api/v1/health">/api/v1/health</a>,
       спецификация: <a href="/api/openapi.yaml">/api/openapi.yaml</a>,
       документация: <a href="/api/docs">/api/docs</a>.</p>
  </main>
</body>
</html>
`
