package reminders

import (
	"context"
	"encoding/json"
	"fmt"

	unibot "github.com/maksimkurb/cublyverse/unibot"
)

// Delivery moves reminder messages to the user's messengers. The scheduler does
// not know Telegram or Matrix: it sends a transport-neutral message, gets back
// the references of what was actually delivered and stores them.
//
// The feature never imports unibot/platform/*; the adapter over the messenger
// runtime is wired in internal/app.
type Delivery interface {
	// Send delivers message to the user over the preferred messenger. An error
	// means nothing was delivered: the scheduler records it and retries on the
	// next tick. Sent references may be returned together with a nil error only.
	Send(ctx context.Context, principalID string, message unibot.Message) ([]unibot.SentMessage, error)
	// Edit replaces the delivered messages in place, for example to show the
	// completion of a reminder.
	Edit(ctx context.Context, sent []unibot.SentMessage, message unibot.Message) error
	// Delete removes delivered messages, for example the previous copy of a
	// repeated reminder.
	Delete(ctx context.Context, sent []unibot.SentMessage) error
}

// deliveryRecord is one row of reminder_occurrences.deliveries: enough to find
// the message again when it has to be edited or deleted.
type deliveryRecord struct {
	Platform       string `json:"platform"`
	RemoteUserID   string `json:"remote_user_id,omitempty"`
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
}

// encodeDeliveries turns sent messages into the jsonb value of an occurrence.
func encodeDeliveries(sent []unibot.SentMessage) ([]byte, error) {
	records := make([]deliveryRecord, 0, len(sent))
	for _, message := range sent {
		records = append(records, deliveryRecord{
			Platform:       string(message.Platform),
			RemoteUserID:   message.RemoteUserID,
			ConversationID: message.ConversationID,
			MessageID:      message.RemoteMessageID,
		})
	}
	encoded, err := json.Marshal(records)
	if err != nil {
		return nil, fmt.Errorf("encode deliveries: %w", err)
	}
	return encoded, nil
}

// decodeDeliveries reads the jsonb value of an occurrence. A malformed value is
// an error: silently dropping it would hide a real bug behind a missing message.
func decodeDeliveries(raw []byte) ([]unibot.SentMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var records []deliveryRecord
	if err := json.Unmarshal(raw, &records); err != nil {
		return nil, fmt.Errorf("decode deliveries: %w", err)
	}
	sent := make([]unibot.SentMessage, 0, len(records))
	for _, record := range records {
		sent = append(sent, unibot.SentMessage{
			Platform:        unibot.Platform(record.Platform),
			RemoteUserID:    record.RemoteUserID,
			ConversationID:  record.ConversationID,
			RemoteMessageID: record.MessageID,
		})
	}
	return sent, nil
}
