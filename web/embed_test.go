package webassets

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func get(t *testing.T, handler http.Handler, target string) (*http.Response, string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	response := recorder.Result()
	t.Cleanup(func() { _ = response.Body.Close() })
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return response, string(body)
}

func TestHandlerWithoutBuildServesStub(t *testing.T) {
	handler := newHandler(fstest.MapFS{})

	response, body := get(t, handler, "/")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if got, want := response.Header.Get("Content-Type"), "text/html"; !strings.HasPrefix(got, want) {
		t.Errorf("Content-Type = %q, want prefix %q", got, want)
	}
	if !strings.Contains(body, "не собран") {
		t.Errorf("stub page does not explain the missing build:\n%s", body)
	}
	if !strings.Contains(body, "make build") {
		t.Error("stub page does not mention `make build`")
	}
}

func TestHandlerServesIndexAndAssets(t *testing.T) {
	handler := newHandler(fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>cublyverse</title>")},
		"assets/app.js": {Data: []byte("console.log('app')")},
	})

	tests := []struct {
		name       string
		path       string
		wantBody   string
		wantCache  string
		wantStatus int
	}{
		{
			name:       "root serves index",
			path:       "/",
			wantBody:   "cublyverse",
			wantCache:  "no-cache",
			wantStatus: http.StatusOK,
		},
		{
			name:       "fingerprinted asset is cached forever",
			path:       "/assets/app.js",
			wantBody:   "console.log",
			wantCache:  "public, max-age=31536000, immutable",
			wantStatus: http.StatusOK,
		},
		{
			name:       "client side route falls back to index",
			path:       "/reminders/42",
			wantBody:   "cublyverse",
			wantCache:  "no-cache",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, body := get(t, handler, tt.path)
			if response.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.StatusCode, tt.wantStatus)
			}
			if !strings.Contains(body, tt.wantBody) {
				t.Errorf("body %q does not contain %q", body, tt.wantBody)
			}
			if got := response.Header.Get("Cache-Control"); got != tt.wantCache {
				t.Errorf("Cache-Control = %q, want %q", got, tt.wantCache)
			}
		})
	}
}

func TestHandlerWithoutIndexServesStub(t *testing.T) {
	handler := newHandler(fstest.MapFS{
		"assets/app.js": {Data: []byte("console.log('app')")},
	})
	_, body := get(t, handler, "/assets/app.js")
	if !strings.Contains(body, "не собран") {
		t.Errorf("handler without index.html must serve the stub page, got:\n%s", body)
	}
}

func TestHandlerServesFonts(t *testing.T) {
	handler := newHandler(fstest.MapFS{
		"index.html":          {Data: []byte("<html></html>")},
		"fonts/onest.woff2":   {Data: []byte("wOF2")},
		"assets/index-abc.js": {Data: []byte("x")},
	})

	response, _ := get(t, handler, "/fonts/onest.woff2")
	if got, want := response.Header.Get("Content-Type"), "font/woff2"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
	if got := response.Header.Get("Cache-Control"); strings.Contains(got, "immutable") {
		t.Errorf("fonts are not fingerprinted, Cache-Control = %q", got)
	}

	response, _ = get(t, handler, "/assets/index-abc.js")
	if got := response.Header.Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Errorf("fingerprinted asset Cache-Control = %q, want immutable", got)
	}
}
