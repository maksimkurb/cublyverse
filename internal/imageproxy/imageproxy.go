// Package imageproxy serves the product photos of catalogue items through the
// server, so a viewer's device never talks to the shops and manufacturers.
//
// It is not an open proxy: it only fetches addresses the server has signed. A
// signed address may still come from a user (the photo link of an own item), so
// the proxy connects to public internet addresses only and answers with raster
// images only.
package imageproxy

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"syscall"
	"time"

	"github.com/maksimkurb/cublyverse/internal/store/db"
)

// Path is where the application mounts the proxy.
const Path = "/api/v1/images"

const (
	// userAgent makes the request look like a browser's: some sites refuse
	// pictures to anything else.
	userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"
	// maxBytes bounds one picture.
	maxBytes = 8 << 20
	// maxParallel bounds the pictures fetched at once, so a long list does not
	// open dozens of connections to one site.
	maxParallel  = 6
	fetchTimeout = 20 * time.Second
	keyName      = "image-proxy"
)

// allowedTypes are the formats passed on. SVG is left out: it can run scripts.
var allowedTypes = map[string]bool{
	"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true,
}

// reserved are ranges that net/netip does not call private but that are not
// public destinations either.
var reserved = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2002::/16"),
}

// Proxy signs picture addresses and serves them.
type Proxy struct {
	key    []byte
	logger *slog.Logger
	client *http.Client
	slots  chan struct{}
}

// New returns a proxy that signs with key. A nil logger becomes slog.Default().
func New(key []byte, logger *slog.Logger) *Proxy {
	return newProxy(key, logger, publicOnly)
}

// LoadKey returns the signing key kept in the database, creating it first.
// Signed addresses then stay valid, and cached by browsers, across restarts.
func LoadKey(ctx context.Context, q *db.Queries) ([]byte, error) {
	fresh := make([]byte, 32)
	if _, err := rand.Read(fresh); err != nil {
		return nil, fmt.Errorf("imageproxy: generate key: %w", err)
	}
	if err := q.EnsureAppKey(ctx, db.EnsureAppKeyParams{Name: keyName, Value: fresh}); err != nil {
		return nil, fmt.Errorf("imageproxy: store key: %w", err)
	}
	key, err := q.GetAppKey(ctx, keyName)
	if err != nil {
		return nil, fmt.Errorf("imageproxy: read key: %w", err)
	}
	return key, nil
}

func newProxy(key []byte, logger *slog.Logger, control func(network, address string, c syscall.RawConn) error) *Proxy {
	if logger == nil {
		logger = slog.Default()
	}
	// The check runs on the resolved address of every connection, redirects
	// included, so a name that points inside the network is refused too. An
	// HTTP proxy from the environment would bypass it and is not used.
	dialer := &net.Dialer{Timeout: 10 * time.Second, Control: control}
	return &Proxy{
		key:    key,
		logger: logger,
		slots:  make(chan struct{}, maxParallel),
		client: &http.Client{
			Timeout: fetchTimeout,
			Transport: &http.Transport{
				DialContext: dialer.DialContext, ForceAttemptHTTP2: true,
				MaxIdleConnsPerHost: maxParallel, IdleConnTimeout: 90 * time.Second,
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 || !webAddress(req.URL) {
					return errors.New("redirect refused")
				}
				return nil
			},
		},
	}
}

// publicOnly refuses connections to anything but a public address on a web
// port.
func publicOnly(_, address string, _ syscall.RawConn) error {
	target, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("address %q: %w", address, err)
	}
	if target.Port() != 80 && target.Port() != 443 {
		return fmt.Errorf("port %d is not allowed", target.Port())
	}
	if !public(target.Addr()) {
		return fmt.Errorf("address %s is not public", target.Addr())
	}
	return nil
}

func public(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return false
	}
	for _, prefix := range reserved {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

func webAddress(u *url.URL) bool {
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil
}

// URL returns the proxied address of a picture, or "" for an address the proxy
// would not fetch.
func (p *Proxy) URL(raw string) string {
	parsed, err := url.Parse(raw)
	if raw == "" || err != nil || !webAddress(parsed) {
		return ""
	}
	return Path + "?" + url.Values{
		"u": {base64.RawURLEncoding.EncodeToString([]byte(raw))},
		"s": {base64.RawURLEncoding.EncodeToString(p.sign(raw))},
	}.Encode()
}

func (p *Proxy) sign(raw string) []byte {
	mac := hmac.New(sha256.New, p.key)
	mac.Write([]byte(raw))
	return mac.Sum(nil)
}

// ServeHTTP answers with the picture of a signed address. It needs no session:
// an <img> cannot send the Mini App's token, and the signature already proves
// the server handed the address to someone allowed to see it.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, errURL := base64.RawURLEncoding.DecodeString(r.URL.Query().Get("u"))
	signature, errSig := base64.RawURLEncoding.DecodeString(r.URL.Query().Get("s"))
	if errURL != nil || errSig != nil || !hmac.Equal(signature, p.sign(string(raw))) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	select {
	case p.slots <- struct{}{}:
		defer func() { <-p.slots }()
	case <-r.Context().Done():
		return
	}
	body, contentType, err := p.fetch(r.Context(), string(raw))
	if err != nil {
		if r.Context().Err() == nil {
			p.logger.WarnContext(r.Context(), "image proxy failed", slog.String("error", err.Error()))
		}
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "picture unavailable", http.StatusBadGateway)
		return
	}
	header := w.Header()
	header.Set("Content-Type", contentType)
	header.Set("Content-Length", strconv.Itoa(len(body)))
	header.Set("Cache-Control", "private, max-age=604800")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	_, _ = w.Write(body)
}

func (p *Proxy) fetch(ctx context.Context, raw string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("fetch %s: status %s", raw, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("read %s: %w", raw, err)
	}
	if len(body) > maxBytes {
		return nil, "", fmt.Errorf("fetch %s: larger than %d bytes", raw, maxBytes)
	}
	// The type comes from the content, never from the site's header.
	contentType := http.DetectContentType(body)
	if !allowedTypes[contentType] {
		return nil, "", fmt.Errorf("fetch %s: not a picture (%s)", raw, contentType)
	}
	return body, contentType, nil
}
