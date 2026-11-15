package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	unibot "github.com/maksimkurb/cublyverse/unibot"
)

// Command is one entry of the bot command menu published with setMyCommands.
type Command struct {
	// Command is the name without the leading slash, for example "help".
	Command     string `json:"command"`
	Description string `json:"description"`
}

type Config struct {
	Token       string
	BaseURL     string
	PollTimeout time.Duration
	HTTPClient  *http.Client
	// Logger records setup and background failures. Nil means slog.Default().
	Logger *slog.Logger
	// Commands are published with setMyCommands on start. Empty skips the call.
	Commands []Command
	// WebAppURL is the HTTPS address of the Mini App used by the chat menu
	// button (setChatMenuButton). Empty skips the call.
	WebAppURL string
	// MenuButtonText is the label of the menu button. Defaults to a Russian
	// "Открыть приложение".
	MenuButtonText string
}

type Adapter struct {
	cfg          Config
	interactions unibot.InteractionStore
	logger       *slog.Logger
	http         *http.Client
	events       chan unibot.Event
	cancel       context.CancelFunc
	closeOnce    sync.Once
}

func New(cfg Config, interactions unibot.InteractionStore) (*Adapter, error) {
	if cfg.Token == "" {
		return nil, errors.New("telegram token is required")
	}
	if interactions == nil {
		return nil, errors.New("interaction store is required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.telegram.org"
	}
	if cfg.PollTimeout <= 0 {
		cfg.PollTimeout = 30 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.MenuButtonText == "" {
		cfg.MenuButtonText = "Открыть приложение"
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: cfg.PollTimeout + 10*time.Second}
	}
	return &Adapter{
		cfg:          cfg,
		interactions: interactions,
		logger:       cfg.Logger,
		http:         client,
		events:       make(chan unibot.Event, 128),
	}, nil
}

func (a *Adapter) Platform() unibot.Platform { return unibot.PlatformTelegram }

func (a *Adapter) Capabilities() unibot.Capabilities {
	return unibot.Capabilities{
		Text:               true,
		Edit:               true,
		Delete:             true,
		NativeActions:      true,
		NativeChoice:       true,
		EmbeddedWebApp:     true,
		EndToEndEncryption: false,
	}
}

func (a *Adapter) Events() <-chan unibot.Event { return a.events }

func (a *Adapter) Start(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	a.cancel = cancel
	go a.poll(ctx)
	go a.setup(ctx)
	return nil
}

// setup publishes the command menu and the Mini App menu button. The bot works
// without them, so failures are logged as warnings instead of failing the
// start: a messenger outage must not stop message delivery.
func (a *Adapter) setup(ctx context.Context) {
	if len(a.cfg.Commands) > 0 {
		payload := map[string]any{
			"commands":      a.cfg.Commands,
			"scope":         map[string]any{"type": "all_private_chats"},
			"language_code": "ru",
		}
		if err := a.call(ctx, "setMyCommands", payload, nil); err != nil {
			a.logger.WarnContext(ctx, "telegram setMyCommands failed", slog.String("error", err.Error()))
		}
	}
	if a.cfg.WebAppURL != "" {
		payload := map[string]any{
			"menu_button": map[string]any{
				"type":    "web_app",
				"text":    a.cfg.MenuButtonText,
				"web_app": map[string]any{"url": a.cfg.WebAppURL},
			},
		}
		if err := a.call(ctx, "setChatMenuButton", payload, nil); err != nil {
			a.logger.WarnContext(ctx, "telegram setChatMenuButton failed", slog.String("error", err.Error()))
		}
	}
}

func (a *Adapter) Close() error {
	a.closeOnce.Do(func() {
		if a.cancel != nil {
			a.cancel()
		}
	})
	return nil
}

func (a *Adapter) poll(ctx context.Context) {
	defer close(a.events)
	var offset int64
	for ctx.Err() == nil {
		updates, err := a.getUpdates(ctx, offset)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		for _, update := range updates {
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
			a.handleUpdate(ctx, update)
		}
	}
}

func (a *Adapter) handleUpdate(ctx context.Context, update tgUpdate) {
	now := time.Now()
	if update.Message != nil && update.Message.From != nil {
		// The bot only serves private chats: group and channel messages are
		// ignored, so no reminder or binding can leak into a shared room.
		if update.Message.Chat.Type != chatTypePrivate {
			return
		}
		message := &unibot.IncomingMessage{Text: update.Message.Text}
		if command, ok := unibot.ParseCommand(update.Message.Text); ok {
			message.Command = &command
		}
		a.emit(ctx, unibot.Event{
			Kind:           unibot.EventMessage,
			Platform:       unibot.PlatformTelegram,
			RemoteUserID:   strconv.FormatInt(update.Message.From.ID, 10),
			ConversationID: strconv.FormatInt(update.Message.Chat.ID, 10),
			RemoteEventID:  strconv.FormatInt(update.Message.MessageID, 10),
			ReceivedAt:     now,
			Message:        message,
		})
		return
	}

	q := update.CallbackQuery
	if q == nil || q.Message == nil {
		return
	}
	// Callback queries are only served from private chats. Telegram expects an
	// answer for every callback query, so the dropped ones are acknowledged
	// here; handled ones are acknowledged by the runtime or the handler.
	if q.Message.Chat.Type != chatTypePrivate {
		a.answerQuietly(ctx, q.ID)
		return
	}
	if !strings.HasPrefix(q.Data, "u:") {
		a.answerQuietly(ctx, q.ID)
		return
	}
	interaction, ok, err := a.interactions.ResolveToken(ctx, strings.TrimPrefix(q.Data, "u:"))
	if err != nil {
		a.logger.ErrorContext(ctx, "resolve callback token failed", slog.String("error", err.Error()))
		a.answerQuietly(ctx, q.ID)
		return
	}
	if !ok {
		// The button expired or was already cleaned up: stop the spinner.
		a.answerQuietly(ctx, q.ID)
		return
	}
	remoteUserID := strconv.FormatInt(q.From.ID, 10)
	if interaction.ExpectedUserID != "" && interaction.ExpectedUserID != remoteUserID {
		a.answerQuietly(ctx, q.ID)
		return
	}
	base := unibot.Event{
		Platform:       unibot.PlatformTelegram,
		RemoteUserID:   remoteUserID,
		ConversationID: strconv.FormatInt(q.Message.Chat.ID, 10),
		RemoteEventID:  q.ID,
		ReceivedAt:     now,
		// The callback stays unanswered until the handler calls Context.Ack or
		// the runtime sends an empty acknowledgement.
		AckToken: q.ID,
	}
	if interaction.ChoiceID != "" {
		base.Kind = unibot.EventChoice
		base.Choice = &unibot.ChoiceEvent{
			ChoiceID:    interaction.ChoiceID,
			OptionIDs:   []string{interaction.OptionID},
			PrincipalID: interaction.PrincipalID,
		}
	} else {
		base.Kind = unibot.EventAction
		base.Action = &unibot.ActionEvent{
			ActionID:    interaction.ActionID,
			Payload:     interaction.Payload,
			PrincipalID: interaction.PrincipalID,
		}
	}
	a.emit(ctx, base)
}

// answerQuietly acknowledges a callback query that produces no event.
func (a *Adapter) answerQuietly(ctx context.Context, id string) {
	if err := a.Ack(ctx, id, "", false); err != nil {
		a.logger.WarnContext(ctx, "telegram answerCallbackQuery failed", slog.String("error", err.Error()))
	}
}

// Ack implements unibot.Acknowledger with answerCallbackQuery.
func (a *Adapter) Ack(ctx context.Context, token, text string, alert bool) error {
	if token == "" {
		return nil
	}
	payload := map[string]any{"callback_query_id": token}
	if text != "" {
		payload["text"] = text
	}
	if alert {
		payload["show_alert"] = true
	}
	return a.call(ctx, "answerCallbackQuery", payload, nil)
}

func (a *Adapter) emit(ctx context.Context, event unibot.Event) {
	select {
	case a.events <- event:
	case <-ctx.Done():
	}
}

func (a *Adapter) Send(ctx context.Context, address unibot.Address, message unibot.Message) (unibot.SentMessage, error) {
	if err := message.Validate(); err != nil {
		return unibot.SentMessage{}, err
	}

	if message.Choice != nil {
		return a.sendChoice(ctx, address, message.Choice)
	}

	keyboard, err := a.renderKeyboard(ctx, address, message.Actions)
	if err != nil {
		return unibot.SentMessage{}, err
	}
	payload := map[string]any{
		"chat_id": chatIDValue(address.ConversationID),
		"text":    textPayload(message),
	}
	if message.HTML != "" {
		payload["parse_mode"] = "HTML"
	}
	if len(keyboard.InlineKeyboard) > 0 {
		payload["reply_markup"] = keyboard
	}
	var result tgMessage
	if err := a.call(ctx, "sendMessage", payload, &result); err != nil {
		return unibot.SentMessage{}, err
	}
	return unibot.SentMessage{
		Platform:        unibot.PlatformTelegram,
		RemoteUserID:    address.RemoteUserID,
		ConversationID:  address.ConversationID,
		RemoteMessageID: strconv.FormatInt(result.MessageID, 10),
	}, nil
}

func (a *Adapter) sendChoice(ctx context.Context, address unibot.Address, choice *unibot.Choice) (unibot.SentMessage, error) {
	rows := make([][]tgInlineKeyboardButton, 0, len(choice.Options))
	for _, option := range choice.Options {
		token, err := unibot.RandomToken(16)
		if err != nil {
			return unibot.SentMessage{}, err
		}
		if err := a.interactions.PutToken(ctx, token, unibot.Interaction{
			ChoiceID:       choice.ID,
			OptionID:       option.ID,
			ExpectedUserID: address.RemoteUserID,
			ExpiresAt:      time.Now().Add(unibot.DefaultActionTTL),
		}); err != nil {
			return unibot.SentMessage{}, err
		}
		rows = append(rows, []tgInlineKeyboardButton{{Text: option.Label, CallbackData: "u:" + token}})
	}
	question := choice.Question
	if question == "" {
		question = "Choose an option"
	}
	payload := map[string]any{
		"chat_id":      chatIDValue(address.ConversationID),
		"text":         question,
		"reply_markup": tgInlineKeyboardMarkup{InlineKeyboard: rows},
	}
	var result tgMessage
	if err := a.call(ctx, "sendMessage", payload, &result); err != nil {
		return unibot.SentMessage{}, err
	}
	return unibot.SentMessage{
		Platform:        unibot.PlatformTelegram,
		RemoteUserID:    address.RemoteUserID,
		ConversationID:  address.ConversationID,
		RemoteMessageID: strconv.FormatInt(result.MessageID, 10),
	}, nil
}

func (a *Adapter) renderKeyboard(ctx context.Context, address unibot.Address, actions [][]unibot.Action) (tgInlineKeyboardMarkup, error) {
	markup := tgInlineKeyboardMarkup{InlineKeyboard: make([][]tgInlineKeyboardButton, 0, len(actions))}
	for _, row := range actions {
		buttons := make([]tgInlineKeyboardButton, 0, len(row))
		for _, action := range row {
			button := tgInlineKeyboardButton{Text: label(action), Style: telegramStyle(action.Style)}
			switch action.Kind {
			case unibot.ActionURL:
				button.URL = action.URL
			case unibot.ActionOpenApp:
				button.WebApp = &tgWebAppInfo{URL: action.URL}
			case unibot.ActionSubmit:
				token, err := unibot.RandomToken(16)
				if err != nil {
					return tgInlineKeyboardMarkup{}, err
				}
				if err := a.interactions.PutToken(ctx, token, unibot.Interaction{
					ActionID:       action.ID,
					ExpectedUserID: address.RemoteUserID,
					PrincipalID:    action.PrincipalID,
					Payload:        action.Payload,
					ExpiresAt:      time.Now().Add(actionTTL(action)),
				}); err != nil {
					return tgInlineKeyboardMarkup{}, err
				}
				button.CallbackData = "u:" + token
			default:
				return tgInlineKeyboardMarkup{}, fmt.Errorf("unsupported action kind %q", action.Kind)
			}
			buttons = append(buttons, button)
		}
		markup.InlineKeyboard = append(markup.InlineKeyboard, buttons)
	}
	return markup, nil
}

func label(action unibot.Action) string {
	if action.Icon == "" {
		return action.Label
	}
	return action.Icon + " " + action.Label
}

// actionTTL is how long a submit button stays valid.
func actionTTL(action unibot.Action) time.Duration {
	if action.TTL <= 0 {
		return unibot.DefaultActionTTL
	}
	return action.TTL
}

func telegramStyle(style unibot.ActionStyle) string {
	switch style {
	case unibot.ActionStylePrimary:
		return "primary"
	case unibot.ActionStyleSuccess:
		return "success"
	case unibot.ActionStyleDanger:
		return "danger"
	default:
		return ""
	}
}

func (a *Adapter) Edit(ctx context.Context, sent unibot.SentMessage, message unibot.Message) error {
	if message.Choice != nil {
		return errors.New("editing choice messages is not implemented in starter adapter")
	}
	keyboard, err := a.renderKeyboard(ctx, unibot.Address{
		Platform:       unibot.PlatformTelegram,
		RemoteUserID:   sent.RemoteUserID,
		ConversationID: sent.ConversationID,
	}, message.Actions)
	if err != nil {
		return err
	}
	messageID, err := strconv.ParseInt(sent.RemoteMessageID, 10, 64)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"chat_id":    chatIDValue(sent.ConversationID),
		"message_id": messageID,
		"text":       textPayload(message),
	}
	if message.HTML != "" {
		payload["parse_mode"] = "HTML"
	}
	if len(keyboard.InlineKeyboard) > 0 {
		payload["reply_markup"] = keyboard
	}
	return a.call(ctx, "editMessageText", payload, nil)
}

