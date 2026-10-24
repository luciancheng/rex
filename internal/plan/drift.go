package plan

import (
	"path"
	"strings"
	"unicode"
)

// DriftLevel says how far an edit or a prompt is from the pin.
type DriftLevel int

const (
	OnPin DriftLevel = iota
	Soft             // probably fine, worth a glance
	Hard             // clearly off the pin
)

func (l DriftLevel) String() string {
	return [...]string{"none", "soft", "hard"}[l]
}

// Drift is one judgement: the level, what it was about (a path or a
// prompt excerpt), and why, in words for row 1 and AGENT. Names is the
// task a prompt names instead of the pin: asking the agent to switch to
// it may be what moves the pin next (docs/ux.md §9a).
type Drift struct {
	Level   DriftLevel
	Subject string
	Reason  string
	Names   string
}

// CheckEdit judges a completed edit of rel, a repo-relative path with "/"
// separators (docs/ux.md §9a). fold compares paths case-insensitively, as
// on darwin and windows. Rex only shows the result; nothing is written.
func CheckEdit(p *Plan, pin *Task, rel string, fold bool) Drift {
	rel = path.Clean(strings.ReplaceAll(rel, "\\", "/"))
	d := Drift{Subject: rel}
	if p == nil || pin == nil || strings.HasPrefix(rel, "rex/plans/") || rel == "." {
		return d
	}
	norm := func(s string) string {
		s = path.Clean(strings.ReplaceAll(strings.TrimSpace(s), "\\", "/"))
		if fold {
			s = strings.ToLower(s)
		}
		return s
	}
	target := norm(rel)
	for _, f := range pin.Files {
		if matchPath(norm(f), target) || testSibling(norm(f), target) {
			return d
		}
	}
	if w := outPath(p, pin, target, fold); w != "" {
		d.Level, d.Reason = Hard, "touches "+w+", which is out of scope"
		return d
	}
	if len(pin.Files) == 0 {
		return d // with no files listed, only scope.out can call an edit off the pin
	}
	for i := range p.Tasks {
		t := &p.Tasks[i]
		if t == pin || t.ID == "" {
			continue
		}
		for _, f := range t.Files {
			if matchPath(norm(f), target) {
				d.Level, d.Reason = Soft, "listed for "+t.ID
				return d
			}
		}
	}
	dir := path.Dir(target)
	for _, f := range pin.Files {
		fdir := path.Dir(norm(f))
		if globDir(fdir) {
			continue
		}
		switch {
		case fdir == dir:
			d.Level, d.Reason = Soft, "next to "+path.Base(f)
			return d
		case fdir != "." && strings.HasPrefix(dir, fdir+"/"):
			// testdata/, __tests__/, a subpackage: related, not unrelated.
			d.Level, d.Reason = Soft, "under the folder of "+path.Base(f)
			return d
		}
	}
	d.Level, d.Reason = Hard, "outside "+pin.ID+" files"
	return d
}

