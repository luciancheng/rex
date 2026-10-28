package tui

import (
	"strings"
	"unicode"
)

// Verbs are the /words Plans' text field runs on enter (docs/ux.md
// §2): Rex's actions on a plan are single keys on the task list (p and
// d, §7b), so these only show help or dance.
var verbNames = map[string]string{
	"help": "help", "?": "help",
	"breakdance": "breakdance",
}

// parseVerb reads a submitted draft as a verb. A leading "/" and any case
// are accepted; no verb takes an argument.
func parseVerb(text string) (verb string, ok bool) {
	verb, ok = verbNames[strings.ToLower(strings.TrimPrefix(strings.TrimSpace(text), "/"))]
	return verb, ok
}

// forcedVerb is text in Plans' field that asks for a verb: "/name" or a lone
// "?". Other text there is a find query, filter or name, taken literally.
func forcedVerb(text string) bool {
	t := strings.TrimSpace(text)
	return t == "?" || strings.HasPrefix(t, "/")
}

// deleteWord removes the word before the cursor (Ctrl+W), like a shell.
func deleteWord(s string) string {
	r := []rune(s)
	i := len(r)
	for i > 0 && unicode.IsSpace(r[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(r[i-1]) {
		i--
	}
	return string(r[:i])
}

func backspace(s string) string {
	if r := []rune(s); len(r) > 0 {
		return string(r[:len(r)-1])
	}
	return s
}
