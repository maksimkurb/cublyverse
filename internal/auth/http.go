package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/maksimkurb/cublyverse/internal/store/db"
	unibot "github.com/maksimkurb/cublyverse/unibot"
)

// maxFormBytes and maxJSONBytes cap the bodies of the sign-in endpoints.
const (
	maxFormBytes = 64 << 10
	maxJSONBytes = 64 << 10
)

// Handler returns the /auth routes: the OIDC entry points, the confirmation
// POST and the Mini App initData check. They are browser pages, not part of the
// JSON API, so they are deliberately not described in api/openapi.yaml; the
// behaviour is documented in docs/API.md.
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/login", s.handleLogin)
	mux.HandleFunc("GET /auth/callback", s.handleCallback)
	mux.HandleFunc("POST /auth/confirm", s.handleConfirm)
	mux.HandleFunc("POST /auth/telegram/verify", s.handleTelegramVerify)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.renderError(w, http.StatusNotFound, "Страница не найдена", "Такой страницы входа нет.")
	})
	return mux
}

// handleLogin is the entry point of every sign-in.
//
// With a state it continues a flow started by the bot (or by the Mini App):
// a Telegram binding is first checked against initData, everything else goes
// straight to the identity provider. Without a state it starts a plain browser
// login and honours return_to.
func (s *Service) handleLogin(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	if state == "" {
		s.startBrowserLogin(w, r)
		return
	}
	if s.oidc == nil {
		s.renderError(w, http.StatusServiceUnavailable, "Вход недоступен", "Вход через SSO не настроен на сервере.")
		return
	}

	pending, err := s.loadPending(r.Context(), state)
	if err != nil {
		s.renderError(w, http.StatusGone, "Ссылка недействительна", "Ссылка для входа устарела или уже была использована.")
		return
	}

	switch pending.Status {
	case statusPendingOIDC:
	case statusAwaitingConfirmation:
		s.renderError(w, http.StatusGone, "Ссылка уже использована",
			"Подтверждение привязки уже открывалось. Если страница потерялась, начните вход заново из чата.")
		return
	default:
		s.renderError(w, http.StatusGone, "Ссылка уже использована",
			"Эта ссылка для входа уже использована. Начните вход заново из чата.")
		return
	}

	// A Telegram binding may only continue when the page is running inside the
	// Telegram Mini App of the very account that is being linked. Otherwise an
	// attacker's login link could bind their messenger to the victim's SSO
	// account.
	if pending.Flow == FlowBind && pending.Platform == string(unibot.PlatformTelegram) && !pending.InitDataVerifiedAt.Valid {
		s.renderPage(w, http.StatusOK, telegramVerifyPage, pageView{
			Title: "Вход через Telegram",
			State: state,
		})
		return
	}

	http.Redirect(w, r, s.oidc.authCodeURL(state, pending.Nonce, pending.Verifier), http.StatusFound)
}

// startBrowserLogin starts a plain SSO login for a browser and redirects to the
// identity provider.
func (s *Service) startBrowserLogin(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil {
		s.renderError(w, http.StatusServiceUnavailable, "Вход недоступен", "Вход через SSO не настроен на сервере.")
		return
	}
	redirect, _, err := s.BeginBrowserLogin(r.Context(), r.URL.Query().Get("return_to"))
	if errors.Is(err, ErrInvalidReturnTo) {
		s.renderError(w, http.StatusBadRequest, "Некорректный адрес возврата",
			"Адрес возврата должен быть относительным путём на этом сайте.")
		return
	}
	if err != nil {
		s.logger.ErrorContext(r.Context(), "start browser login failed", "error", err)
		s.renderError(w, http.StatusInternalServerError, "Ошибка входа", "Не удалось начать вход. Попробуйте ещё раз.")
		return
	}
	http.Redirect(w, r, redirect, http.StatusFound)
}

