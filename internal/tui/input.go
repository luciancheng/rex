package tui

import (
	"strings"
	"unicode"
)

// Verbs are typed words that run on ⏎ (docs/ux.md §2). Nothing a person
// types does anything until ⏎, so a paste, a scroll burst or a stray key
// can never act on a plan.
var verbNames = map[string]string{
	"sendoff": "sendoff", "handoff": "sendoff", "s": "sendoff",
	"brief": "brief", "b": "brief",
	"done": "done", "d": "done",
	"pin": "pin", "p": "pin",
	"help": "help", "?": "help",
	"breakdance": "breakdance",
}

// parseVerb reads a submitted draft as a verb. A leading "/" and any case
// are accepted, "send off" is sendoff, and only pin takes an argument.
func parseVerb(text string) (verb, arg string, ok bool) {
	t := strings.TrimSpace(text)
	t = strings.TrimPrefix(t, "/")
	fields := strings.Fields(t)
	if len(fields) == 2 && strings.EqualFold(fields[0], "send") && strings.EqualFold(fields[1], "off") {
		fields = []string{"sendoff"}
	}
	if len(fields) == 0 || len(fields) > 2 {
		return "", "", false
	}
	verb, ok = verbNames[strings.ToLower(fields[0])]
	if !ok {
		return "", "", false
	}
	if len(fields) == 2 {
		if verb != "pin" {
			return "", "", false
		}
		arg = fields[1]
	}
	return verb, arg, true
}

// forcedVerb is a Library draft that asks for a verb: "/name" or a lone
// "?". Other Library text is a find query, filter or name, taken literally.
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
