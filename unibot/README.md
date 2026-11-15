# UniBot starter

A Go starter framework for one logical bot session across multiple messengers.

The framework deliberately keeps **identity**, **business state**, and **messenger transports** separate:

```text
                         OIDC issuer
                            │
                    canonical Principal
                            │
                 ┌──────────┴──────────┐
                 │                     │
          Telegram binding       Matrix binding
                 │                     │
          Telegram adapter       Element/Matrix adapter
                 │                     │
                 └──────────┬──────────┘
                            │
                      UniBot Runtime
                            │
                     your Handler
```

The same handler receives normalized `message`, `action`, and `choice` events regardless of where the user continued the conversation.

## What is implemented

- Go core with pluggable `Transport` interface.
- Canonical identity through OIDC; messenger IDs are only verified bindings.
- Telegram Bot API adapter without a Telegram-specific dependency.
- Telegram inline callbacks, URL buttons, Mini App buttons, button styles, editing and deletion.
- Matrix client adapter based on `mautrix-go` with E2EE via `cryptohelper`.
- Element-oriented actions rendered as **seeded reactions**.
- Element choices rendered as Matrix polls (MSC3381 event format supported by Element).
- Optional Element room widget installation.
- Common WebApp page which detects Telegram Mini Apps vs an Element Matrix widget.
- Delivery by principal with preferred transport order and failover.
- Generic session store contract keyed by canonical `PrincipalID`, with an in-memory demo implementation.
- In-memory reference stores for identity and interaction mappings.
- Demo application showing SSO linking, actions, choices and WebApp launching.

## Message formatting

`Message.Text` is the plain body every transport can render. `Message.HTML` is an
optional formatted variant limited to `b`, `i`, `code` and `a href`:

```go
unibot.Message{
    Text: "Первое напоминание: 15.11.2026",
    HTML: "Первое напоминание: <b>15.11.2026</b>",
}
```

- `unibot.EscapeHTML(text)` escapes user data for that subset;
  `unibot.ValidateHTML` is the validator `Message.Validate` uses. Any other tag,
  an attribute outside `a href`, an unbalanced tag or a non-http(s)/tg link is
  rejected before a transport is called.
- Telegram sends `parse_mode=HTML`.
- Matrix sends `format=org.matrix.custom.html` plus `formatted_body` and keeps
  `Text` as `body`. An HTML-only message gets `unibot.HTMLToText(HTML)` as the
  plain body.

## Commands, acknowledgements, action data

Incoming texts starting with `/` are parsed by `unibot.ParseCommand` into
`IncomingMessage.Command` (`Name`, `Addressee` from `/cmd@botname`, `Args`), so
command handling is transport independent.

A pressed button can be answered from the handler:

```go
func handle(ctx context.Context, botctx *unibot.Context, evt unibot.ResolvedEvent) error {
    if err := botctx.Ack(ctx, "Подтверждено ✅", false); err != nil {
        return err
    }
    _, err := botctx.Reply(ctx, unibot.Message{Text: "Готово"})
    return err
}
```

- The adapter no longer answers immediately: it puts the platform handle of the
  event into `Event.AckToken`.
- If the handler does not call `Ack`, the runtime sends an empty acknowledgement
  after it returns (also after a timeout or a panic), so a Telegram button never
  keeps spinning.
- `alert=true` shows a modal dialog instead of a toast.
- Matrix has no acknowledgement concept: its `Ack` is a no-op.

`Action` carries data that must survive the 64-byte Telegram `callback_data`
limit:

| Field | Meaning |
|---|---|
| `Payload` | opaque string up to `MaxActionPayloadBytes` (512), returned in `ActionEvent.Payload` |
| `PrincipalID` | optional restriction to one SSO account |
| `TTL` | how long the button stays valid; zero means `DefaultActionTTL` (24h) |

The interaction registry stores them (`Interaction.Payload`,
`Interaction.PrincipalID`, `Interaction.ExpiresAt`). An expired button is not
resolved, so it never reaches the handler.

## Runtime guarantees

`unibot.Runtime` serializes events of one remote user, runs different users in
parallel and bounds the work:

- at most `MaxConcurrentHandlers` handlers (default 32) run at once;
- at most `MaxPendingEvents` (1024) events wait for their user, and the
  transport hand-off channel is bounded — a flood creates backpressure, not
  goroutines;
- each handler gets a context with `HandlerTimeout` (default 30s);
- handler errors, `Resolve` errors, timeouts and panics are logged through the
  optional `*slog.Logger`; a panic in one handler does not stop the bot;
- `Close` cancels the context, closes the transports, waits for the scheduler
  and the handlers and is safe to call twice.

```go
runtime, err := unibot.NewRuntimeWithConfig(unibot.RuntimeConfig{
    Logger:         logger,
    HandlerTimeout: 30 * time.Second,
}, identityStore, handler, transports...)
```

## Telegram: private chats, commands, menu

The Telegram adapter serves **private chats only**: messages and callback
queries from groups, supergroups and channels are dropped (callback queries are
still acknowledged so the client stops its spinner).

`telegram.Config` can publish the command menu and the Mini App menu button on
start:

```go
adapter, err := telegram.New(telegram.Config{
    Token:    token,
    Logger:   logger,
    Commands: []telegram.Command{
        {Command: "start", Description: "начать работу"},
        {Command: "help", Description: "список команд"},
    },
    WebAppURL: "https://bot.example.com/app/", // https only
}, interactions)
```

`setMyCommands` is registered for `all_private_chats` with `language_code=ru`;
`setChatMenuButton` creates a `web_app` button. Both failures are logged as
warnings and never fail the start — a bot without a menu still works.


