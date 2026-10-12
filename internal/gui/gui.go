//go:build !nogui

// Package gui is the Wails front end. It is another client of the same
// engine the CLI drives; launching programs goes through `cellar run` in a
// child process, so a closed window never takes a running app with it.
package gui

import (
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/jing2uo/cellar/internal/appmgr"
	"github.com/jing2uo/cellar/internal/config"
	"github.com/jing2uo/cellar/internal/desktop"
	"github.com/jing2uo/cellar/internal/fonts"
	"github.com/jing2uo/cellar/internal/importer"
	"github.com/jing2uo/cellar/internal/launch"
	"github.com/jing2uo/cellar/internal/reconcile"
	"github.com/jing2uo/cellar/internal/runner"
	"github.com/jing2uo/cellar/internal/store"
	"github.com/jing2uo/cellar/internal/winapp"
)

//go:embed assets
var assets embed.FS

// AppID is the Flatpak id; Wails passes it to GTK so WebKit's sandboxed
// web process gets an a11y bus name the Flatpak portal accepts.
const AppID = "org.flatpark.Cellar"

// Run opens the window; view picks the first page ("runners", or a prefix
// name), "" for the default.
func Run(version, view string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	// GTK's default Vulkan renderer cannot import the dmabufs WebKit hands it
	// on some GPUs (Intel Lunar Lake's CCS modifier): it reads every frame
	// back to the CPU, and the window stalls for seconds while the page
	// animates. The GL renderer imports them directly.
	if os.Getenv("GSK_RENDERER") == "" {
		os.Setenv("GSK_RENDERER", "gl")
	}
	svc := &Service{self: self, version: version}
	app := application.New(application.Options{
		Name:        "Cellar",
		Description: "Wine prefixes from cellar.toml",
		Services:    []application.Service{application.NewService(svc)},
		Assets:      application.AssetOptions{Handler: application.BundledAssetFileServer(assets)},
		Linux:       application.LinuxOptions{ApplicationID: AppID},
	})
	svc.app = app
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "Cellar",
		Width:  1080,
		Height: 720,
		URL:    "/#" + view,
	})
	return app.Run()
}

type Service struct {
	self    string
	version string
	app     *application.App
	tasks   tasks

	mu        sync.Mutex
	bottleDir string // picked in the import page; replaces the default places

	vmu   sync.Mutex
	verbs []runner.Verb // winetricks' list, once asked
}

type Env struct {
	Version  string           `json:"version"`
	Home     string           `json:"home"`
	Packaged bool             `json:"packaged"`
	Flatpak  bool             `json:"flatpak"`
	Runners  []runner.Info    `json:"runners"`
	Default  string           `json:"default_runner"`
	Locales  []config.Locale  `json:"locales"`
	FontSets []config.FontSet `json:"font_sets"`
	// HostLocale and its font sets are what a new prefix starts with.
	HostLocale string   `json:"host_locale"`
	HostFonts  []string `json:"host_fonts"`
}

func (s *Service) Env() Env {
	def, _ := runner.Default()
	infos := runner.Infos()
	if infos == nil {
		infos = []runner.Info{}
	}
	return Env{
		Version: s.version, Home: store.Home(), Packaged: store.Packaged(), Flatpak: runner.InFlatpak(),
		Runners: infos, Default: def,
		Locales: config.Locales, FontSets: config.FontSets,
		HostLocale: config.HostLocale(), HostFonts: config.SuggestedFontSets(config.HostLocale()),
	}
}

type Catalog struct {
	Releases []runner.Release `json:"releases"`
	Warnings []string         `json:"warnings"`
}

func (s *Service) Available() (Catalog, error) {
	rels, warns, err := runner.Available(3)
	if rels == nil {
		rels = []runner.Release{}
	}
	if warns == nil {
		warns = []string{}
	}
	return Catalog{Releases: rels, Warnings: warns}, err
}

// FontStatus tells what each font set would use on this machine (it runs
// fc-list, so the dialogs ask for it, not every refresh).
func (s *Service) FontStatus() []fonts.Status { return fonts.Statuses() }

// RemoveRunner deletes a downloaded runner that no prefix uses.
func (s *Service) RemoveRunner(name string) error { return appmgr.RemoveRunner(name) }

// InstallRunner downloads "<source>[@<version>]" and returns its name.
func (s *Service) InstallRunner(id string) (string, error) {
	return runner.InstallID(id, nil)
}

type AppRow struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Exe     string `json:"exe"`
	Icon    string `json:"icon,omitempty"` // data: URL
	Desktop bool   `json:"desktop"`        // launcher installed
}

