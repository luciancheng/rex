package watch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/luciancheng/rex/internal/jsonedit"
)

// ErrConfigChanged means another program wrote the file while Rex was
// merging into it. Nothing was written; running the command again is safe.
var ErrConfigChanged = errors.New("file changed while rex was editing it; nothing written, run again")

// UpdateJSONFile merges into an agent's JSON config without clobbering it.
// The file is parsed (an unparseable file is left untouched and reported),
// mutate returns the new bytes via jsonedit so every key Rex does not own
// keeps its bytes, the previous version is saved as <file>.bak, and the
// result replaces the file atomically. When the file does not exist, empty
// is used as the starting document. It reports whether anything changed.
func UpdateJSONFile(path string, empty []byte, mutate func(*jsonedit.Doc) ([]byte, error)) (bool, error) {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return false, fmt.Errorf("%s is a broken symlink; fix it first, rex did not change it", path)
		}
		path = resolved
	}
	original, readErr := os.ReadFile(path)
	exists := readErr == nil
	if readErr != nil && !os.IsNotExist(readErr) {
		return false, readErr
	}
	src := original
	if !exists || len(bytes.TrimSpace(original)) == 0 {
		src = empty // a missing or empty file starts from scratch
	}
	doc, err := jsonedit.Parse(src)
	if err != nil {
		return false, fmt.Errorf("%s is not valid JSON (%v); fix it by hand, rex did not change it", path, err)
	}
	out, err := mutate(doc)
	if err != nil {
		return false, err
	}
	if exists && bytes.Equal(out, original) {
		return false, nil
	}
	if !json.Valid(bytes.TrimPrefix(out, []byte("\xef\xbb\xbf"))) {
		return false, fmt.Errorf("rex produced invalid JSON for %s; nothing written", path)
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".rex-*")
	if err != nil {
		return false, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		return false, err
	}
	current, err := os.ReadFile(path)
	switch {
	case exists && (err != nil || !bytes.Equal(current, original)):
		return false, ErrConfigChanged
	case !exists && err == nil:
		return false, ErrConfigChanged
	}
	if exists {
		if err := os.WriteFile(path+".bak", original, mode); err != nil {
			return false, err
		}
	}
	if err := os.Rename(tmpPath, path); err != nil {
		// Windows refuses to replace a file another process holds open; fall
		// back to writing in place (not atomic, but the .bak is already saved).
		if werr := os.WriteFile(path, out, mode); werr != nil {
			return false, err
		}
	}
	return true, nil
}

// editor applies a series of edits, re-indexing the document after each
// one because offsets move.
type editor struct {
	doc *jsonedit.Doc
}

func (e *editor) apply(edit func(*jsonedit.Doc) ([]byte, error)) error {
	out, err := edit(e.doc)
	if err != nil {
		return err
	}
	doc, err := jsonedit.Parse(out)
	if err != nil {
		return fmt.Errorf("rex produced invalid JSON: %v", err)
	}
	e.doc = doc
	return nil
}
