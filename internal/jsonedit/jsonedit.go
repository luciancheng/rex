// Package jsonedit changes one member of a JSON document in place and keeps
// every other byte as it was: key order, spacing, line endings, a leading
// BOM, and fields Rex does not know about. Plan files and agent config
// files are shared with humans, agents, and git, so Rex never round-trips
// them through a map.
package jsonedit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

var (
	ErrDuplicateKey = errors.New("duplicate key")
	ErrNotObject    = errors.New("not an object")
	ErrUTF16        = errors.New("file is UTF-16; save it as UTF-8")
)

var bomUTF8 = []byte("\xef\xbb\xbf")

// Doc is a parsed JSON document with the byte span of every value.
type Doc struct {
	bom  []byte
	body []byte
	root *Node
}

// Node is one JSON value. Start and End are offsets into the document body
// (after any BOM); End is exclusive.
type Node struct {
	Kind    byte // '{', '[', '"', or 0 for numbers, booleans and null
	Start   int
	End     int
	Members []Member
	Elems   []*Node
}

// Member is one key of an object. KeyStart is the offset of the key's
// opening quote and KeyEnd the offset just past its closing quote.
type Member struct {
	Key      string
	KeyStart int
	KeyEnd   int
	Value    *Node
}

// Parse indexes data. Duplicate keys are refused, because encoding/json
// keeps the last one and an edit would silently target the wrong copy.
func Parse(data []byte) (*Doc, error) {
	if bytes.HasPrefix(data, []byte{0xff, 0xfe}) || bytes.HasPrefix(data, []byte{0xfe, 0xff}) {
		return nil, ErrUTF16
	}
	d := &Doc{}
	if bytes.HasPrefix(data, bomUTF8) {
		d.bom = bomUTF8
		data = data[len(bomUTF8):]
	}
	d.body = data
	p := &parser{src: data, dec: json.NewDecoder(bytes.NewReader(data))}
	p.dec.UseNumber()
	root, err := p.value()
	if err != nil {
		return nil, err
	}
	if _, err := p.dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("unexpected data after the top-level value")
	}
	d.root = root
	return d, nil
}

type parser struct {
	src   []byte
	dec   *json.Decoder
	last  int
	depth int
}

// maxDepth bounds nesting so a hostile config can't exhaust the stack.
const maxDepth = 512

// next reads one token and returns where it starts. InputOffset is the
// offset just past the token, so the start is found by skipping the
// whitespace, commas and colons that follow the previous token.
func (p *parser) next() (json.Token, int, error) {
	tok, err := p.dec.Token()
	if err != nil {
		return nil, 0, err
	}
	start := p.last
	for start < len(p.src) {
		c := p.src[start]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',' || c == ':' {
			start++
			continue
		}
		break
	}
	p.last = int(p.dec.InputOffset())
	return tok, start, nil
}

