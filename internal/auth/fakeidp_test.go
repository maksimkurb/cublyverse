package auth_test

// This file contains the in-process OpenID Connect provider used by the
// end-to-end tests. It implements exactly the endpoints the service needs:
// discovery, JWKS, authorization and token.

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
)

const (
	testClientID     = "cublyverse-test"
	testClientSecret = "test-secret"
	// testBotToken is a syntactically valid Telegram bot token. Tests sign
	// initData with it; it belongs to no real bot.
	testBotToken = "5768337691:AAH5YkoiEuPk8-FZa32hStHTqXiLPtAEhx8"
)

// authCode is one authorization code the provider has handed out.
type authCode struct {
	redirectURI string
	nonce       string
	challenge   string
	subject     string
	email       string
	name        string
}

// fakeIDP is a minimal but real OIDC provider: it signs ID tokens with an RSA
// key and verifies the PKCE challenge, so the service under test performs the
// same cryptographic work as against a production provider.
type fakeIDP struct {
	server *httptest.Server
	key    *rsa.PrivateKey
	issuer string

	mu       sync.Mutex
	codes    map[string]authCode
	subject  string
	email    string
	name     string
	requests int
}

func newFakeIDP(t *testing.T) *fakeIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	idp := &fakeIDP{key: key, codes: make(map[string]authCode)}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", idp.discovery)
	mux.HandleFunc("GET /jwks", idp.jwks)
	mux.HandleFunc("GET /authorize", idp.authorize)
	mux.HandleFunc("POST /token", idp.token)

	idp.server = httptest.NewServer(mux)
	idp.issuer = idp.server.URL
	t.Cleanup(idp.server.Close)
	return idp
}

// signIn sets the identity the next authorization returns.
func (f *fakeIDP) signIn(subject, email, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subject = subject
	f.email = email
	f.name = name
}

// requestCount reports how many authorization requests were seen; tests use it
// to prove the login page did not disclose anything before verifying initData.
func (f *fakeIDP) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

func (f *fakeIDP) discovery(w http.ResponseWriter, _ *http.Request) {
	writeFakeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                f.issuer,
		"authorization_endpoint":                f.issuer + "/authorize",
		"token_endpoint":                        f.issuer + "/token",
		"jwks_uri":                              f.issuer + "/jwks",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_post", "client_secret_basic"},
		"scopes_supported":                      []string{"openid", "profile", "email"},
	})
}

func (f *fakeIDP) jwks(w http.ResponseWriter, _ *http.Request) {
	public := f.key.Public().(*rsa.PublicKey)
	writeFakeJSON(w, http.StatusOK, map[string]any{
		"keys": []map[string]any{{
			"kty": "RSA",
			"use": "sig",
			"alg": "RS256",
			"kid": "test-key",
			"n":   base64.RawURLEncoding.EncodeToString(public.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(public.E)).Bytes()),
		}},
	})
}

func (f *fakeIDP) authorize(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if got := query.Get("response_type"); got != "code" {
		http.Error(w, "unsupported response_type "+got, http.StatusBadRequest)
		return
	}
	if got := query.Get("code_challenge_method"); got != "S256" {
		http.Error(w, "PKCE with S256 is required", http.StatusBadRequest)
		return
	}
	if got := query.Get("client_id"); got != testClientID {
		http.Error(w, "unknown client", http.StatusBadRequest)
		return
	}
	code := "code-" + randomHex()
	f.mu.Lock()
	f.requests++
	f.codes[code] = authCode{
		redirectURI: query.Get("redirect_uri"),
		nonce:       query.Get("nonce"),
		challenge:   query.Get("code_challenge"),
		subject:     f.subject,
		email:       f.email,
		name:        f.name,
	}
	f.mu.Unlock()

	target, err := url.Parse(query.Get("redirect_uri"))
	if err != nil {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	values := target.Query()
	values.Set("code", code)
	values.Set("state", query.Get("state"))
	target.RawQuery = values.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

func (f *fakeIDP) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	clientID, clientSecret := r.PostFormValue("client_id"), r.PostFormValue("client_secret")
	if clientID == "" {
		clientID, clientSecret, _ = r.BasicAuth()
	}
	if clientID != testClientID || clientSecret != testClientSecret {
		writeFakeJSON(w, http.StatusUnauthorized, map[string]string{
			"error":             "invalid_client",
			"error_description": "bad client credentials",
		})
		return
	}

	code := r.PostFormValue("code")
	f.mu.Lock()
	entry, ok := f.codes[code]
	delete(f.codes, code)
	f.mu.Unlock()
	if !ok {
		writeFakeJSON(w, http.StatusBadRequest, map[string]string{
			"error":             "invalid_grant",
			"error_description": "unknown or reused code",
		})
		return
	}
	verifier := r.PostFormValue("code_verifier")
	sum := sha256.Sum256([]byte(verifier))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != entry.challenge {
		writeFakeJSON(w, http.StatusBadRequest, map[string]string{
			"error":             "invalid_grant",
			"error_description": "PKCE verification failed",
		})
		return
	}

	idToken, err := f.idToken(entry)
	if err != nil {
		http.Error(w, "sign ID token: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeFakeJSON(w, http.StatusOK, map[string]any{
		"access_token": "access-" + randomHex(),
		"token_type":   "Bearer",
		"expires_in":   300,
		"id_token":     idToken,
	})
}

func (f *fakeIDP) idToken(entry authCode) (string, error) {
	header := map[string]string{"alg": "RS256", "typ": "JWT", "kid": "test-key"}
	now := time.Now()
	claims := map[string]any{
		"iss":   f.issuer,
		"sub":   entry.subject,
		"aud":   testClientID,
		"exp":   now.Add(5 * time.Minute).Unix(),
		"iat":   now.Unix(),
		"nonce": entry.nonce,
	}
	if entry.email != "" {
		claims["email"] = entry.email
	}
	if entry.name != "" {
		claims["name"] = entry.name
	}

	encodedHeader, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	encodedClaims, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := base64.RawURLEncoding.EncodeToString(encodedHeader) + "." +
		base64.RawURLEncoding.EncodeToString(encodedClaims)
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func writeFakeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func randomHex() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("crypto/rand: %v", err))
	}
	return hex.EncodeToString(buf)
}
