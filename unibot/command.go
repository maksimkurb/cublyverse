package unibot

import "strings"

// MaxCommandNameLength is Telegram's limit for a command name without slash.
const MaxCommandNameLength = 32

// ParseCommand parses a slash command:
//
//	/start            -> Command{Name: "start"}
//	/help             -> Command{Name: "help"}
//	/reminders@bot 30 -> Command{Name: "reminders", Addressee: "bot", Args: "30"}
//
// It is shared by every adapter so command handling does not depend on the
// messenger. Anything that does not start with a slash is not a command; a
// message that only looks like one ("/", "/ bad") is rejected too.
func ParseCommand(text string) (Command, bool) {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) < 2 || trimmed[0] != '/' {
		return Command{}, false
	}

	body := trimmed[1:]
	name := body
	args := ""
	if idx := strings.IndexAny(body, " \t\r\n"); idx >= 0 {
		name = body[:idx]
		args = strings.TrimSpace(body[idx+1:])
	}

	addressee := ""
	if at := strings.IndexByte(name, '@'); at >= 0 {
		name, addressee = name[:at], name[at+1:]
	}
	name = strings.ToLower(name)
	if !validCommandName(name) {
		return Command{}, false
	}
	return Command{Name: name, Addressee: addressee, Args: args}, true
}

// validCommandName accepts the Telegram command charset: ASCII letters, digits
// and underscores. A stricter alphabet keeps /help output and setMyCommands
// valid for every transport.
func validCommandName(name string) bool {
	if name == "" || len(name) > MaxCommandNameLength {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '_':
		default:
			return false
		}
	}
	return true
}