type PrefixRow struct {
	Name     string       `json:"name"`
	Source   string       `json:"source"` // local | flatpak
	Runner   string       `json:"runner"`
	Locale   string       `json:"locale"`
	FontSets []string     `json:"font_sets"`
	State    string       `json:"state"` // new | applied | changed | error
	Apps     []AppRow     `json:"apps"`
	Config   string       `json:"config"`
	Error    string       `json:"error,omitempty"`
	LastRun  *launch.Meta `json:"last_run,omitempty"`
}

func (s *Service) Prefixes() ([]PrefixRow, error) {
	names, err := store.List()
	if err != nil {
		return nil, err
	}
	rows := []PrefixRow{}
	for _, n := range names {
		r := PrefixRow{Name: n, Source: "local", State: "error", Apps: []AppRow{}}
		p, err := store.Open(n)
		var c *config.Config
		if err == nil {
			r.Config = p.Config
			c, err = p.Load()
		}
		if err != nil {
			r.Error = err.Error()
			rows = append(rows, r)
			continue
		}
		st, _ := p.State()
		r.Runner, r.Locale, r.FontSets = c.Runner, c.Locale, c.FontSets
		switch {
		case st.Hash == "":
			r.State = "new"
		case st.Hash != c.Hash():
			r.State = "changed"
		default:
			r.State = "applied"
		}
		for _, a := range c.Apps {
			name := a.Name
			if name == "" {
				name = a.ID
			}
			row := AppRow{ID: a.ID, Name: name, Exe: a.Exe, Icon: iconURL(p, a.Icon)}
			_, err := os.Stat(filepath.Join(desktop.Dir(), desktop.FileName(p.Name, a.ID)))
			row.Desktop = err == nil
			r.Apps = append(r.Apps, row)
		}
		if runs, _ := launch.Runs(p); len(runs) > 0 {
			r.LastRun = &runs[0]
		}
		rows = append(rows, r)
	}
	return rows, nil
}

func open(name string) (*store.Prefix, *config.Config, error) {
	p, err := store.Open(name)
	if err != nil {
		return nil, nil, err
	}
	c, err := p.Load()
	return p, c, err
}

func (s *Service) Plan(name string) (*reconcile.Plan, error) {
	p, c, err := open(name)
	if err != nil {
		return nil, err
	}
	return reconcile.Compute(p, c, false)
}

func (s *Service) Apply(name string) (*reconcile.Plan, error) {
	pl, err := s.Plan(name)
	if err != nil {
		return nil, err
	}
	return pl, pl.Apply(func(string) {})
}

func (s *Service) Runs(name string) ([]launch.Meta, error) {
	p, err := store.Open(name)
	if err != nil {
		return nil, err
	}
	runs, err := launch.Runs(p)
	if runs == nil {
		runs = []launch.Meta{}
	}
	return runs, err
}

func (s *Service) ClearRuns(name string) error {
	p, err := store.Open(name)
	if err != nil {
		return err
	}
	return launch.ClearRuns(p)
}

func (s *Service) Log(name, id string) (string, error) {
	runs, err := s.Runs(name)
	if err != nil {
		return "", err
	}
	for _, m := range runs {
		if m.ID == id {
			b, err := os.ReadFile(m.Log)
			if err != nil {
				return "", err
			}
			lines := strings.Split(string(b), "\n")
			for i, l := range lines {
				lines[i] = launch.Readable(l)
			}
			return strings.Join(lines, "\n"), nil
		}
	}
	return "", errors.New("no such run")
}

func (s *Service) ReadConfig(name string) (string, error) {
	p, err := store.Open(name)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p.Config)
	return string(b), err
}

// SaveConfig writes cellar.toml only if it still loads; the old file is kept
// otherwise.
func (s *Service) SaveConfig(name, text string) error {
	p, err := store.Open(name)
	if err != nil {
		return err
	}
	tmp := filepath.Join(p.Dir, ".cellar.toml.new")
	if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
		return err
	}
	if _, err := config.Load(tmp, ""); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, p.Config); err != nil {
		return err
	}
	return appmgr.RefreshDesktop(s.self, p)
}

