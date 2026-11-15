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
