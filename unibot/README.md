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

This starter targets **Go 1.26** because `mautrix-go v0.31.0` requires it.

The Matrix bot account is a normal Matrix client/device, not an Application Service. Its crypto DB must be persisted.

## Demo configuration

Copy `.env.example` into your deployment environment and set at least one transport.

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

Then:

```bash
go mod tidy
go run ./cmd/demo
```

Or build the included container:

```bash
docker build -t unibot-demo .
docker run --rm --env-file .env -p 8080:8080 -v "$PWD/data:/data" unibot-demo
```

A minimal GitHub Actions workflow runs `go test ./...` and `go vet ./...` on Go 1.26.x.

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
