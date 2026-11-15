package matrix

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strings"
	"sync"
	"time"

	unibot "github.com/maksimkurb/cublyverse/unibot"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto/cryptohelper"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

type Config struct {
	Homeserver   string
	Username     string
	Password     string
	CryptoDBPath string
	PickleKey    []byte
}

type Adapter struct {
	cfg          Config
	interactions unibot.InteractionStore
	client       *mautrix.Client
	crypto       *cryptohelper.CryptoHelper
	events       chan unibot.Event
	cancel       context.CancelFunc
	closeOnce    sync.Once
	syncWG       sync.WaitGroup
}

func New(cfg Config, interactions unibot.InteractionStore) (*Adapter, error) {
	if cfg.Homeserver == "" || cfg.Username == "" || cfg.Password == "" {
		return nil, errors.New("matrix homeserver, username and password are required")
	}
	if cfg.CryptoDBPath == "" {
		cfg.CryptoDBPath = "matrix-crypto.db"
	}
	if len(cfg.PickleKey) < 16 {
		return nil, errors.New("matrix pickle key must be at least 16 bytes")
	}
	if interactions == nil {
		return nil, errors.New("interaction store is required")
	}
	client, err := mautrix.NewClient(cfg.Homeserver, "", "")
	if err != nil {
		return nil, err
	}
	return &Adapter{
		cfg:          cfg,
		interactions: interactions,
		client:       client,
		events:       make(chan unibot.Event, 128),
	}, nil
}

func (a *Adapter) Platform() unibot.Platform { return unibot.PlatformMatrix }

func (a *Adapter) Capabilities() unibot.Capabilities {
	return unibot.Capabilities{
		Text:               true,
		Edit:               true,
		Delete:             true,
		Reactions:          true,
		NativeActions:      false, // rendered as seeded reactions in Element
		NativeChoice:       true,  // Element polls (MSC3381)
		EmbeddedWebApp:     true,  // Element widget, if the bot can write widget room state
		EndToEndEncryption: true,
	}
}

func (a *Adapter) Events() <-chan unibot.Event { return a.events }

func (a *Adapter) Start(parent context.Context) error {
	syncer, ok := a.client.Syncer.(*mautrix.DefaultSyncer)
	if !ok {
		return errors.New("matrix client is not using DefaultSyncer")
	}

	syncer.OnEventType(event.EventMessage, a.onMessage)
	syncer.OnEventType(event.EventReaction, a.onReaction)
	syncer.OnEventType(event.EventUnstablePollResponse, a.onPollResponse)
	syncer.OnEventType(event.StateMember, a.onMember)

	helper, err := cryptohelper.NewCryptoHelper(a.client, a.cfg.PickleKey, a.cfg.CryptoDBPath)
	if err != nil {
		return fmt.Errorf("create crypto helper: %w", err)
	}
	helper.LoginAs = &mautrix.ReqLogin{
		Type: mautrix.AuthTypePassword,
		Identifier: mautrix.UserIdentifier{
			Type: mautrix.IdentifierTypeUser,
			User: a.cfg.Username,
		},
		Password: a.cfg.Password,
	}
	if err := helper.Init(parent); err != nil {
		return fmt.Errorf("init matrix crypto: %w", err)
	}
	a.crypto = helper
	a.client.Crypto = helper

	ctx, cancel := context.WithCancel(parent)
	a.cancel = cancel
	a.syncWG.Add(1)
	go func() {
		defer a.syncWG.Done()
		defer close(a.events)
		_ = a.client.SyncWithContext(ctx)
	}()
	return nil
}

func (a *Adapter) Close() error {
	var out error
	a.closeOnce.Do(func() {
		if a.cancel != nil {
			a.cancel()
		}
		a.syncWG.Wait()
		if a.crypto != nil {
			out = a.crypto.Close()
		}
	})
	return out
}

func (a *Adapter) onMember(ctx context.Context, evt *event.Event) {
	if evt.GetStateKey() != a.client.UserID.String() {
		return
	}
	member := evt.Content.AsMember()
	if member == nil || member.Membership != event.MembershipInvite {
		return
	}
	_, _ = a.client.JoinRoomByID(ctx, evt.RoomID)
}

