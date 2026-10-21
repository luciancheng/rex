package watch

import (
	"strings"
	"testing"
)

func TestCleanTextStripsTerminalControl(t *testing.T) {
	cases := map[string]string{
		"plain":                                  "plain",
		"red \x1b[31mtext\x1b[0m":                "red text",
		"clip \x1b]52;c;cHduZWQ=\x07grab":        "clip grab",
		"title \x1b]0;evil\x1b\\ok":              "title ok",
		"clear\x1b[2J\x1b[Hscreen":               "clearscreen",
		"over\rwrite":                            "overwrite",
		"crlf\r\nline":                           "crlf\nline",
		"bell\x07 null\x00 del\x7f":              "bell null del",
		"c1 \u009b31m csi":                       "c1 31m csi",
		"bidi \u202eevil\u202c ok":               "bidi evil ok",
		"isolate \u2066x\u2069":                  "isolate x",
		"bad \xff\xfe utf8":                      "bad  utf8",
		"\ttab\ta":                               "    tab a",
		"code:\n\tindented":                      "code:\n    indented",
		"keeps unicode café 中文 ✓ ▌":              "keeps unicode café 中文 ✓ ▌",
		"dcs \x1bPq#0;2;0;0;0#0~\x1b\\ after":    "dcs  after",
		"lone escape at end \x1b":                "lone escape at end ",
		"unterminated osc \x1b]8;;http://x link": "unterminated osc ",
	}
	for in, want := range cases {
		if got := CleanText(in); got != want {
			t.Errorf("CleanText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanTextLeavesNoControlBytes(t *testing.T) {
	hostile := "\x1b]52;c;ZXZpbA==\x07\x1b[?1049l\x1b[2J\r\x08\x1b(0\x1bc\u009d0;x\u009c\x05\x1a"
	got := CleanText(hostile + "visible")
	for _, r := range got {
		if r < 0x20 && r != '\n' || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			t.Fatalf("control %U left in %q", r, got)
		}
	}
	if !strings.HasSuffix(got, "visible") {
		t.Fatalf("%q", got)
	}
}
