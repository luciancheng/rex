package watch

import (
	"regexp"
	"strings"
)

var ProseKinds = map[string]bool{
	"thought": true,
	"text":    true,
	"user":    true,
}

var (
	headingRe  = regexp.MustCompile(`^#{1,6}\s+`)
	ulRe       = regexp.MustCompile(`^(\s*)([-*+])\s+`)
	olRe       = regexp.MustCompile(`^(\s*)(\d+)\.\s+`)
	linkRe     = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	boldStarRe = regexp.MustCompile(`\*\*(.+?)\*\*`)
	boldUndRe  = regexp.MustCompile(`__(.+?)__`)
	codeRe     = regexp.MustCompile("`([^`]+)`")
	ruleRe     = regexp.MustCompile(`^\s*([-*_]\s*){3,}$`)
)

func NormalizeText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func IsGrowingText(prev, next Event) bool {
	if prev.Kind != "text" || next.Kind != "text" {
		return false
	}
	if prev.Source != next.Source {
		return false
	}
	a, b := NormalizeText(prev.Text), NormalizeText(next.Text)
	if a == "" || b == "" || a == b {
		return false
	}
	return strings.HasPrefix(b, a)
}

func LongerText(a, b string) string {
	if len([]rune(b)) >= len([]rune(a)) {
		return b
	}
	return a
}

func IsDuplicate(event Event, recent []Event, window float64) bool {
	if window == 0 {
		window = 4
	}
	key := NormalizeText(event.Text)
	if key == "" {
		return true
	}
	if event.Kind == "idle" {
		return false // two turns may end with the same words; each end counts
	}
	for i := len(recent) - 1; i >= 0; i-- {
		prev := recent[i]
		if prev.Kind != event.Kind || !sameOrigin(prev, event) || !sameCall(prev, event) {
			continue
		}
		if NormalizeText(prev.Text) != key {
			continue
		}
		if event.Kind == "thought" {
			return true
		}
		diff := event.TS - prev.TS
		if diff < 0 {
			diff = -diff
		}
		if diff <= window {
			return true
		}
	}
	return false
}

func MarkdownLines(text string) []string {
	if strings.TrimSpace(text) == "" {
		return []string{""}
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var out []string
	inFence := false
	for _, raw := range strings.Split(text, "\n") {
		stripped := strings.TrimSpace(raw)
		if strings.HasPrefix(stripped, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			out = append(out, "│ "+strings.TrimRight(raw, "\r\n"))
			continue
		}
		if ruleRe.MatchString(raw) {
			if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			continue
		}
		out = append(out, inlineMD(blockMD(strings.TrimRight(raw, "\r\n"))))
	}
	return compactLines(out)
}

func blockMD(line string) string {
	if headingRe.MatchString(line) {
		return strings.TrimSpace(headingRe.ReplaceAllString(line, ""))
	}
	if m := ulRe.FindStringSubmatchIndex(line); m != nil {
		indentRaw := strings.ReplaceAll(line[m[2]:m[3]], "\t", "  ")
		indent := strings.Repeat("  ", len(indentRaw)/2)
		return indent + "• " + line[m[1]:]
	}
	if m := olRe.FindStringSubmatchIndex(line); m != nil {
		indentRaw := strings.ReplaceAll(line[m[2]:m[3]], "\t", "  ")
		num := line[m[4]:m[5]]
		indent := strings.Repeat("  ", len(indentRaw)/2)
		return indent + num + ". " + line[m[1]:]
	}
	return line
}

func inlineMD(line string) string {
	line = linkRe.ReplaceAllString(line, "$1")
	line = codeRe.ReplaceAllString(line, "$1")
	line = boldStarRe.ReplaceAllString(line, "$1")
	line = boldUndRe.ReplaceAllString(line, "$1")
	return stripItalic(line)
}

func stripItalic(s string) string {
	r := []rune(s)
	var b strings.Builder
	for i := 0; i < len(r); {
		if r[i] == '*' && (i == 0 || r[i-1] != '*') {
			j := i + 1
			for j < len(r) {
				if r[j] == '*' && r[j-1] != '*' && (j+1 >= len(r) || r[j+1] != '*') && j > i+1 {
					b.WriteString(string(r[i+1 : j]))
					i = j + 1
					break
				}
				j++
			}
			if j >= len(r) {
				b.WriteRune(r[i])
				i++
			}
			continue
		}
		b.WriteRune(r[i])
		i++
	}
	return b.String()
}

func compactLines(lines []string) []string {
	var compacted []string
	blank := false
	for _, line := range lines {
		empty := strings.TrimSpace(line) == ""
		if empty {
			if len(compacted) > 0 && !blank {
				compacted = append(compacted, "")
			}
			blank = true
			continue
		}
		compacted = append(compacted, line)
		blank = false
	}
	if len(compacted) == 0 {
		return []string{""}
	}
	return compacted
}

// sameOrigin is false when both events say where they came from and the
// places differ: the same command run in two repos is two events.
func sameOrigin(a, b Event) bool {
	ca, _ := a.Meta["cwd"].(string)
	cb, _ := b.Meta["cwd"].(string)
	return ca == "" || cb == "" || ca == cb
}

// sameCall is false when both events name their tool call and the calls
// differ: two writes of one file are two events. Cursor's afterFileEdit
// names none, so it still matches the postToolUse it repeats.
func sameCall(a, b Event) bool {
	ia, _ := a.Meta["tool_use_id"].(string)
	ib, _ := b.Meta["tool_use_id"].(string)
	return ia == "" || ib == "" || ia == ib
}
