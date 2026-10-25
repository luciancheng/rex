package tui

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/luciancheng/rex/internal/plan"
)

type PaneKind int

const (
	PaneChat PaneKind = iota
	PanePlans
)

type PlanScreen int

const (
	ScreenHome PlanScreen = iota
	ScreenBrowse
	ScreenCreate
	ScreenFolder
)

type PlanRowKind int

const (
	PlanRowPlan        PlanRowKind = iota // a plan: the open one, a recent, a root plan, a match, a Browse row
	PlanRowDir                            // a folder, under FOLDERS or in Browse
	PlanRowUp                             // ../ in Browse
	PlanRowFile                           // a file in Browse that is not a plan
	PlanRowNew                            // + New plan
	PlanRowMkdir                          // + New folder
	PlanRowCreateNamed                    // + Create 'x' at the end of a find
)

// The plan list's groups (docs/ux.md §11), each under its H2.
const (
	groupOpen    = "OPEN"
	groupRecent  = "RECENT"
	groupFolders = "FOLDERS"
)

type PlanRow struct {
	Kind  PlanRowKind
	Name  string // a plan's path under rex/plans (or its file name in Browse), a folder's name
	Path  string
	Group string // the plan list's group it sits in; "" in Find and Browse
	Plans int    // a folder's plans, in it and below
	Open  bool   // the open plan, by file, whatever its spelling (readSums)
}

type PlanBrowser struct {
	Repo    string
	Root    string
	Dir     string
	Filter  string // Browse: narrows the current folder
	Find    string // Home: searches every plan under rex/plans
	Name    string // name prompt: the name being typed
	Cursor  int
	Screen  PlanScreen
	From    PlanScreen
	Current string // the open plan
	Err     string
	Notice  string
	Rows    []PlanRow
	View    int // first visible body line, kept between frames

	fresh    bool   // the screen just changed: put the cursor on its default row
	openPath string // Current as the walk names it (Rebuild)
	walk     planWalk
	sums     map[string]planSummary
	doc      *planSummary // the open plan as Rex shows it (showDoc), for docPath
	docPath  string
	opened   []string // the plans opened this run, the latest first; memory only
	findMore int      // find matches past maxMatches, left out
	total    int      // what the page title counts: plans, matches, or the plans in and under this folder
	now      func() time.Time

	inGit bool
}

// planWalk caches the list of plans and top folders so typing a find query
// doesn't walk the tree on every key.
type planWalk struct {
	at    time.Time
	plans []planStat
	dirs  []string // the folders right under rex/plans, by name
}

const walkTTL = 500 * time.Millisecond

type PlanAction int

const (
	PlanNone PlanAction = iota
	PlanStay
	PlanOpenChat
	PlanStartCreate
	PlanStartFolder
)

// maxRecent is how many plans RECENT lists besides the open one.
const maxRecent = 5

func PlansDir(repo string) string { return plan.Dir(repo) }

// errRexFile means something that is not a folder sits where the plans
// folder goes. Rex never deletes it; plans stay off until the human moves it.
var errRexFile = errors.New("move the rex file to create plans") // fits a 38-cell pane

const (
	rexFileNotice    = "rex/ is a file here · plans off"
	outsideGitPrefix = "not in a git repo · plans go to "
)

// EnsurePlansDir creates rex/plans when the human creates a plan or
// folder. Starting Rex never calls it, and it never deletes anything.
func EnsurePlansDir(repo string) (string, error) {
	root := PlansDir(repo)
	if rexIsFile(repo) {
		return root, errRexFile
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return root, err
	}
	keep := filepath.Join(root, ".gitkeep")
	if _, err := os.Stat(keep); err != nil {
		if err := os.WriteFile(keep, []byte(""), 0o644); err != nil {
			return root, err
		}
	}
	return root, nil
}

// rexIsFile reports whether repo/rex exists but is not a usable folder: a
// regular file, a symlink to one, or a dangling symlink.
func rexIsFile(repo string) bool {
	path := filepath.Join(repo, "rex")
	if _, err := os.Lstat(path); err != nil {
		return false
	}
	info, err := os.Stat(path)
	return err != nil || !info.IsDir()
}

// legacyPlans counts plans left in an old rex-plans/ folder. Rex only
// reports them; moving them is up to the human.
func legacyPlans(repo string) int {
	ents, err := os.ReadDir(filepath.Join(repo, "rex-plans"))
	if err != nil {
		return 0
	}
	n := 0
	for _, ent := range ents {
		if !ent.IsDir() && plan.IsFile(ent.Name()) {
			n++
		}
	}
	return n
}

func PlanFileName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return ""
	}
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".rex.json") {
		if !plan.IsFile(name) {
			return ""
		}
		return name
	}
	return name + ".rex.json"
}

func NewPlanBrowser(start string) *PlanBrowser {
	repo, inGit := plan.FindRepo(start)
	b := &PlanBrowser{Repo: repo, Screen: ScreenHome, inGit: inGit, fresh: true}
	b.Root, b.Dir = PlansDir(repo), PlansDir(repo)
	b.Rebuild()
	return b
}

// refreshNotice says what is off about where plans live. It is recomputed
// on every rebuild, so moving a stray rex file away clears it.
func (b *PlanBrowser) refreshNotice() {
	switch {
	case rexIsFile(b.Repo):
		b.Notice = rexFileNotice
	case !b.inGit:
		b.Notice = outsideGitPrefix + homePath(filepath.Join(b.Repo, "rex", "plans"))
	case legacyPlans(b.Repo) > 0:
		b.Notice = fmt.Sprintf("rex-plans/ has %d old plan(s) · move them into rex/plans", legacyPlans(b.Repo))
	default:
		b.Notice = ""
	}
}

// homePath shows path with ~ for the home folder.
func homePath(path string) string {
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(filepath.Join("~", rel))
		}
	}
	return filepath.ToSlash(path)
}

func (b *PlanBrowser) Prompting() bool {
	return b.Screen == ScreenCreate || b.Screen == ScreenFolder
}

func (b *PlanBrowser) Browsing() bool {
	return b.Screen == ScreenBrowse
}

// plans lists every plan under the root, newest first, from a short cache.
func (b *PlanBrowser) plans() []planStat {
	return b.walked().plans
}

// walked is the cached walk of rex/plans: its plans and top folders.
func (b *PlanBrowser) walked() planWalk {
	if b.walk.plans == nil || time.Since(b.walk.at) > walkTTL {
		b.walk = planWalk{at: time.Now(), plans: walkPlans(b.Root), dirs: topDirs(b.Root)}
	}
	return b.walk
}

func (b *PlanBrowser) RelDir() string {
	dir := b.Dir
	if b.Screen == ScreenHome {
		dir = b.Root // Home finds and creates from rex/plans
	}
	rel, err := filepath.Rel(b.Repo, dir)
	if err != nil {
		return "rex/plans"
	}
	return filepath.ToSlash(rel)
}

func (b *PlanBrowser) CurrentRel() string {
	if b == nil || b.Current == "" {
		return ""
	}
	rel, err := filepath.Rel(b.Repo, b.Current)
	if err != nil {
		return filepath.ToSlash(filepath.Join(b.RelDir(), filepath.Base(b.Current)))
	}
	return filepath.ToSlash(rel)
}

