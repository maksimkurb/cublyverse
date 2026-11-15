package unibot

import (
	"strings"
	"testing"
)

func TestEscapeHTML(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain", in: "напоминание", want: "напоминание"},
		{name: "tags", in: "<b>жирный</b>", want: "&lt;b&gt;жирный&lt;/b&gt;"},
		{name: "ampersand", in: "музыка & кино", want: "музыка &amp; кино"},
		{name: "quotes", in: `a "b" 'c'`, want: "a &#34;b&#34; &#39;c&#39;"},
		{name: "newline stays", in: "первая\nвторая", want: "первая\nвторая"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EscapeHTML(tt.in); got != tt.want {
				t.Fatalf("EscapeHTML(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestValidateHTML(t *testing.T) {
	tests := []struct {
		name    string
		html    string
		wantErr string
	}{
		{name: "empty"},
		{name: "plain text", html: "Просто текст"},
		{name: "bold", html: "<b>Первое напоминание</b>"},
		{name: "italic and code", html: "<i>курсив</i> и <code>код</code>"},
		{name: "nested formatting", html: "<b><i>оба</i></b>"},
		{name: "link", html: `<a href="https://example.com/app/">Открыть</a>`},
		{name: "telegram deep link", html: `<a href="tg://user?id=1">Профиль</a>`},
		{name: "escaped angle brackets", html: EscapeHTML("<b>не тег</b>")},
		{name: "unsupported tag", html: "<u>подчёркнутый</u>", wantErr: "not supported"},
		{name: "script", html: "<script>alert(1)</script>", wantErr: "not supported"},
		{name: "attribute on bold", html: `<b class="x">x</b>`, wantErr: "does not accept attributes"},
		{name: "javascript link", html: `<a href="javascript:alert(1)">x</a>`, wantErr: "scheme"},
		{name: "link without href", html: "<a>x</a>", wantErr: "href"},
		{name: "nested links", html: `<a href="https://a.example/"><a href="https://b.example/">b</a></a>`, wantErr: "nested"},
		{name: "unclosed", html: "<b>текст", wantErr: "leaves <b> open"},
		{name: "stray closing", html: "текст</b>", wantErr: "unexpected closing"},
		{name: "wrong nesting", html: "<b><i>текст</b></i>", wantErr: "closes </b>"},
		{name: "bare <", html: "1 < 2", wantErr: "unterminated"},
		{name: "unterminated", html: "<b", wantErr: "unterminated"},
		{name: "empty tag", html: "<>", wantErr: "empty tag"},
		{name: "too long", html: strings.Repeat("a", MaxHTMLLength+1), wantErr: "too long"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHTML(tt.html)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateHTML(%q) = %v, want nil", tt.html, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateHTML(%q) = nil, want an error containing %q", tt.html, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateHTML(%q) = %v, want an error containing %q", tt.html, err, tt.wantErr)
			}
		})
	}
}

func TestHTMLToText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain", in: "текст", want: "текст"},
		{name: "bold", in: "<b>напоминание</b>", want: "напоминание"},
		{name: "entities", in: "1 &lt; 2 &amp; 3", want: "1 < 2 & 3"},
		{name: "link keeps the label", in: `<a href="https://example.com/">Открыть</a>`, want: "Открыть"},
		{name: "line break", in: "первая<br>вторая", want: "первая\nвторая"},
		{name: "unterminated tag keeps the tail", in: "текст <b", want: "текст <b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HTMLToText(tt.in); got != tt.want {
				t.Fatalf("HTMLToText(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
