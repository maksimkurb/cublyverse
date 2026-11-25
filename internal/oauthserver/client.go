package oauthserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/maksimkurb/cublyverse/internal/store/db"
)

const (
	maxRedirectURIs  = 10
	maxMetadataBytes = 32 << 10
	fetchTimeout     = 5 * time.Second
	// dcrBurst and dcrRefill bound unauthenticated registrations per remote
	// address.
	dcrBurst  = 10
	dcrRefill = 6 * time.Second
)

var customScheme = regexp.MustCompile(`^[a-z][a-z0-9+.\-]*$`)

// blockedSchemes can never be a redirect target: they would execute or leak
// code in the browser.
var blockedSchemes = map[string]bool{
	"javascript": true, "data": true, "file": true, "vbscript": true, "about": true, "blob": true,
	"http": false, "https": false,
}

// Client is a registered OAuth client.
type Client struct {
	ID           string
	Name         string
	RedirectURIs []string
	CIMD         bool
}

type bucket struct {
	tokens float64
	last   time.Time
}

// validateRedirectURI accepts https URLs, http URLs on a loopback host (native
// apps listen on a random local port) and private-use custom schemes
// (cursor://, vscode://). Fragments are forbidden by RFC 6749.
func validateRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Fragment != "" || u.User != nil || len(raw) > 2048 {
		return errors.New("некорректный redirect_uri")
	}
	switch u.Scheme {
	case "https":
		if u.Host == "" {
			return errors.New("redirect_uri без хоста")
		}
	case "http":
		if !isLoopbackHost(u.Hostname()) {
			return errors.New("http разрешён только для localhost")
		}
	default:
		if blockedSchemes[u.Scheme] || !customScheme.MatchString(u.Scheme) {
			return fmt.Errorf("схема %q недопустима", u.Scheme)
		}
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}

// redirectMatches compares a requested redirect URI with the registered ones.
// The match is exact, except that the port of a loopback http URI is ignored,
// as RFC 8252 §7.3 requires for native apps.
func redirectMatches(registered []string, requested string) bool {
	req, err := url.Parse(requested)
	if err != nil {
		return false
	}
	for _, r := range registered {
		if r == requested {
			return true
		}
		reg, err := url.Parse(r)
		if err != nil || reg.Scheme != "http" || req.Scheme != "http" {
			continue
		}
		if isLoopbackHost(reg.Hostname()) && reg.Hostname() == req.Hostname() &&
			reg.Path == req.Path && reg.RawQuery == req.RawQuery {
			return true
		}
	}
	return false
}

func validateClientName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Без названия"
	}
	if utf8.RuneCountInString(name) > 100 {
		name = string([]rune(name)[:100])
	}
	return name
}

// --- dynamic registration (RFC 7591) ------------------------------------------

type registrationRequest struct {
	RedirectURIs []string `json:"redirect_uris"`
	ClientName   string   `json:"client_name"`
	AuthMethod   string   `json:"token_endpoint_auth_method"`
	GrantTypes   []string `json:"grant_types"`
}

func (s *Service) handleRegister(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if !s.allowRegistration(r) {
		writeOAuthError(w, http.StatusTooManyRequests, "invalid_request", "Слишком много регистраций, попробуйте позже.")
		return
	}
	var req registrationRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxMetadataBytes)).Decode(&req); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "Тело запроса — не JSON.")
		return
	}
	if len(req.RedirectURIs) == 0 || len(req.RedirectURIs) > maxRedirectURIs {
		writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", "Нужен от 1 до 10 redirect_uris.")
		return
	}
	for _, uri := range req.RedirectURIs {
		if err := validateRedirectURI(uri); err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", err.Error())
			return
		}
	}
	if req.AuthMethod != "" && req.AuthMethod != "none" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "Поддерживаются только публичные клиенты (token_endpoint_auth_method=none).")
		return
	}
	id, err := newUUID()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	name := validateClientName(req.ClientName)
	if _, err := s.st.Queries().UpsertOAuthClient(r.Context(), db.UpsertOAuthClientParams{
		ClientID: id, Kind: "dcr", Name: name, RedirectUris: req.RedirectURIs,
	}); err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"client_id":                  id,
		"client_name":                name,
		"redirect_uris":              req.RedirectURIs,
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
	})
}

