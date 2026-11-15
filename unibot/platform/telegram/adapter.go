package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	unibot "github.com/maksimkurb/cublyverse/unibot"
)

type Config struct {
	Token       string
	BaseURL     string
	PollTimeout time.Duration
	HTTPClient  *http.Client
}

type Adapter struct {
	cfg          Config
	interactions unibot.InteractionStore
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
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: cfg.PollTimeout + 10*time.Second}
	}
	return &Adapter{
		cfg:          cfg,
		interactions: interactions,
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
	return nil
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
		a.emit(ctx, unibot.Event{
			Kind:           unibot.EventMessage,
			Platform:       unibot.PlatformTelegram,
			RemoteUserID:   strconv.FormatInt(update.Message.From.ID, 10),
			ConversationID: strconv.FormatInt(update.Message.Chat.ID, 10),
			RemoteEventID:  strconv.FormatInt(update.Message.MessageID, 10),
			ReceivedAt:     now,
			Message:        &unibot.IncomingMessage{Text: update.Message.Text},
		})
		return
	}

	q := update.CallbackQuery
	if q == nil || q.Message == nil {
		return
	}
	_ = a.answerCallbackQuery(ctx, q.ID)
	if !strings.HasPrefix(q.Data, "u:") {
		return
	}
	interaction, ok, err := a.interactions.ResolveToken(ctx, strings.TrimPrefix(q.Data, "u:"))
	if err != nil || !ok {
		return
	}
	remoteUserID := strconv.FormatInt(q.From.ID, 10)
	if interaction.ExpectedUserID != "" && interaction.ExpectedUserID != remoteUserID {
		return
	}
	base := unibot.Event{
		Platform:       unibot.PlatformTelegram,
		RemoteUserID:   remoteUserID,
		ConversationID: strconv.FormatInt(q.Message.Chat.ID, 10),
		RemoteEventID:  q.ID,
		ReceivedAt:     now,
	}
	if interaction.ChoiceID != "" {
		base.Kind = unibot.EventChoice
		base.Choice = &unibot.ChoiceEvent{ChoiceID: interaction.ChoiceID, OptionIDs: []string{interaction.OptionID}}
	} else {
		base.Kind = unibot.EventAction
		base.Action = &unibot.ActionEvent{ActionID: interaction.ActionID}
	}
	a.emit(ctx, base)
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
		"text":    message.Text,
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
			ExpiresAt:      time.Now().Add(24 * time.Hour),
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
					ExpiresAt:      time.Now().Add(24 * time.Hour),
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
		"text":       message.Text,
	}
	if len(keyboard.InlineKeyboard) > 0 {
		payload["reply_markup"] = keyboard
	}
	return a.call(ctx, "editMessageText", payload, nil)
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

func (a *Adapter) answerCallbackQuery(ctx context.Context, id string) error {
	return a.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": id}, nil)
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
	ID int64 `json:"id"`
}

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