func (b *PlanBrowser) Rebuild() {
	b.refreshNotice()
	if b.Dir == "" {
		b.Dir = b.Root
	}
	if rel, err := filepath.Rel(b.Root, b.Dir); err != nil || strings.HasPrefix(rel, "..") {
		b.Dir = b.Root
	}
	if b.Screen == ScreenBrowse && b.Dir == b.Root {
		b.Screen = ScreenHome // no Browse root: FOLDERS is it (docs/ux.md §11)
	}
	if b.Screen == ScreenHome {
		b.Dir = b.Root
	}
	keep := b.selectedKey()
	all, set := b.planSets()
	switch b.Screen {
	case ScreenBrowse:
		b.Rows, b.findMore = buildBrowseRows(b.Root, b.Dir, b.Filter), 0
	default:
		b.Rows, b.findMore = buildHomeRows(b.Root, all, set, b.walked().dirs, b.Find, b.openPath, b.opened)
	}
	// Every count is of one plan set, each file once (docs/ux.md §11).
	b.total = 0
	switch {
	case b.Screen == ScreenBrowse:
		// The plans in this folder and below (docs/ux.md §4), under the
		// rows a filter leaves: a listed folder counts every plan in it.
		listed := map[string]bool{}
		for _, row := range b.Rows {
			if row.Kind == PlanRowDir || row.Kind == PlanRowPlan {
				listed[row.Path] = true
			}
		}
		for _, p := range set {
			if top := topUnder(b.Dir, p.path); top != "" && listed[filepath.Join(b.Dir, top)] {
				b.total++
			}
		}
	case b.Find != "":
		b.total = b.findMore
		for _, row := range b.Rows {
			if row.Kind == PlanRowPlan {
				b.total++
			}
		}
	default:
		b.total = len(set)
	}
	for i := range b.Rows {
		if row := &b.Rows[i]; row.Kind == PlanRowDir {
			for _, p := range set {
				if topUnder(row.Path, p.path) != "" {
					row.Plans++
				}
			}
		}
	}
	b.readSums()
	switch {
	case b.fresh:
		b.Cursor = b.defaultRow()
		b.View = 0
	case keep != "":
		// Keep the selected row; if it is gone, stay at the same place
		// in the list (clamped below), next to where it was.
		for i, row := range b.Rows {
			if row.key() == keep {
				b.Cursor = i
				break
			}
		}
	}
	b.fresh = false
	if b.Cursor < 0 {
		b.Cursor = 0
	}
	if b.Cursor >= len(b.Rows) {
		b.Cursor = max(0, len(b.Rows)-1)
	}
}

// planSets is every plan under rex/plans, links too, which a find matches
// by name (all), and the plans the list shows and counts, each file once
// (set). The set leaves hidden folders out, as Browse does, but the open
// plan is listed wherever it lives (docs/ux.md §11).
func (b *PlanBrowser) planSets() (all, set []planStat) {
	all = b.plans()
	b.openPath = walkName(all, b.Current)
	if b.openPath != "" && !slices.ContainsFunc(all, func(p planStat) bool { return p.path == b.openPath }) {
		if info, err := os.Stat(b.openPath); err == nil && info.Mode().IsRegular() {
			all = append(all[:len(all):len(all)], planStat{path: b.openPath, rel: b.relName(b.openPath), mod: info.ModTime(), info: info})
		}
	}
	// A find and start's newest plan still reach a hidden folder's plans.
	shown := make([]planStat, 0, len(all))
	for _, p := range all {
		if p.path == b.openPath || !inHidden(p.rel) {
			shown = append(shown, p)
		}
	}
	return all, distinct(shown, b.openPath)
}