// handleCallback finishes the OIDC round trip:
//
//   - a browser login gets a cookie session and lands on return_to;
//   - a messenger binding gets the confirmation page with a single-use token.
func (s *Service) handleCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query := r.URL.Query()

	state := query.Get("state")
	pending, err := s.loadPending(ctx, state)
	if err != nil {
		s.renderError(w, http.StatusGone, "Ссылка недействительна", "Ссылка для входа устарела или уже была использована.")
		return
	}
	if providerErr := query.Get("error"); providerErr != "" {
		s.logger.WarnContext(ctx, "OIDC provider returned an error",
			"error", providerErr,
			"description", query.Get("error_description"),
		)
		s.renderError(w, http.StatusBadRequest, "Вход отменён", "Провайдер входа сообщил об ошибке. Попробуйте ещё раз.")
		return
	}
	if pending.Status != statusPendingOIDC {
		s.renderError(w, http.StatusGone, "Ссылка уже использована", "Эта ссылка для входа уже использована.")
		return
	}
	if s.oidc == nil {
		s.renderError(w, http.StatusServiceUnavailable, "Вход недоступен", "Вход через SSO не настроен на сервере.")
		return
	}
	// Refuse a Telegram binding that was not proven by the Mini App before any
	// token is exchanged: a link opened in a browser must never bind an
	// account.
	if pending.Flow == FlowBind && pending.Platform == string(unibot.PlatformTelegram) && !pending.InitDataVerifiedAt.Valid {
		s.renderError(w, http.StatusForbidden, "Нужно подтверждение Telegram",
			"Привязку можно завершить только из Telegram: откройте ссылку из чата с ботом.")
		return
	}
	code := query.Get("code")
	if code == "" {
		s.renderError(w, http.StatusBadRequest, "Некорректный ответ провайдера", "Провайдер входа не передал код авторизации.")
		return
	}

	identity, err := s.oidc.exchange(ctx, code, pending.Verifier, pending.Nonce)
	if err != nil {
		s.logger.ErrorContext(ctx, "OIDC code exchange failed", "error", err)
		s.renderError(w, http.StatusBadGateway, "Не удалось завершить вход",
			"Провайдер входа отклонил запрос. Попробуйте ещё раз.")
		return
	}
	user, err := s.UpsertFromOIDC(ctx, identity)
	if err != nil {
		s.logger.ErrorContext(ctx, "upsert user from OIDC failed", "error", err)
		s.renderError(w, http.StatusInternalServerError, "Ошибка входа", "Не удалось сохранить профиль. Попробуйте ещё раз.")
		return
	}

	if pending.Flow == FlowBrowser {
		s.completeBrowserLogin(w, r, pending, user)
		return
	}

	confirmToken, err := newToken()
	if err != nil {
		s.logger.ErrorContext(ctx, "mint confirmation token failed", "error", err)
		s.renderError(w, http.StatusInternalServerError, "Ошибка входа", "Не удалось подготовить подтверждение.")
		return
	}
	userID, err := parseUUID(user.ID)
	if err != nil {
		s.logger.ErrorContext(ctx, "parse user id failed", "error", err)
		s.renderError(w, http.StatusInternalServerError, "Ошибка входа", "Не удалось подготовить подтверждение.")
		return
	}
	claimed, err := s.st.Queries().BeginPendingAuthConfirmation(ctx, db.BeginPendingAuthConfirmationParams{
		StateHash:        pending.StateHash,
		UserID:           userID,
		ConfirmTokenHash: hashToken(confirmToken),
		ExpiresAt:        time.Now().UTC().Add(s.cfg.PendingTTL),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		s.renderError(w, http.StatusGone, "Ссылка уже использована", "Эта ссылка для входа уже использована.")
		return
	}
	if err != nil {
		s.logger.ErrorContext(ctx, "claim pending sign-in failed", "error", err)
		s.renderError(w, http.StatusInternalServerError, "Ошибка входа", "Не удалось подготовить подтверждение.")
		return
	}

	s.renderPage(w, http.StatusOK, confirmPage, pageView{
		Title:        "Привязка аккаунта",
		State:        state,
		Token:        confirmToken,
		Platform:     platformLabel(claimed.Platform),
		RemoteUserID: claimed.RemoteUserID,
		SSOName:      ssoName(user),
	})
}

