package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	unibot "github.com/maksimkurb/cublyverse/unibot"
)

// errNoMessenger reports that no messenger transport is configured, so a
// reminder has nowhere to go.
var errNoMessenger = errors.New("no messenger transport is configured")

// messengerDelivery is the reminders.Delivery adapter over the messenger
// runtime.
//
// The runtime is built in startBot, after the feature registry, so it is
// attached here instead of being a constructor argument. Without transports
// every send fails with errNoMessenger and the scheduler records it in
// last_error, which is exactly the behaviour of a service that runs with a
// database but no bot.
type messengerDelivery struct {
	logger *slog.Logger

	mu      sync.RWMutex
	runtime *unibot.Runtime
}

// newMessengerDelivery returns a delivery that is not attached yet.
func newMessengerDelivery(logger *slog.Logger) *messengerDelivery {
	if logger == nil {
		logger = slog.Default()
	}
	return &messengerDelivery{logger: logger}
}

// attach binds the runtime once the transports are running.
func (d *messengerDelivery) attach(runtime *unibot.Runtime) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.runtime = runtime
}

// Send delivers a reminder over the first working messenger. Telegram is
// preferred because it renders the HTML subset and inline buttons best; Matrix
// is the fallback.
func (d *messengerDelivery) Send(ctx context.Context, principalID string, message unibot.Message) ([]unibot.SentMessage, error) {
	runtime := d.attached()
	if runtime == nil {
		return nil, errNoMessenger
	}
	sent, err := runtime.SendToPrincipal(ctx, unibot.PrincipalID(principalID), message, unibot.DeliveryOptions{
		Preferred: []unibot.Platform{unibot.PlatformTelegram, unibot.PlatformMatrix},
	})
	if len(sent) > 0 {
		// Partially successful: the message reached at least one messenger, so
		// the reminder did fire. The failed platform is worth a warning only.
		if err != nil {
			d.logger.Warn("reminder delivery failed on some messengers", slog.String("error", err.Error()))
		}
		return sent, nil
	}
	if err == nil {
		err = errNoMessenger
	}
	return nil, err
}

// Edit rewrites the delivered messages in place.
func (d *messengerDelivery) Edit(ctx context.Context, sent []unibot.SentMessage, message unibot.Message) error {
	runtime := d.attached()
	if runtime == nil {
		return errNoMessenger
	}
	var out error
	for _, reference := range sent {
		if err := runtime.Edit(ctx, reference, message); err != nil {
			out = errors.Join(out, fmt.Errorf("%s: %w", reference.Platform, err))
		}
	}
	return out
}

// Delete removes the delivered messages.
func (d *messengerDelivery) Delete(ctx context.Context, sent []unibot.SentMessage) error {
	runtime := d.attached()
	if runtime == nil {
		return errNoMessenger
	}
	var out error
	for _, reference := range sent {
		if err := runtime.Delete(ctx, reference); err != nil {
			out = errors.Join(out, fmt.Errorf("%s: %w", reference.Platform, err))
		}
	}
	return out
}

// attached returns the runtime, or nil when no transport is configured.
func (d *messengerDelivery) attached() *unibot.Runtime {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.runtime
}