## Semantic UI

Application code never creates Telegram buttons or Matrix events directly:

```go
unibot.Message{
    Text: "Delete reminder?",
    Actions: [][]unibot.Action{{
        {
            ID:    "delete",
            Label: "Delete",
            Icon:  "🗑️",
            Kind:  unibot.ActionSubmit,
            Style: unibot.ActionStyleDanger,
        },
        {
            ID:    "cancel",
            Label: "Cancel",
            Icon:  "❌",
            Kind:  unibot.ActionSubmit,
        },
    }},
}
```

Telegram renders this as native inline callback buttons.

Element renders the same semantic actions as a legend plus seeded reactions:

```text
Delete reminder?
🗑️ Delete
❌ Cancel

🗑️ 1   ❌ 1
```

When the user joins a reaction, the Matrix adapter resolves `(room_id, message_event_id, reaction_key)` back to `ActionID`. Both transports therefore emit:

```go
unibot.Event{
    Kind: unibot.EventAction,
    Action: &unibot.ActionEvent{ActionID: "delete"},
}
```

For a real selection rather than a command, use `Choice`. Telegram renders it with inline buttons; Element renders it as a poll and maps the poll answer ID back to your domain option ID.

## Adding a third messenger

Implement only this interface:

```go
type Transport interface {
    Platform() Platform
    Capabilities() Capabilities
    Start(context.Context) error
    Close() error
    Events() <-chan Event
    Send(context.Context, Address, Message) (SentMessage, error)
    Edit(context.Context, SentMessage, Message) error
    Delete(context.Context, SentMessage) error
}
```

A WhatsApp adapter, for example, maps `ActionSubmit` to interactive reply buttons and emits the same `EventAction`. Core, OIDC, sessions and application handlers do not change.

## Requirements

This starter is part of the Cublyverse module and targets the Go version in
`go.mod` (**Go 1.27.4**); `mautrix-go` is pinned there as well.

The Matrix bot account is a normal Matrix client/device, not an Application Service. Its crypto DB must be persisted.

## Demo configuration

Copy the repository's `.env.example` into your deployment environment and set at
least one transport.

```bash
export PUBLIC_BASE_URL=https://bot.example.com
export HTTP_ADDR=:8080

export OIDC_ISSUER=https://auth.example.com/application/o/unibot/
export OIDC_CLIENT_ID=unibot
export OIDC_CLIENT_SECRET=...
export OIDC_REDIRECT_URL=https://bot.example.com/auth/callback

export TELEGRAM_BOT_TOKEN=...

export MATRIX_HOMESERVER=https://matrix.example.com
export MATRIX_USERNAME=unibot
export MATRIX_PASSWORD=...
export MATRIX_PICKLE_KEY='replace-with-a-long-random-secret'
export MATRIX_CRYPTO_DB=/data/matrix-crypto.db
```

Run the demo from the repository root:

```bash
go run ./unibot/cmd/demo
```

The demo is a starter, not a deployable image: the production image of
Cublyverse is built from `deploy/Dockerfile` and embeds `unibot` as a package.
The repository CI (`.github/workflows/ci.yml`) runs `make check`, which includes
`go test ./...`, `go vet ./...`, the WebApp checks and the documentation checks.

An unlinked user receives a **Sign in** action. After OIDC callback, the framework stores:

```text
OIDC principal <-> telegram user ID / chat ID
OIDC principal <-> Matrix MXID / room ID
```

The in-memory implementations are intentionally only a demo. Replace `identity/memory`, `interaction/memory`, and `session/memory` with PostgreSQL/SQLite implementations for production.

## Demo interactions

After SSO linking, send any text to the bot. The demo increments a session counter keyed only by the OIDC `PrincipalID`, then sends:

1. The shared session turn number (switch Telegram → Element and it keeps increasing).
2. An `approve/reject` action message.
3. A three-option `Choice` (Element poll).
4. A WebApp action if `PUBLIC_BASE_URL` is configured.

In an Element room, `/widget` asks the Matrix adapter to install the demo as a room widget. The bot needs sufficient power level to write the widget state event. A clean deployment model is to have the bot create the canonical DM room after Matrix linking, so it owns the room state it needs.

## WebApp

`/app/` is the same HTML application for both backends.

- In Telegram it uses `window.Telegram.WebApp`.
- In Element it starts `matrix-widget-api` and reads the Element-substituted widget context parameters.
- Outside a messenger it works as a normal browser page.

Do **not** trust WebApp query parameters as identity. Telegram Mini App `initData` has a validator in `webapp/telegram_init.go`; Matrix widget metadata is host context. Authentication and cross-messenger identity come from OIDC.

## Security / production TODOs

The starter intentionally marks the boundaries instead of hiding them:

- Replace both memory stores with persistent transactional stores.
- Add idempotency/consumption policy for actions according to your application semantics.
- Add rate limits to OIDC/linking and WebApp endpoints.
- Store the Matrix pickle key in a secret manager and persist the crypto DB.
- Decide the Matrix device trust/cross-signing policy you require.
- Restrict the bot to expected DM rooms/users if it will not be used in groups.
- Add webhook mode for Telegram if you prefer it over long polling.
- Add a signed application session for WebApp API calls after OIDC login.
- Keep SSO linking in a one-user DM. If group-room linking is ever allowed, add a platform proof step before binding the messenger identity to the OIDC principal.
- Add media normalization once the core message contract is stable.

See `docs/ARCHITECTURE.md` for the design boundaries.