func (s *Service) allowRegistration(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.registry[host]
	if b == nil {
		b = &bucket{tokens: dcrBurst, last: now}
		s.registry[host] = b
		if len(s.registry) > 10000 { // bound memory under a flood of addresses
			s.registry = map[string]*bucket{host: b}
		}
	}
	b.tokens += now.Sub(b.last).Seconds() / dcrRefill.Seconds()
	if b.tokens > dcrBurst {
		b.tokens = dcrBurst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// --- client lookup incl. client ID metadata documents ------------------------

// errUnknownClient means the client_id is not registered and cannot be
// resolved.
var errUnknownClient = errors.New("oauth: unknown client")

// loadClient resolves a client_id. An https URL is a client ID metadata
// document: it is fetched and cached for a day.
func (s *Service) loadClient(ctx context.Context, clientID string) (Client, error) {
	if len(clientID) == 0 || len(clientID) > 2048 {
		return Client{}, errUnknownClient
	}
	row, err := s.st.Queries().GetOAuthClient(ctx, clientID)
	found := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Client{}, fmt.Errorf("get client: %w", err)
	}
	if strings.HasPrefix(clientID, "https://") {
		if !found || !row.FetchedAt.Valid || s.now().Sub(row.FetchedAt.Time) > cimdRefresh {
			fetched, ferr := s.fetchClientMetadata(ctx, clientID)
			switch {
			case ferr == nil:
				fetchedAt := pgtype.Timestamptz{Time: s.now(), Valid: true}
				row, err = s.st.Queries().UpsertOAuthClient(ctx, db.UpsertOAuthClientParams{
					ClientID: clientID, Kind: "cimd", Name: fetched.Name, RedirectUris: fetched.RedirectURIs, FetchedAt: fetchedAt,
				})
				if err != nil {
					return Client{}, fmt.Errorf("store client: %w", err)
				}
			case found:
				// Keep serving the cached document when a refresh fails.
				s.cfg.Logger.WarnContext(ctx, "client metadata refresh failed", "client_id", clientID, "error", ferr)
			default:
				s.cfg.Logger.InfoContext(ctx, "client metadata fetch failed", "client_id", clientID, "error", ferr)
				return Client{}, errUnknownClient
			}
		}
		return Client{ID: row.ClientID, Name: row.Name, RedirectURIs: row.RedirectUris, CIMD: true}, nil
	}
	if !found {
		return Client{}, errUnknownClient
	}
	return Client{ID: row.ClientID, Name: row.Name, RedirectURIs: row.RedirectUris}, nil
}

type clientMetadata struct {
	ClientID     string   `json:"client_id"`
	ClientName   string   `json:"client_name"`
	RedirectURIs []string `json:"redirect_uris"`
	AuthMethod   string   `json:"token_endpoint_auth_method"`
}

// fetchClientMetadata downloads and validates a client metadata document.
func (s *Service) fetchClientMetadata(ctx context.Context, clientID string) (Client, error) {
	u, err := url.Parse(clientID)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Path == "" || u.Path == "/" ||
		u.Fragment != "" || u.User != nil {
		return Client{}, errors.New("client_id is not a valid metadata document URL")
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, nil)
	if err != nil {
		return Client{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return Client{}, fmt.Errorf("fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Client{}, fmt.Errorf("status %d", resp.StatusCode)
	}
	var doc clientMetadata
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxMetadataBytes)).Decode(&doc); err != nil {
		return Client{}, fmt.Errorf("decode: %w", err)
	}
	if doc.ClientID != clientID {
		return Client{}, errors.New("client_id in the document does not match its URL")
	}
	if doc.AuthMethod != "" && doc.AuthMethod != "none" {
		return Client{}, errors.New("only public clients are supported")
	}
	if len(doc.RedirectURIs) == 0 || len(doc.RedirectURIs) > maxRedirectURIs {
		return Client{}, errors.New("redirect_uris must have 1..10 entries")
	}
	for _, uri := range doc.RedirectURIs {
		if err := validateRedirectURI(uri); err != nil {
			return Client{}, err
		}
	}
	return Client{ID: clientID, Name: validateClientName(doc.ClientName), RedirectURIs: doc.RedirectURIs, CIMD: true}, nil
}

// newFetchClient returns an HTTP client for metadata documents that connects
// only to public addresses (the host is resolved and every address checked at
// dial time, so a DNS rebinding answer cannot reach the internal network) and
// does not follow redirects.
func newFetchClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: fetchTimeout,
		Control: func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(host)
			if err != nil {
				return err
			}
			ip = ip.Unmap()
			if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
				ip.IsMulticast() || ip.IsUnspecified() || isCGNAT(ip) {
				return fmt.Errorf("address %s is not public", ip)
			}
			return nil
		},
	}
	return &http.Client{
		Timeout:   fetchTimeout,
		Transport: &http.Transport{DialContext: dialer.DialContext, Proxy: nil, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

func isCGNAT(ip netip.Addr) bool { return cgnat.Contains(ip) }

func (s *Service) fail(w http.ResponseWriter, r *http.Request, err error) {
	s.cfg.Logger.ErrorContext(r.Context(), "oauth request failed", "error", err)
	writeOAuthError(w, http.StatusInternalServerError, "server_error", "Внутренняя ошибка сервера.")
}

// writeOAuthError writes the RFC 6749 JSON error body.
func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": description})
}