// textPayload returns what Telegram should render: the HTML variant when it is
// set, the plain text otherwise.
func textPayload(message unibot.Message) string {
	if message.HTML != "" {
		return message.HTML
	}
	return message.Text
}

func (a *Adapter) Delete(ctx context.Context, sent unibot.SentMessage) error {
	messageID, err := strconv.ParseInt(sent.RemoteMessageID, 10, 64)
	if err != nil {
		return err
	}
	return a.call(ctx, "deleteMessage", map[string]any{
		"chat_id":    chatIDValue(sent.ConversationID),
		"message_id": messageID,
	}, nil)
}

func (a *Adapter) getUpdates(ctx context.Context, offset int64) ([]tgUpdate, error) {
	var result []tgUpdate
	err := a.call(ctx, "getUpdates", map[string]any{
		"offset":          offset,
		"timeout":         int(a.cfg.PollTimeout.Seconds()),
		"allowed_updates": []string{"message", "callback_query"},
	}, &result)
	return result, err
}

func (a *Adapter) call(ctx context.Context, method string, payload any, result any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	url := strings.TrimRight(a.cfg.BaseURL, "/") + "/bot" + a.cfg.Token + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	var envelope struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("telegram %s: decode: %w", method, err)
	}
	if !envelope.OK {
		return fmt.Errorf("telegram %s: %s", method, envelope.Description)
	}
	if result != nil && len(envelope.Result) > 0 {
		if err := json.Unmarshal(envelope.Result, result); err != nil {
			return fmt.Errorf("telegram %s result: %w", method, err)
		}
	}
	return nil
}

