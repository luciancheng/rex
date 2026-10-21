package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/luciancheng/rex/internal/watch"

	"github.com/luciancheng/rex/internal/mascot"
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
	PlanRowLatest PlanRowKind = iota
	PlanRowNew
	PlanRowOpen
	PlanRowUp
	PlanRowDir
	PlanRowPlan
	PlanRowFile
	PlanRowRecent
	PlanRowMkdir
)

type PlanRow struct {
	Kind PlanRowKind
	Name string
	Path string
	Hint string
}

type PlanBrowser struct {
	Repo    string
	Root    string
	Dir     string
	Filter  string
	Cursor  int
	Screen  PlanScreen
	From    PlanScreen
	Current string
	Err     string
	Notice  string
	Rows    []PlanRow

	inGit bool
}

type PlanAction int

const (
	PlanNone PlanAction = iota
	PlanStay
	PlanOpenChat
	PlanStartCreate
	PlanStartFolder
	PlanStartBrowse
)

const maxRecents = 6

func FindRepoRoot(start string) string {
	root, _ := findRepo(start)
	return root
}

// findRepo walks up from start to the directory holding .git. Outside a
// repo it returns start itself and false.
func findRepo(start string) (string, bool) {
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

func PlansDir(repo string) string {
	return filepath.Join(repo, "rex", "plans")
}

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
		if !ent.IsDir() && IsPlanFile(ent.Name()) {
			n++
		}
	}
	return n
}

func IsPlanFile(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".rex.json") && len(name) > len(".rex.json")
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
		if !IsPlanFile(name) {
			return ""
		}
		return name
	}
	return name + ".rex.json"
}

func FolderName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return ""
	}
	return name
}

func NewPlanBrowser(start string) *PlanBrowser {
	repo, inGit := findRepo(start)
	b := &PlanBrowser{Repo: repo, Screen: ScreenHome, inGit: inGit}
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

func (b *PlanBrowser) Empty() bool {
	return latestPlan(b.Root) == ""
}

func (b *PlanBrowser) RelDir() string {
	rel, err := filepath.Rel(b.Repo, b.Dir)
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

func (b *PlanBrowser) Latest() string {
	return latestPlan(b.Root)
}

func (b *PlanBrowser) Recents() []PlanRow {
	return recentPlans(b.Root, maxRecents)
}

func (b *PlanBrowser) Rebuild() {
	b.refreshNotice()
	if b.Dir == "" {
		b.Dir = b.Root
	}
	if rel, err := filepath.Rel(b.Root, b.Dir); err != nil || strings.HasPrefix(rel, "..") {
		b.Dir = b.Root
	}
	switch b.Screen {
	case ScreenBrowse:
		b.Rows = buildBrowseRows(b.Root, b.Dir, b.Filter)
	default:
		b.Rows = buildHomeRows(b.Root)
	}
	if b.Cursor < 0 {
		b.Cursor = 0
	}
	if b.Cursor >= len(b.Rows) {
		b.Cursor = max(0, len(b.Rows)-1)
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

func (b *PlanBrowser) Up() bool {
	if b.Screen != ScreenBrowse {
		return false
	}
	if b.Dir == b.Root {
		b.Screen = ScreenHome
		b.Filter = ""
		b.Cursor = 0
		b.Rebuild()
		return true
	}
	parent := filepath.Dir(b.Dir)
	if rel, err := filepath.Rel(b.Root, parent); err != nil || strings.HasPrefix(rel, "..") {
		return false
	}
	b.Dir = parent
	b.Cursor = 0
	b.Rebuild()
	return true
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
	case PlanRowLatest, PlanRowRecent, PlanRowPlan:
		if row.Path == "" {
			b.Err = "no plans yet"
			return PlanStay
		}
		return b.open(row.Path)
	case PlanRowNew:
		b.From = b.Screen
		return PlanStartCreate
	case PlanRowOpen:
		b.Screen = ScreenBrowse
		b.Dir = b.Root
		b.Filter = ""
		b.Cursor = 0
		b.Rebuild()
		return PlanStartBrowse
	case PlanRowMkdir:
		b.From = b.Screen
		return PlanStartFolder
	case PlanRowUp:
		b.Up()
		return PlanStay
	case PlanRowDir:
		b.Dir = row.Path
		b.Filter = ""
		b.Cursor = 0
		b.Rebuild()
		return PlanStay
	default:
		return PlanStay
	}
}

func (b *PlanBrowser) CommitCreate(name string) PlanAction {
	filename := PlanFileName(name)
	if filename == "" {
		b.Err = "type a name first"
		return PlanStay
	}
	dir := b.Dir
	if b.From == ScreenHome {
		dir = b.Root
	}
	if _, err := EnsurePlansDir(b.Repo); err != nil {
		b.Err = err.Error()
		return PlanStay
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		b.Err = err.Error()
		return PlanStay
	}
	path := filepath.Join(dir, filename)
	if _, err := os.Stat(path); err == nil {
		b.Err = filename + " already exists"
		return PlanStay
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		b.Err = err.Error()
		return PlanStay
	}
	b.Err = ""
	b.Screen = ScreenHome
	b.Filter = ""
	b.Rebuild()
	return b.open(path)
}

func (b *PlanBrowser) CommitFolder(name string) PlanAction {
	folder := FolderName(name)
	if folder == "" {
		b.Err = "type a folder name first"
		return PlanStay
	}
	if _, err := EnsurePlansDir(b.Repo); err != nil {
		b.Err = err.Error()
		return PlanStay
	}
	path := filepath.Join(b.Dir, folder)
	if _, err := os.Stat(path); err == nil {
		b.Err = folder + " already exists"
		return PlanStay
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		b.Err = err.Error()
		return PlanStay
	}
	b.Err = ""
	b.Screen = ScreenBrowse
	b.Dir = path
	b.Filter = ""
	b.Cursor = 0
	b.Rebuild()
	return PlanStay
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
	b.Current = path
	b.Load(path)
	return PlanOpenChat
}

func (b *PlanBrowser) Load(_ string) {
	// Plan JSON is not read into the agent tape this turn.
}

func latestPlan(root string) string {
	recents := recentPlans(root, 1)
	if len(recents) == 0 {
		return ""
	}
	return recents[0].Path
}

type planStat struct {
	path string
	rel  string
	mod  time.Time
}

func recentPlans(root string, limit int) []PlanRow {
	var found []planStat
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !IsPlanFile(d.Name()) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			rel = d.Name()
		}
		found = append(found, planStat{path: path, rel: filepath.ToSlash(rel), mod: info.ModTime()})
		return nil
	})
	sort.Slice(found, func(i, j int) bool {
		return found[i].mod.After(found[j].mod)
	})
	if limit > 0 && len(found) > limit {
		found = found[:limit]
	}
	rows := make([]PlanRow, 0, len(found))
	for _, item := range found {
		rows = append(rows, PlanRow{Kind: PlanRowRecent, Name: item.rel, Path: item.path})
	}
	return rows
}

