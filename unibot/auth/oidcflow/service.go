package oidcflow

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	unibot "github.com/maksimkurb/cublyverse/unibot"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type Config struct {
	Issuer        string
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	PublicBaseURL string
	BasePath      string
	Scopes        []string
	PendingTTL    time.Duration
}

type Service struct {
	cfg      Config
	identity unibot.IdentityStore
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauth    oauth2.Config

	mu      sync.Mutex
	pending map[string]pendingAuth

	OnLinked func(context.Context, unibot.PrincipalID, unibot.Binding)
}

type pendingAuth struct {
	Binding  unibot.Binding
	Nonce    string
	Verifier string
	Expires  time.Time
}

func New(ctx context.Context, cfg Config, identity unibot.IdentityStore) (*Service, error) {
	if identity == nil {
		return nil, errors.New("identity store is required")
	}
	if cfg.Issuer == "" || cfg.ClientID == "" || cfg.RedirectURL == "" || cfg.PublicBaseURL == "" {
		return nil, errors.New("issuer, client id, redirect url and public base url are required")
	}
	if cfg.BasePath == "" {
		cfg.BasePath = "/auth"
	}
	cfg.BasePath = "/" + strings.Trim(cfg.BasePath, "/")
	if cfg.PendingTTL <= 0 {
		cfg.PendingTTL = 10 * time.Minute
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}

	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC provider: %w", err)
	}
	oauthCfg := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  cfg.RedirectURL,
		Scopes:       cfg.Scopes,
	}
	return &Service{
		cfg:      cfg,
		identity: identity,
		provider: provider,
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		oauth:    oauthCfg,
		pending:  make(map[string]pendingAuth),
	}, nil
}

// BeginURL creates a short-lived OIDC linking session for a messenger identity.
// The caller normally puts this URL into an OpenApp action.
func (s *Service) BeginURL(binding unibot.Binding) (string, error) {
	if binding.Platform == "" || binding.RemoteUserID == "" {
		return "", errors.New("platform and remote user id are required")
	}
	state, err := unibot.RandomToken(24)
	if err != nil {
		return "", err
	}
	nonce, err := unibot.RandomToken(24)
	if err != nil {
		return "", err
	}
	verifier := oauth2.GenerateVerifier()

	s.mu.Lock()
	s.cleanupLocked(time.Now())
	s.pending[state] = pendingAuth{
		Binding:  binding,
		Nonce:    nonce,
		Verifier: verifier,
		Expires:  time.Now().Add(s.cfg.PendingTTL),
	}
	s.mu.Unlock()

	return strings.TrimRight(s.cfg.PublicBaseURL, "/") + s.cfg.BasePath + "/login?state=" + url.QueryEscape(state), nil
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(s.cfg.BasePath+"/login", s.handleLogin)
	mux.HandleFunc(s.cfg.BasePath+"/callback", s.handleCallback)
	return mux
}

func (s *Service) handleLogin(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	pending, ok := s.getPending(state)
	if !ok {
		http.Error(w, "login link expired", http.StatusBadRequest)
		return
	}
	redirect := s.oauth.AuthCodeURL(
		state,
		oidc.Nonce(pending.Nonce),
		oauth2.S256ChallengeOption(pending.Verifier),
	)
	http.Redirect(w, r, redirect, http.StatusFound)
}

func (s *Service) handleCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	state := r.URL.Query().Get("state")
	pending, ok := s.getPending(state)
	if !ok {
		http.Error(w, "OIDC state expired or unknown", http.StatusBadRequest)
		return
	}
	if providerErr := r.URL.Query().Get("error"); providerErr != "" {
		http.Error(w, "OIDC provider returned: "+providerErr, http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing authorization code", http.StatusBadRequest)
		return
	}

	token, err := s.oauth.Exchange(
		r.Context(),
		code,
		oauth2.VerifierOption(pending.Verifier),
	)
	if err != nil {
		var retrieveErr *oauth2.RetrieveError

		if errors.As(err, &retrieveErr) {
			log.Printf(
				"OIDC token exchange failed: status=%s error=%q description=%q body=%s",
				retrieveErr.Response.Status,
				retrieveErr.ErrorCode,
				retrieveErr.ErrorDescription,
				string(retrieveErr.Body),
			)
		} else {
			log.Printf("OIDC token exchange failed: %T: %v", err, err)
		}

		http.Error(w, "OIDC token exchange failed; see server log", http.StatusBadGateway)
		return
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		http.Error(w, "provider did not return an ID token", http.StatusBadGateway)
		return
	}
	idToken, err := s.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		http.Error(w, "invalid ID token", http.StatusUnauthorized)
		return
	}
	if idToken.Nonce != pending.Nonce {
		http.Error(w, "invalid OIDC nonce", http.StatusUnauthorized)
		return
	}
	principal := principalID(s.cfg.Issuer, idToken.Subject)
	binding := pending.Binding
	binding.PrincipalID = principal
	binding.VerifiedAt = time.Now()
	if err := s.identity.Bind(ctx, binding); err != nil {
		http.Error(w, "failed to persist messenger binding", http.StatusInternalServerError)
		return
	}
	s.deletePending(state)
	if s.OnLinked != nil {
		s.OnLinked(ctx, principal, binding)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = linkedPage.Execute(w, map[string]string{"Platform": string(binding.Platform)})
}

func principalID(issuer, subject string) unibot.PrincipalID {
	sum := sha256.Sum256([]byte(issuer + "\x00" + subject))
	return unibot.PrincipalID(base64.RawURLEncoding.EncodeToString(sum[:]))
}

func (s *Service) getPending(state string) (pendingAuth, bool) {
	if state == "" {
		return pendingAuth{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(time.Now())
	item, ok := s.pending[state]
	return item, ok
}

func (s *Service) deletePending(state string) {
	s.mu.Lock()
	delete(s.pending, state)
	s.mu.Unlock()
}

func (s *Service) cleanupLocked(now time.Time) {
	for state, item := range s.pending {
		if now.After(item.Expires) {
			delete(s.pending, state)
		}
	}
}

var linkedPage = template.Must(template.New("linked").Parse(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Linked</title></head>
<body style="font:16px system-ui,sans-serif;max-width:34rem;margin:4rem auto;padding:1rem">
  <h1>Account linked</h1>
  <p>Your {{.Platform}} identity is now linked to the SSO account. You can return to the chat.</p>
  <button onclick="window.Telegram?.WebApp?.close?.(); window.close()">Close</button>
</body>
</html>`))