// inHidden says rel (slash-separated, under rex/plans) is in a folder
// whose name starts with a dot.
func inHidden(rel string) bool {
	parts := strings.Split(rel, "/")
	for _, part := range parts[:len(parts)-1] {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

// walkName is the open plan as the walk names it. The agent may have
// written it in another case (macOS, Windows) or through a link, and the
// list goes by the file, not the spelling: the open plan is under OPEN and
// nowhere else (docs/ux.md §11).
func walkName(plans []planStat, cur string) string {
	if cur == "" {
		return ""
	}
	for _, p := range plans {
		if p.path == cur {
			return cur
		}
	}
	info, err := os.Stat(cur)
	if err != nil {
		return cur
	}
	name := cur
	for _, p := range plans {
		if os.SameFile(info, p.info) {
			if !p.link {
				return p.path // its own name over a link's
			}
			if name == cur {
				name = p.path
			}
		}
	}
	return name
}

// topUnder is the first part of path under dir ("cuj" for a plan in
// dir/cuj/x), or "" when path isn't under dir.
func topUnder(dir, path string) string {
	rel, err := filepath.Rel(dir, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	top, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
	return top
}

func (row PlanRow) key() string {
	return fmt.Sprintf("%d|%s|%s", row.Kind, row.Path, row.Name)
}

func (b *PlanBrowser) selectedKey() string {
	if b.Cursor < 0 || b.Cursor >= len(b.Rows) {
		return ""
	}
	return b.Rows[b.Cursor].key()
}

// defaultRow is where the cursor lands on a fresh screen (docs/ux.md §11):
// on the open plan, else the newest other one, else + New plan; in a find,
// the first match, else "+ Create"; in Browse, the first folder or plan.
func (b *PlanBrowser) defaultRow() int {
	switch {
	case b.Screen == ScreenBrowse:
		for i, row := range b.Rows {
			if row.Kind == PlanRowDir || row.Kind == PlanRowPlan || row.Kind == PlanRowFile {
				return i
			}
		}
		for i, row := range b.Rows {
			if row.Kind == PlanRowNew {
				return i
			}
		}
	case b.Find != "":
		return 0
	default:
		for i, row := range b.Rows {
			if row.Group == groupOpen || row.Group == groupRecent {
				return i // OPEN comes first
			}
		}
		for i, row := range b.Rows {
			if row.Kind == PlanRowNew {
				return i
			}
		}
	}
	return 0
}

func (b *PlanBrowser) Move(delta int) {
	if b.Prompting() || len(b.Rows) == 0 {
		return
	}
	b.Cursor += delta
	if b.Cursor < 0 {
		b.Cursor = 0
	}
	if b.Cursor >= len(b.Rows) {
		b.Cursor = len(b.Rows) - 1
	}
}

// Up leaves the folder being browsed for its parent, and a top folder for
// the plan list (docs/ux.md §11), landing on the folder it came out of.
func (b *PlanBrowser) Up() bool {
	if b.Screen != ScreenBrowse {
		return false
	}
	child := b.Dir
	b.Dir = filepath.Dir(child)
	if child == b.Root || b.Dir == b.Root {
		b.Screen, b.Dir = ScreenHome, b.Root
	}
	b.Filter = ""
	b.fresh = true
	b.Rebuild()
	for i, row := range b.Rows {
		if row.Kind == PlanRowDir && row.Path == child {
			b.Cursor = i
		}
	}
	return true
}

// topFolder reports that Browse is in a folder right under rex/plans,
// where Esc goes back to the plan list.
func (b *PlanBrowser) topFolder() bool {
	return b.Dir == b.Root || filepath.Dir(b.Dir) == b.Root
}

func (b *PlanBrowser) Activate() PlanAction {
	if b.Prompting() {
		return PlanStay
	}
	if b.Cursor < 0 || b.Cursor >= len(b.Rows) {
		return PlanNone
	}
	row := b.Rows[b.Cursor]
	switch row.Kind {
	case PlanRowPlan:
		return b.open(row.Path)
	case PlanRowNew:
		b.From = b.Screen
		return PlanStartCreate
	case PlanRowMkdir:
		b.From = b.Screen
		return PlanStartFolder
	case PlanRowUp:
		b.Up()
		return PlanStay
	case PlanRowDir:
		// Under FOLDERS or in Browse alike: browse it.
		b.Screen = ScreenBrowse
		b.Dir = row.Path
		b.Filter = ""
		b.fresh = true
		b.Rebuild()
		return PlanStay
	case PlanRowCreateNamed:
		b.From = ScreenHome
		return b.CommitCreate(row.Name)
	case PlanRowFile:
		b.Err = row.Name + " is not a .rex.json plan"
		return PlanStay
	default:
		return PlanStay
	}
}

func (b *PlanBrowser) CommitCreate(name string) PlanAction {
	path, err := b.createTarget(name)
	if err != nil {
		b.Err = cause(err)
		return PlanStay
	}
	if _, err := EnsurePlansDir(b.Repo); err != nil {
		b.Err = cause(err)
		return PlanStay
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		b.Err = cause(err)
		return PlanStay
	}
	if err := writeNew(path, plan.Stub(planStem(filepath.Base(path)))); err != nil {
		if os.IsExist(err) {
			err = errors.New(b.relName(path) + " already exists")
		}
		b.Err = cause(err)
		return PlanStay
	}
	b.Err = ""
	b.Screen = ScreenHome
	b.Filter, b.Find, b.Name = "", "", ""
	b.walk = planWalk{}
	b.fresh = true
	b.Rebuild()
	return b.open(path)
}

// writeNew creates path with data and fails if it already exists, so two
// Rex windows creating the same name cannot overwrite each other.
func writeNew(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	return f.Close()
}

// planStem is a plan file name without .rex.json.
func planStem(name string) string {
	if plan.IsFile(name) {
		return name[:len(name)-len(".rex.json")]
	}
	return name
}

// planName is a typed name as a slash path under the folder it is created
// in: "a/b" is plan b.rex.json inside folder a. Names may not climb out
// of rex/plans.
func planName(name string) (string, error) {
	parts := strings.Split(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"), "/")
	var dirs []string
	for _, part := range parts[:len(parts)-1] {
		part = strings.TrimSpace(part)
		switch part {
		case "", ".":
			continue
		case "..":
			return "", errors.New("plan names stay inside rex/plans")
		}
		dirs = append(dirs, part)
	}
	file := PlanFileName(parts[len(parts)-1])
	if file == "" {
		return "", errors.New("type a name first")
	}
	return path.Join(append(dirs, file)...), nil
}

// createTarget is where a typed name will be created: under the folder
// being browsed, or under rex/plans from Home.
func (b *PlanBrowser) createTarget(name string) (string, error) {
	rel, err := planName(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(b.promptBase(), filepath.FromSlash(rel)), nil
}

// promptBase is the folder a Name prompt creates in: rex/plans from the
// plan list, the folder being browsed from Browse. The preview and the
// commit both go by it.
func (b *PlanBrowser) promptBase() string {
	if b.From == ScreenHome || b.Dir == "" {
		return b.Root
	}
	return b.Dir
}

// relName is path relative to rex/plans, with "/" separators.
func (b *PlanBrowser) relName(path string) string {
	if rel, err := filepath.Rel(b.Root, path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.Base(path)
}

func (b *PlanBrowser) CommitFolder(name string) PlanAction {
	rel, err := folderPath(name)
	if err != nil {
		b.Err = cause(err)
		return PlanStay
	}
	if _, err := EnsurePlansDir(b.Repo); err != nil {
		b.Err = cause(err)
		return PlanStay
	}
	path := filepath.Join(b.promptBase(), filepath.FromSlash(rel))
	if _, err := os.Stat(path); err == nil {
		b.Err = b.relName(path) + " already exists"
		return PlanStay
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		b.Err = cause(err)
		return PlanStay
	}
	b.Err = ""
	b.Screen = ScreenBrowse
	b.Dir = path
	b.Filter, b.Name = "", ""
	b.walk = planWalk{}
	b.fresh = true
	b.Rebuild()
	return PlanStay
}

// folderPath is a typed folder name as a slash path: "a/b" makes b
// inside a. Like plan names, it may not climb out of rex/plans.
func folderPath(name string) (string, error) {
	var parts []string
	for _, part := range strings.Split(strings.ReplaceAll(name, "\\", "/"), "/") {
		switch part = strings.TrimSpace(part); part {
		case "", ".":
		case "..":
			return "", errors.New("folder names stay inside rex/plans")
		default:
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return "", errors.New("type a folder name first")
	}
	return path.Join(parts...), nil
}

func (b *PlanBrowser) CancelPrompt() {
	b.Err = ""
	if b.From == ScreenBrowse {
		b.Screen = ScreenBrowse
	} else {
		b.Screen = ScreenHome
	}
	b.Rebuild()
}

func (b *PlanBrowser) open(path string) PlanAction {
	b.remember(path)
	b.Current = path
	return PlanOpenChat
}

// remember puts path first among the plans opened this run, which lead
// RECENT (docs/ux.md §11). It is memory only: state.json keeps just the
// repo's last plan.
func (b *PlanBrowser) remember(path string) {
	kept := []string{path}
	for _, p := range b.opened {
		if len(kept) <= maxRecent && !samePlan(p, path) {
			kept = append(kept, p)
		}
	}
	b.opened = kept
}

// showDoc hands the list the open plan as Rex shows it (docs/ux.md §6):
// its last good copy, or the error row 1 shows. OPEN's row and accordion
// read it, not the file, so a save caught half-written never puts
// "can't read" under OPEN while the outline still shows the plan.
func (b *PlanBrowser) showDoc(d *planDoc) {
	b.doc, b.docPath = nil, b.Current
	switch {
	case d == nil:
	case d.err != "":
		b.doc = &planSummary{err: strings.TrimPrefix(d.err, unreadable)}
	case d.plan != nil:
		s := summarize(d.plan)
		b.doc = &s
	}
}

// loadPlan reads a plan for the plan list; a test counts the reads.
var loadPlan = plan.Load

// planSummary is a plan as the plan list shows it (docs/ux.md §11): its
// row's progress and the accordion's lines. It is read once per version
// of the file, by (path, mtime, size), when the list is rebuilt, so
// drawing the list and moving the cursor read no files.
type planSummary struct {
	mod                 time.Time
	size                int64
	title, status, goal string
	done, total         int
	pin                 *planItem // nil: nothing to pin
	pinned              bool      // the pin is the agent's active task, not next up
	next                []planItem
	last                *planItem // the last done task, its title what it accomplished
	err                 string    // why it doesn't parse
	shown               bool      // the open plan as Rex shows it, not a read of the file
}

// planItem is a task as one accordion line names it; mark is its glyph
// (docs/ux.md §5) where the line shows one.
type planItem struct{ mark, id, title string }

// readSums marks the open plan's rows, by file, and fills the summary of
// every plan row: the open plan's from the plan Rex shows (showDoc), the
// others re-read only when their size or modification time changed.
func (b *PlanBrowser) readSums() {
	if b.sums == nil {
		b.sums = map[string]planSummary{}
	}
	var open os.FileInfo
	if b.Current != "" {
		open, _ = os.Stat(b.Current)
	}
	for i := range b.Rows {
		row := &b.Rows[i]
		if row.Kind != PlanRowPlan {
			continue
		}
		info, err := os.Stat(row.Path)
		if err != nil {
			delete(b.sums, row.Path)
			continue
		}
		row.Open = row.Path == b.openPath || open != nil && os.SameFile(info, open)
		if row.Open && b.doc != nil && b.docPath == b.Current {
			s := *b.doc
			s.mod, s.size, s.shown = info.ModTime(), info.Size(), true
			b.sums[row.Path] = s
			continue
		}
		if s, ok := b.sums[row.Path]; ok && !s.shown && s.mod.Equal(info.ModTime()) && s.size == info.Size() {
			continue
		}
		s := readSum(row.Path)
		s.mod, s.size = info.ModTime(), info.Size()
		b.sums[row.Path] = s
	}
}

// readSum reads a plan file once for its summary.
func readSum(path string) planSummary {
	p, err := loadPlan(path)
	if err != nil {
		return planSummary{err: cause(err)}
	}
	return summarize(p)
}

// unreadable leads the outline's error for a plan Rex can't open; row 1
// and the plan list show only the cause after it.
const unreadable = "can't read the file: "

// cause is err as a row shows it. An *fs.PathError leads with the
// absolute path ("open /…/rex/plans/x.rex.json: permission denied"),
// which would push the cause off the row, so it is just the cause.
func cause(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

// summarize is a plan as the list sums it up: the pin as the outline has
// it (docs/ux.md §7), the next one or two unfinished leaves after it, and
// the done task the PINNED card names (Graph.BuildsOn), so the list and the
// outline never disagree. A plan with no title (a new stub has only an id,
// its name) has none here: its name is already on the row.
func summarize(p *plan.Plan) planSummary {
	g := p.Graph()
	s := planSummary{status: string(p.Status), goal: p.Goal}
	if strings.TrimSpace(p.Title) != "" {
		s.title = p.Title
	}
	s.done, s.total = g.Progress()
	pin := g.Pin()
	if pin != nil {
		s.pin = &planItem{id: pin.ID, title: pin.Title}
		s.pinned = pin.Status == plan.Active
	}
	after := false // past the pin; with none, nothing comes next
	for i := range p.Tasks {
		t := &p.Tasks[i]
		switch {
		case t == pin:
			after = true
		case after && len(s.next) < 2 && g.Leaf(t) && !plan.Finished(t.Status) && findTask(p, taskKey(t)) == t:
			mark, _, _ := glyph(t, g, pin)
			s.next = append(s.next, planItem{mark: mark, id: taskKey(t), title: t.Title})
		}
	}
	if t := g.BuildsOn(pin); t != nil {
		s.last = &planItem{id: taskKey(t), title: t.DoneWhat()}
	}
	return s
}

// meta is a plan row's right-hand side (docs/ux.md §11): its progress,
// "✓ 13/13" once every leaf is finished, "no tasks", or "can't read".
func (s planSummary) meta() (text, role string) {
	switch {
	case s.err != "":
		return "can't read", "error"
	case s.total == 0:
		return "no tasks", "muted"
	case s.done == s.total:
		return fmt.Sprintf("✓ %d/%d", s.done, s.total), "muted"
	}
	return fmt.Sprintf("%d/%d", s.done, s.total), "muted"
}

// finished reports a plan whose every leaf is done or skipped: its whole
// row is quiet.
func (s planSummary) finished() bool { return s.err == "" && s.total > 0 && s.done == s.total }

type planStat struct {
	path string
	rel  string
	mod  time.Time
	info os.FileInfo // tells one file under two names apart
	link bool
}

// walkPlans lists every plan under root, newest first. Hidden folders are
// walked too: the plan list leaves them out (planSets) but a find and
// start's newest plan reach them.
func walkPlans(root string) []planStat {
	files, _ := plan.Files(root, plan.Limit{})
	found := make([]planStat, 0, len(files))
	for _, f := range files {
		rel, err := filepath.Rel(root, f.Path)
		if err != nil {
			rel = filepath.Base(f.Path)
		}
		found = append(found, planStat{path: f.Path, rel: filepath.ToSlash(rel), mod: f.Info.ModTime(), info: f.Info, link: f.Link})
	}
	sort.SliceStable(found, func(i, j int) bool {
		if !found[i].mod.Equal(found[j].mod) {
			return found[i].mod.After(found[j].mod)
		}
		return found[i].rel < found[j].rel
	})
	return found
}

// distinct is plans with each file once, for the groups and the counts
// (docs/ux.md §11: each plan once). A symlink or a hard link inside
// rex/plans names a plan already there: the file keeps the open plan's
// name, else its own (a symlink's target), else the first. A find still
// matches a link by its name.
func distinct(plans []planStat, open string) []planStat {
	// Two names of one file have the same size and time: only those are
	// compared.
	type stamp struct {
		size int64
		mod  int64
	}
	same := map[stamp][]int{}
	for i, p := range plans {
		k := stamp{p.info.Size(), p.mod.UnixNano()}
		same[k] = append(same[k], i)
	}
	rank := func(i int) int {
		switch {
		case plans[i].path == open:
			return 0
		case !plans[i].link:
			return 1
		}
		return 2
	}
	dup := make([]bool, len(plans))
	for _, group := range same {
		for _, i := range group {
			for _, j := range group {
				better := rank(j) < rank(i) || rank(j) == rank(i) && j < i
				if j != i && better && os.SameFile(plans[i].info, plans[j].info) {
					dup[i] = true
					break
				}
			}
		}
	}
	out := make([]planStat, 0, len(plans))
	for i, p := range plans {
		if !dup[i] {
			out = append(out, p)
		}
	}
	return out
}

// indexOf is where path's file is in plans, by file, not spelling, or -1.
func indexOf(plans []planStat, path string) int {
	for i, p := range plans {
		if p.path == path {
			return i
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		return -1
	}
	for i, p := range plans {
		if p.info.Size() == info.Size() && p.mod.Equal(info.ModTime()) && os.SameFile(p.info, info) {
			return i
		}
	}
	return -1
}

// topDirs lists the folders right under root by name, as Browse does:
// hidden ones left out.
func topDirs(root string) []string {
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, ent := range ents {
		if ent.IsDir() && !strings.HasPrefix(ent.Name(), ".") {
			dirs = append(dirs, ent.Name())
		}
	}
	sort.Strings(dirs)
	return dirs
}

const maxMatches = 30

// buildHomeRows is the plan list (docs/ux.md §11). With a find query it is
// the matching plans across rex/plans/** (all), best match first, then
// "+ Create '<query>'" unless that exact plan exists; more counts the
// matches left out. Without one it is the groups, each plan of set in one
// of them: OPEN, the open plan; RECENT, the others opened this run (the
// latest first), then the newest; FOLDERS, the top folders, the root
// plans not shown above, then + New plan and + New folder.
func buildHomeRows(root string, all, set []planStat, dirs []string, find, open string, opened []string) (rows []PlanRow, more int) {
	plans := all
	if q := findKey(find); q != "" {
		// An exact name or path ranks first, then a name or path that
		// starts with the query, then any other match; newest first within
		// each. The .rex.json suffix is not matched, so "json" finds nothing.
		var ranked [3][]planStat
		exact := false
		want, err := planName(find)
		want = strings.ToLower(want)
		for _, p := range plans {
			lower := strings.ToLower(p.rel)
			if err == nil && lower == want {
				exact = true // + Create would make this same file
			}
			name := strings.TrimSuffix(lower, ".rex.json")
			base := path.Base(name)
			switch {
			case name == q || base == q:
				ranked[0] = append(ranked[0], p)
			case strings.HasPrefix(name, q) || strings.HasPrefix(base, q):
				ranked[1] = append(ranked[1], p)
			case strings.Contains(name, q):
				ranked[2] = append(ranked[2], p)
			}
		}
		for _, group := range ranked {
			for _, p := range group {
				if len(rows) == maxMatches {
					more++
					continue
				}
				rows = append(rows, PlanRow{Kind: PlanRowPlan, Name: p.rel, Path: p.path})
			}
		}
		if !exact && err == nil {
			rows = append(rows, PlanRow{Kind: PlanRowCreateNamed, Name: strings.TrimSpace(find)})
		}
		return rows, more
	}
	plans = set
	shown := map[string]bool{}
	for _, p := range plans {
		if p.path == open {
			rows = append(rows, PlanRow{Kind: PlanRowPlan, Group: groupOpen, Name: p.rel, Path: p.path})
			shown[p.path] = true
		}
	}
	recent := 0
	add := func(p planStat) {
		if recent < maxRecent && !shown[p.path] {
			rows = append(rows, PlanRow{Kind: PlanRowPlan, Group: groupRecent, Name: p.rel, Path: p.path})
			shown[p.path] = true
			recent++
		}
	}
	for _, o := range opened {
		if i := indexOf(plans, o); i >= 0 {
			add(plans[i])
		}
	}
	for _, p := range plans {
		add(p)
	}
	for _, d := range dirs {
		rows = append(rows, PlanRow{Kind: PlanRowDir, Group: groupFolders, Name: d, Path: filepath.Join(root, d)})
	}
	var loose []PlanRow
	for _, p := range plans {
		if !shown[p.path] && !strings.Contains(p.rel, "/") {
			loose = append(loose, PlanRow{Kind: PlanRowPlan, Group: groupFolders, Name: p.rel, Path: p.path})
		}
	}
	sort.Slice(loose, func(i, j int) bool { return loose[i].Name < loose[j].Name })
	rows = append(rows, loose...)
	return append(rows, PlanRow{Kind: PlanRowNew, Group: groupFolders, Name: "New plan"}, PlanRow{Kind: PlanRowMkdir, Group: groupFolders, Name: "New folder"}), 0
}

// findKey is a Home find query as matched: lowercase, "/" separators,
// without a typed .rex.json.
func findKey(find string) string {
	// The same cleaning as planName, so ./b, a//b and "a / b" find what
	// they would create; "" means there is nothing to find.
	var parts []string
	for _, part := range strings.Split(strings.ToLower(strings.ReplaceAll(find, "\\", "/")), "/") {
		if part = strings.TrimSpace(part); part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	q := strings.Join(parts, "/")
	if strings.HasSuffix(strings.ToLower(strings.TrimSpace(find)), "/") && q != "" {
		q += "/" // "team/" is on its way into a folder
	}
	return strings.TrimSuffix(q, ".rex.json")
}

// buildBrowseRows lists one folder (docs/ux.md §11): "../", folders,
// plans and other files, then + New plan and + New folder below them.
func buildBrowseRows(root, dir, filter string) []PlanRow {
	filter = strings.ToLower(strings.TrimSpace(filter))
	var rows []PlanRow
	if dir != root {
		rows = append(rows, PlanRow{Kind: PlanRowUp, Name: "..", Path: filepath.Dir(dir)})
	}
	actions := []PlanRow{
		{Kind: PlanRowNew, Name: "New plan"},
		{Kind: PlanRowMkdir, Name: "New folder"},
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return append(rows, actions...)
	}
	var dirs, plans, files []PlanRow
	for _, ent := range ents {
		name := ent.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if filter != "" && !strings.Contains(strings.ToLower(name), filter) {
			continue
		}
		path := filepath.Join(dir, name)
		switch {
		case ent.IsDir():
			dirs = append(dirs, PlanRow{Kind: PlanRowDir, Name: name, Path: path})
		case plan.IsFile(name):
			plans = append(plans, PlanRow{Kind: PlanRowPlan, Name: name, Path: path})
		default:
			files = append(files, PlanRow{Kind: PlanRowFile, Name: name, Path: path})
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	sort.Slice(plans, func(i, j int) bool { return plans[i].Name < plans[j].Name })
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	rows = append(rows, dirs...)
	rows = append(rows, plans...)
	rows = append(rows, files...)
	return append(rows, actions...)
}

// composerPrefix marks the plan list's composer, the only one (docs/ux.md
// §2): + names a plan or folder, / finds on Home and filters in Browse.
func composerPrefix(opt ComposeOptions) string {
	if b := opt.Plans; b != nil && (b.Screen == ScreenCreate || b.Screen == ScreenFolder) {
		return " + "
	}
	return " / "
}

// listFit is the room the plan list is drawn in: its body rows under the
// page title, whether the terminal is tall enough (16 rows) for blank
// rows and the composer's box, and the OPEN group's meta, the follow word.
type listFit struct {
	body   int
	tall   bool
	follow string
}

// tallBody is the plan list's body at 16 rows, where the composer becomes
// its 3-row box and takes two of the rows a 15-row terminal gives the list.
var tallBody = layoutFor(TextOnlyBelow, 16).bodyH

// BuildPlanView is the plan list's body under its page title as an 80×24
// terminal lays it out: 17 body rows, blank rows above its H2s.
func BuildPlanView(b *PlanBrowser, width int) []LogLine {
	return planList(b, width, listFit{body: 17, tall: true})
}

// planList is the plan list's body under its page title, which names the
// screen (docs/ux.md §4, §11): the notice and the error, which stay put
// while the rest scrolls, then the groups, a find's matches, a folder or
// the Name prompt.
func planList(b *PlanBrowser, width int, fit listFit) []LogLine {
	if width <= 0 || b == nil {
		return nil
	}
	inner := max(1, width-2)
	var head []LogLine
	if b.Notice != "" {
		head = append(head, LogLine{Kind: "idle", Text: "  " + clip(fitNotice(b.Notice, inner), inner), Head: true})
	}
	if b.Err != "" {
		head = append(head, LogLine{Kind: "error", Text: "  " + clip(b.Err, inner), Head: true})
	}
	gap := LogLine{Kind: "gap", Head: true}
	viewH := fit.body - len(head)
	if b.Prompting() {
		if len(head) > 0 && fit.tall {
			return append(append(head, gap), b.promptLines(inner, viewH-1)...)
		}
		return append(head, b.promptLines(inner, viewH)...)
	}
	draw := func(t tier) []LogLine {
		switch {
		case b.Browsing():
			return b.browseLines(width, t)
		case b.Find != "":
			return b.findLines(width, t)
		}
		return b.homeLines(width, fit.follow, t)
	}
	// The screen without the accordion and the blank rows sizes them both.
	t := tier{heads: viewH >= 5}
	rest := draw(t)
	need := 0 // blank rows in view: the one under the head, or above two H2s
	switch {
	case !b.Browsing() && b.Find == "" && t.heads:
		need = 2
	case len(head) > 0:
		need = 1
	}
	plans := slices.ContainsFunc(b.Rows, func(r PlanRow) bool { return r.Kind == PlanRowPlan })
	t = listTier(t, viewH, len(head), len(rest), need, fit.tall, plans)
	body := rest
	if t.fold > 0 || t.gaps {
		body = draw(t)
	}
	if len(head) > 0 && t.gaps && (len(body) == 0 || body[0].Kind != "gap") {
		head = append(head, gap)
	}
	return append(head, body...)
}

// tier is how much of the plan list's shape fits (docs/ux.md §12).
type tier struct {
	heads bool // the group H2s
	gaps  bool // the blank rows: under the head, and above each H2
	fold  int  // the accordion's lines under the selected plan
}

// listTier is the plan list's shape in viewH body rows under head lines
// (docs/ux.md §12), on a screen that draws rest lines besides the
// accordion and the blank rows. The H2s need 5 rows. The accordion takes
// the rows the rest leaves, up to its 5 lines; the rest keeps 5 of them
// at most, the selected row and two either side. The blank rows go in from
// 16 rows only where the accordion keeps its lines with need of them in
// view. The selection margin reads what this lays out, so the edges agree.
func listTier(t tier, viewH, head, rest, need int, tall, plans bool) tier {
	if !plans {
		t.gaps = tall // no accordion to make room for
		return t
	}
	room := viewH
	if !tall {
		// At 16 rows the composer's box takes two of a 15-row terminal's
		// rows: 15 rows size the accordion as 16 do, so a taller terminal
		// never shows fewer of its lines.
		room = min(room, tallBody-head)
	}
	keep := min(rest, 5)
	t.fold = min(5, max(0, room-keep))
	t.gaps = tall && viewH-t.fold-keep >= need
	return t
}

// homeLines is the plan list itself (docs/ux.md §11): OPEN, RECENT and
// FOLDERS under their H2s, the selected plan opened in place; with no
// plans yet, a sentence over the actions.
func (b *PlanBrowser) homeLines(width int, follow string, t tier) []LogLine {
	var lines []LogLine
	if b.total == 0 {
		if t.heads && b.Notice != rexFileNotice {
			if t.gaps {
				lines = append(lines, LogLine{Kind: "gap"})
			}
			// Creating here isn't the only way in: the agent's write opens a
			// plan (docs/ux.md §11).
			lines = appendWrapped(lines, "text", "No plans in rex/plans/ yet. Type a name below, or ask your agent to write one.", max(1, width-2))
			lines = append(lines, LogLine{Kind: "gap"})
		}
		for i, row := range b.Rows {
			lines = append(lines, b.rowLines(row, i == b.Cursor, width, t)...)
		}
		return lines
	}
	group := ""
	for i, row := range b.Rows {
		if t.heads && row.Group != group {
			group = row.Group
			if t.gaps {
				lines = append(lines, LogLine{Kind: "gap"})
			}
			meta := ""
			switch group {
			case groupOpen:
				meta = follow
			case groupFolders:
				meta = "rex/plans/"
			}
			h := h2Line(group, meta, width)
			h.Kind = "h2" // the window never puts ↑ more on it, nor ends on it
			lines = append(lines, h)
		}
		lines = append(lines, b.rowLines(row, i == b.Cursor, width, t)...)
	}
	return lines
}

// findLines is a find (docs/ux.md §11): the matches in one flat list, the
// selected one opened in place, then + Create 'x'.
func (b *PlanBrowser) findLines(width int, t tier) []LogLine {
	inner := max(1, width-2)
	var lines []LogLine
	if len(b.Rows) == 0 {
		lines = append(lines, LogLine{Kind: "idle", Text: "  " + clip("no match for '"+b.Find+"'", inner)})
	}
	for i, row := range b.Rows {
		if row.Kind == PlanRowCreateNamed && b.findMore > 0 {
			lines = append(lines, LogLine{Kind: "idle", Text: "  " + clip(fmt.Sprintf("+%d more · keep typing", b.findMore), inner)})
		}
		lines = append(lines, b.rowLines(row, i == b.Cursor, width, t)...)
	}
	return lines
}

// browseLines is one folder (docs/ux.md §11): ../, its folders and plans,
// the selected plan opened in place, then a rule over + New plan and
// + New folder.
func (b *PlanBrowser) browseLines(width int, t tier) []LogLine {
	inner := max(1, width-2)
	var lines []LogLine
	items := 0
	for i, row := range b.Rows {
		if row.Kind == PlanRowNew {
			if items == 0 && b.Filter != "" {
				lines = append(lines, LogLine{Kind: "idle", Text: "  " + clip("no match for '"+b.Filter+"' in "+b.RelDir(), inner)})
			}
			if i > 0 {
				lines = append(lines, LogLine{Kind: "idle", Text: "  " + strings.Repeat("─", max(8, inner-2))})
			}
		}
		if row.Kind == PlanRowDir || row.Kind == PlanRowPlan || row.Kind == PlanRowFile {
			items++
		}
		lines = append(lines, b.rowLines(row, i == b.Cursor, width, t)...)
	}
	return lines
}

// promptLines is the Name prompt (docs/ux.md §11): what to type, where it
// will go as you type, and that it never overwrites. Short of rows, the
// blank row goes first, then the a/b line, the overwrite line and what to
// type; where it goes stays.
func (b *PlanBrowser) promptLines(inner, rows int) []LogLine {
	what, never := "plan", "never overwrites a file"
	if b.Screen == ScreenFolder {
		what, never = "folder", "never overwrites a folder"
	}
	name := appendWrapped(nil, "text", "Name the "+what+", then enter.", inner)
	ab := appendWrapped(nil, "text", "a/b makes "+what+" b in folder a.", inner)
	var to []LogLine
	if target, err := b.promptTarget(); err != nil {
		to = appendWrapped(nil, "error", err.Error(), inner)
	} else {
		for i, row := range wrapPath(target, max(1, inner-4)) {
			lead := "  "
			if i == 0 {
				lead = "→ "
			}
			to = append(to, LogLine{Kind: "idle", Text: "  " + lead + row})
		}
	}
	safe := appendWrapped(nil, "idle", never, inner)
	join := func(parts ...[]LogLine) []LogLine {
		var out []LogLine
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	gap := []LogLine{{Kind: "gap"}}
	for _, v := range [][]LogLine{join(name, ab, gap, to, safe), join(name, ab, to, safe), join(name, to, safe), join(name, to)} {
		if len(v) <= rows {
			return v
		}
	}
	return to
}

// promptTarget is where the Name prompt's name goes, repo-relative: a
// placeholder name while it is empty, or while the name ends in a folder
// on its way to the plan ("team/" before "team/auth").
func (b *PlanBrowser) promptTarget() (string, error) {
	base := b.promptBase()
	rel := func(p string) string {
		if r, err := filepath.Rel(b.Repo, p); err == nil {
			return filepath.ToSlash(r)
		}
		return filepath.ToSlash(p)
	}
	if strings.TrimSpace(b.Name) == "" {
		if b.Screen == ScreenFolder {
			return rel(base) + "/<name>/", nil
		}
		return rel(base) + "/<name>.rex.json", nil
	}
	if b.Screen == ScreenFolder {
		name, err := folderPath(b.Name)
		if err != nil {
			return "", err
		}
		return rel(filepath.Join(base, filepath.FromSlash(name))) + "/", nil
	}
	if typed := strings.ReplaceAll(strings.TrimSpace(b.Name), "\\", "/"); strings.HasSuffix(typed, "/") {
		into, err := planName(typed + "x") // planName judges the folder part
		if err != nil {
			return "", err
		}
		return rel(filepath.Join(base, filepath.FromSlash(path.Dir(into)))) + "/<name>.rex.json", nil
	}
	target, err := b.createTarget(b.Name)
	if err != nil {
		return "", err
	}
	return rel(target), nil
}

// rowLines is a row and, under the selected plan, its accordion.
func (b *PlanBrowser) rowLines(row PlanRow, selected bool, width int, t tier) []LogLine {
	line := b.rowLine(row, selected, width, t.heads)
	s, ok := b.sums[row.Path]
	if !selected || row.Kind != PlanRowPlan || !ok || t.fold == 0 {
		return []LogLine{line}
	}
	return append([]LogLine{line}, accordion(s, width, t.fold, b.clock())...)
}

func (b *PlanBrowser) clock() time.Time {
	if b.now != nil {
		return b.now()
	}
	return time.Now()
}

// nameColumn is a plan row's name column from 60 cells: the name, then
// its title at nameColumn cells (docs/ux.md §11).
const nameColumn = 22

// rowLine is one row of the plan list (docs/ux.md §11): under 60 cells
// "name … meta", from 60 "name  title … meta" with the title muted. A
// plan's folder prefix is orange; a finished plan's whole row is quiet.
// The open plan's meta says so wherever no OPEN H2 does (heads).
// Selected, it gets ▌ and the fill, and only it does.
func (b *PlanBrowser) rowLine(row PlanRow, selected bool, width int, heads bool) LogLine {
	inner := max(1, width-2)
	kind := "text"
	var left []Span
	meta := Span{Role: "muted"}
	switch row.Kind {
	case PlanRowPlan:
		s, ok := b.sums[row.Path]
		folder := "folder"
		if ok {
			meta.Text, meta.Role = s.meta()
			if s.finished() {
				kind, folder = "muted", ""
			}
		}
		if row.Open && (row.Group != groupOpen || !heads) {
			meta.Text = strings.TrimSuffix("open · "+meta.Text, " · ")
		}
		name := planStem(row.Name)
		if i := strings.LastIndex(name, "/"); i >= 0 {
			left = []Span{{Text: name[:i+1], Role: folder}, {Text: name[i+1:]}}
		} else {
			left = []Span{{Text: name}}
		}
		if width >= progressWide && ok {
			left = padSpans(cutSpans(left, nameColumn-2), nameColumn)
			left = append(left, Span{Text: s.title, Role: "muted"})
		}
	case PlanRowDir:
		left = []Span{{Text: row.Name + "/", Role: "folder"}}
		meta.Text = plural(row.Plans, "%d plan", "%d plans")
	case PlanRowUp:
		left = []Span{{Text: "../", Role: "folder"}}
	case PlanRowFile:
		kind = "muted"
		left = []Span{{Text: row.Name}}
	case PlanRowNew:
		left = []Span{{Text: "+ New plan", Role: "action"}}
	case PlanRowMkdir:
		left = []Span{{Text: "+ New folder", Role: "action"}}
	case PlanRowCreateNamed:
		left = []Span{{Text: "+ Create '" + row.Name + "'", Role: "action"}}
	}
	lead := Span{Text: "  "}
	fill := ""
	if selected {
		kind, fill, lead = "pick", "pickbg", Span{Text: "▌ "}
	}
	line := spanLine(kind, append([]Span{lead}, sideSpans(left, meta, inner)...)...)
	line.Fill, line.Selected = fill, selected
	return line
}

// accordion is the selected plan opened in place under its row (docs/ux.md
// §11): ▌ down each line, none of them filled, so the pin keeps its
// yellow. Its lines, the H3 labels in their 8-cell column: the title and
// status (from 60 cells, the status and when it changed; a plan with no
// title, just its status), goal, pinned or next up, then, done. At most n
// lines: then goes first, then goal, then under 60 cells done and the
// title, from 60 the status and done; the pin stays. Under 30 cells it is
// the title and the pin's id, unlabeled.
func accordion(s planSummary, width, n int, now time.Time) []LogLine {
	labels, wide := width >= 30, width >= progressWide
	type item struct {
		line LogLine
		drop int // lowest goes first
	}
	var items []item
	// A line with a label has it in the H3 column; one without starts
	// where the labels do.
	add := func(drop int, label string, value ...Span) {
		spans := []Span{{Text: "▌ ", Role: "pick"}, {Text: "  "}}
		if labels && label != "" {
			spans = append(spans, Span{Text: padRight(label, 8), Role: "muted"})
		}
		line := spanLine("text", append(spans, value...)...)
		line.Selected = true
		items = append(items, item{line, drop})
	}
	if s.err != "" {
		add(5, "", Span{Text: "can't read: " + s.err, Role: "error"})
		return []LogLine{items[0].line}
	}
	switch {
	case !labels:
		if s.title != "" {
			add(4, "", Span{Text: s.title, Bold: true})
		}
	case wide:
		when := "changed " + ago(now.Sub(s.mod))
		if s.status != "" {
			when = s.status + " · " + when
		}
		add(3, "", Span{Text: when, Role: "muted"})
	case s.title != "":
		add(4, "", Span{Text: s.title, Bold: true}, Span{Text: strings.TrimSuffix(" · "+s.status, " · "), Role: "muted"})
	case s.status != "":
		add(4, "", Span{Text: s.status, Role: "muted"})
	}
	if labels && strings.TrimSpace(s.goal) != "" {
		add(2, "goal", Span{Text: s.goal, Role: "muted"})
	}
	switch {
	case s.total == 0:
		add(5, "", Span{Text: "no tasks yet · the agent adds them", Role: "muted"}) // fits 38 cells
	case s.pin != nil:
		label := "next up"
		if s.pinned {
			label = "pinned"
		}
		pin := []Span{{Text: "▸ " + s.pin.id, Role: "title", Bold: true}}
		if labels && s.pin.title != "" {
			pin = append(pin, Span{Text: "  " + s.pin.title})
		}
		add(5, label, pin...)
	case s.finished():
		add(5, " ", Span{Text: "✓ all done", Role: "muted"}) // a blank label: under the other values
	default:
		add(5, "", Span{Text: "no pin · nothing unblocked", Role: "muted"})
	}
	if labels && len(s.next) > 0 {
		var then []string
		for _, t := range s.next {
			it := t.mark + " " + t.id
			if wide && t.title != "" {
				it += "  " + t.title
			}
			then = append(then, it)
		}
		sep := "  "
		if wide {
			sep = "   "
		}
		add(1, "then", Span{Text: strings.Join(then, sep)})
	}
	if labels && s.last != nil {
		done := 3
		if wide {
			done = 4
		}
		add(done, "done", Span{Text: strings.TrimSuffix("✓ "+s.last.id+"  "+s.last.title, "  "), Role: "muted"})
	}
	for len(items) > n {
		low := 0
		for i, it := range items {
			if it.drop < items[low].drop {
				low = i
			}
		}
		items = append(items[:low], items[low+1:]...)
	}
	lines := make([]LogLine, len(items))
	for i, it := range items {
		lines[i] = it.line
	}
	return lines
}

// appendWrapped adds text as indented lines wrapped to inner cells, so a
// hint or a target path isn't cut off in a narrow pane.
func appendWrapped(lines []LogLine, kind, text string, inner int) []LogLine {
	for _, part := range wrap(text, max(1, inner-2)) {
		lines = append(lines, LogLine{Kind: kind, Text: "  " + part})
	}
	return lines
}

// fitNotice fits a plan list notice into width cells without losing what
// it is for: each notice has shorter wordings, and the outside-git notice
// keeps the end of its path ("…/rex/plans"), which says where plans go.
func fitNotice(notice string, width int) string {
	fits := func(s string) bool { return cellLen(s) <= width }
	var variants []string
	switch {
	case strings.HasPrefix(notice, outsideGitPrefix):
		path := strings.TrimPrefix(notice, outsideGitPrefix)
		for _, head := range []string{outsideGitPrefix, "not in git · plans go to ", "no git · plans in ", "no git · "} {
			variants = append(variants, head+path)
			if room := width - cellLen(head) - 1; room >= cellLen("…/rex/plans") && room < cellLen(path) {
				variants = append(variants, head+"…"+tailCells(path, room))
			}
		}
	case notice == rexFileNotice:
		variants = []string{notice, "rex is a file · plans off", "plans off: rex file"}
	case strings.HasPrefix(notice, "rex-plans/ has "):
		n := strings.Fields(strings.TrimPrefix(notice, "rex-plans/ has "))[0]
		variants = []string{notice, n + " old plan(s) in rex-plans/ · move them", "rex-plans/: " + n + " old plan(s)", "rex-plans/: " + n + " old"}
	default:
		variants = []string{notice}
	}
	for _, v := range variants {
		if fits(v) {
			return v
		}
	}
	return variants[len(variants)-1]
}

// planHead is the part of the plan list's head that stays on screen above
// a list of total lines in viewH rows. When the list would get fewer than
// three rows, the gaps go first; the error and notice stay longest, so
// enter on a far-down row still says what went wrong.
func planHead(head []LogLine, total, viewH int) []LogLine {
	if total <= viewH || viewH-len(head) >= 3 {
		return head
	}
	keep := max(0, viewH-3)
	rank := map[string]int{"error": 1, "idle": 2}
	picked := make([]bool, len(head))
	for r, n := 0, 0; r <= 2 && n < keep; r++ {
		for i, line := range head {
			got, ok := rank[line.Kind]
			if line.Selected {
				got, ok = 0, true // the selection never leaves the screen
			}
			if ok && got == r && n < keep {
				picked[i] = true
				n++
			}
		}
	}
	var out []LogLine
	for i, line := range head {
		if picked[i] {
			out = append(out, line)
		}
	}
	return out
}

// window picks the first body line to show so the selected row stays
// visible with a two-line margin, scrolling no further than needed. It
// never starts on an H2, which ↑ more would cover (docs/ux.md §11): it
// starts a row above it while the selection still fits with the row under
// it, else a row below it. A blank row goes above each H2, so it never
// scrolls just to hide one either.
func (b *PlanBrowser) window(body []LogLine, viewH int) int {
	start := windowStart(body, viewH, b.View)
	if start > 0 && body[start].Kind == "h2" {
		first, last := -1, -1
		for i, line := range body {
			if line.Selected {
				if first < 0 {
					first = i
				}
				last = i
			}
		}
		switch {
		case min(last+1, len(body)-1) < start-1+viewH:
			start-- // ↑ more goes on the blank row or the row above
		case first < 0 || start < first:
			start++ // or on the first row under the heading
		}
	}
	b.View = start
	return start
}

// notGap reports a list line that is not a blank row.
func notGap(line LogLine) bool { return line.Kind != "gap" }

// listMore says where the plan list goes on past its window (docs/ux.md
// §11), never over the selection: ↑ more over the top row only when it
// hides a row that isn't blank, and never over an H2; ↓ more right under
// the last row that isn't blank or an H2, so the window never ends on a
// heading with nothing under it.
func listMore(visible, above []LogLine, below bool) []LogLine {
	if len(above) > 0 && !visible[0].Selected && visible[0].Kind != "h2" && slices.ContainsFunc(above, notGap) {
		visible[0] = LogLine{Kind: "idle", Text: "  ↑ more"}
	}
	n := len(visible) - 1
	if !below || visible[n].Selected {
		return visible
	}
	for n > 1 && !visible[n-1].Selected && (!notGap(visible[n-1]) || visible[n-1].Kind == "h2") {
		n--
	}
	visible = visible[:n+1]
	visible[n] = LogLine{Kind: "idle", Text: "  ↓ more"}
	return visible
}

// windowStart is window for any list with Selected lines: prev is the
// first line shown last frame. A selection taller than the window shows
// its top, so a plan's own row stays on screen above its accordion.
func windowStart(body []LogLine, viewH, prev int) int {
	if viewH <= 0 || len(body) <= viewH {
		return 0
	}
	first, last := -1, -1
	for i, line := range body {
		if line.Selected {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	start := min(prev, len(body)-viewH)
	if first >= 0 {
		margin := min(2, max(0, (viewH-(last-first+1))/2))
		if last+margin >= start+viewH {
			start = last + margin - viewH + 1
		}
		if first-margin < start {
			start = first - margin
		}
	}
	return max(0, min(start, len(body)-viewH))
}