func (a *Adapter) onMessage(ctx context.Context, evt *event.Event) {
	if evt.Sender == a.client.UserID {
		return
	}
	content := evt.Content.AsMessage()
	if content == nil {
		return
	}
	message := &unibot.IncomingMessage{Text: content.Body}
	if command, ok := unibot.ParseCommand(content.Body); ok {
		message.Command = &command
	}
	a.emit(ctx, unibot.Event{
		Kind:           unibot.EventMessage,
		Platform:       unibot.PlatformMatrix,
		RemoteUserID:   evt.Sender.String(),
		ConversationID: evt.RoomID.String(),
		RemoteEventID:  evt.ID.String(),
		ReceivedAt:     time.Now(),
		Message:        message,
	})
}

// Ack implements unibot.Acknowledger. A Matrix reaction needs no
// acknowledgement, so the call is accepted and ignored.
func (a *Adapter) Ack(context.Context, string, string, bool) error { return nil }

func (a *Adapter) onReaction(ctx context.Context, evt *event.Event) {
	if evt.Sender == a.client.UserID {
		return // seed reaction from the bot itself
	}
	content := evt.Content.AsReaction()
	if content == nil {
		return
	}
	target := content.RelatesTo.EventID.String()
	key := content.RelatesTo.Key
	interaction, ok, err := a.interactions.ResolveMessageKey(ctx, unibot.PlatformMatrix, evt.RoomID.String(), target, key)
	if err != nil || !ok {
		return
	}
	if interaction.ExpectedUserID != "" && interaction.ExpectedUserID != evt.Sender.String() {
		return
	}
	a.emit(ctx, unibot.Event{
		Kind:           unibot.EventAction,
		Platform:       unibot.PlatformMatrix,
		RemoteUserID:   evt.Sender.String(),
		ConversationID: evt.RoomID.String(),
		RemoteEventID:  evt.ID.String(),
		ReceivedAt:     time.Now(),
		Action: &unibot.ActionEvent{
			ActionID:    interaction.ActionID,
			Payload:     interaction.Payload,
			PrincipalID: interaction.PrincipalID,
		},
	})
}

func (a *Adapter) onPollResponse(ctx context.Context, evt *event.Event) {
	if evt.Sender == a.client.UserID {
		return
	}
	content, ok := evt.Content.Parsed.(*event.PollResponseEventContent)
	if !ok || content == nil {
		return
	}
	pollID := content.RelatesTo.EventID.String()
	optionIDs := make([]string, 0, len(content.Response.Answers))
	var choiceID string
	var principalID unibot.PrincipalID
	for _, answerID := range content.Response.Answers {
		interaction, found, err := a.interactions.ResolveMessageKey(ctx, unibot.PlatformMatrix, evt.RoomID.String(), pollID, answerID)
		if err != nil || !found {
			continue
		}
		if interaction.ExpectedUserID != "" && interaction.ExpectedUserID != evt.Sender.String() {
			continue
		}
		if choiceID == "" {
			choiceID = interaction.ChoiceID
			principalID = interaction.PrincipalID
		}
		if interaction.ChoiceID != choiceID || interaction.PrincipalID != principalID {
			continue
		}
		optionIDs = append(optionIDs, interaction.OptionID)
	}
	if choiceID == "" || len(optionIDs) == 0 {
		return
	}
	a.emit(ctx, unibot.Event{
		Kind:           unibot.EventChoice,
		Platform:       unibot.PlatformMatrix,
		RemoteUserID:   evt.Sender.String(),
		ConversationID: evt.RoomID.String(),
		RemoteEventID:  evt.ID.String(),
		ReceivedAt:     time.Now(),
		Choice:         &unibot.ChoiceEvent{ChoiceID: choiceID, OptionIDs: optionIDs, PrincipalID: principalID},
	})
}

func (a *Adapter) emit(ctx context.Context, evt unibot.Event) {
	select {
	case a.events <- evt:
	case <-ctx.Done():
	}
}

