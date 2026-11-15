package unibot

import (
	"strings"
	"testing"
	"time"
)

func TestMessageValidation(t *testing.T) {
	tests := []struct {
		name    string
		message Message
		wantErr string
	}{
		{name: "text", message: Message{Text: "hello"}},
		{name: "html only", message: Message{HTML: "<b>Привет</b>"}},
		{name: "text and html", message: Message{Text: "Привет", HTML: "<b>Привет</b>"}},
		{name: "submit", message: Message{Text: "confirm", Actions: [][]Action{{{ID: "ok", Label: "OK", Kind: ActionSubmit}}}}},
		{name: "action with payload and ttl", message: Message{
			Text:    "Done?",
			Actions: [][]Action{{{ID: "done", Label: "Готово", Kind: ActionSubmit, Payload: "7:9", TTL: time.Hour}}},
		}},
		{name: "url requires URL", message: Message{Text: "docs", Actions: [][]Action{{{ID: "docs", Label: "Docs", Kind: ActionURL}}}}, wantErr: "url"},
		{name: "choice", message: Message{Choice: &Choice{ID: "env", Options: []ChoiceOption{{ID: "prod", Label: "Prod"}, {ID: "dev", Label: "Dev"}}}}},
		{name: "empty message", message: Message{}, wantErr: "text, html or a choice"},
		{name: "unsupported html", message: Message{HTML: "<h1>Заголовок</h1>"}, wantErr: "not supported"},
		{name: "broken html", message: Message{HTML: "<b>текст"}, wantErr: "leaves <b> open"},
		{name: "payload too long", message: Message{
			Text:    "Done?",
			Actions: [][]Action{{{ID: "done", Label: "Готово", Kind: ActionSubmit, Payload: strings.Repeat("x", MaxActionPayloadBytes+1)}}},
		}, wantErr: "payload"},
		{name: "negative ttl", message: Message{
			Text:    "Done?",
			Actions: [][]Action{{{ID: "done", Label: "Готово", Kind: ActionSubmit, TTL: -time.Minute}}},
		}, wantErr: "negative ttl"},
		{name: "choice and actions", message: Message{
			Text:    "x",
			Actions: [][]Action{{{ID: "ok", Label: "OK", Kind: ActionSubmit}}},
			Choice:  &Choice{ID: "env", Options: []ChoiceOption{{ID: "prod", Label: "Prod"}, {ID: "dev", Label: "Dev"}}},
		}, wantErr: "actions and choice"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.message.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want an error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() = %v, want an error containing %q", err, tt.wantErr)
			}
		})
	}
}