func buildHomeRows(root string) []PlanRow {
	latest := latestPlan(root)
	latestName := "none yet"
	if latest != "" {
		rel, err := filepath.Rel(root, latest)
		if err != nil {
			rel = filepath.Base(latest)
		}
		latestName = filepath.ToSlash(rel)
	}
	rows := []PlanRow{
		{Kind: PlanRowNew, Name: "Create a plan", Hint: "New .rex.json in rex/plans"},
		{Kind: PlanRowOpen, Name: "Open a plan", Hint: "Browse folders and files"},
		{Kind: PlanRowLatest, Name: "Open latest", Hint: latestName, Path: latest},
	}
	rows = append(rows, recentPlans(root, maxRecents)...)
	return rows
}

func buildBrowseRows(root, dir, filter string) []PlanRow {
	filter = strings.ToLower(strings.TrimSpace(filter))
	rows := []PlanRow{
		{Kind: PlanRowMkdir, Name: "New folder"},
		{Kind: PlanRowNew, Name: "New plan"},
	}
	if dir != root {
		rows = append(rows, PlanRow{Kind: PlanRowUp, Name: "..", Path: filepath.Dir(dir)})
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return rows
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
		case IsPlanFile(name):
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
	return rows
}

func (row PlanRow) Label() string {
	switch row.Kind {
	case PlanRowUp:
		return "../"
	case PlanRowDir:
		return row.Name + "/"
	case PlanRowPlan, PlanRowRecent:
		return row.Name
	case PlanRowMkdir:
		return "New folder"
	case PlanRowNew:
		if row.Hint != "" {
			return row.Name
		}
		return "New plan"
	case PlanRowFile:
		return row.Name
	default:
		return row.Name
	}
}

func (row PlanRow) Color() string {
	switch row.Kind {
	case PlanRowMkdir, PlanRowNew:
		if row.Hint != "" {
			return "text"
		}
		return "action"
	case PlanRowDir, PlanRowUp:
		return "folder"
	case PlanRowFile:
		return "muted"
	default:
		return "text"
	}
}

func composerPrefix(opt ComposeOptions) string {
	if opt.Pane != PanePlans || opt.Plans == nil {
		return InputPrefix
	}
	switch opt.Plans.Screen {
	case ScreenCreate, ScreenFolder:
		return " + "
	case ScreenBrowse:
		return " / "
	default:
		return "   "
	}
}

func BuildPlanView(b *PlanBrowser, width int) []LogLine {
	if width <= 0 || b == nil {
		return nil
	}
	inner := max(1, width-2)
	title := "plans"
	switch b.Screen {
	case ScreenBrowse:
		title = "open"
	case ScreenCreate:
		title = "create"
	case ScreenFolder:
		title = "folder"
	}
	lines := []LogLine{
		{Kind: "command", Text: rule(title, width)},
		{Kind: "folder", Text: "  " + clip(b.RelDir(), inner)},
		{Kind: "gap", Text: ""},
	}
	if b.Notice != "" {
		lines = append(lines, LogLine{Kind: "idle", Text: "  " + clip(fitNotice(b.Notice, inner), inner)}, LogLine{Kind: "gap", Text: ""})
	}
	if b.Err != "" {
		lines = append(lines, LogLine{Kind: "error", Text: "  " + clip(b.Err, inner)}, LogLine{Kind: "gap", Text: ""})
	}
	switch b.Screen {
	case ScreenCreate:
		lines = append(lines,
			LogLine{Kind: "text", Text: "  " + clip("Name the plan, then enter.", inner)},
			LogLine{Kind: "idle", Text: "  " + clip("Creates name.rex.json. Won't overwrite.", inner)},
		)
		return lines
	case ScreenFolder:
		lines = append(lines,
			LogLine{Kind: "text", Text: "  " + clip("Name the folder, then enter.", inner)},
			LogLine{Kind: "idle", Text: "  " + clip("For grouping plans. Esc cancels.", inner)},
		)
		return lines
	case ScreenBrowse:
		actions := 0
		for i, row := range b.Rows {
			if row.Kind == PlanRowMkdir || row.Kind == PlanRowNew {
				actions++
			}
			if i == actions && actions > 0 {
				lines = append(lines, LogLine{Kind: "idle", Text: "  " + strings.Repeat("─", max(8, inner-2))})
			}
			lines = append(lines, browseLine(row, i == b.Cursor, inner))
		}
		return lines
	}
	recents := false
	for i, row := range b.Rows {
		if row.Kind == PlanRowRecent {
			if !recents {
				if i > 0 {
					lines = append(lines, LogLine{Kind: "gap", Text: ""})
				}
				lines = append(lines, LogLine{Kind: "idle", Text: "  recents"})
				recents = true
			}
			lines = append(lines, browseLine(row, i == b.Cursor, inner))
			continue
		}
		lines = append(lines, planCard(row, i == b.Cursor, width)...)
		if i+1 < len(b.Rows) && b.Rows[i+1].Kind != PlanRowRecent {
			lines = append(lines, LogLine{Kind: "gap", Text: ""})
		}
	}
	return lines
}

// fitNotice fits a Library notice into width cells without losing what
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

func browseLine(row PlanRow, selected bool, inner int) LogLine {
	mark := "  "
	kind := row.Color()
	fill := ""
	if selected {
		mark = "▌ "
		kind = "pick"
		fill = "pickbg"
	}
	return LogLine{Kind: kind, Text: mark + clip(browseLabel(row), inner), Fill: fill}
}

func browseLabel(row PlanRow) string {
	switch row.Kind {
	case PlanRowMkdir:
		return "[ + New folder ]"
	case PlanRowNew:
		if row.Hint != "" {
			return row.Name
		}
		return "[ + New plan ]"
	case PlanRowUp:
		return "../"
	case PlanRowDir:
		return row.Name + "/"
	case PlanRowRecent:
		return row.Name
	default:
		return row.Label()
	}
}

func planCard(row PlanRow, selected bool, width int) []LogLine {
	inner := max(8, width-2)
	title := strings.ReplaceAll(watch.CleanText(row.Name), "\n", " ")
	sub := row.Hint
	if sub == "" {
		sub = row.Label()
	}
	edge, head, body := "muted", "text", "idle"
	fill := ""
	mark := "  "
	if selected {
		edge, head, body = "pick", "pick", "pick"
		fill = "pickbg"
		mark = "▌ "
	}
	top := "╭ " + title + " "
	if cellLen(top) >= inner {
		top = clip(top, inner-1) + "╮"
	} else {
		top += strings.Repeat("─", inner-cellLen(top)-1) + "╮"
	}
	mid := "│ " + clip(sub, inner-3)
	mid = padRight(mid, inner-1) + "│"
	bot := "╰" + strings.Repeat("─", inner-2) + "╯"
	return []LogLine{
		{Kind: edge, Text: mark + clip(top, inner), Fill: fill},
		{Kind: head, Text: mark + clip(mid, inner), Fill: fill},
		{Kind: body, Text: mark + clip(bot, inner), Fill: fill},
	}
}

func planFooter(b *PlanBrowser, quitPending bool) (string, mascot.RGB) {
	if quitPending {
		return "press ctrl+c again to exit", mascot.UI["error"]
	}
	if b == nil {
		return "tab chat · ctrl+c twice", mascot.UI["muted"]
	}
	switch b.Screen {
	case ScreenCreate, ScreenFolder:
		return "enter save · esc back · tab chat", mascot.UI["muted"]
	case ScreenBrowse:
		return "↑↓ move · enter open · esc back · tab chat", mascot.UI["muted"]
	default:
		return "↑↓ choose · enter · tab chat", mascot.UI["muted"]
	}
}
