package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/maksimkurb/cublyverse/internal/store/db"
	unibot "github.com/maksimkurb/cublyverse/unibot"
	unibotwebapp "github.com/maksimkurb/cublyverse/unibot/webapp"
)

// Statuses of the Telegram Mini App sign-in response.
const (
	TelegramStatusOK           = "ok"
	TelegramStatusLinkRequired = "link_required"
)

// TelegramAuthResult is the outcome of POST /auth/telegram.
type TelegramAuthResult struct {
	Status string
	// Session and Profile are set when Status is TelegramStatusOK.
	Session Session
	Profile Profile
	// LinkURL is set when Status is TelegramStatusLinkRequired.
	LinkURL string
}

// telegramUser is the part of a verified initData payload the service needs.
type telegramUser struct {
	ID        string
	FirstName string
	LastName  string
	Username  string
}

// VerifyInitData validates a Mini App initData payload with the bot token and
// returns the Telegram user it belongs to. The signature covers every field,
// and auth_date must be younger than InitDataMaxAge.
func (s *Service) verifyInitData(initData string) (telegramUser, error) {
	if s.cfg.TelegramBotToken == "" {
		return telegramUser{}, ErrTelegramDisabled
	}
	values, err := unibotwebapp.ValidateTelegramInitData(initData, s.cfg.TelegramBotToken, s.cfg.InitDataMaxAge)
	if err != nil {
		return telegramUser{}, fmt.Errorf("%w: %v", ErrInvalidInitData, err)
	}
	rawUser := values.Get("user")
	if rawUser == "" {
		return telegramUser{}, fmt.Errorf("%w: no user in initData", ErrInvalidInitData)
	}
	var payload struct {
		ID        int64  `json:"id"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Username  string `json:"username"`
	}
	if err := json.Unmarshal([]byte(rawUser), &payload); err != nil {
		return telegramUser{}, fmt.Errorf("%w: decode user: %v", ErrInvalidInitData, err)
	}
	if payload.ID <= 0 {
		return telegramUser{}, fmt.Errorf("%w: user id is missing", ErrInvalidInitData)
	}
	return telegramUser{
		ID:        strconv.FormatInt(payload.ID, 10),
		FirstName: payload.FirstName,
		LastName:  payload.LastName,
		Username:  payload.Username,
	}, nil
}

// AuthenticateTelegram turns a Mini App initData payload into a session, or
// asks the client to link the Telegram account first.
//
// The link URL is created from the signature-checked Telegram user id, never
// from a client supplied one: an attacker cannot start a binding flow for
// somebody else's Telegram account.
func (s *Service) AuthenticateTelegram(ctx context.Context, initData string) (TelegramAuthResult, error) {
	user, err := s.verifyInitData(initData)
	if err != nil {
		return TelegramAuthResult{}, err
	}

	binding, err := s.st.Queries().GetBinding(ctx, db.GetBindingParams{
		Platform:     string(unibot.PlatformTelegram),
		RemoteUserID: user.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Telegram private chats use the user id as the chat id, so the binding
		// gets a usable conversation from the start.
		linkURL, err := s.BeginMessengerBinding(ctx, unibot.PlatformTelegram, user.ID, user.ID)
		if err != nil {
			return TelegramAuthResult{}, err
		}
		return TelegramAuthResult{Status: TelegramStatusLinkRequired, LinkURL: linkURL}, nil
	}
	if err != nil {
		return TelegramAuthResult{}, fmt.Errorf("resolve telegram binding: %w", err)
	}

	principalID := formatUUID(binding.UserID)
	session, err := s.CreateSession(ctx, principalID, SessionKindBearer)
	if err != nil {
		return TelegramAuthResult{}, err
	}
	profile, err := s.Profile(ctx, principalID)
	if err != nil {
		return TelegramAuthResult{}, err
	}
	return TelegramAuthResult{Status: TelegramStatusOK, Session: session, Profile: profile}, nil
}

// checkTelegramBindingUser verifies that a pending binding belongs to the
// Telegram user of a fresh initData payload. It is used by the login page before
// the SSO round trip.
func (s *Service) checkTelegramBindingUser(row db.AuthPending, initData string) (telegramUser, error) {
	user, err := s.verifyInitData(initData)
	if err != nil {
		return telegramUser{}, err
	}
	if user.ID != row.RemoteUserID {
		return telegramUser{}, ErrIdentityMismatch
	}
	return user, nil
}

// MarkInitDataVerified records that the Telegram user of the pending binding
// proved control of the account. A Telegram binding cannot be confirmed
// without it.
func (s *Service) MarkInitDataVerified(ctx context.Context, state, initData string) error {
	row, err := s.loadPending(ctx, state)
	if err != nil {
		return err
	}
	if row.Flow != FlowBind || row.Platform != string(unibot.PlatformTelegram) {
		return ErrPendingUnusable
	}
	user, err := s.checkTelegramBindingUser(row, initData)
	if err != nil {
		return err
	}
	if _, err := s.st.Queries().VerifyPendingAuthInitData(ctx, db.VerifyPendingAuthInitDataParams{
		StateHash:    row.StateHash,
		RemoteUserID: user.ID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPendingUnusable
		}
		return fmt.Errorf("mark initData verified: %w", err)
	}
	return nil
}
