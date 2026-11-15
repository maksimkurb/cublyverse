package unibot

import (
	"strings"
	"testing"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		want      Command
		wantIsCmd bool
	}{
		{name: "start", wantIsCmd: true, text: "/start", want: Command{Name: "start"}},
		{name: "help with spaces", wantIsCmd: true, text: "  /help  ", want: Command{Name: "help"}},
		{name: "args", wantIsCmd: true, text: "/reminders 30 мин", want: Command{Name: "reminders", Args: "30 мин"}},
		{name: "addressed", wantIsCmd: true, text: "/start@cublyverse_bot", want: Command{Name: "start", Addressee: "cublyverse_bot"}},
		{name: "addressed with args", wantIsCmd: true, text: "/reminders@cublyverse_bot 7", want: Command{Name: "reminders", Addressee: "cublyverse_bot", Args: "7"}},
		{name: "uppercase", wantIsCmd: true, text: "/START", want: Command{Name: "start"}},
		{name: "underscore", wantIsCmd: true, text: "/my_cmd_1", want: Command{Name: "my_cmd_1"}},
		{name: "tab separator", wantIsCmd: true, text: "/help\targs", want: Command{Name: "help", Args: "args"}},
		{name: "only slash", text: "/"},
		{name: "slash and space", text: "/ help"},
		{name: "plain text", text: "привет"},
		{name: "no command in the middle", text: "скажи /help"},
		{name: "empty", text: ""},
		{name: "cyrillic name", text: "/помощь"},
		{name: "too long", text: "/" + strings.Repeat("a", MaxCommandNameLength+1)},
		{name: "bad character", text: "/he-lp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseCommand(tt.text)
			if ok != tt.wantIsCmd {
				t.Fatalf("ParseCommand(%q) ok = %v, want %v", tt.text, ok, tt.wantIsCmd)
			}
			if !ok {
				return
			}
			if got != tt.want {
				t.Fatalf("ParseCommand(%q) = %+v, want %+v", tt.text, got, tt.want)
			}
		})
	}
}
