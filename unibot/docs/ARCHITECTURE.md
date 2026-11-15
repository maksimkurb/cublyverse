# Architecture notes

## 1. Principal is not a messenger user

`PrincipalID` is derived from the OIDC issuer and subject. A Telegram ID or Matrix MXID can never become the application identity directly.

`IdentityStore` owns the N:1 mapping from messenger bindings to a principal. This is what lets an application state machine continue when the ingress transport changes.

## 2. Transport adapters are translators

A transport has two responsibilities:

- outbound: render semantic `Message` objects into platform-native events;
- inbound: normalize platform events back into `Event`.

Business code must not import `platform/telegram` or `platform/matrix`.

## 3. Session state follows the principal

Application session state is keyed by `PrincipalID`, never by a Telegram chat ID or Matrix room ID. The generic `session.Store[T]` contract makes this explicit. A user can therefore enter through Telegram, continue through Element, and load the same state machine/session object.

The demo increments one `Turns` field from both transports to make this behavior visible.

## 4. Interaction registry

A platform may not be able to carry your domain action ID directly.

Telegram uses a short opaque callback token because callback data is size-limited. Matrix reactions are identified by the target event plus reaction key. Matrix polls use opaque answer IDs.

All three resolve through `InteractionStore`:

```text
Telegram callback token ─────┐
                             │
Matrix event + emoji ────────┼──> domain ActionID / Choice option ID
                             │
Matrix poll + answer ID ─────┘
```

Persist this registry in production if interactions must survive process restarts.

An interaction row carries two optional restrictions and one payload:

- `ExpectedUserID` is checked by the adapter before an event is emitted (the
  messenger account);
- `PrincipalID` is checked by the runtime after `Resolve` (the SSO account), so
  relinking the messenger cannot redirect a button to another person;
- `Payload` is the opaque `Action.Payload` (up to 512 bytes) returned in
  `ActionEvent.Payload`; Telegram's 64-byte `callback_data` cannot hold ids, so
  they live here;
- `ExpiresAt` is `Action.TTL` from `now()`; the default is
  `unibot.DefaultActionTTL` (24 hours). Expired rows are not resolved, so a stale
  button never reaches the handler.

## 4a. Acknowledgement of interactive events

Telegram requires an `answerCallbackQuery` for every callback query. Answering
immediately with an empty result makes it impossible to show "Подтверждено ✅" or
"This reminder is not yours".

The adapter therefore only publishes the event with `Event.AckToken` set. The
handler calls `botctx.Ack(ctx, text, alert)`, and the runtime sends an empty
acknowledgement after the handler returns when it did not. The runtime also
acknowledges after a handler timeout or panic, so the client never spins
forever. Transports without the concept (`Acknowledger` is optional) simply do
nothing, which is what the Matrix adapter does.

## 4b. Event ordering and bounded work

The runtime guarantees that events of the same remote user are handled one at a
time, in arrival order, while different users run in parallel. Work is bounded:

- `MaxConcurrentHandlers` handlers at once (default 32);
- `MaxPendingEvents` buffered events (default 1024) plus a bounded transport
  hand-off channel — backpressure instead of unbounded goroutines;
- a per-handler timeout (default 30s, `RuntimeConfig.HandlerTimeout`);
- errors and `Resolve` failures are logged, panics are recovered per event;
- `Close` stops the transports, the scheduler and the handlers.

The ordering key is `(platform, remote user id)`: the same person on two
messengers is two independent streams.

## 5. Element action strategy

`ActionSubmit` is rendered as a reaction because Element already presents reactions as compact controls attached to the message.

The bot first sends the message, registers a unique emoji for each action, then sends those reactions itself. A user clicking the same emoji creates their own `m.reaction`; the adapter ignores the bot's seed events and converts the user's reaction to `EventAction`.

Removing a reaction does not undo the action. Application handlers should be idempotent where necessary.

## 6. Choice is different from Action

A `Choice` is a semantic selection and maps naturally to Element polls. It should not be used merely to imitate a button.

The starter uses the MSC3381 event types currently understood by Element and represented directly by `mautrix-go`.

## 7. WebApp

`ActionOpenApp` means "open an application surface", not "Telegram Mini App".

- Telegram: native `web_app` inline button.
- Element message: clickable HTTPS link.
- Element room with an installed widget: the same `/app/` URL can run inside the widget host.

`InstallElementWidget` is intentionally explicit because creating/updating room widgets is a room-state operation and depends on Matrix power levels.

## 8. E2EE boundary

Matrix is a full E2EE Matrix client/device. The crypto helper owns the Olm/Megolm state and its DB must survive restarts.

Telegram Bot API is not E2EE. `Capabilities.EndToEndEncryption` therefore belongs to the transport and is not a global promise made by the framework.

## 9. Delivery/failover

`Runtime.SendToPrincipal` resolves all bindings for a principal and orders them by `DeliveryOptions.Preferred`.

For example:

```go
runtime.SendToPrincipal(ctx, principal, msg, unibot.DeliveryOptions{
    Preferred: []unibot.Platform{
        unibot.PlatformTelegram,
        unibot.PlatformMatrix,
    },
})
```

If Telegram delivery errors, the runtime tries Matrix. `Fanout: true` sends through every usable binding.

The framework deliberately does not infer that "no read receipt" means a transport is unavailable. Availability/failover policy should later be a separate service using explicit delivery errors and whatever acknowledgement semantics the application needs.

## 10. Formatting

`Message.Text` is always the plain fallback. `Message.HTML` is a restricted
subset — `b`, `i`, `code`, `a href` — validated by `unibot.ValidateHTML` before
a transport sees the message. Telegram renders it with `parse_mode=HTML`; Matrix
puts it into `formatted_body` with `format=org.matrix.custom.html`. `Text` stays
the Matrix `body`, so clients without HTML support still show the message.

Actions attached to an HTML message are appended to both variants: the plain
body gets `icon label` lines, the formatted body gets escaped HTML lines.

## 11. Telegram: private chats, commands, menu

The adapter serves private chats only. Group/channel messages are dropped, and
callback queries from them are answered empty so the client stops its spinner.
Private-chat text starting with `/` is parsed by `unibot.ParseCommand`
(`/start`, `/help`, `/cmd@botname args`) and travels in
`IncomingMessage.Command`; the same parser is used by the Matrix adapter, so
command dispatch does not depend on the messenger.

On start the adapter publishes `setMyCommands` (scope `all_private_chats`,
`language_code=ru`) and a `web_app` `setChatMenuButton` when a WebApp URL is
configured. Both calls are best effort: failures are logged and do not fail the
start.