// CheckPrompt judges a user prompt against the pin, the plan goal and
// scope. Short prompts never drift.
func CheckPrompt(p *Plan, pin *Task, prompt string) Drift {
	d := Drift{Subject: excerpt(prompt)}
	if p == nil || pin == nil {
		return d
	}
	words := significant(prompt)
	idChar := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' }
	ids := map[string]bool{}
	for i := range p.Tasks {
		if id := strings.ToLower(p.Tasks[i].ID); strings.ContainsAny(id, "-_0123456789") {
			ids[id] = true
		}
	}
	// A compound id like load-outline is a name, not words that can hit
	// scope.out; it is dropped as a whole word ("build" stays whole). A
	// one-word id ("ui") is a word like any other.
	var bare []string
	set := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(prompt), func(r rune) bool { return !idChar(r) }) {
		w = strings.Trim(w, "-_")
		set[w] = true
		if !ids[w] {
			bare = append(bare, w)
		}
	}
	if w := outPhrase(p, pin, bare); w != "" {
		d.Level, d.Reason = Hard, "asks for "+w+", which is out of scope"
		return d
	}
	related := map[string]bool{} // the pin's parents and what it waits on are not "another task"
	for _, dep := range pin.DependsOn {
		related[dep] = true
	}
	seen := map[string]bool{}
	for parent := pin.Parent; parent != "" && !seen[parent]; {
		seen[parent], related[parent] = true, true
		next := p.Task(parent)
		if next == nil {
			break
		}
		parent = next.Parent
	}
	for i := range p.Tasks {
		if t := &p.Tasks[i]; t != pin && t.ID != "" && !related[t.ID] && set[strings.ToLower(t.ID)] {
			d.Level, d.Reason, d.Names = Soft, "names "+t.ID+", not the pin", t.ID
			return d
		}
	}
	if len(words) < 6 {
		return d
	}
	known := map[string]bool{}
	for _, s := range append(append(append([]string{pin.ID, pin.Title, pin.Description, p.Goal, p.Title}, pin.Does...), pin.Needs...), append(pin.Files, append(pin.Scope.In, p.Scope.In...)...)...) {
		for _, w := range significant(s) {
			known[stem(w)] = true
		}
	}
	for _, w := range words {
		if known[stem(w)] {
			return d
		}
	}
	d.Level, d.Reason = Soft, "shares no words with "+pin.ID
	return d
}

// matchPath reports whether pattern names rel: the same path, a folder
// that holds it, or a glob ("*" within a segment, "**" across segments).
// A leading "./" or "/" is the repo root.
func matchPath(pattern, rel string) bool {
	pattern = strings.TrimPrefix(strings.TrimPrefix(pattern, "./"), "/")
	if pattern == "" || pattern == "." {
		return false
	}
	pattern = strings.TrimSuffix(pattern, "/")
	if pattern == rel || strings.HasPrefix(rel, pattern+"/") {
		return true
	}
	if !strings.ContainsAny(pattern, "*?[") {
		return false
	}
	var pat []string
	for _, seg := range strings.Split(pattern, "/") {
		if seg == "**" && len(pat) > 0 && pat[len(pat)-1] == "**" {
			continue // a run of ** is one **
		}
		pat = append(pat, seg)
	}
	return globMatch(pat, strings.Split(rel, "/"), map[[2]int]bool{})
}

// globMatch matches pattern segments against path segments; tried
// remembers dead ends, so a plan's glob can't cost more than
// len(pat)×len(segs) steps on the UI goroutine.
func globMatch(pat, segs []string, tried map[[2]int]bool) bool {
	key := [2]int{len(pat), len(segs)}
	if tried[key] {
		return false
	}
	ok := func() bool {
		for len(pat) > 0 {
			if pat[0] == "**" {
				for i := 0; i <= len(segs); i++ {
					if globMatch(pat[1:], segs[i:], tried) {
						return true
					}
				}
				return false
			}
			if len(segs) == 0 {
				return false
			}
			if ok, err := path.Match(pat[0], segs[0]); err != nil || !ok {
				return false
			}
			pat, segs = pat[1:], segs[1:]
		}
		return len(segs) == 0
	}()
	if !ok {
		tried[key] = true
	}
	return ok
}

func globDir(dir string) bool { return strings.ContainsAny(dir, "*?[") }

// testSibling is a test file for a listed source file: foo_test.go,
// foo.test.ts, foo.spec.js, test_foo.py next to foo.*, or in test/,
// tests/ or __tests__/ beside it.
func testSibling(listed, rel string) bool {
	ld, rd := path.Dir(listed), path.Dir(rel)
	if rd != ld && rd != path.Join(ld, "test") && rd != path.Join(ld, "tests") && rd != path.Join(ld, "__tests__") {
		return false
	}
	base := strings.TrimSuffix(path.Base(listed), path.Ext(listed))
	name := path.Base(rel)
	stemName := strings.TrimSuffix(name, path.Ext(name))
	for _, s := range []string{base + "_test", base + ".test", base + ".spec", "test_" + base} {
		if stemName == s {
			return true
		}
	}
	return false
}

