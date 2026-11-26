package imageproxy

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

var png = "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 32)

func serve(p *Proxy, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestProxyServesSignedPicturesOnly(t *testing.T) {
	var agent string
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent = r.UserAgent()
		if r.URL.Path == "/page" {
			w.Header().Set("Content-Type", "image/png")
			_, _ = io.WriteString(w, "<html><script>alert(1)</script></html>")
			return
		}
		if r.URL.Path == "/svg" {
			_, _ = io.WriteString(w, `<svg xmlns="http://www.w3.org/2000/svg"/>`)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, png)
	}))
	defer site.Close()
	// The test site is on a loopback address, which New refuses.
	p := newProxy([]byte("test key"), quiet(), nil)

	got := serve(p, p.URL(site.URL+"/photo.png"))
	if got.Code != http.StatusOK || got.Body.String() != png || got.Header().Get("Content-Type") != "image/png" {
		t.Errorf("picture = %d %q", got.Code, got.Header().Get("Content-Type"))
	}
	if !strings.Contains(agent, "Chrome/") {
		t.Errorf("User-Agent = %q, want a browser's", agent)
	}
	if got.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("nosniff is missing")
	}
	for _, path := range []string{"/page", "/svg"} {
		if got := serve(p, p.URL(site.URL+path)); got.Code != http.StatusBadGateway {
			t.Errorf("%s = %d, want it refused", path, got.Code)
		}
	}

	signed := p.URL(site.URL + "/photo.png")
	other := newProxy([]byte("another key"), quiet(), nil).URL(site.URL + "/photo.png")
	for name, target := range map[string]string{
		"no signature": Path + "?u=aHR0cDovL2V4YW1wbGUuY29t", "foreign key": other, "changed address": strings.Replace(signed, "u=a", "u=b", 1),
	} {
		if got := serve(p, target); got.Code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", name, got.Code)
		}
	}

	// A validly signed address inside the network is not fetched.
	agent = ""
	if got := serve(New([]byte("test key"), quiet()), signed); got.Code != http.StatusBadGateway || agent != "" {
		t.Errorf("loopback = %d, request sent: %v", got.Code, agent != "")
	}
	for _, raw := range []string{"", "file:///etc/passwd", "ftp://example.com/a.png", "https://user:pass@example.com/a.png", "/relative.png"} {
		if got := p.URL(raw); got != "" {
			t.Errorf("URL(%q) = %q, want nothing", raw, got)
		}
	}
}

func TestOnlyPublicAddresses(t *testing.T) {
	for address, want := range map[string]bool{
		"93.184.216.34": true, "2606:2800:220:1::1": true,
		"127.0.0.1": false, "10.0.0.1": false, "172.16.5.1": false, "192.168.55.1": false,
		"169.254.169.254": false, "100.64.0.1": false, "0.0.0.0": false, "224.0.0.1": false,
		"::1": false, "fc00::1": false, "fe80::1": false, "::ffff:127.0.0.1": false, "64:ff9b::a00:1": false,
	} {
		if got := public(netip.MustParseAddr(address)); got != want {
			t.Errorf("public(%s) = %v, want %v", address, got, want)
		}
	}
	if err := publicOnly("tcp", "93.184.216.34:5432", nil); err == nil {
		t.Error("a database port must be refused")
	}
}