// Launch starts `cellar run` detached and returns at once; the run shows up
// in Runs when it ends.
func (s *Service) Launch(name, app, exe string) error {
	args := []string{"run", name}
	if app != "" {
		args[1] = name + "/" + app
	}
	if exe != "" {
		args = append(args, "--exe", exe)
	}
	cmd := exec.Command(s.self, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// Create makes the prefix and starts its first apply (wineboot, fonts),
// returning the task id.
func (s *Service) Create(name, runnerName, locale string, fontSets []string) (string, error) {
	if runnerName == "" {
		var err error
		if runnerName, err = runner.Default(); err != nil {
			return "", err
		}
	}
	if _, err := runner.Resolve(runnerName); err != nil {
		return "", err
	}
	if fontSets == nil {
		fontSets = []string{}
	}
	if _, err := store.Create(name, runnerName, locale, fontSets); err != nil {
		return "", err
	}
	return s.ApplyTask(name)
}

func iconURL(p *store.Prefix, rel string) string {
	if rel == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(p.Dir, rel))
	if err != nil || len(b) > 1<<20 {
		return ""
	}
	mime := "image/png"
	if strings.HasSuffix(strings.ToLower(rel), ".svg") {
		mime = "image/svg+xml"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)
}

// Desktop installs (or with remove, deletes) the host launcher of an app.
func (s *Service) Desktop(name, app string, remove bool) (string, error) {
	p, c, err := open(name)
	if err != nil {
		return "", err
	}
	if remove {
		return desktop.Remove(p, app)
	}
	a, err := c.App(app)
	if err != nil {
		return "", err
	}
	return desktop.Install(s.self, p, a)
}

type BottleRow struct {
	importer.Bottle
	Imported bool `json:"imported"`
}

// Bottles lists Bottles bottles that could be imported.
func (s *Service) Bottles() []BottleRow {
	have := map[string]bool{}
	names, _ := store.List()
	for _, n := range names {
		have[n] = true
	}
	s.mu.Lock()
	dir := s.bottleDir
	s.mu.Unlock()
	found := importer.Find()
	if dir != "" {
		found = importer.Scan(dir)
	}
	rows := []BottleRow{}
	for _, b := range found {
		rows = append(rows, BottleRow{Bottle: b, Imported: have[b.Prefix]})
	}
	return rows
}

// PickBottles asks for Bottles' bottles directory, or one bottle, with the
// system file chooser. In the Flatpak that is the portal's, which grants
// access to what the user picks; it also finds bottles Bottles keeps
// somewhere other than its default. The page then lists only what is in
// it. "" means cancelled.
func (s *Service) PickBottles() (string, error) {
	start := importer.BottlesDirs()[0]
	if _, err := os.Stat(start); err != nil {
		start, _ = os.UserHomeDir()
	}
	dir, err := s.app.Dialog.OpenFile().
		SetTitle("选择 Bottles 的 bottles 目录，或其中一个 bottle").
		SetDirectory(start).
		CanChooseDirectories(true).
		CanChooseFiles(false).
		ShowHiddenFiles(true).
		PromptForSingleSelection()
	if err != nil || dir == "" {
		return "", err
	}
	if len(importer.Scan(dir)) == 0 {
		return "", fmt.Errorf("%s 里没有 bottle：选 Bottles 的 bottles 目录，或某个含 bottle.yml 的 bottle 目录", dir)
	}
	s.mu.Lock()
	s.bottleDir = dir
	s.mu.Unlock()
	return dir, nil
}

func (s *Service) ImportBottle(dir string) (*importer.Result, error) {
	return importer.Import(dir, "")
}

// ApplyTask runs apply in the background. The plan is computed in the task
// too: it runs wine and fc-list, and the page should not wait on them.
func (s *Service) ApplyTask(name string) (string, error) {
	if id := s.tasks.running(name); id != "" {
		return id, nil
	}
	p, err := store.Open(name)
	if err != nil {
		return "", err
	}
	title := "应用配置"
	if !p.Initialized() {
		title = "初始化 prefix"
	}
	return s.tasks.start(name, title, func(log func(string)) error {
		pl, err := s.Plan(name)
		if err != nil {
			return err
		}
		if len(pl.Changes) == 0 {
			log("prefix 已经和配置一致，不用改动")
		}
		for _, c := range pl.Changes {
			log(c.String())
		}
		return pl.Apply(log)
	}), nil
}

// ConfigEdited reports whether cellar.toml has edits since the last apply,
// which RevertConfig can undo.
func (s *Service) ConfigEdited(name string) bool {
	p, err := store.Open(name)
	return err == nil && p.ConfigEdited()
}

// RevertConfig undoes the edits to cellar.toml since the last apply.
func (s *Service) RevertConfig(name string) error {
	p, err := store.Open(name)
	if err != nil {
		return err
	}
	if err := p.RevertConfig(); err != nil {
		return err
	}
	return appmgr.RefreshDesktop(s.self, p)
}

// Delete removes the prefix and everything in it.
func (s *Service) Delete(name string) error {
	if s.tasks.running(name) != "" {
		return errors.New("这个 prefix 还有任务在跑，等它结束再删")
	}
	p, err := store.Open(name)
	if err != nil {
		return err
	}
	return appmgr.DeletePrefix(p)
}

func (s *Service) Task(id string) (Task, error) {
	t, ok := s.tasks.get(id)
	if !ok {
		return t, errors.New("no such task")
	}
	return t, nil
}

func (s *Service) Running(name string) string { return s.tasks.running(name) }

// Settings / SaveSettings back the settings dialog.
func (s *Service) Settings(name string) (config.Settings, error) {
	p, err := store.Open(name)
	if err != nil {
		return config.Settings{}, err
	}
	return config.ReadSettings(p.Config)
}

func (s *Service) SaveSettings(name string, st config.Settings) error {
	p, err := store.Open(name)
	if err != nil {
		return err
	}
	return config.WriteSettings(p.Config, st)
}

type Dep struct {
	Verb string `json:"verb"`
	Note string `json:"note"`
	Slow bool   `json:"slow,omitempty"`
}

// common is the short list the dialog puts first, with a note on when each
// helps; verbs this winetricks lacks are left out.
func common() []Dep {
	return []Dep{
		{"vcrun2022", "VC++ 2015–2022 运行库，大多数新软件需要", false},
		{"vcrun2013", "VC++ 2013 运行库", false},
		{"vcrun2010", "VC++ 2010 运行库", false},
		{"vcrun2008", "VC++ 2008 运行库，较老的软件", false},
		{"vcrun2005", "VC++ 2005 运行库", false},
		{"vcrun6", "VC6 运行库，很老的软件", false},
		{"mfc42", "MFC 4.2，老式国产软件常用", false},
		{"vb6run", "VB6 运行库，老式国产软件常用", false},
		{"riched20", "RichEdit 控件，聊天框、编辑框显示异常时装", false},
		{"gdiplus", "GDI+，图片或图表显示异常时装", false},
		{"msxml6", "MSXML 6", false},
		{"msxml3", "MSXML 3，老软件读写 XML 用", false},
		{"quartz", "DirectShow，程序里放视频", false},
		{"corefonts", "微软核心英文字体（Arial、Times 等）", false},
		{"d3dx9", "DirectX 9 组件，老游戏", false},
		{"d3dcompiler_47", "D3D 着色器编译器，新一些的程序和浏览器内核用", false},
		{"xact", "XAudio / XACT 音频，游戏没声音时装", false},
		{"dxvk", "DXVK：用 Vulkan 跑 D3D 9–11，游戏用", false},
		{"vkd3d", "VKD3D：用 Vulkan 跑 D3D 12", false},
		{"dotnet48", ".NET Framework 4.8", true},
		{"dotnet40", ".NET Framework 4.0", true},
		{"dotnet35", ".NET Framework 3.5", true},
		{"dotnetdesktop8", ".NET 8 桌面运行时", false},
	}
}

// DepState is what the dependency dialog shows: the verbs winetricks has,
// the prefix's list and what winetricks logged as installed in it.
type DepState struct {
	Common    []Dep         `json:"common"`
	Verbs     []runner.Verb `json:"verbs"`
	Want      []string      `json:"want"`
	Installed []string      `json:"installed"`
}

// verbList keeps winetricks' list: asking takes it a couple of seconds.
func (s *Service) verbList() ([]runner.Verb, error) {
	s.vmu.Lock()
	defer s.vmu.Unlock()
	if s.verbs == nil {
		v, err := runner.Verbs()
		if err != nil {
			return nil, err
		}
		s.verbs = v
	}
	return s.verbs, nil
}

func (s *Service) Dependencies(name string) (DepState, error) {
	p, err := store.Open(name)
	if err != nil {
		return DepState{}, err
	}
	st := DepState{Common: []Dep{}, Installed: []string{}}
	if st.Want, err = config.ReadDependencies(p.Config); err != nil {
		return st, err
	}
	if st.Verbs, err = s.verbList(); err != nil {
		return st, err
	}
	have := map[string]bool{}
	for _, v := range st.Verbs {
		have[v.Verb] = true
	}
	for _, d := range common() {
		if have[d.Verb] {
			st.Common = append(st.Common, d)
		}
	}
	b, _ := os.ReadFile(filepath.Join(p.Pfx, "winetricks.log"))
	for _, v := range strings.Fields(string(b)) {
		if !slices.Contains(st.Installed, v) {
			st.Installed = append(st.Installed, v)
		}
	}
	return st, nil
}

// SaveDependencies writes the prefix's list; the page applies after.
func (s *Service) SaveDependencies(name string, deps []string) error {
	p, err := store.Open(name)
	if err != nil {
		return err
	}
	return config.WriteDependencies(p.Config, deps)
}

// OpenURL opens a web page in the host's browser.
func (s *Service) OpenURL(url string) error {
	if !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("not a web address: %s", url)
	}
	return s.app.Browser.OpenURL(url)
}

