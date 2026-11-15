package matrix

import (
	"strings"
	"testing"

	unibot "github.com/maksimkurb/cublyverse/unibot"
)

// TestRenderMessageHTML covers M2-1 for Matrix: the formatted body carries the
// HTML subset, the plain body stays the fallback.
func TestRenderMessageHTML(t *testing.T) {
	tests := []struct {
		name          string
		message       unibot.Message
		wantBody      string
		wantFormatted string
		wantFormat    bool
		wantReactions int
	}{
		{
			name:     "plain text",
			message:  unibot.Message{Text: "первая\nвторая"},
			wantBody: "первая\nвторая",
		},
		{
			name:     "plain text is not formatted",
			message:  unibot.Message{Text: "музыка & кино"},
			wantBody: "музыка & кино",
		},
		{
			name:          "html",
			message:       unibot.Message{Text: "Первое напоминание", HTML: "<b>Первое напоминание</b>"},
			wantBody:      "Первое напоминание",
			wantFormatted: "<b>Первое напоминание</b>",
			wantFormat:    true,
		},
		{
			name:          "html only",
			message:       unibot.Message{HTML: "Готово <i>сегодня</i>"},
			wantBody:      "Готово сегодня",
			wantFormatted: "Готово <i>сегодня</i>",
			wantFormat:    true,
		},
		{
			name: "html with an action",
			message: unibot.Message{
				Text: "Напоминание",
				HTML: "<b>Напоминание</b>",
				Actions: [][]unibot.Action{{{
					ID:    "done",
					Label: "Готово",
					Icon:  "✅",
					Kind:  unibot.ActionSubmit,
				}}},
			},
			wantBody:      "Напоминание\n✅ Готово",
			wantFormatted: "<b>Напоминание</b><br>✅ Готово",
			wantFormat:    true,
			wantReactions: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, reactions, err := renderMessage(tt.message)
			if err != nil {
				t.Fatalf("renderMessage: %v", err)
			}
			if content.Body != tt.wantBody {
				t.Errorf("body = %q, want %q", content.Body, tt.wantBody)
			}
			if tt.wantFormat {
				if content.FormattedBody != tt.wantFormatted {
					t.Errorf("formatted body = %q, want %q", content.FormattedBody, tt.wantFormatted)
				}
				if content.Format != "org.matrix.custom.html" {
					t.Errorf("format = %q, want org.matrix.custom.html", content.Format)
				}
			} else if content.FormattedBody != "" {
				t.Errorf("formatted body = %q, want none", content.FormattedBody)
			}
			if len(reactions) != tt.wantReactions {
				t.Errorf("reactions = %v, want %d", reactions, tt.wantReactions)
			}
		})
	}
}

// TestRenderMessageEscapesActionLabels checks that action labels and URLs of
// a message are escaped inside the formatted body.
func TestRenderMessageEscapesActionLabels(t *testing.T) {
	content, _, err := renderMessage(unibot.Message{
		Text: "Открыть",
		Actions: [][]unibot.Action{{{
			ID:    "open",
			Label: "Открыть <приложение>",
			Kind:  unibot.ActionURL,
			URL:   "https://example.com/app/?a=1&b=2",
		}}},
	})
	if err != nil {
		t.Fatalf("renderMessage: %v", err)
	}
	if strings.Contains(content.FormattedBody, "<приложение>") {
		t.Fatalf("formatted body is not escaped: %q", content.FormattedBody)
	}
	if !strings.Contains(content.FormattedBody, "a=1&amp;b=2") {
		t.Fatalf("formatted body does not escape the URL: %q", content.FormattedBody)
	}
}

// TestAckIsANoOp checks that Matrix accepts an acknowledgement without doing
// anything.
func TestAckIsANoOp(t *testing.T) {
	adapter := &Adapter{}
	if err := adapter.Ack(t.Context(), "token", "текст", true); err != nil {
		t.Fatalf("Ack: %v", err)
	}
}