func (p *parser) value() (*Node, error) {
	tok, start, err := p.next()
	if err != nil {
		if err == io.EOF {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return p.valueFrom(tok, start)
}

func (p *parser) valueFrom(tok json.Token, start int) (*Node, error) {
	switch t := tok.(type) {
	case json.Delim:
		if p.depth++; p.depth > maxDepth {
			return nil, fmt.Errorf("JSON nested deeper than %d levels", maxDepth)
		}
		defer func() { p.depth-- }()
		switch t {
		case '{':
			n := &Node{Kind: '{', Start: start}
			seen := map[string]bool{}
			for p.dec.More() {
				keyTok, keyStart, err := p.next()
				if err != nil {
					return nil, err
				}
				key, ok := keyTok.(string)
				if !ok {
					return nil, fmt.Errorf("object key is not a string")
				}
				if seen[key] {
					return nil, fmt.Errorf("%w %q", ErrDuplicateKey, key)
				}
				seen[key] = true
				keyEnd := p.last
				val, err := p.value()
				if err != nil {
					return nil, err
				}
				n.Members = append(n.Members, Member{Key: key, KeyStart: keyStart, KeyEnd: keyEnd, Value: val})
			}
			if _, _, err := p.next(); err != nil {
				return nil, err
			}
			n.End = p.last
			return n, nil
		case '[':
			n := &Node{Kind: '[', Start: start}
			for p.dec.More() {
				val, err := p.value()
				if err != nil {
					return nil, err
				}
				n.Elems = append(n.Elems, val)
			}
			if _, _, err := p.next(); err != nil {
				return nil, err
			}
			n.End = p.last
			return n, nil
		}
		return nil, fmt.Errorf("unexpected %v", t)
	case string:
		return &Node{Kind: '"', Start: start, End: p.last}, nil
	default:
		return &Node{Start: start, End: p.last}, nil
	}
}

// Root is the top-level value.
func (d *Doc) Root() *Node { return d.root }

// Bytes is the document as written, BOM included.
func (d *Doc) Bytes() []byte {
	out := make([]byte, 0, len(d.bom)+len(d.body))
	out = append(out, d.bom...)
	return append(out, d.body...)
}

// Raw is the exact source text of n.
func (d *Doc) Raw(n *Node) []byte { return d.body[n.Start:n.End] }

// Decode unmarshals the value at n into v.
func (d *Doc) Decode(n *Node, v any) error { return json.Unmarshal(d.Raw(n), v) }

// Member returns the value of key in object n, or nil.
func (n *Node) Member(key string) *Node {
	if n == nil {
		return nil
	}
	for i := range n.Members {
		if n.Members[i].Key == key {
			return n.Members[i].Value
		}
	}
	return nil
}

func (n *Node) memberIndex(key string) int {
	for i := range n.Members {
		if n.Members[i].Key == key {
			return i
		}
	}
	return -1
}

// Set writes value under key in object obj, replacing the old value or
// adding the key after the last member. It returns the new document bytes;
// d itself is not changed.
func (d *Doc) Set(obj *Node, key string, value any) ([]byte, error) {
	if obj == nil || obj.Kind != '{' {
		return nil, ErrNotObject
	}
	style := d.styleFor(obj)
	if i := obj.memberIndex(key); i >= 0 {
		old := obj.Members[i].Value
		enc, err := style.encode(value, d.lineIndent(obj.Members[i].KeyStart))
		if err != nil {
			return nil, err
		}
		return d.splice(old.Start, old.End, enc), nil
	}
	if len(obj.Members) == 0 {
		return d.insertIntoEmpty(obj, key, value, style)
	}
	last := obj.Members[len(obj.Members)-1]
	indent := d.lineIndent(last.KeyStart)
	enc, err := style.encode(value, indent)
	if err != nil {
		return nil, err
	}
	k, _ := encodeJSON(key, "", "")
	var ins []byte
	if style.multiline {
		ins = append(ins, ',')
		ins = append(ins, style.newline...)
		ins = append(ins, indent...)
	} else {
		ins = append(ins, ", "...)
	}
	ins = append(ins, k...)
	ins = append(ins, style.colon...)
	ins = append(ins, enc...)
	return d.splice(last.Value.End, last.Value.End, ins), nil
}

func (d *Doc) insertIntoEmpty(obj *Node, key string, value any, style style) ([]byte, error) {
	k, _ := encodeJSON(key, "", "")
	interior := d.body[obj.Start+1 : obj.End-1]
	if !bytes.ContainsAny(interior, "\n") {
		enc, err := style.encode(value, "")
		if err != nil {
			return nil, err
		}
		var ins []byte
		ins = append(ins, '{')
		ins = append(ins, k...)
		ins = append(ins, style.colon...)
		ins = append(ins, enc...)
		ins = append(ins, '}')
		return d.splice(obj.Start, obj.End, ins), nil
	}
	outer := d.lineIndent(obj.Start)
	inner := outer + style.unit
	style.multiline = true
	enc, err := style.encode(value, inner)
	if err != nil {
		return nil, err
	}
	var ins []byte
	ins = append(ins, '{')
	ins = append(ins, style.newline...)
	ins = append(ins, inner...)
	ins = append(ins, k...)
	ins = append(ins, style.colon...)
	ins = append(ins, enc...)
	ins = append(ins, style.newline...)
	ins = append(ins, outer...)
	ins = append(ins, '}')
	return d.splice(obj.Start, obj.End, ins), nil
}

// Remove deletes key from object obj along with its separator. A missing
// key returns the document unchanged.
func (d *Doc) Remove(obj *Node, key string) ([]byte, error) {
	if obj == nil || obj.Kind != '{' {
		return nil, ErrNotObject
	}
	i := obj.memberIndex(key)
	if i < 0 {
		return d.Bytes(), nil
	}
	m := obj.Members[i]
	switch {
	case len(obj.Members) == 1:
		return d.splice(obj.Start+1, obj.End-1, nil), nil
	case i == 0:
		return d.splice(m.KeyStart, obj.Members[1].KeyStart, nil), nil
	default:
		prev := obj.Members[i-1]
		return d.splice(prev.Value.End, m.Value.End, nil), nil
	}
}

// ReplaceElem writes value in place of element i of array arr.
func (d *Doc) ReplaceElem(arr *Node, i int, value any) ([]byte, error) {
	if arr == nil || arr.Kind != '[' || i < 0 || i >= len(arr.Elems) {
		return nil, fmt.Errorf("no element %d", i)
	}
	el := arr.Elems[i]
	enc, err := d.arrayStyle(arr).encode(value, d.lineIndent(el.Start))
	if err != nil {
		return nil, err
	}
	return d.splice(el.Start, el.End, enc), nil
}

// RemoveElem deletes element i of array arr along with its separator.
func (d *Doc) RemoveElem(arr *Node, i int) ([]byte, error) {
	if arr == nil || arr.Kind != '[' || i < 0 || i >= len(arr.Elems) {
		return nil, fmt.Errorf("no element %d", i)
	}
	switch {
	case len(arr.Elems) == 1:
		return d.splice(arr.Start+1, arr.End-1, nil), nil
	case i == 0:
		return d.splice(arr.Elems[0].Start, arr.Elems[1].Start, nil), nil
	default:
		return d.splice(arr.Elems[i-1].End, arr.Elems[i].End, nil), nil
	}
}

// Append adds value after the last element of array arr, laid out like its
// neighbours.
func (d *Doc) Append(arr *Node, value any) ([]byte, error) {
	if arr == nil || arr.Kind != '[' {
		return nil, fmt.Errorf("not an array")
	}
	st := d.arrayStyle(arr)
	if len(arr.Elems) == 0 {
		if !bytes.ContainsAny(d.body[arr.Start+1:arr.End-1], "\n") {
			enc, err := st.encode(value, "")
			if err != nil {
				return nil, err
			}
			return d.splice(arr.Start, arr.End, append(append([]byte{'['}, enc...), ']')), nil
		}
		outer := d.lineIndent(arr.Start)
		inner := outer + st.unit
		enc, err := st.encode(value, inner)
		if err != nil {
			return nil, err
		}
		var ins []byte
		ins = append(ins, '[')
		ins = append(ins, st.newline...)
		ins = append(ins, inner...)
		ins = append(ins, enc...)
		ins = append(ins, st.newline...)
		ins = append(ins, outer...)
		ins = append(ins, ']')
		return d.splice(arr.Start, arr.End, ins), nil
	}
	last := arr.Elems[len(arr.Elems)-1]
	indent := d.lineIndent(last.Start)
	enc, err := st.encode(value, indent)
	if err != nil {
		return nil, err
	}
	var ins []byte
	if st.multiline {
		ins = append(ins, ',')
		ins = append(ins, st.newline...)
		ins = append(ins, indent...)
	} else {
		ins = append(ins, ", "...)
	}
	ins = append(ins, enc...)
	return d.splice(last.End, last.End, ins), nil
}

// Prepend adds value before the first element of array arr, laid out like
// its neighbours.
func (d *Doc) Prepend(arr *Node, value any) ([]byte, error) {
	if arr == nil || arr.Kind != '[' {
		return nil, fmt.Errorf("not an array")
	}
	if len(arr.Elems) == 0 {
		return d.Append(arr, value)
	}
	st := d.arrayStyle(arr)
	first := arr.Elems[0]
	indent := d.lineIndent(first.Start)
	enc, err := st.encode(value, indent)
	if err != nil {
		return nil, err
	}
	ins := append(enc, ',')
	if st.multiline {
		ins = append(ins, st.newline...)
		ins = append(ins, indent...)
	} else {
		ins = append(ins, ' ')
	}
	return d.splice(first.Start, first.Start, ins), nil
}

func (d *Doc) arrayStyle(arr *Node) style {
	s := d.styleFor(&Node{Kind: '{', Start: arr.Start, End: arr.End})
	if len(arr.Elems) > 0 {
		child := d.lineIndent(arr.Elems[0].Start)
		parent := d.lineIndent(arr.Start)
		if strings.HasPrefix(child, parent) && len(child) > len(parent) {
			s.unit = child[len(parent):]
		}
	}
	return s
}

func (d *Doc) splice(start, end int, with []byte) []byte {
	out := make([]byte, 0, len(d.bom)+len(d.body)-(end-start)+len(with))
	out = append(out, d.bom...)
	out = append(out, d.body[:start]...)
	out = append(out, with...)
	return append(out, d.body[end:]...)
}

// lineIndent is the run of spaces or tabs that starts the line holding
// offset.
func (d *Doc) lineIndent(offset int) string {
	start := offset
	for start > 0 && d.body[start-1] != '\n' {
		start--
	}
	end := start
	for end < offset && (d.body[end] == ' ' || d.body[end] == '\t') {
		end++
	}
	return string(d.body[start:end])
}

type style struct {
	multiline bool
	unit      string
	newline   string
	colon     string
}

// styleFor copies the layout of obj's neighbours: one member per line or
// one line, the indent unit, the line ending, and ":" vs ": ".
func (d *Doc) styleFor(obj *Node) style {
	s := style{unit: "  ", newline: "\n", colon: ": "}
	if bytes.Contains(d.body, []byte("\r\n")) {
		s.newline = "\r\n"
	}
	s.multiline = bytes.ContainsAny(d.body[obj.Start:obj.End], "\n")
	if len(obj.Members) > 0 {
		m := obj.Members[0]
		sep := d.body[m.KeyEnd:m.Value.Start]
		if c := bytes.IndexByte(sep, ':'); c >= 0 && c+1 == len(sep) {
			s.colon = ":"
		}
		child := d.lineIndent(m.KeyStart)
		parent := d.lineIndent(obj.Start)
		if strings.HasPrefix(child, parent) && len(child) > len(parent) {
			s.unit = child[len(parent):]
		}
	} else if unit := d.guessUnit(); unit != "" {
		s.unit = unit
	}
	return s
}

func (d *Doc) guessUnit() string {
	for _, line := range strings.Split(string(d.body), "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if lead := line[:len(line)-len(trimmed)]; lead != "" && trimmed != "" {
			return lead
		}
	}
	return ""
}

func (s style) encode(value any, indent string) ([]byte, error) {
	if !s.multiline {
		return encodeJSON(value, "", "")
	}
	out, err := encodeJSON(value, indent, s.unit)
	if err != nil {
		return nil, err
	}
	if s.newline != "\n" {
		out = bytes.ReplaceAll(out, []byte("\n"), []byte(s.newline))
	}
	return out, nil
}

// encodeJSON is json.Marshal without HTML escaping, so "<" stays "<" and a
// rewritten value does not churn the diff.
func encodeJSON(value any, prefix, indent string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent != "" {
		enc.SetIndent(prefix, indent)
	}
	if err := enc.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
