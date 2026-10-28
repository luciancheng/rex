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
	"github.com/luciancheng/rex/internal/skill"
)

type PaneKind int

const (
	PaneChat PaneKind = iota
	PanePlans
)

// PlanScreen is which page of Plans is on screen (docs/ux.md §11).
type PlanScreen int

const (
	ScreenHome   PlanScreen = iota // Plans: the cards and Recent, or a find's matches
	ScreenBrowse                   // Open a plan: one folder of rex/plans
	ScreenCreate                   // New plan: the name field
)

// PlanRowKind is what a row of Plans is. Every row is one enter acts on
// (docs/ux.md §11): labels and prose are never rows.
type PlanRowKind int

const (
	PlanRowPlan        PlanRowKind = iota // a plan: under Recent, a match, a plan in a folder
	PlanRowDir                            // a folder, in Open a plan
	PlanRowContinue                       // the Continue card: the plan you had open
	PlanRowNew                            // the Create a plan card
	PlanRowOpen                           // the Open a plan card
	PlanRowLatest                         // the Open latest card: the newest plan
	PlanRowCreateNamed                    // + Create <name> at the end of a find
)

type PlanRow struct {
	Kind  PlanRowKind
	Name  string // a plan's path under rex/plans (its file name in a folder), a folder's name
	Path  string
	Plans int // a folder's plans, in it and below
}

// card reports one of Plans' four cards (docs/ux.md §11).
func (row PlanRow) card() bool { return row.Kind >= PlanRowContinue && row.Kind <= PlanRowLatest }

type PlanBrowser struct {
	Repo   string
	Root   string
	Dir    string
	Filter string // Open a plan: narrows the current folder
	Find   string // Plans: searches every plan under rex/plans
	Name   string // New plan: the name being typed
	Cursor int
	Screen PlanScreen
	// Current is the open plan; Continue is the plan the Continue card
	// opens: the open plan, else this repo's last from ~/.rex/state.json.
	Current  string
	Continue string
	Err      string
	Notice   string
	Rows     []PlanRow
	View     int // first visible body line, kept between frames

	fresh    bool   // the screen just changed: put the cursor on its default row
	contPath string // Continue as the walk names it (Rebuild)
	walk     planWalk
	sums     map[string]planSummary
	doc      *planSummary // the open plan as Rex shows it (showDoc), for docPath
	docPath  string
	opened   []string // the plans opened this run, the latest first; memory only
	offer    string   // the plan your agent wrote while you were latched: Open latest says so
	skill    skill.Copy
	findMore int // find matches past maxMatches, left out
	total    int // what the title's meta counts: plans, or matches
	browsed  int // the plans Open a plan reaches: those outside hidden folders
	folders  int // the folders right under rex/plans
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
)

// maxRecent is how many plans Recent lists under the cards.
const maxRecent = 5

func PlansDir(repo string) string { return plan.Dir(repo) }

// errRexFile means something that is not a folder sits where the plans
// folder goes. Rex never deletes it; plans stay off until the human moves it.
var errRexFile = errors.New("move the rex file to create plans") // fits a 38-cell pane

const (
	rexFileNotice    = "rex/ is a file here · plans off"
	outsideGitPrefix = "Not in a git repo · plans go in "
)

// EnsurePlansDir creates rex/plans when the human creates a plan. Starting
// Rex never calls it, and it never deletes anything.
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
		b.Notice = outsideGitPrefix + homePath(b.Root) + "/"
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
	return b.Screen == ScreenCreate
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