// PickInstaller asks for an installer (.exe / .msi) with the system file
// chooser.
func (s *Service) PickInstaller() (string, error) {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Downloads")
	if out, err := exec.Command("xdg-user-dir", "DOWNLOAD").Output(); err == nil {
		dir = strings.TrimSpace(string(out))
	}
	return s.app.Dialog.OpenFile().
		SetTitle("选择要运行的程序（安装程序或绿色版）").
		SetDirectory(dir).
		AddFilter("Windows 程序 (*.exe, *.msi)", "*.exe;*.msi;*.EXE;*.MSI").
		AddFilter("所有文件", "*").
		PromptForSingleSelection()
}

// RunInstaller starts an installer detached; .msi goes through msiexec.
func (s *Service) RunInstaller(name, path string) error {
	args := []string{"run", name, "--exe", path}
	if strings.EqualFold(filepath.Ext(path), ".msi") {
		args = []string{"run", name, "--exe", "msiexec", "--", "/i", path}
	}
	cmd := exec.Command(s.self, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

type Found struct {
	winapp.Program
	Icon string `json:"icon,omitempty"`
}

// Programs lists what is installed in the prefix but not yet an app.
func (s *Service) Programs(name string) ([]Found, error) {
	p, c, err := open(name)
	if err != nil {
		return nil, err
	}
	out := []Found{}
	tmp, _ := os.MkdirTemp("", "cellar-icon-")
	defer os.RemoveAll(tmp)
	for i, prog := range appmgr.Discover(p, c) {
		f := Found{Program: prog}
		if i < 40 {
			png := filepath.Join(tmp, fmt.Sprintf("%d.png", i))
			if winapp.ExtractIcon(winapp.UnixPath(p.Pfx, prog.Exe), png) == nil {
				f.Icon = iconURL(&store.Prefix{Dir: tmp}, filepath.Base(png))
			}
		}
		out = append(out, f)
	}
	return out, nil
}

type Entry struct {
	Name string `json:"name"`
	Path string `json:"path"` // Windows path
	Dir  bool   `json:"dir"`
}

// Browse lists a folder of the prefix's drive_c for picking a program.
func (s *Service) Browse(name, win string) ([]Entry, error) {
	p, err := store.Open(name)
	if err != nil {
		return nil, err
	}
	if win == "" {
		win = `C:\`
	}
	ents, err := os.ReadDir(winapp.UnixPath(p.Pfx, win))
	if err != nil {
		return nil, err
	}
	out := []Entry{}
	for _, e := range ents {
		isDir := e.IsDir()
		if e.Type()&os.ModeSymlink != 0 {
			continue
		}
		if !isDir && !strings.EqualFold(filepath.Ext(e.Name()), ".exe") {
			continue
		}
		out = append(out, Entry{Name: e.Name(), Path: strings.TrimRight(win, `\`) + `\` + e.Name(), Dir: isDir})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Dir && !out[j].Dir })
	return out, nil
}

// AddApp records a program as an app, extracts its icon and installs its
// menu launcher.
func (s *Service) AddApp(name string, prog winapp.Program) (string, error) {
	p, c, err := open(name)
	if err != nil {
		return "", err
	}
	a, err := appmgr.Add(p, c, prog, s.self, false)
	if a == nil {
		return "", err
	}
	return a.ID, err
}

func (s *Service) RenameApp(name, id, label string) error {
	p, err := store.Open(name)
	if err != nil {
		return err
	}
	return appmgr.Rename(s.self, p, id, label)
}

func (s *Service) RemoveApp(name, id string) error {
	p, err := store.Open(name)
	if err != nil {
		return err
	}
	return appmgr.Remove(p, id)
}

// Kill ends every process in the prefix (wineserver -k).
func (s *Service) Kill(name string) error {
	p, c, err := open(name)
	if err != nil {
		return err
	}
	r, err := runner.Resolve(c.Runner)
	if err != nil {
		return err
	}
	r.Server(p.Pfx, "-k")
	return nil
}
