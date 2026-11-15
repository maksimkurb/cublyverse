package unibot

import "testing"

func TestMessageValidation(t *testing.T) {
	tests := []struct {
		name    string
		message Message
		wantErr bool
	}{
		{name: "text", message: Message{Text: "hello"}},
		{name: "submit", message: Message{Text: "confirm", Actions: [][]Action{{{ID: "ok", Label: "OK", Kind: ActionSubmit}}}}},
		{name: "url requires URL", message: Message{Text: "docs", Actions: [][]Action{{{ID: "docs", Label: "Docs", Kind: ActionURL}}}}, wantErr: true},
		{name: "choice", message: Message{Choice: &Choice{ID: "env", Options: []ChoiceOption{{ID: "prod", Label: "Prod"}, {ID: "dev", Label: "Dev"}}}}},
		{name: "choice and actions", message: Message{Text: "x", Actions: [][]Action{{{ID: "ok", Label: "OK", Kind: ActionSubmit}}}, Choice: &Choice{ID: "env", Options: []ChoiceOption{{ID: "prod", Label: "Prod"}, {ID: "dev", Label: "Dev"}}}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.message.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