func (a *Adapter) Send(ctx context.Context, address unibot.Address, message unibot.Message) (unibot.SentMessage, error) {
	if err := message.Validate(); err != nil {
		return unibot.SentMessage{}, err
	}
	roomID := id.RoomID(address.ConversationID)
	if message.Choice != nil {
		return a.sendChoice(ctx, roomID, address.RemoteUserID, message.Choice)
	}

	content, reactionMap, err := renderMessage(message)
	if err != nil {
		return unibot.SentMessage{}, err
	}
	resp, err := a.client.SendMessageEvent(ctx, roomID, event.EventMessage, content)
	if err != nil {
		return unibot.SentMessage{}, err
	}

	for reaction, action := range reactionMap {
		if err := a.interactions.PutMessageKey(ctx, unibot.PlatformMatrix, roomID.String(), resp.EventID.String(), reaction, unibot.Interaction{
			ActionID:       action.ID,
			ExpectedUserID: address.RemoteUserID,
			PrincipalID:    action.PrincipalID,
			Payload:        action.Payload,
			ExpiresAt:      time.Now().Add(actionTTL(action)),
		}); err != nil {
			return unibot.SentMessage{}, err
		}
		if _, err := a.client.SendReaction(ctx, roomID, resp.EventID, reaction); err != nil {
			return unibot.SentMessage{}, fmt.Errorf("seed matrix reaction %q: %w", reaction, err)
		}
	}

	return unibot.SentMessage{
		Platform:        unibot.PlatformMatrix,
		RemoteUserID:    address.RemoteUserID,
		ConversationID:  roomID.String(),
		RemoteMessageID: resp.EventID.String(),
	}, nil
}

func (a *Adapter) sendChoice(ctx context.Context, roomID id.RoomID, remoteUserID string, choice *unibot.Choice) (unibot.SentMessage, error) {
	question := choice.Question
	if question == "" {
		question = "Choose an option"
	}
	maxSelections := choice.MaxSelections
	if maxSelections <= 0 {
		maxSelections = 1
	}
	kind := "org.matrix.msc3381.poll.undisclosed"
	if choice.Disclosed {
		kind = "org.matrix.msc3381.poll.disclosed"
	}

	options := make([]event.PollOption, 0, len(choice.Options))
	type mapping struct {
		wireID   string
		optionID string
	}
	mappings := make([]mapping, 0, len(choice.Options))
	for _, option := range choice.Options {
		wireID, err := unibot.RandomToken(12)
		if err != nil {
			return unibot.SentMessage{}, err
		}
		options = append(options, event.PollOption{
			ID:             wireID,
			MSC1767Message: event.MSC1767Message{Text: option.Label},
		})
		mappings = append(mappings, mapping{wireID: wireID, optionID: option.ID})
	}

	content := &event.PollStartEventContent{
		PollStart: event.PollStart{
			Kind:          kind,
			MaxSelections: maxSelections,
			Question:      event.MSC1767Message{Text: question},
			Answers:       options,
		},
	}
	resp, err := a.client.SendMessageEvent(ctx, roomID, event.EventUnstablePollStart, content)
	if err != nil {
		return unibot.SentMessage{}, err
	}
	for _, item := range mappings {
		if err := a.interactions.PutMessageKey(ctx, unibot.PlatformMatrix, roomID.String(), resp.EventID.String(), item.wireID, unibot.Interaction{
			ChoiceID:       choice.ID,
			OptionID:       item.optionID,
			ExpectedUserID: remoteUserID,
			ExpiresAt:      time.Now().Add(unibot.DefaultActionTTL),
		}); err != nil {
			return unibot.SentMessage{}, err
		}
	}
	return unibot.SentMessage{
		Platform:        unibot.PlatformMatrix,
		RemoteUserID:    remoteUserID,
		ConversationID:  roomID.String(),
		RemoteMessageID: resp.EventID.String(),
	}, nil
}

func renderMessage(message unibot.Message) (*event.MessageEventContent, map[string]unibot.Action, error) {
	// Body is the plain fallback Element shows when HTML rendering is
	// unavailable; FormattedBody is the rich variant.
	body := message.Text
	formatted := html.EscapeString(message.Text)
	formatted = strings.ReplaceAll(formatted, "\n", "<br>")
	if message.HTML != "" {
		formatted = message.HTML
		if body == "" {
			body = unibot.HTMLToText(message.HTML)
		}
	}
	hasHTML := message.HTML != ""
	reactions := make(map[string]unibot.Action)
	used := make(map[string]struct{})
	fallback := []string{"1️⃣", "2️⃣", "3️⃣", "4️⃣", "5️⃣", "6️⃣", "7️⃣", "8️⃣", "9️⃣", "🔟"}
	fallbackIndex := 0

	for _, row := range message.Actions {
		for _, action := range row {
			switch action.Kind {
			case unibot.ActionSubmit:
				icon := action.Icon
				if icon == "" {
					if fallbackIndex >= len(fallback) {
						return nil, nil, errors.New("Matrix reaction renderer supports at most 10 iconless actions")
					}
					icon = fallback[fallbackIndex]
					fallbackIndex++
				}
				if _, exists := used[icon]; exists {
					return nil, nil, fmt.Errorf("Matrix actions need unique reaction icons, duplicate %q", icon)
				}
				used[icon] = struct{}{}
				reactions[icon] = action
				body += "\n" + icon + " " + action.Label
				formatted += "<br>" + html.EscapeString(icon+" "+action.Label)
			case unibot.ActionURL, unibot.ActionOpenApp:
				prefix := action.Icon
				if prefix == "" {
					prefix = "🔗"
				}
				body += "\n" + prefix + " " + action.Label + ": " + action.URL
				formatted += "<br>" + html.EscapeString(prefix+" ") + `<a href="` + html.EscapeString(action.URL) + `">` + html.EscapeString(action.Label) + `</a>`
			default:
				return nil, nil, fmt.Errorf("unsupported action kind %q", action.Kind)
			}
			hasHTML = true
		}
	}

	content := &event.MessageEventContent{MsgType: event.MsgText, Body: body}
	if hasHTML {
		content.Format = event.FormatHTML
		content.FormattedBody = formatted
	}
	return content, reactions, nil
}

