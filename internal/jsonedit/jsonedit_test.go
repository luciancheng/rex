package jsonedit

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func mustParse(t *testing.T, src string) *Doc {
	t.Helper()
	d, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, src)
	}
	return d
}

func TestSetReplacesOnlyThatValue(t *testing.T) {
	src := "{\n  \"version\": 1,\n  \"zeta\": \"keep <me>\",\n  \"hooks\": {\"a\": []},\n  \"alpha\": true\n}\n"
	d := mustParse(t, src)
	out, err := d.Set(d.Root(), "version", 2)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(src, `"version": 1`, `"version": 2`, 1)
	if string(out) != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
}

func TestSetInsertsAfterLastMemberWithNeighbourIndent(t *testing.T) {
	src := "{\n\t\"a\": 1,\n\t\"b\": [\n\t\t\"x\"\n\t]\n}"
	d := mustParse(t, src)
	out, err := d.Set(d.Root(), "c", []string{"y", "z"})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n\t\"a\": 1,\n\t\"b\": [\n\t\t\"x\"\n\t],\n\t\"c\": [\n\t\t\"y\",\n\t\t\"z\"\n\t]\n}"
	if string(out) != want {
		t.Fatalf("got\n%q\nwant\n%q", out, want)
	}
}

func TestSetNestedMultilineValueUsesKeyIndent(t *testing.T) {
	src := "{\n  \"hooks\": {\n    \"stop\": [\n      {\"command\": \"x\"}\n    ]\n  }\n}\n"
	d := mustParse(t, src)
	hooks := d.Root().Member("hooks")
	out, err := d.Set(hooks, "stop", []map[string]any{{"command": "rex"}})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"hooks\": {\n    \"stop\": [\n      {\n        \"command\": \"rex\"\n      }\n    ]\n  }\n}\n"
	if string(out) != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
}

func TestCompactObjectStaysOnOneLine(t *testing.T) {
	d := mustParse(t, `{"a":1,"b":2}`)
	out, err := d.Set(d.Root(), "c", []string{"x"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"a":1,"b":2, "c":["x"]}` {
		t.Fatalf("%s", out)
	}
	out, err = d.Set(d.Root(), "a", map[string]int{"n": 1})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"a":{"n":1},"b":2}` {
		t.Fatalf("%s", out)
	}
}

func TestInsertIntoEmptyObjects(t *testing.T) {
	d := mustParse(t, `{}`)
	out, err := d.Set(d.Root(), "k", "v")
	if err != nil || string(out) != `{"k": "v"}` {
		t.Fatalf("%v %s", err, out)
	}
	d = mustParse(t, "{\n  \"hooks\": {\n  }\n}")
	out, err = d.Set(d.Root().Member("hooks"), "stop", []int{1})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"hooks\": {\n    \"stop\": [\n      1\n    ]\n  }\n}"
	if string(out) != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
}

func TestCRLFAndBOMArePreserved(t *testing.T) {
	src := "\xef\xbb\xbf{\r\n  \"a\": 1\r\n}\r\n"
	d := mustParse(t, src)
	out, err := d.Set(d.Root(), "b", []string{"x"})
	if err != nil {
		t.Fatal(err)
	}
	want := "\xef\xbb\xbf{\r\n  \"a\": 1,\r\n  \"b\": [\r\n    \"x\"\r\n  ]\r\n}\r\n"
	if string(out) != want {
		t.Fatalf("got %q\nwant %q", out, want)
	}
}

func TestNoHTMLEscaping(t *testing.T) {
	d := mustParse(t, `{"a": 1}`)
	out, err := d.Set(d.Root(), "a", "<b> & </b>")
	if err != nil || !bytes.Contains(out, []byte(`"<b> & </b>"`)) {
		t.Fatalf("%v %s", err, out)
	}
}

func TestRemove(t *testing.T) {
	src := "{\n  \"a\": 1,\n  \"b\": 2,\n  \"c\": 3\n}"
	d := mustParse(t, src)
	cases := map[string]string{
		"a": "{\n  \"b\": 2,\n  \"c\": 3\n}",
		"b": "{\n  \"a\": 1,\n  \"c\": 3\n}",
		"c": "{\n  \"a\": 1,\n  \"b\": 2\n}",
		"z": src,
	}
	for key, want := range cases {
		out, err := d.Remove(d.Root(), key)
		if err != nil || string(out) != want {
			t.Fatalf("remove %s: %v\n%s", key, err, out)
		}
	}
	one := mustParse(t, `{"only": [1, 2]}`)
	out, _ := one.Remove(one.Root(), "only")
	if string(out) != `{}` {
		t.Fatalf("%s", out)
	}
}

func TestParseRefusesBadInput(t *testing.T) {
	for _, src := range []string{
		``, `{`, `{"a":1,}`, `{"a":1} {}`, `// comment
{}`, `{"a":1,"a":2}`, "\xff\xfe{\x00}\x00",
	} {
		if _, err := Parse([]byte(src)); err == nil {
			t.Fatalf("expected error for %q", src)
		}
	}
	if _, err := Parse([]byte(`{"a":1,"a":2}`)); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate keys: %v", err)
	}
}

func TestNestedKeysAreNotTopLevel(t *testing.T) {
	d := mustParse(t, `{"meta":{"status":"x"},"tasks":[{"id":"a","status":"pending"}]}`)
	if d.Root().Member("status") != nil {
		t.Fatal("nested status leaked to the top level")
	}
	task := d.Root().Member("tasks").Elems[0]
	if got := string(d.Raw(task.Member("status"))); got != `"pending"` {
		t.Fatalf("%s", got)
	}
}