// RelDir is the folder being browsed, repo-relative ("rex/plans/ux").
func (b *PlanBrowser) RelDir() string {
	dir := b.Dir
	if b.Screen != ScreenBrowse {
		dir = b.Root // Plans finds and creates from rex/plans
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

// Rebuild lists the screen's rows again from the files: the cards and
// Recent, a find's matches, or a folder. The selection stays on its row by
// key; a fresh screen puts it on its default row.
func (b *PlanBrowser) Rebuild() {
	b.refreshNotice()
	if b.Dir == "" || b.Screen != ScreenBrowse {
		b.Dir = b.Root
	}
	if rel, err := filepath.Rel(b.Root, b.Dir); err != nil || strings.HasPrefix(rel, "..") {
		b.Dir = b.Root
	}
	keep := b.selectedKey()
	all, set := b.planSets()
	b.folders = len(b.walked().dirs)
	b.browsed = 0
	for _, p := range set {
		if !inHidden(p.rel) {
			b.browsed++ // all but Continue's plan in a hidden folder
		}
	}
	switch {
	case b.Screen == ScreenBrowse:
		b.Rows, b.findMore = buildBrowseRows(b.Root, b.Dir, b.Filter), 0
	case b.Screen == ScreenHome && b.Find != "":
		b.Rows, b.findMore = b.findRows(all)
	default:
		b.Rows, b.findMore = b.homeRows(set), 0
	}
	// Every count is of one plan set, each file once (docs/ux.md §11).
	b.total = len(set)
	if b.Screen == ScreenHome && b.Find != "" {
		b.total = b.findMore
		for _, row := range b.Rows {
			if row.Kind == PlanRowPlan {
				b.total++
			}
		}
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
	if b.Cursor >= len(b.Rows) {
		b.Cursor = len(b.Rows) - 1
	}
	if b.Cursor < 0 {
		b.Cursor = 0
	}
}

// planSets is every plan under rex/plans, links too, which a find matches
// by name (all), and the plans the cards, Recent and the counts go by,
// each file once (set). The set leaves hidden folders out, as Open a plan
// does, but Continue's plan is in it wherever it lives (docs/ux.md §11).
func (b *PlanBrowser) planSets() (all, set []planStat) {
	all = b.plans()
	b.contPath = walkName(all, b.Continue)
	if b.contPath != "" && !slices.ContainsFunc(all, func(p planStat) bool { return p.path == b.contPath }) {
		if info, err := os.Stat(b.contPath); err == nil && info.Mode().IsRegular() {
			all = append(all[:len(all):len(all)], planStat{path: b.contPath, rel: b.relName(b.contPath), mod: info.ModTime(), info: info})
		}
	}
	// A find still reaches a hidden folder's plans.
	shown := make([]planStat, 0, len(all))
	for _, p := range all {
		if p.path == b.contPath || !inHidden(p.rel) {
			shown = append(shown, p)
		}
	}
	return all, distinct(shown, b.contPath)
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

// walkName is a plan as the walk names it. The agent may have written it
// in another case (macOS, Windows) or through a link, and Plans goes by
// the file, not the spelling: Continue's plan is on no other card and
// nowhere under Recent (docs/ux.md §11).
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

// key is what keeps a row selected across a rebuild: a plan by its path,
// whichever card or row names it, so the selection follows a plan from
// Recent to Open latest; anything else by what it is.
func (row PlanRow) key() string {
	switch row.Kind {
	case PlanRowPlan, PlanRowContinue, PlanRowLatest:
		return "plan|" + row.Path
	}
	return fmt.Sprintf("%d|%s|%s", row.Kind, row.Path, row.Name)
}

func (b *PlanBrowser) selectedKey() string {
	if b.Cursor < 0 || b.Cursor >= len(b.Rows) {
		return ""
	}
	return b.Rows[b.Cursor].key()
}

// defaultRow is where the cursor lands on a fresh screen (docs/ux.md §11):
// on Continue, else Open latest, else Create a plan; in a find, the first
// match, else + Create; in a folder, its first folder or plan.
func (b *PlanBrowser) defaultRow() int {
	if b.Screen != ScreenHome || b.Find != "" {
		return 0
	}
	for _, kind := range []PlanRowKind{PlanRowContinue, PlanRowLatest, PlanRowNew} {
		if i := b.rowOf(kind); i >= 0 {
			return i
		}
	}
	return 0
}

// rowOf is the first row of kind, or -1.
func (b *PlanBrowser) rowOf(kind PlanRowKind) int {
	return slices.IndexFunc(b.Rows, func(row PlanRow) bool { return row.Kind == kind })
}

// pick puts the cursor on the first row of kind, if there is one.
func (b *PlanBrowser) pick(kind PlanRowKind) {
	if i := b.rowOf(kind); i >= 0 {
		b.Cursor = i
	}
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

// Up leaves the folder being browsed for its parent, landing on the folder
// it came out of, and rex/plans itself for Plans, on the Open a plan card
// (docs/ux.md §11).
func (b *PlanBrowser) Up() bool {
	if b.Screen != ScreenBrowse {
		return false
	}
	child := b.Dir
	b.Filter = ""
	b.fresh = true
	if child == b.Root {
		b.Screen = ScreenHome
		b.Rebuild()
		b.pick(PlanRowOpen)
		return true
	}
	b.Dir = filepath.Dir(child)
	b.Rebuild()
	for i, row := range b.Rows {
		if row.Kind == PlanRowDir && row.Path == child {
			b.Cursor = i
		}
	}
	return true
}

// topFolder reports that Open a plan is at rex/plans itself, where Esc
// goes back to Plans.
func (b *PlanBrowser) topFolder() bool {
	return b.Dir == b.Root
}

// Activate is enter on the selected row (docs/ux.md §11): a card's page
// or plan, a plan, a folder, or + Create.
func (b *PlanBrowser) Activate() PlanAction {
	if b.Prompting() {
		return PlanStay
	}
	if b.Cursor < 0 || b.Cursor >= len(b.Rows) {
		return PlanNone
	}
	row := b.Rows[b.Cursor]
	switch row.Kind {
	case PlanRowPlan, PlanRowContinue, PlanRowLatest:
		return b.open(row.Path)
	case PlanRowNew:
		b.Screen, b.Name = ScreenCreate, ""
		return PlanStartCreate
	case PlanRowOpen:
		b.browse(b.Root)
		return PlanStay
	case PlanRowDir:
		b.browse(row.Path)
		return PlanStay
	case PlanRowCreateNamed:
		return b.CommitCreate(row.Name)
	}
	return PlanStay
}

// browse shows dir in Open a plan, unfiltered.
func (b *PlanBrowser) browse(dir string) {
	b.Screen, b.Dir, b.Filter = ScreenBrowse, dir, ""
	b.fresh = true
	b.Rebuild()
}

// CommitCreate writes the new plan's stub and opens it. A name that is
// taken is refused, and nothing is written (docs/ux.md §11).
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
			err = errors.New(planStem(b.relName(path)) + " already exists · pick another name")
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

// planName is a typed name as a slash path under rex/plans: "a/b" is plan
// b.rex.json inside folder a. Names may not climb out of rex/plans.
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

// createTarget is where a typed name will be created, under rex/plans:
// New plan's preview and its commit both go by it.
func (b *PlanBrowser) createTarget(name string) (string, error) {
	rel, err := planName(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(b.Root, filepath.FromSlash(rel)), nil
}

// where is Plans' title meta (docs/ux.md §4): the repo and how many plans
// it holds, or outside git the folder and that it is no repo; short, just
// the count, or "no git repo".
func (b *PlanBrowser) where() (meta, short []Span) {
	if !b.inGit {
		return metaSpans(homePath(b.Repo) + " · no git repo"), metaSpans("no git repo")
	}
	count := plural(b.total, "%d plan", "%d plans")
	if b.total == 0 {
		count = "no plans yet"
	}
	return metaSpans(filepath.Base(b.Repo) + " · " + count), metaSpans(count)
}

// relName is path relative to rex/plans, with "/" separators.
func (b *PlanBrowser) relName(path string) string {
	if rel, err := filepath.Rel(b.Root, path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.Base(path)
}

// CancelPrompt leaves New plan for Plans, on the Create a plan card;
// nothing is written.
func (b *PlanBrowser) CancelPrompt() {
	b.Err = ""
	b.Screen, b.Name = ScreenHome, ""
	b.fresh = true
	b.Rebuild()
	b.pick(PlanRowNew)
}

func (b *PlanBrowser) open(path string) PlanAction {
	b.remember(path)
	b.Current, b.Continue = path, path
	return PlanOpenChat
}

// remember puts path first among the plans opened this run, which lead
// Recent (docs/ux.md §11). It is memory only: state.json keeps just the
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

// showDoc hands Plans the open plan as Rex shows it (docs/ux.md §6): its
// last good copy, or the error the notice row shows. Its card and rows
// read it, not the file, so a save caught half-written never puts "can't
// read" on Continue while the task list still shows the plan.
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

// loadPlan reads a plan for Plans; a test counts the reads.
var loadPlan = plan.Load

// planSummary is a plan as Plans shows it (docs/ux.md §11): its title, its
// progress as the task list counts it (tally: done of the tasks it lists,
// and how many are finished, done or skipped) and its pin. It is read
// once per version of the file, by (path, mtime, size), when the rows are
// rebuilt, so drawing them and moving the cursor read no files.
type planSummary struct {
	mod                   time.Time
	size                  int64
	title                 string
	done, finished, total int
	pin                   string // the pin's id; "" with nothing to pin
	pinned                bool   // the pin is the agent's active task, not next up
	err                   string // why it doesn't parse
	shown                 bool   // the open plan as Rex shows it, not a read of the file
}

// readSums fills the summary of every row that names a plan: the open
// plan's from the plan Rex shows (showDoc), the others re-read only when
// their size or modification time changed.
func (b *PlanBrowser) readSums() {
	var open os.FileInfo
	if b.Current != "" {
		open, _ = os.Stat(b.Current)
	}
	for _, row := range b.Rows {
		switch row.Kind {
		case PlanRowPlan, PlanRowContinue, PlanRowLatest:
			if info, err := os.Stat(row.Path); err != nil {
				delete(b.sums, row.Path)
			} else {
				b.sumOf(row.Path, info, open)
			}
		}
	}
}

// sumOf is the summary of the plan at path, whose file is info: from the
// plan Rex shows when it is the open plan (open), else from the cache
// while the file is unchanged, else read now.
func (b *PlanBrowser) sumOf(path string, info, open os.FileInfo) planSummary {
	if b.sums == nil {
		b.sums = map[string]planSummary{}
	}
	if b.doc != nil && b.docPath == b.Current && (path == b.Current || open != nil && os.SameFile(info, open)) {
		s := *b.doc
		s.mod, s.size, s.shown = info.ModTime(), info.Size(), true
		b.sums[path] = s
		return s
	}
	if s, ok := b.sums[path]; ok && !s.shown && s.mod.Equal(info.ModTime()) && s.size == info.Size() {
		return s
	}
	s := readSum(path)
	s.mod, s.size = info.ModTime(), info.Size()
	b.sums[path] = s
	return s
}

// readSum reads a plan file once for its summary.
func readSum(path string) planSummary {
	p, err := loadPlan(path)
	if err != nil {
		return planSummary{err: cause(err)}
	}
	return summarize(p)
}

// unreadable leads the task list's error for a plan Rex can't open; the
// notice row and Plans show only the cause after it.
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

// summarize is a plan as Plans sums it up: its title, and its progress and
// pin as the task list has them (docs/ux.md §6, §7), so Plans and the task
// list never disagree. A plan with no title (a new stub has only an id, its
// name) has none here: its name is already on the row.
func summarize(p *plan.Plan) planSummary {
	g := p.Graph()
	s := planSummary{}
	if strings.TrimSpace(p.Title) != "" {
		s.title = p.Title
	}
	s.done, s.finished, s.total = tallyOf(p, g)
	if pin := g.Pin(); pin != nil {
		s.pin, s.pinned = pin.ID, pin.Status == plan.Active
	}
	return s
}

// meta is a plan's progress as its row and card say it (docs/ux.md §11),
// counted as the task list's row 0 counts it: "1/3 done", the done tasks
// of those it lists; "✔ 12/13 done" once every one is done or skipped;
// "no tasks", or "can't read".
func (s planSummary) meta() (text, role string) {
	switch {
	case s.err != "":
		return "can't read", "error"
	case s.total == 0:
		return "no tasks", "muted"
	case s.finished == s.total:
		return fmt.Sprintf("%s %d/%d done", doneMark, s.done, s.total), "muted"
	}
	return fmt.Sprintf("%d/%d done", s.done, s.total), "muted"
}

// over reports a plan whose every listed task is done or skipped: its
// whole row is quiet.
func (s planSummary) over() bool { return s.err == "" && s.total > 0 && s.finished == s.total }

type planStat struct {
	path string
	rel  string
	mod  time.Time
	info os.FileInfo // tells one file under two names apart
	link bool
}

// walkPlans lists every plan under root, newest first. Hidden folders are
// walked too: Plans leaves them out (planSets) but a find reaches them.
func walkPlans(root string) []planStat {
	files := plan.Files(root)
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

// distinct is plans with each file once, for the cards, Recent and the
// counts (docs/ux.md §11: each plan once). A symlink or a hard link
// inside rex/plans names a plan already there: the file keeps Continue's
// name, else its own (a symlink's target), else the first. A find still
// matches a link by its name.
func distinct(plans []planStat, cont string) []planStat {
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
		case plans[i].path == cont:
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
	if path == "" {
		return -1
	}
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

// topDirs lists the folders right under root by name, as Open a plan
// does: hidden ones left out.
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

// homeRows are Plans' rows (docs/ux.md §11): the cards, in #70's order and
// each only when it does something, then Recent. Continue is the plan you
// had open; Create a plan is always there while plans are on; Open a plan
// once it has a plan to show, one outside a hidden folder; Open latest is
// the newest plan, unless that is Continue's. Recent is up to maxRecent more plans, those opened earlier
// in this run first, the latest first, then the newest, never one a card
// already names. With plans off there is nothing to pick.
func (b *PlanBrowser) homeRows(set []planStat) []PlanRow {
	if b.Notice == rexFileNotice {
		return nil
	}
	var rows []PlanRow
	cont := indexOf(set, b.contPath)
	if cont >= 0 {
		rows = append(rows, PlanRow{Kind: PlanRowContinue, Name: set[cont].rel, Path: b.Continue})
	}
	rows = append(rows, PlanRow{Kind: PlanRowNew})
	if len(set) == 0 {
		return rows
	}
	if b.browsed > 0 {
		rows = append(rows, PlanRow{Kind: PlanRowOpen})
	}
	taken := map[int]bool{cont: true}
	if cont != 0 {
		rows = append(rows, PlanRow{Kind: PlanRowLatest, Name: set[0].rel, Path: set[0].path})
		taken[0] = true
	}
	recent := 0
	add := func(i int) {
		if i >= 0 && !taken[i] && recent < maxRecent {
			rows = append(rows, PlanRow{Kind: PlanRowPlan, Name: set[i].rel, Path: set[i].path})
			taken[i] = true
			recent++
		}
	}
	for _, o := range b.opened {
		add(indexOf(set, o))
	}
	for i := range set {
		add(i)
	}
	return rows
}

const maxMatches = 30

// findRows are a find's matches across rex/plans/** (all, links too, by
// name), best first: an exact name or path, then a name or path that
// starts with the query, then one that has it, then a plan whose title
// has it; newest first within each. "+ Create <query>" comes last unless
// that exact plan exists; more counts the matches left out. The .rex.json
// suffix is not matched, so "json" finds nothing.
func (b *PlanBrowser) findRows(all []planStat) (rows []PlanRow, more int) {
	q := findKey(b.Find)
	if q == "" {
		return nil, 0
	}
	words := strings.ToLower(strings.TrimSpace(b.Find))
	var open os.FileInfo
	if b.Current != "" {
		open, _ = os.Stat(b.Current)
	}
	var ranked [4][]planStat
	exact := false
	want, err := planName(b.Find)
	want = strings.ToLower(want)
	for _, p := range all {
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
		case strings.Contains(strings.ToLower(flat(b.sumOf(p.path, p.info, open).title)), words):
			ranked[3] = append(ranked[3], p)
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
		rows = append(rows, PlanRow{Kind: PlanRowCreateNamed, Name: strings.TrimSpace(b.Find)})
	}
	return rows, more
}

// findKey is a find query as matched: lowercase, "/" separators, without
// a typed .rex.json.
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

// buildBrowseRows lists one folder in Open a plan (docs/ux.md §11): its
// folders, then its plans, each by name. Hidden folders, and files that
// aren't plans, are left out; a filter narrows both by name. A symlink is
// listed only where it leads under root, rex/plans (plan.Within): a link
// out leads nowhere.
func buildBrowseRows(root, dir, filter string) []PlanRow {
	filter = strings.ToLower(strings.TrimSpace(filter))
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var dirs, plans []PlanRow
	for _, ent := range ents {
		name := ent.Name()
		if strings.HasPrefix(name, ".") || filter != "" && !strings.Contains(strings.ToLower(planStem(name)), filter) {
			continue
		}
		path := filepath.Join(dir, name)
		switch info, err := os.Stat(path); {
		case err != nil:
		case ent.Type()&fs.ModeSymlink != 0 && !linksIn(root, path, info):
		case info.IsDir():
			dirs = append(dirs, PlanRow{Kind: PlanRowDir, Name: name, Path: path})
		case plan.IsFile(name) && info.Mode().IsRegular():
			// Only a regular file is a plan: a FIFO named *.rex.json would
			// hang the read of its summary.
			plans = append(plans, PlanRow{Kind: PlanRowPlan, Name: name, Path: path})
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	sort.Slice(plans, func(i, j int) bool { return plans[i].Name < plans[j].Name })
	return append(dirs, plans...)
}

// linksIn reports that the symlink at path, whose target info is, leads
// under root: to a folder, or to a plan file.
func linksIn(root, path string, info os.FileInfo) bool {
	rel, in := plan.Within(root, path)
	return in && (info.IsDir() || plan.IsFile(filepath.Base(filepath.FromSlash(rel))))
}

// fieldLabel is the text field's title on this screen (docs/ux.md §2).
func (b *PlanBrowser) fieldLabel() string {
	switch b.Screen {
	case ScreenBrowse:
		return "filter"
	case ScreenCreate:
		return "name"
	}
	return "find"
}

// listFit is the room Plans is drawn in: its body rows, whether the
// terminal is tall enough (16 rows) for the field's box, whether it is
// too short (under blankFrom rows) for blank rows, and the text field:
// open or not, and its text.
type listFit struct {
	body        int
	tall, short bool
	field       bool
	draft       string
}

// BuildPlanView is Plans' body under its title as an 80×24 terminal lays
// it out, with no text field open.
func BuildPlanView(b *PlanBrowser, width int) []LogLine {
	return planList(b, width, listFit{body: layoutFor(80, 24).bodyH, tall: true})
}

// planList is Plans' body under its title, which names the page
// (docs/ux.md §4, §11): the text field while it is open, which stays put,
// then the cards and Recent, a find's matches, a folder, or New plan's
// page. Its notice is row 1 and its error the notice row's.
func planList(b *PlanBrowser, width int, fit listFit) []LogLine {
	if width <= 0 || b == nil {
		return nil
	}
	var head []LogLine
	if fit.field {
		head, _, _ = fieldLines(b.fieldLabel(), fit.draft, width, fit.tall)
	}
	if b.Prompting() {
		if len(head) == 0 {
			head, _, _ = fieldLines(b.fieldLabel(), b.Name, width, fit.tall)
		}
		return append(markHead(head), b.promptLines(width, fit.body-len(head), fit.short)...)
	}
	if len(head) > 0 && !fit.short {
		head = append(head, LogLine{Kind: "gap"})
	}
	var body []LogLine
	switch {
	case b.Browsing():
		body = b.browseLines(width)
	case b.Find != "":
		body = b.findLines(width, fit.short)
	default:
		body = b.homeLines(width, fit.body-len(head), fit.short)
	}
	return append(markHead(head), body...)
}

// markHead marks lines as the head that stays put over a list.
func markHead(lines []LogLine) []LogLine {
	for i := range lines {
		lines[i].Head = true
	}
	return lines
}

// para is text as muted prose under the cards, wrapped to the row with
// one cell to spare.
func para(kind, text string, width int) []LogLine {
	return appendWrapped(nil, kind, text, max(1, width-1))
}

// homeLines are Plans' cards and Recent (docs/ux.md §11): the cards one
// row each, then a blank row (from blankFrom rows), the bold Recent label
// and its rows. With no plans yet, under the Create a plan card, how your
// agent's plan gets here and how it learns the format; outside git, where
// plans belong; with plans off, only that and how to turn them on.
func (b *PlanBrowser) homeLines(width, rows int, short bool) []LogLine {
	if b.Notice == rexFileNotice {
		return para("muted", "A file named rex sits where the rex/plans/ folder goes. Move it to turn plans on; Rex never deletes it.", width)
	}
	var lines []LogLine
	col := b.nameColumn()
	recent := false
	for i, row := range b.Rows {
		if row.Kind == PlanRowPlan && !recent {
			recent = true
			if !short {
				lines = append(lines, LogLine{Kind: "gap"})
			}
			lines = append(lines, labelLine("Recent"))
		}
		if row.card() {
			lines = append(lines, b.cardLine(row, i == b.Cursor, width))
			continue
		}
		lines = append(lines, b.rowLine(row, i == b.Cursor, width, col))
	}
	if b.total == 0 {
		lines = append(lines, b.firstRun(width, rows-len(lines), short)...)
	}
	return lines
}

// labelLine is a section label on Plans (docs/ux.md §5): bold text,
// indented to the rows it heads. It is never selected, and the window
// never puts ↑ more on it.
func labelLine(label string) LogLine {
	return LogLine{Kind: "h2", Text: "  " + label, Spans: []Span{{Text: "  "}, {Text: label, Role: "text", Bold: true}}}
}

// firstRun is what Plans says under the Create a plan card while there are
// no plans (docs/ux.md §11), in rows rows: that your agent can write the
// plan and Rex opens it, and, while the rex-plan skill is missing or an
// older Rex's, how to teach it the format. Short of rows the blank rows go
// first, then how the agent's plan gets here, then the long hint for the
// short one, which never goes; what still doesn't fit scrolls. Outside git
// it says where plans belong instead.
func (b *PlanBrowser) firstRun(width, rows int, short bool) []LogLine {
	gap := []LogLine{{Kind: "gap"}}
	join := func(parts ...[]LogLine) []LogLine {
		var out []LogLine
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	if !b.inGit {
		text := para("muted", "Start Rex from your project's folder to keep plans next to the code they plan.", width)
		if short {
			return text
		}
		return join(gap, text)
	}
	agent := para("muted", "Or ask your agent to plan the work. Rex opens the plan as soon as your agent writes it under rex/plans/.", width)
	long, brief := skillLines(b.skill, width)
	var variants [][]LogLine
	if !short {
		if long == nil {
			variants = append(variants, join(gap, agent))
		} else {
			variants = append(variants, join(gap, agent, gap, long))
		}
	}
	variants = append(variants, join(agent, long), long)
	for _, v := range variants {
		if len(v) <= rows {
			return v
		}
	}
	return brief
}

// skillLines are Plans' hint about the rex-plan skill on a first run
// (docs/ux.md §11), muted with the command to run in text on its own row:
// long, a sentence over the command; and short, a word beside it where it
// fits. The command wraps rather than clips, so it can still be read. Both
// are nil while every copy agents read is this Rex's.
func skillLines(c skill.Copy, width int) (long, short []LogLine) {
	hint := skillHint(c)
	if hint == "" {
		return nil, nil
	}
	_, cmd, _ := strings.Cut(hint, " · ")
	lead, verb := "Agents learn the plan format from the rex-plan skill:", "teach it:"
	if c != skill.Missing {
		lead, verb = "The rex-plan skill your agents read is out of date. Update it:", "update:"
	}
	room := max(1, width-3)
	command := func(lines []LogLine) []LogLine {
		for _, row := range wrap(cmd, room) {
			lines = append(lines, LogLine{Kind: "text", Text: "  " + row})
		}
		return lines
	}
	long = command(para("muted", lead, width))
	if cellLen(verb+" "+cmd) <= room {
		return long, []LogLine{spanLine("muted", Span{Text: "  " + verb + " "}, Span{Text: cmd, Role: "text"})}
	}
	return long, command([]LogLine{{Kind: "muted", Text: "  " + verb}})
}

// skillHint is what to do about the rex-plan skill, or "" when every copy
// agents read is this Rex's.
func skillHint(c skill.Copy) string {
	switch c {
	case skill.Missing:
		return "agents learn plans from the rex-plan skill · rex ingest --install-skill"
	case skill.Stale:
		return "rex-plan skill is out of date · rex ingest --install-skill"
	case skill.StaleProject:
		return "rex-plan skill is out of date · rex ingest --install-skill --project"
	}
	return ""
}

// cardColumn is the cells a card's label takes on Plans, and the fewest a
// row's name column takes (docs/ux.md §11).
const cardColumn = 16

// maxNameColumn is the most a row's name column takes; a longer name
// clips.
const maxNameColumn = 28

var cardLabels = map[PlanRowKind]string{
	PlanRowContinue: "Continue",
	PlanRowNew:      "Create a plan",
	PlanRowOpen:     "Open a plan",
	PlanRowLatest:   "Open latest",
}

// cardLine is one of Plans' cards (docs/ux.md §11, A2): its label bold in
// the bright title role in a cardColumn-cell column, then what it does,
// muted. Selected, it gets ❯ and the selection's purple, but for the marks
// that keep their meaning (✔'s green, can't read's red).
func (b *PlanBrowser) cardLine(row PlanRow, selected bool, width int) LogLine {
	label := cardLabels[row.Kind]
	spans := []Span{{Text: label, Role: "title", Bold: true}, {Text: strings.Repeat(" ", max(1, cardColumn-cellLen(label)))}}
	spans = cutSpans(append(spans, b.cardText(row)...), max(1, width-3))
	kind, lead := "text", Span{Text: "  "}
	if selected {
		kind, lead, spans = "pick", Span{Text: selMark}, picked(spans)
	}
	line := spanLine(kind, append([]Span{lead}, spans...)...)
	line.Selected = selected
	return line
}

// cardText is what a card does, muted (docs/ux.md §11): Continue's and Open
// latest's plan by title, its progress, and Continue's pin or when Open
// latest's file changed (or that your agent just wrote it); where Create a
// plan puts it; what Open a plan browses.
func (b *PlanBrowser) cardText(row PlanRow) []Span {
	muted := func(text string) Span { return Span{Text: text, Role: "muted"} }
	switch row.Kind {
	case PlanRowNew:
		if _, err := os.Stat(b.Root); err != nil && !b.inGit {
			return []Span{muted("Rex makes " + homePath(b.Root) + "/ for it")}
		}
		return []Span{muted("name it; you or your agent fill in the tasks")}
	case PlanRowOpen:
		parts := []string{"browse rex/plans/"}
		if b.folders > 0 {
			parts = append(parts, plural(b.folders, "%d folder", "%d folders"))
		}
		return []Span{muted(strings.Join(append(parts, plural(b.browsed, "%d plan", "%d plans")), " · "))}
	}
	s, ok := b.sums[row.Path]
	name := planStem(row.Name)
	if ok && s.title != "" {
		name = s.title
	}
	spans := []Span{muted(name)}
	if !ok {
		return spans
	}
	text, role := s.meta()
	progress := []Span{muted(" · "), {Text: text, Role: role}}
	switch {
	case row.Kind == PlanRowContinue:
		spans = append(spans, progress...)
		if s.pinned {
			spans = append(spans, muted(" · pinned "+s.pin))
		}
	case b.offer != "" && samePlan(row.Path, b.offer):
		spans = append(append(spans, muted(" · new from your agent")), progress...)
	default:
		spans = append(append(spans, progress...), muted(" · changed "+ago(b.clock().Sub(s.mod))))
	}
	return spans
}

func (b *PlanBrowser) clock() time.Time {
	if b.now != nil {
		return b.now()
	}
	return time.Now()
}

// nameColumn is the plan rows' name column from progressWide cells: as
// wide as the longest name on the list and two cells, from cardColumn to
// maxNameColumn cells, so their titles line up.
func (b *PlanBrowser) nameColumn() int {
	col := cardColumn
	for _, row := range b.Rows {
		if row.Kind == PlanRowPlan {
			col = max(col, cellLen(flat(b.rowName(row)))+2)
		}
	}
	return min(col, maxNameColumn)
}

// rowName is how a plan row names its plan: by its path under rex/plans
// on Plans and in a find, by its file in a folder; without .rex.json.
func (b *PlanBrowser) rowName(row PlanRow) string {
	if b.Browsing() {
		return planStem(filepath.Base(row.Path))
	}
	return planStem(row.Name)
}

// rowLine is a row of Plans that isn't a card (docs/ux.md §11): under 60
// cells "name … meta", from 60 "name  title … meta" with the title muted
// ("untitled" for a plan that has none) in a column. A folder is text with
// its "/" and its plans; a finished plan's whole row is quiet. Selected,
// it gets ❯ and the selection's purple, and only it does.
func (b *PlanBrowser) rowLine(row PlanRow, selected bool, width, col int) LogLine {
	inner := max(1, width-3)
	kind := "text"
	var left []Span
	meta := Span{Role: "muted"}
	switch row.Kind {
	case PlanRowPlan:
		left = []Span{{Text: b.rowName(row)}}
		if s, ok := b.sums[row.Path]; ok {
			meta.Text, meta.Role = s.meta()
			if s.over() {
				kind = "muted"
			}
			if width >= progressWide && s.err == "" {
				title := s.title
				if title == "" {
					title = "untitled"
				}
				left = append(padSpans(cutSpans(left, col-2), col), Span{Text: title, Role: "muted"})
			}
		}
	case PlanRowDir:
		left = []Span{{Text: row.Name + "/"}}
		meta.Text = plural(row.Plans, "%d plan", "%d plans")
	case PlanRowCreateNamed:
		left = []Span{{Text: "+ Create " + row.Name}}
	}
	lead, spans := Span{Text: "  "}, sideSpans(left, meta, inner)
	if selected {
		kind, lead, spans = "pick", Span{Text: selMark}, picked(spans)
	}
	line := spanLine(kind, append([]Span{lead}, spans...)...)
	line.Selected = selected
	return line
}

// findLines is a find (docs/ux.md §11): the matches in one flat list, the
// matches left out counted under them, then + Create <name> when it is
// offered, a blank row above it from blankFrom rows.
func (b *PlanBrowser) findLines(width int, short bool) []LogLine {
	inner := max(1, width-3)
	col := b.nameColumn()
	more := LogLine{Kind: "idle", Text: "  " + clip(fmt.Sprintf("+%d more · keep typing", b.findMore), inner)}
	var lines []LogLine
	if len(b.Rows) == 0 {
		lines = append(lines, LogLine{Kind: "idle", Text: "  " + clip("no match for '"+b.Find+"'", inner)})
	}
	create := false
	for i, row := range b.Rows {
		if row.Kind == PlanRowCreateNamed {
			create = true
			if b.findMore > 0 {
				lines = append(lines, more)
			}
			if i > 0 && !short {
				lines = append(lines, LogLine{Kind: "gap"})
			}
		}
		lines = append(lines, b.rowLine(row, i == b.Cursor, width, col))
	}
	if b.findMore > 0 && !create {
		lines = append(lines, more) // the exact plan exists, or the text names none
	}
	return lines
}

// browseLines is one folder in Open a plan (docs/ux.md §11): its folders
// with their plan counts, then its plans. Esc goes up; there is no ../.
func (b *PlanBrowser) browseLines(width int) []LogLine {
	inner := max(1, width-3)
	if len(b.Rows) == 0 {
		text := "no plans in " + b.RelDir() + "/ yet"
		if b.Filter != "" {
			text = "no match for '" + b.Filter + "' in " + b.RelDir() + "/"
		}
		return []LogLine{{Kind: "idle", Text: "  " + clip(text, inner)}}
	}
	col := b.nameColumn()
	var lines []LogLine
	for i, row := range b.Rows {
		lines = append(lines, b.rowLine(row, i == b.Cursor, width, col))
	}
	return lines
}

// promptLines is New plan's page under its field (docs/ux.md §11): where
// the plan will be written, updated as you type, then that Rex writes an
// empty plan there and never overwrites a file. Short of rows, the blank
// row goes, then the sentence; where it goes stays.
func (b *PlanBrowser) promptLines(width, rows int, short bool) []LogLine {
	var to []LogLine
	if target, err := b.promptTarget(); err != nil {
		to = para("error", err.Error(), width)
	} else {
		for i, row := range wrapPath(target, max(1, width-5)) {
			lead := "  "
			if i == 0 {
				lead = "→ "
			}
			to = append(to, spanLine("text", Span{Text: "  "}, Span{Text: lead, Role: "muted"}, Span{Text: row}))
		}
	}
	never := para("muted", "Rex writes an empty plan there and opens it. It never overwrites a file.", width)
	var variants [][]LogLine
	if !short {
		variants = append(variants, append(append(append([]LogLine(nil), to...), LogLine{Kind: "gap"}), never...))
	}
	variants = append(variants, append(append([]LogLine(nil), to...), never...))
	for _, v := range variants {
		if len(v) <= rows {
			return v
		}
	}
	return to
}

// promptTarget is where New plan's name goes, repo-relative: a placeholder
// name while it is empty, or while the name ends in a folder on its way to
// the plan ("team/" before "team/auth").
func (b *PlanBrowser) promptTarget() (string, error) {
	rel := func(p string) string {
		if r, err := filepath.Rel(b.Repo, p); err == nil {
			return filepath.ToSlash(r)
		}
		return filepath.ToSlash(p)
	}
	if strings.TrimSpace(b.Name) == "" {
		return rel(b.Root) + "/<name>.rex.json", nil
	}
	if typed := strings.ReplaceAll(strings.TrimSpace(b.Name), "\\", "/"); strings.HasSuffix(typed, "/") {
		into, err := planName(typed + "x") // planName judges the folder part
		if err != nil {
			return "", err
		}
		return rel(filepath.Join(b.Root, filepath.FromSlash(path.Dir(into)))) + "/<name>.rex.json", nil
	}
	target, err := b.createTarget(b.Name)
	if err != nil {
		return "", err
	}
	return rel(target), nil
}

// appendWrapped adds text as indented lines wrapped to inner cells, so a
// hint or a target path isn't cut off in a narrow pane.
func appendWrapped(lines []LogLine, kind, text string, inner int) []LogLine {
	for _, part := range wrap(text, max(1, inner-2)) {
		lines = append(lines, LogLine{Kind: kind, Text: "  " + part})
	}
	return lines
}

// fitNotice fits Plans' notice into width cells without losing what it is
// for: each notice has shorter wordings, and the outside-git notice keeps
// the end of its path ("…/rex/plans/"), which says where plans go.
func fitNotice(notice string, width int) string {
	fits := func(s string) bool { return cellLen(s) <= width }
	var variants []string
	switch {
	case strings.HasPrefix(notice, outsideGitPrefix):
		path := strings.TrimPrefix(notice, outsideGitPrefix)
		for _, head := range []string{outsideGitPrefix, "Not in git · plans go in ", "No git · plans in ", "No git · "} {
			variants = append(variants, head+path)
			if room := width - cellLen(head) - 1; room >= cellLen("…/rex/plans/") && room < cellLen(path) {
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

// window picks the first body line to show so the selected row stays
// visible with a two-line margin, scrolling no further than needed. It
// never starts on a section label (Recent), which ↑ more would cover
// (docs/ux.md §11): it starts a row above it while the selection still
// fits with the row under it, else a row below it. A blank row goes above
// the label, so it never scrolls just to hide one either.
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

// listMore says where Plans' list goes on past its window, and by how
// many rows (docs/ux.md §11), never over the selection: ↑ N more over the
// top row only when it hides a row that isn't blank, and never over a
// section label; ↓ N more right under the last row that isn't blank or a
// label, so the window never ends on a label with nothing under it.
// Blank rows and labels aren't counted.
func listMore(visible, above, below []LogLine) []LogLine {
	rows := func(lines []LogLine) int {
		n := 0
		for _, line := range lines {
			if notGap(line) && line.Kind != "h2" {
				n++
			}
		}
		return n
	}
	if len(above) > 0 && !visible[0].Selected && visible[0].Kind != "h2" && slices.ContainsFunc(above, notGap) {
		visible[0] = LogLine{Kind: "idle", Text: fmt.Sprintf("  ↑ %d more", rows(above)+rows(visible[:1]))}
	}
	n := len(visible) - 1
	if len(below) == 0 || visible[n].Selected {
		return visible
	}
	for n > 1 && !visible[n-1].Selected && (!notGap(visible[n-1]) || visible[n-1].Kind == "h2") {
		n--
	}
	hidden := rows(visible[n:]) + rows(below)
	if hidden == 0 {
		return visible // only blank rows below
	}
	visible = append(visible[:n:n], LogLine{Kind: "idle", Text: fmt.Sprintf("  ↓ %d more", hidden)})
	return visible
}

// windowStart is window for any list with Selected lines: prev is the
// first line shown last frame. A selection taller than the window shows
// its top.
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
