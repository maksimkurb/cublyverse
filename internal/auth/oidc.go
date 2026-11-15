package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// oidcClient wraps provider discovery, the ID token verifier and the OAuth2
// client used for the authorization code flow. PKCE (S256) and a nonce protect
// every login; both are generated per pending flow and stored in PostgreSQL.
type oidcClient struct {
	issuer   string
	verifier *oidc.IDTokenVerifier
	oauth    oauth2.Config
}

func newOIDCClient(ctx context.Context, cfg OIDCConfig) (*oidcClient, error) {
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC provider %q: %w", cfg.Issuer, err)
	}
	return &oidcClient{
		issuer:   cfg.Issuer,
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		oauth: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.RedirectURL,
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		},
	}, nil
}

// authCodeURL builds the provider redirect for one sign-in attempt.
func (c *oidcClient) authCodeURL(state, nonce, verifier string) string {
	return c.oauth.AuthCodeURL(
		state,
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(verifier),
	)
}

// exchange swaps the authorization code for tokens and returns the verified
// identity claims. The ID token must be signed for this client, must not be
// expired, and must carry the nonce of the pending flow.
func (c *oidcClient) exchange(ctx context.Context, code, verifier, nonce string) (OIDCIdentity, error) {
	token, err := c.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return OIDCIdentity{}, fmt.Errorf("exchange authorization code: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return OIDCIdentity{}, errors.New("provider returned no ID token")
	}
	idToken, err := c.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return OIDCIdentity{}, fmt.Errorf("verify ID token: %w", err)
	}
	// The nonce is a per-flow secret, so compare it in constant time.
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(nonce)) != 1 {
		return OIDCIdentity{}, errors.New("ID token nonce does not match the pending flow")
	}

	var claims struct {
		Email             string `json:"email"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return OIDCIdentity{}, fmt.Errorf("decode ID token claims: %w", err)
	}
	name := claims.Name
	if name == "" {
		name = claims.PreferredUsername
	}
	return OIDCIdentity{
		Issuer:      idToken.Issuer,
		Subject:     idToken.Subject,
		Email:       optionalString(claims.Email),
		DisplayName: optionalString(name),
	}, nil
}
