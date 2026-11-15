package unibot

import (
	"errors"
	"fmt"
	"html"
	"net/url"
	"strings"
	"unicode/utf8"
)

// Supported HTML tags of Message.HTML. The subset is deliberately tiny so that
// every transport can render it faithfully: Telegram has a native HTML parse
// mode, Matrix has formatted_body, and any future transport can map the same
// four tags. Everything else has to be escaped.
const (
	htmlTagBold   = "b"
	htmlTagItalic = "i"
	htmlTagCode   = "code"
	htmlTagLink   = "a"
)

// MaxHTMLLength bounds Message.HTML. Telegram rejects messages longer than 4096
// characters, and nobody sends more through a chat button.
const MaxHTMLLength = 4096

// EscapeHTML escapes plain text so it can be embedded in Message.HTML without
// being interpreted as markup. Newlines are kept as-is: they are the portable
// way to break a line in both renderers.
func EscapeHTML(text string) string {
	return html.EscapeString(text)
}

// HTMLToText turns the supported HTML subset back into a plain-text fallback.
// Transports that need a plain body next to a formatted one (Matrix
// formatted_body/body pair) use it when Message.Text is empty.
func HTMLToText(value string) string {
	var out strings.Builder
	out.Grow(len(value))
	for i := 0; i < len(value); {
		if value[i] != '<' {
			out.WriteByte(value[i])
			i++
			continue
		}
		end := strings.IndexByte(value[i:], '>')
		if end < 0 {
			// Not markup after all; ValidateHTML would reject it, but a plain
			// fallback must never panic or lose the tail.
			out.WriteString(value[i:])
			break
		}
		if strings.EqualFold(strings.TrimSpace(value[i+1:i+end]), "br") {
			out.WriteByte('\n')
		}
		i += end + 1
	}
	return html.UnescapeString(out.String())
}

// ValidateHTML checks Message.HTML against the supported subset: well-formed
// <b>, <i>, <code> and <a href="…"></a>, no other tags or attributes, no
// nesting of links. It returns a descriptive error so a feature author sees
// what exactly broke, not just a Telegram 400 at runtime.
//
// The validator is intentionally strict: a literal '<' that is not a tag must
// be escaped with EscapeHTML. That keeps rendering identical on both
// transports and prevents markup injection through user data.
func ValidateHTML(value string) error {
	if value == "" {
		return nil
	}
	if len(value) > MaxHTMLLength {
		return fmt.Errorf("html message is too long: %d bytes, max %d", len(value), MaxHTMLLength)
	}
	if !utf8.ValidString(value) {
		return errors.New("html message is not valid UTF-8")
	}

	var stack []string
	linkDepth := 0
	for i := 0; i < len(value); {
		if value[i] != '<' {
			i++
			continue
		}
		end := strings.IndexByte(value[i:], '>')
		if end < 0 {
			return fmt.Errorf("html message has an unterminated tag at offset %d", i)
		}
		raw := value[i+1 : i+end]
		i += end + 1

		name, closing, err := parseHTMLTag(raw)
		if err != nil {
			return err
		}
		switch name {
		case htmlTagBold, htmlTagItalic, htmlTagCode:
		case htmlTagLink:
			if closing {
				linkDepth--
			} else {
				linkDepth++
			}
		default:
			return fmt.Errorf("html tag %q is not supported; allowed: b, i, code, a", name)
		}
		if linkDepth > 1 {
			return errors.New("html links cannot be nested")
		}

		if closing {
			if len(stack) == 0 {
				return fmt.Errorf("html message has an unexpected closing tag </%s>", name)
			}
			if stack[len(stack)-1] != name {
				return fmt.Errorf("html message closes </%s> while <%s> is open", name, stack[len(stack)-1])
			}
			stack = stack[:len(stack)-1]
			continue
		}
		stack = append(stack, name)
	}
	if len(stack) > 0 {
		return fmt.Errorf("html message leaves <%s> open", stack[len(stack)-1])
	}
	return nil
}

// parseHTMLTag parses the inside of one tag ("b", "/b", `a href="…"`). It
// rejects attributes everywhere except a single href on <a>.
func parseHTMLTag(raw string) (name string, closing bool, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false, errors.New("html message has an empty tag")
	}
	if strings.HasPrefix(raw, "/") {
		closing = true
		raw = strings.TrimSpace(raw[1:])
		if raw == "" || strings.ContainsAny(raw, " \t\r\n") {
			return "", false, fmt.Errorf("html message has a malformed closing tag </%s>", raw)
		}
		if !isSupportedHTMLTag(strings.ToLower(raw)) {
			return "", false, fmt.Errorf("html tag %q is not supported; allowed: b, i, code, a", raw)
		}
		return strings.ToLower(raw), true, nil
	}

	name = raw
	rest := ""
	if idx := strings.IndexAny(raw, " \t\r\n"); idx >= 0 {
		name, rest = raw[:idx], strings.TrimSpace(raw[idx+1:])
	}
	name = strings.ToLower(name)
	if !isSupportedHTMLTag(name) {
		return "", false, fmt.Errorf("html tag %q is not supported; allowed: b, i, code, a", name)
	}
	if rest == "" {
		if name == htmlTagLink {
			return "", false, errors.New("html link needs an href attribute")
		}
		return name, false, nil
	}
	if name != htmlTagLink {
		return "", false, fmt.Errorf("html tag <%s> does not accept attributes", name)
	}
	href, err := parseHrefAttribute(rest)
	if err != nil {
		return "", false, err
	}
	if err := validateHref(href); err != nil {
		return "", false, err
	}
	return name, false, nil
}

// parseHrefAttribute accepts exactly one href="…" attribute.
func parseHrefAttribute(value string) (string, error) {
	const prefix = `href="`
	if !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, `"`) {
		return "", errors.New(`html link supports a single href="…" attribute`)
	}
	href := value[len(prefix) : len(value)-1]
	if href == "" {
		return "", errors.New("html link has an empty href")
	}
	return href, nil
}

// validateHref allows absolute http(s) and Telegram deep links, which are the
// only schemes a button or a chat message may use.
func validateHref(href string) error {
	if strings.ContainsAny(href, "\"<>") {
		return errors.New("html link has characters that cannot be escaped")
	}
	parsed, err := url.Parse(href)
	if err != nil {
		return fmt.Errorf("html link has an invalid href: %w", err)
	}
	switch parsed.Scheme {
	case "http", "https", "tg":
	default:
		return fmt.Errorf("html link scheme %q is not allowed", parsed.Scheme)
	}
	if parsed.Host == "" && parsed.Scheme != "tg" {
		return fmt.Errorf("html link %q has no host", href)
	}
	return nil
}

func isSupportedHTMLTag(name string) bool {
	switch name {
	case htmlTagBold, htmlTagItalic, htmlTagCode, htmlTagLink:
		return true
	default:
		return false
	}
}
