package httpapi

import "net/http"

// docsPage renders the OpenAPI document with Swagger UI loaded from a CDN. The
// page is deliberately static: it only points at /api/openapi.yaml.
const docsPage = `<!doctype html>
<html lang="ru">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Cublyverse API</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
  <style>
    body { margin: 0; background: #fafafa; }
    .cv-fallback { font-family: system-ui, sans-serif; padding: 2rem; }
  </style>
</head>
<body>
  <div id="swagger-ui">
    <div class="cv-fallback">
      <p>Загрузка документации…</p>
      <p>Если страница не появилась, откройте спецификацию напрямую:
        <a href="/api/openapi.yaml">/api/openapi.yaml</a>.</p>
    </div>
  </div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js" crossorigin></script>
  <script>
    window.addEventListener("load", function () {
      if (!window.SwaggerUIBundle) { return; }
      window.ui = window.SwaggerUIBundle({
        url: "/api/openapi.yaml",
        dom_id: "#swagger-ui",
        deepLinking: true,
        displayRequestDuration: true,
        presets: [window.SwaggerUIBundle.presets.apis]
      });
    });
  </script>
</body>
</html>
`

// openapi serves the embedded specification. It is byte-for-byte the file the
// generated server and the TypeScript client were built from.
func (h *handlers) openapi(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(h.deps.Spec)
}

// docs serves the human readable API reference.
func (h *handlers) docs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(docsPage))
}
