package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/maksimkurb/cublyverse/internal/auth"
	"github.com/maksimkurb/cublyverse/internal/httpapi/gen"
)

// msgLoginRequired is shown when an operation needs a session.
const msgLoginRequired = "Требуется вход"

// errNoSession signals that the request carries no valid session.
var errNoSession = errors.New("no session")

// handlers implements the generated strict server interface and the probe
// endpoints. It performs no business logic of its own; operations delegate to
// feature services.
type handlers struct {
	deps Deps
}

var _ gen.StrictServerInterface = (*handlers)(nil)

// GetHealth implements GET /api/v1/health.
func (h *handlers) GetHealth(_ context.Context, _ gen.GetHealthRequestObject) (gen.GetHealthResponseObject, error) {
	return gen.GetHealth200JSONResponse{
		Status:  "ok",
		Version: h.deps.Version,
	}, nil
}

// GetMe implements GET /api/v1/me.
func (h *handlers) GetMe(ctx context.Context, _ gen.GetMeRequestObject) (gen.GetMeResponseObject, error) {
	profile, err := h.currentProfile(ctx)
	if errors.Is(err, errNoSession) {
		return gen.GetMe401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.GetMe200JSONResponse(meFromProfile(profile)), nil
}

// UpdateMe implements PATCH /api/v1/me.
func (h *handlers) UpdateMe(ctx context.Context, request gen.UpdateMeRequestObject) (gen.UpdateMeResponseObject, error) {
	session, ok := sessionFrom(ctx)
	if !ok || h.deps.Auth == nil {
		return gen.UpdateMe401JSONResponse{
			UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(errorPayload(codeUnauthorized, msgLoginRequired)),
		}, nil
	}
	if request.Body == nil {
		return gen.UpdateMe400JSONResponse(errorPayload(codeBadRequest, "Некорректный запрос")), nil
	}
	if err := auth.ValidateTimezone(request.Body.Timezone); err != nil {
		return gen.UpdateMe400JSONResponse(errorPayload(codeBadRequest, "Неизвестный часовой пояс")), nil
	}
	profile, err := h.deps.Auth.UpdateTimezone(ctx, session.PrincipalID, request.Body.Timezone)
	if err != nil {
		return nil, err
	}
	return gen.UpdateMe200JSONResponse(meFromProfile(profile)), nil
}

// Logout implements POST /api/v1/auth/logout. Revoking is idempotent: a client
// that lost its session must still be able to drop local state.
func (h *handlers) Logout(ctx context.Context, _ gen.LogoutRequestObject) (gen.LogoutResponseObject, error) {
	response := gen.Logout204Response{}
	if h.deps.Auth == nil {
		return response, nil
	}
	if session, ok := sessionFrom(ctx); ok {
		if err := h.deps.Auth.RevokeSession(ctx, session.ID); err != nil {
			return nil, err
		}
	}
	cookie := h.deps.Auth.ClearedSessionCookie()
	response.Headers.SetCookie = &cookie
	return response, nil
}

// AuthTelegram implements POST /api/v1/auth/telegram.
func (h *handlers) AuthTelegram(ctx context.Context, request gen.AuthTelegramRequestObject) (gen.AuthTelegramResponseObject, error) {
	if h.deps.Auth == nil {
		return gen.AuthTelegram503JSONResponse(errorPayload(codeUnavailable, "Вход через Telegram не настроен")), nil
	}
	if request.Body == nil {
		return gen.AuthTelegram400JSONResponse(errorPayload(codeBadRequest, "Некорректный запрос")), nil
	}
	if strings.TrimSpace(request.Body.InitData) == "" {
		return gen.AuthTelegram400JSONResponse(errorPayload(codeBadRequest, "Поле init_data обязательно")), nil
	}

	result, err := h.deps.Auth.AuthenticateTelegram(ctx, request.Body.InitData)
	switch {
	case errors.Is(err, auth.ErrTelegramDisabled):
		return gen.AuthTelegram503JSONResponse(errorPayload(codeUnavailable, "Вход через Telegram не настроен")), nil
	case errors.Is(err, auth.ErrInvalidInitData):
		return gen.AuthTelegram401JSONResponse(
			errorPayload(codeInvalidInitData, "Не удалось проверить данные Telegram"),
		), nil
	case err != nil:
		return nil, err
	}

	switch result.Status {
	case auth.TelegramStatusOK:
		token := result.Session.Token
		expiresAt := result.Session.ExpiresAt
		user := meFromProfile(result.Profile)
		return gen.AuthTelegram200JSONResponse(gen.TelegramAuthResponse{
			Status:    gen.Ok,
			Token:     &token,
			ExpiresAt: &expiresAt,
			User:      &user,
		}), nil
	case auth.TelegramStatusLinkRequired:
		linkURL := result.LinkURL
		return gen.AuthTelegram200JSONResponse(gen.TelegramAuthResponse{
			Status:  gen.LinkRequired,
			LinkUrl: &linkURL,
		}), nil
	default:
		return nil, fmt.Errorf("unexpected telegram sign-in status %q", result.Status)
	}
}

// currentProfile resolves the session and loads the profile it belongs to.
func (h *handlers) currentProfile(ctx context.Context) (auth.Profile, error) {
	if h.deps.Auth == nil {
		return auth.Profile{}, errNoSession
	}
	session, ok := sessionFrom(ctx)
	if !ok {
		return auth.Profile{}, errNoSession
	}
	return h.deps.Auth.Profile(ctx, session.PrincipalID)
}

// meFromProfile maps the service profile to the API schema.
func meFromProfile(profile auth.Profile) gen.Me {
	me := gen.Me{
		Id:          profile.User.ID,
		Email:       profile.User.Email,
		DisplayName: profile.User.DisplayName,
		Timezone:    profile.User.Timezone,
		CreatedAt:   profile.User.CreatedAt,
		Bindings:    make([]gen.Binding, 0, len(profile.Bindings)),
	}
	for _, binding := range profile.Bindings {
		me.Bindings = append(me.Bindings, gen.Binding{
			Platform:       gen.BindingPlatform(binding.Platform),
			RemoteUserId:   binding.RemoteUserID,
			ConversationId: binding.ConversationID,
			VerifiedAt:     binding.VerifiedAt,
		})
	}
	return me
}

// errorPayload builds the shared error body.
func errorPayload(code, message string) gen.Error {
	return gen.Error{Code: code, Message: message}
}

// readinessResponse is the body of /readyz. The probe is infrastructure
// facing, so it is not part of the public OpenAPI document.
type readinessResponse struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// healthz reports that the process is alive. It never touches dependencies.
func (h *handlers) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

// readyz reports whether the service can serve traffic. The database is the
// only hard dependency, so an unreachable database means 503.
func (h *handlers) readyz(w http.ResponseWriter, r *http.Request) {
	if h.deps.Ready != nil {
		if err := h.deps.Ready(r.Context()); err != nil {
			h.deps.Logger.WarnContext(r.Context(), "readiness check failed",
				slog.String("error", err.Error()),
				slog.String("request_id", RequestIDFrom(r.Context())),
			)
			writeJSON(w, http.StatusServiceUnavailable, readinessResponse{
				Status:  "unavailable",
				Message: "База данных недоступна",
			})
			return
		}
	}
	writeJSON(w, http.StatusOK, readinessResponse{Status: "ok"})
}