func chatIDValue(value string) any {
	if id, err := strconv.ParseInt(value, 10, 64); err == nil {
		return id
	}
	return value
}

type tgUpdate struct {
	UpdateID      int64            `json:"update_id"`
	Message       *tgMessage       `json:"message"`
	CallbackQuery *tgCallbackQuery `json:"callback_query"`
}

type tgCallbackQuery struct {
	ID      string     `json:"id"`
	From    tgUser     `json:"from"`
	Message *tgMessage `json:"message"`
	Data    string     `json:"data"`
}

type tgMessage struct {
	MessageID int64   `json:"message_id"`
	From      *tgUser `json:"from"`
	Chat      tgChat  `json:"chat"`
	Text      string  `json:"text"`
}

type tgUser struct {
	ID int64 `json:"id"`
}

type tgChat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

// chatTypePrivate is the only chat type this bot serves.
const chatTypePrivate = "private"

type tgInlineKeyboardMarkup struct {
	InlineKeyboard [][]tgInlineKeyboardButton `json:"inline_keyboard"`
}

type tgInlineKeyboardButton struct {
	Text         string        `json:"text"`
	Style        string        `json:"style,omitempty"`
	URL          string        `json:"url,omitempty"`
	CallbackData string        `json:"callback_data,omitempty"`
	WebApp       *tgWebAppInfo `json:"web_app,omitempty"`
}

type tgWebAppInfo struct {
	URL string `json:"url"`
}
