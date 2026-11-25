package oauthserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/maksimkurb/cublyverse/internal/store/db"
)

// handleToken implements the token endpoint: authorization_code and
// refresh_token grants for public clients.
func (s *Service) handleToken(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "Некорректная форма.")
		return
	}
	form := r.PostForm
	client, err := s.loadClient(r.Context(), form.Get("client_id"))
	if err != nil {
		if errors.Is(err, errUnknownClient) {
			writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "Неизвестный client_id.")
			return
		}
		s.fail(w, r, err)
		return
	}
	var resp tokenResponse
	switch form.Get("grant_type") {
	case "authorization_code":
		resp, err = s.exchangeCode(r.Context(), client, form.Get("code"), form.Get("redirect_uri"), form.Get("code_verifier"), form.Get("resource"))
	case "refresh_token":
		resp, err = s.refresh(r.Context(), client, form.Get("refresh_token"))
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "Поддерживаются authorization_code и refresh_token.")
		return
	}
	var grantErr grantError
	if errors.As(err, &grantErr) {
		writeOAuthError(w, http.StatusBadRequest, grantErr.code, grantErr.description)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	_ = json.NewEncoder(w).Encode(resp)
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// grantError is a protocol error with an RFC 6749 error code.
type grantError struct{ code, description string }

func (e grantError) Error() string { return e.code + ": " + e.description }

func invalidGrant(description string) error { return grantError{"invalid_grant", description} }

func (s *Service) exchangeCode(ctx context.Context, client Client, code, redirectURI, verifier, resource string) (tokenResponse, error) {
	if code == "" {
		return tokenResponse{}, grantError{"invalid_request", "Не указан code."}
	}
	q := s.st.Queries()
	hash := hashSecret(code)
	row, err := q.ConsumeOAuthCode(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		// A code that exists but is already used means a replay: revoke what it
		// produced.
		if old, getErr := q.GetOAuthCode(ctx, hash); getErr == nil && old.UsedAt.Valid {
			if err := q.RevokeOAuthFamily(ctx, old.FamilyID); err != nil {
				return tokenResponse{}, fmt.Errorf("revoke family: %w", err)
			}
		}
		return tokenResponse{}, invalidGrant("Код недействителен, просрочен или уже использован.")
	}
	if err != nil {
		return tokenResponse{}, fmt.Errorf("consume code: %w", err)
	}
	if row.ClientID != client.ID || row.RedirectUri != redirectURI {
		return tokenResponse{}, invalidGrant("Код выдан другому клиенту или для другого redirect_uri.")
	}
	if !pkceMatches(row.CodeChallenge, verifier) {
		return tokenResponse{}, invalidGrant("Не пройдена проверка PKCE (code_verifier).")
	}
	if resource != "" && row.Resource != "" && resource != row.Resource {
		return tokenResponse{}, grantError{"invalid_target", "resource не совпадает с запрошенным при авторизации."}
	}
	return s.issue(ctx, issueParams{
		family: row.FamilyID, clientID: client.ID, userID: row.UserID, workspaceID: row.WorkspaceID,
		scopes: row.Scopes, resource: row.Resource,
	})
}

func (s *Service) refresh(ctx context.Context, client Client, refreshToken string) (tokenResponse, error) {
	if !strings.HasPrefix(refreshToken, RefreshTokenPrefix) {
		return tokenResponse{}, invalidGrant("Недействительный refresh_token.")
	}
	q := s.st.Queries()
	row, err := q.GetOAuthTokenByRefresh(ctx, hashSecret(refreshToken))
	if errors.Is(err, pgx.ErrNoRows) {
		return tokenResponse{}, invalidGrant("Недействительный refresh_token.")
	}
	if err != nil {
		return tokenResponse{}, fmt.Errorf("get refresh token: %w", err)
	}
	if row.ClientID != client.ID {
		return tokenResponse{}, invalidGrant("refresh_token выдан другому клиенту.")
	}
	if row.RevokedAt.Valid || !row.RefreshExpiresAt.After(s.now()) {
		return tokenResponse{}, invalidGrant("refresh_token отозван или просрочен.")
	}
	if row.RefreshUsedAt.Valid {
		// A spent refresh token is being replayed: the family is compromised.
		if err := q.RevokeOAuthFamily(ctx, row.FamilyID); err != nil {
			return tokenResponse{}, fmt.Errorf("revoke family: %w", err)
		}
		return tokenResponse{}, invalidGrant("refresh_token уже использован.")
	}
	if _, err := q.UseOAuthRefresh(ctx, row.ID); errors.Is(err, pgx.ErrNoRows) {
		// Lost a race with a concurrent refresh of the same token.
		_ = q.RevokeOAuthFamily(ctx, row.FamilyID)
		return tokenResponse{}, invalidGrant("refresh_token уже использован.")
	} else if err != nil {
		return tokenResponse{}, fmt.Errorf("use refresh token: %w", err)
	}
	return s.issue(ctx, issueParams{
		family: row.FamilyID, clientID: row.ClientID, userID: row.UserID, workspaceID: row.WorkspaceID,
		scopes: row.Scopes, resource: row.Resource,
	})
}

type issueParams struct {
	family, userID, workspaceID pgtype.UUID
	clientID, resource          string
	scopes                      []string
}

func (s *Service) issue(ctx context.Context, p issueParams) (tokenResponse, error) {
	access, err := randomToken(AccessTokenPrefix)
	if err != nil {
		return tokenResponse{}, err
	}
	refresh, err := randomToken(RefreshTokenPrefix)
	if err != nil {
		return tokenResponse{}, err
	}
	now := s.now()
	if _, err := s.st.Queries().CreateOAuthToken(ctx, db.CreateOAuthTokenParams{
		FamilyID: p.family, ClientID: p.clientID, UserID: p.userID, WorkspaceID: p.workspaceID,
		Scopes: p.scopes, Resource: p.resource,
		AccessHash: hashSecret(access), AccessExpiresAt: now.Add(accessTokenTTL),
		RefreshHash: hashSecret(refresh), RefreshExpiresAt: now.Add(refreshTokenTTL),
	}); err != nil {
		return tokenResponse{}, fmt.Errorf("create token: %w", err)
	}
	return tokenResponse{
		AccessToken: access, TokenType: "Bearer", ExpiresIn: int(accessTokenTTL.Seconds()),
		RefreshToken: refresh, Scope: strings.Join(p.scopes, " "),
	}, nil
}

// handleRevoke implements RFC 7009. It answers 200 for any token, known or not,
// so the endpoint cannot be used to probe tokens.
func (s *Service) handleRevoke(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "Некорректная форма.")
		return
	}
	token := r.PostForm.Get("token")
	clientID := r.PostForm.Get("client_id")
	q := s.st.Queries()
	var row db.OauthToken
	var err error
	switch {
	case strings.HasPrefix(token, AccessTokenPrefix):
		row, err = q.GetOAuthTokenByAccess(r.Context(), hashSecret(token))
	case strings.HasPrefix(token, RefreshTokenPrefix):
		row, err = q.GetOAuthTokenByRefresh(r.Context(), hashSecret(token))
	default:
		w.WriteHeader(http.StatusOK)
		return
	}
	if err == nil && row.ClientID == clientID {
		if err := q.RevokeOAuthFamily(r.Context(), row.FamilyID); err != nil {
			s.fail(w, r, err)
			return
		}
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}