// actionTTL is how long a reaction or poll answer stays valid.
func actionTTL(action unibot.Action) time.Duration {
	if action.TTL <= 0 {
		return unibot.DefaultActionTTL
	}
	return action.TTL
}

func (a *Adapter) Edit(ctx context.Context, sent unibot.SentMessage, message unibot.Message) error {
	if message.Choice != nil || len(message.Actions) > 0 {
		return errors.New("starter Matrix editor only edits plain text; re-send interactive messages instead")
	}
	content := &event.MessageEventContent{MsgType: event.MsgText, Body: message.Text}
	if message.HTML != "" {
		content.Format = event.FormatHTML
		content.FormattedBody = message.HTML
		if content.Body == "" {
			content.Body = unibot.HTMLToText(message.HTML)
		}
	}
	content.SetEdit(id.EventID(sent.RemoteMessageID))
	_, err := a.client.SendMessageEvent(ctx, id.RoomID(sent.ConversationID), event.EventMessage, content)
	return err
}

func (a *Adapter) Delete(ctx context.Context, sent unibot.SentMessage) error {
	_, err := a.client.RedactEvent(ctx, id.RoomID(sent.ConversationID), id.EventID(sent.RemoteMessageID))
	return err
}

type WidgetSpec struct {
	ID                string
	Name              string
	URL               string
	Type              string
	WaitForIframeLoad bool
	Data              map[string]any
}

// InstallElementWidget writes Element's widget room-state event. The bot must have
// enough power level in the room. A practical deployment can let the bot create the
// canonical DM room so it has permission to install/update its widget.
func (a *Adapter) InstallElementWidget(ctx context.Context, roomID string, spec WidgetSpec) error {
	if spec.ID == "" || spec.Name == "" || spec.URL == "" {
		return errors.New("widget id, name and url are required")
	}
	if spec.Type == "" {
		spec.Type = "m.custom"
	}

	matrixRoomID := id.RoomID(roomID)
	content := map[string]any{
		"id":                spec.ID,
		"name":              spec.Name,
		"type":              spec.Type,
		"url":               spec.URL,
		"creatorUserId":     a.client.UserID.String(),
		"waitForIframeLoad": spec.WaitForIframeLoad,
		"data":              spec.Data,
	}
	if _, err := a.client.SendStateEvent(
		ctx,
		matrixRoomID,
		event.NewEventType("im.vector.modular.widgets"),
		spec.ID,
		content,
	); err != nil {
		return fmt.Errorf("install Element widget: %w", err)
	}

	// Element stores widget placement separately from the widget definition.
	// Preserve layouts for other widgets in the room instead of overwriting the
	// entire io.element.widgets.layout event.
	layoutType := event.NewEventType("io.element.widgets.layout")
	layout := map[string]any{}
	if err := a.client.StateEvent(ctx, matrixRoomID, layoutType, "", &layout); err != nil && !errors.Is(err, mautrix.MNotFound) {
		return fmt.Errorf("load Element widget layout: %w", err)
	}

	widgets, ok := layout["widgets"].(map[string]any)
	if !ok || widgets == nil {
		widgets = map[string]any{}
		layout["widgets"] = widgets
	}
	widgets[spec.ID] = map[string]any{
		"container": "top",
		"index":     0,
		"width":     100,
		"height":    320,
	}

	if _, err := a.client.SendStateEvent(ctx, matrixRoomID, layoutType, "", layout); err != nil {
		return fmt.Errorf("set Element widget layout: %w", err)
	}
	return nil
}
