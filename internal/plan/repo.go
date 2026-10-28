package plan

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/luciancheng/rex/internal/watch"
)

// FindRepo walks up from start to the directory holding .git. Outside a
// repo it returns start itself and false. An empty start is the process's
// own folder, which a hook never asks for: a hook's folder is not the
// agent's.
func FindRepo(start string) (string, bool) {
	if start == "" {
		start, _ = os.Getwd()
	}
	dir, err := filepath.Abs(start)
	if err != nil {
		return start, false
	}
	orig := dir
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return orig, false
		}
		dir = parent
	}
}

// Dir is where repo's plans live: rex/plans.
func Dir(repo string) string {
	return filepath.Join(repo, "rex", "plans")
}

// IsFile reports a plan file's name: *.rex.json, in any case, with a stem.
func IsFile(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".rex.json") && len(name) > len(".rex.json")
}

// Jail is p as one of repo's plan files, or "": an existing regular
// *.rex.json under rex/plans once symlinks are resolved, so a link can't
// lead out. A relative p is taken from cwd. scope is repo's
// (watch.NewScope); its Rel cleans p, so a ".." is judged by where it
// leads.
func Jail(scope watch.Scope, repo, p, cwd string) string {
	if p == "" {
		return ""
	}
	if !filepath.IsAbs(p) {
		if !filepath.IsAbs(cwd) {
			return ""
		}
		p = filepath.Join(cwd, p)
	}
	rel, in := scope.Rel(p)
	if !in || !strings.HasPrefix(rel, "rex/plans/") || !IsFile(path.Base(rel)) {
		return ""
	}
	full := filepath.Join(repo, filepath.FromSlash(rel))
	if info, err := os.Stat(full); err != nil || !info.Mode().IsRegular() {
		return ""
	}
	return full
}

// File is a plan file a walk found.
type File struct {
	Path string
	Info fs.FileInfo // the file's, a symlink followed: tells one file under two names apart
	Link bool        // the name is a symlink
}

// Files lists the plan files under root in lexical order. Only regular
// files (or symlinks to one) are plans: a FIFO or a device named
// *.rex.json would hang the reads that follow. A symlink counts only when
// it leads to a plan file under root (Within), as the jail judges one: a
// link out of rex/plans leads nowhere.
func Files(root string) (files []File) {
	scope := watch.NewScope(root)
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !IsFile(d.Name()) {
			return nil
		}
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		link := d.Type()&fs.ModeSymlink != 0
		if rel, in := scope.Rel(p); link && (!in || !IsFile(path.Base(rel))) {
			return nil
		}
		files = append(files, File{Path: p, Info: info, Link: link})
		return nil
	})
	return files
}

// Within is p as a path under root once both are cleaned and their
// symlinks resolved, and whether it is one: where a link in rex/plans
// leads, judged as Jail judges it.
func Within(root, p string) (rel string, ok bool) {
	rel, in := watch.NewScope(root).Rel(p)
	return rel, in && rel != "." && filepath.IsAbs(p)
}