func TestKeysWithColonsAndEscapes(t *testing.T) {
	d := mustParse(t, `{"a:b":1, "q\"uote": "x:y", "u\u00e9": [ ]}`)
	out, err := d.Set(d.Root(), "q\"uote", "z")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"a:b":1, "q\"uote": "z", "u\u00e9": [ ]}` {
		t.Fatalf("%s", out)
	}
}

// FuzzSplice checks the core promise: an edit yields valid JSON whose tree
// is the old tree with exactly that change, and nothing else moves.
func FuzzSplice(f *testing.F) {
	for _, seed := range []string{
		`{}`, `{"a":1}`, "{\n  \"a\": [1, 2],\n  \"b\": {\"c\": null}\n}\n",
		"\xef\xbb\xbf{\r\n\t\"x\": \"y\"\r\n}", `{"a":{"b":{"c":[{"d":"e"}]}}}`,
		`  {"k": "v<>&"}  `,
	} {
		f.Add([]byte(seed), "a", "new")
		f.Add([]byte(seed), "zz", "")
	}
	f.Fuzz(func(t *testing.T, src []byte, key, val string) {
		if !utf8.ValidString(key) || !utf8.ValidString(val) {
			return // JSON cannot carry invalid UTF-8; the encoder substitutes U+FFFD
		}
		d, err := Parse(src)
		if err != nil || d.Root().Kind != '{' {
			return
		}
		var before map[string]any
		if err := json.Unmarshal(bytes.TrimPrefix(src, bomUTF8), &before); err != nil {
			return
		}
		out, err := d.Set(d.Root(), key, val)
		if err != nil {
			t.Fatalf("set: %v", err)
		}
		body := bytes.TrimPrefix(out, bomUTF8)
		if !json.Valid(body) {
			t.Fatalf("invalid output %q from %q", out, src)
		}
		var after map[string]any
		if err := json.Unmarshal(body, &after); err != nil {
			t.Fatal(err)
		}
		before[key] = val
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("tree changed beyond %q:\n%q\n%q", key, src, out)
		}
		if _, err := Parse(out); err != nil {
			t.Fatalf("reparse: %v", err)
		}
		removed, err := d.Remove(d.Root(), key)
		if err != nil || !json.Valid(bytes.TrimPrefix(removed, bomUTF8)) {
			t.Fatalf("remove: %v %q", err, removed)
		}
	})
}

func TestParseRefusesAbsurdNesting(t *testing.T) {
	deep := strings.Repeat("[", 100000) + strings.Repeat("]", 100000)
	if _, err := Parse([]byte(`{"a":` + deep + `}`)); err == nil || !strings.Contains(err.Error(), "nested") {
		t.Fatalf("err %v", err)
	}
	ok := strings.Repeat("[", 100) + strings.Repeat("]", 100)
	if _, err := Parse([]byte(`{"a":` + ok + `}`)); err != nil {
		t.Fatal(err)
	}
}

func TestArrayEdits(t *testing.T) {
	d := mustParse(t, "{\n  \"a\": [\n    {\"x\": 1},\n    {\"x\": 2}\n  ]\n}")
	arr := d.Root().Member("a")
	out, err := d.Append(arr, map[string]int{"x": 3})
	if err != nil || !strings.Contains(string(out), "  {\"x\": 2},\n    {\n      \"x\": 3\n    }\n  ]") {
		t.Fatalf("append %v\n%s", err, out)
	}
	out, _ = d.RemoveElem(arr, 0)
	if string(out) != "{\n  \"a\": [\n    {\"x\": 2}\n  ]\n}" {
		t.Fatalf("remove first\n%s", out)
	}
	out, _ = d.RemoveElem(arr, 1)
	if string(out) != "{\n  \"a\": [\n    {\"x\": 1}\n  ]\n}" {
		t.Fatalf("remove last\n%s", out)
	}
	out, _ = d.ReplaceElem(arr, 1, map[string]int{"y": 9})
	if !strings.Contains(string(out), "{\"x\": 1},\n    {\n      \"y\": 9\n    }") {
		t.Fatalf("replace\n%s", out)
	}
	empty := mustParse(t, `{"a": []}`)
	out, _ = empty.Append(empty.Root().Member("a"), 1)
	if string(out) != `{"a": [1]}` {
		t.Fatalf("%s", out)
	}
}

// Prepend lays the new first element out like the rest, and removing it
// gives back the document byte for byte.
func TestPrepend(t *testing.T) {
	for src, want := range map[string]string{
		"{\n  \"a\": [\n    {\"x\": 1}\n  ]\n}":         "{\n  \"a\": [\n    {\n      \"x\": 0\n    },\n    {\"x\": 1}\n  ]\n}",
		"{\r\n  \"a\": [\r\n    {\"x\": 1}\r\n  ]\r\n}": "{\r\n  \"a\": [\r\n    {\r\n      \"x\": 0\r\n    },\r\n    {\"x\": 1}\r\n  ]\r\n}",
		`{"a": [{"x": 1}, {"x": 2}]}`:                   `{"a": [{"x":0}, {"x": 1}, {"x": 2}]}`,
		`{"a": []}`:                                     `{"a": [{"x":0}]}`,
	} {
		d := mustParse(t, src)
		out, err := d.Prepend(d.Root().Member("a"), map[string]int{"x": 0})
		if err != nil || string(out) != want {
			t.Fatalf("%q: %v\n%q", src, err, out)
		}
		if src == `{"a": []}` {
			continue
		}
		back := mustParse(t, string(out))
		if out, _ := back.RemoveElem(back.Root().Member("a"), 0); string(out) != src {
			t.Fatalf("remove it again: %q, want %q", out, src)
		}
	}
	if _, err := mustParse(t, `{"a": 1}`).Prepend(nil, 1); err == nil {
		t.Fatal("not an array")
	}
}