// completeBrowserLogin issues the cookie session of a browser login.
func (s *Service) completeBrowserLogin(w http.ResponseWriter, r *http.Request, pending db.AuthPending, user User) {
	ctx := r.Context()
	session, err := s.CreateSession(ctx, user.ID, SessionKindCookie)
	if err != nil {
		s.logger.ErrorContext(ctx, "create browser session failed", "error", err)
		s.renderError(w, http.StatusInternalServerError, "Ошибка входа", "Не удалось создать сессию.")
		return
	}
	if err := s.deletePending(ctx, pending.StateHash); err != nil {
		s.logger.WarnContext(ctx, "delete pending sign-in failed", "error", err)
	}
	http.SetCookie(w, s.SessionCookie(session.Token, session.ExpiresAt))

	target, ok := SafeReturnTo(pending.ReturnTo)
	if !ok {
		target = defaultReturnTo
	}
	s.logger.InfoContext(ctx, "browser sign-in completed", "user_id", user.ID)
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// handleConfirm creates the messenger binding after the user pressed
// "Привязать". The POST carries the state and the single-use confirmation
// token; only a matching pair creates a binding.
func (s *Service) handleConfirm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.renderError(w, http.StatusBadRequest, "Некорректный запрос", "Форма подтверждения повреждена.")
		return
	}
	state := r.PostFormValue("state")
	token := r.PostFormValue("token")

	binding, err := s.ConfirmBinding(ctx, state, token)
	switch {
	case errors.Is(err, ErrConfirmTokenInvalid):
		s.renderError(w, http.StatusForbidden, "Неверный токен подтверждения",
			"Подтверждение не совпадает со ссылкой. Начните вход заново из чата.")
		return
	case errors.Is(err, ErrPendingUnusable):
		s.renderError(w, http.StatusGone, "Ссылка уже использована",
			"Подтверждение устарело или уже было использовано. Начните вход заново из чата.")
		return
	case err != nil:
		s.logger.ErrorContext(ctx, "confirm binding failed", "error", err)
		s.renderError(w, http.StatusInternalServerError, "Ошибка привязки", "Не удалось привязать аккаунт.")
		return
	}

	s.logger.InfoContext(ctx, "messenger account bound",
		"platform", string(binding.Platform),
		"user_id", string(binding.PrincipalID),
	)
	s.notifyLinked(ctx, binding)

	s.renderPage(w, http.StatusOK, linkedPage, pageView{
		Title:        "Аккаунт привязан",
		Platform:     platformLabel(string(binding.Platform)),
		RemoteUserID: binding.RemoteUserID,
		SSOName:      s.ssoNameOfPrincipal(ctx, binding.PrincipalID),
	})
}

// notifyLinked tells the application that a binding is ready. The message is
// sent outside the request lifetime, so a disappearing browser cannot skip it.
func (s *Service) notifyLinked(ctx context.Context, binding unibot.Binding) {
	if s.OnLinked == nil {
		return
	}
	notifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	s.OnLinked(notifyCtx, binding)
}

// handleTelegramVerify is called by the login page when it runs inside the
// Telegram Mini App. It proves that the page is opened by the Telegram account
// of the pending binding.
func (s *Service) handleTelegramVerify(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBytes)
	var payload struct {
		State    string `json:"state"`
		InitData string `json:"init_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "Некорректный запрос")
		return
	}

	err := s.MarkInitDataVerified(ctx, payload.State, payload.InitData)
	switch {
	case errors.Is(err, ErrTelegramDisabled):
		writeJSONError(w, http.StatusServiceUnavailable, "unavailable", "Вход через Telegram не настроен")
		return
	case errors.Is(err, ErrIdentityMismatch):
		writeJSONError(w, http.StatusForbidden, "identity_mismatch",
			"Этот аккаунт Telegram не совпадает с тем, для которого создана ссылка входа")
		return
	case errors.Is(err, ErrInvalidInitData):
		writeJSONError(w, http.StatusUnauthorized, "invalid_init_data", "Не удалось проверить данные Telegram")
		return
	case errors.Is(err, ErrPendingUnusable):
		writeJSONError(w, http.StatusGone, "link_expired", "Ссылка для входа устарела или уже использована")
		return
	case err != nil:
		s.logger.ErrorContext(ctx, "telegram initData verification failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal", "Внутренняя ошибка сервера")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"redirect_url": "/auth/login?state=" + url.QueryEscape(payload.State),
	})
}

// ssoName is the human readable name of the SSO account on the pages.
func ssoName(user User) string {
	if user.DisplayName != nil && *user.DisplayName != "" {
		return *user.DisplayName
	}
	if user.Email != nil && *user.Email != "" {
		return *user.Email
	}
	return "аккаунту SSO"
}

// ssoNameOfPrincipal loads the user behind a principal for the success page.
// A lookup failure must not break the page: the binding already exists.
func (s *Service) ssoNameOfPrincipal(ctx context.Context, principal unibot.PrincipalID) string {
	profile, err := s.Profile(ctx, string(principal))
	if err != nil {
		s.logger.WarnContext(ctx, "load profile for success page failed", "error", err)
		return "SSO-аккаунту"
	}
	return ssoName(profile.User)
}

// writeJSON writes a JSON body produced by the sign-in endpoints.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// writeJSONError writes the shared Error shape for the sign-in endpoints. They
// are outside /api/v1, so the generated type is not available here.
func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: code, Message: message})
}