func pathWords(rel string) []string {
	return strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == '.' || r == '_' || r == '-' })
}

// outPath is what of the plan's or the pin's scope.out rel touches, or
// "". A path-like entry ("internal/mascot/**") matches as a path. Else a
// path touches an entry when its segments hold all of the entry's words,
// or the entry's last word, its head ("extra palettes" → palettes): a
// path names one thing, and that is the thing an entry is about.
func outPath(p *Plan, pin *Task, rel string, fold bool) string {
	have := map[string]bool{}
	for _, s := range pathWords(rel) {
		have[stem(strings.ToLower(s))] = true
	}
	for _, entry := range append(append([]string(nil), p.Scope.Out...), pin.Scope.Out...) {
		entry = strings.TrimSpace(entry)
		if strings.ContainsAny(entry, "/*") && !strings.Contains(entry, " ") {
			pattern := entry
			if fold {
				pattern = strings.ToLower(pattern) // as rel already is
			}
			if matchPath(pattern, rel) {
				return entry
			}
			continue
		}
		words := phraseWords(entry)
		if len(words) == 0 {
			continue
		}
		if head := words[len(words)-1]; len(head) >= 4 && !stopWords[head] && have[stem(head)] {
			return head
		}
		if all(words, have) {
			return entry
		}
	}
	return ""
}

// outPhrase is a scope.out entry whose words all appear in the prompt
// words given ("web UI" needs both web and UI; "agent auto-loads the
// plan" needs all four, not just "loads").
func outPhrase(p *Plan, pin *Task, prompt []string) string {
	have := map[string]bool{}
	for _, w := range prompt {
		for _, part := range strings.FieldsFunc(w, notWord) {
			have[stem(part)] = true
		}
	}
	for _, entry := range append(append([]string(nil), p.Scope.Out...), pin.Scope.Out...) {
		if words := phraseWords(entry); len(words) > 0 && all(words, have) {
			return strings.TrimSpace(entry)
		}
	}
	return ""
}

// phraseWords is every word of a scope.out entry but the articles, short
// ones included, so an entry like "web UI" or "CI" can match.
func phraseWords(entry string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(entry), notWord) {
		if !articles[w] {
			out = append(out, w)
		}
	}
	return out
}

func all(words []string, have map[string]bool) bool {
	for _, w := range words {
		if !have[stem(w)] {
			return false
		}
	}
	return true
}

var articles = map[string]bool{"a": true, "an": true, "the": true, "of": true, "to": true, "and": true, "or": true, "in": true, "on": true, "for": true, "any": true, "no": true}

// significant is the words in s that carry meaning: lowercase, four
// letters or more, and not on the stop list.
func significant(s string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(s), notWord) {
		if len([]rune(w)) >= 4 && !stopWords[w] {
			out = append(out, w)
		}
	}
	return out
}

func notWord(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }

// stem folds simple plurals so "palette" matches "palettes".
func stem(w string) string {
	switch {
	case strings.HasSuffix(w, "ies") && len(w) > 4:
		return w[:len(w)-3] + "y"
	case strings.HasSuffix(w, "es") && len(w) > 4 && strings.ContainsAny(w[len(w)-3:len(w)-2], "sxz"):
		return w[:len(w)-2]
	case strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") && len(w) > 4:
		return w[:len(w)-1]
	}
	return w
}

var stopWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`
		about after again also another anything because been before being both could does doing done
		each even every extra from have here into just like make many more most much must need needs
		only other over please same should some such than that their them then there these they thing
		things this those through very want what when where which while will with would your yours
		agent agents auto code file files plan plans task tasks work rex json`) {
		stopWords[w] = true
	}
}

func excerpt(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 60 {
		return string(r[:59]) + "…"
	}
	return s
}
