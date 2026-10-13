// cellar manages Wine prefixes from declarative cellar.toml files.
//
// Every command takes --json for machine (and AI) consumption. Inside a
// FlatPark wine package (packaged mode) the prefix argument is omitted:
// `cellar run [app]`.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jing2uo/cellar/internal/appmgr"
	"github.com/jing2uo/cellar/internal/config"
	"github.com/jing2uo/cellar/internal/desktop"
	"github.com/jing2uo/cellar/internal/importer"
	"github.com/jing2uo/cellar/internal/launch"
	"github.com/jing2uo/cellar/internal/reconcile"
	"github.com/jing2uo/cellar/internal/runner"
	"github.com/jing2uo/cellar/internal/store"
	"github.com/jing2uo/cellar/internal/winapp"
)

var version = "dev"

const usage = `cellar - Wine prefixes from cellar.toml

  cellar new <prefix> [--runner R] [--locale zh_CN] [--fonts core,cjk|all|none]
                                     (alias: create; fonts default: core, plus cjk for a CJK region)
  cellar list
  cellar rm <prefix> [--yes]         delete it and everything installed in it
  cellar inspect <prefix>            effective config, defaults merged in
  cellar plan <prefix> [--force]     what apply would change
  cellar apply <prefix> [--force]
  cellar revert <prefix>             undo cellar.toml edits made since the last apply
  cellar run <prefix>[/<app>] [--exe X] [--trace +chan] [-- args]
  cellar config <prefix> [key=value ...]   show or change settings, applied at once:
                                     runner locale fonts=core,cjk|all|none winver desktop=WxH|off dpi=N|auto
  cellar deps <prefix> [add|rm <verb>...]  winetricks verbs (dlls, fonts); add installs at once
  cellar deps search <word>...       find winetricks verbs
  cellar fonts [list] | get <pack> | rm <pack> | policy [host-first|auto|library-only]
                                     the font library (packs: cjk cjk-serif scripts)
  cellar logs <prefix> [--run ID] [--tail N] [--clear]
  cellar kill <prefix>
  cellar desktop <prefix>[/<app>] [--remove]
  cellar app discover <prefix>       programs installed but not added yet
  cellar app add <prefix> <C:\path.exe> [--name N] [--args "..."] [--no-desktop]
  cellar app rm <prefix> <id>
  cellar app rename <prefix> <id> <name>
  cellar import bottles [--list] [<bottle>...] [--as NAME]
  cellar runner list | available [--n 3] | install <source[@ver]|url> | rm <name>
  cellar icon <program.exe> <out.png>   extract a program's icon (packaging)
  cellar extract                     a package's apply_extra: unpack its installer
  cellar gui [runners|<prefix>]

Global: --json. Data: $CELLAR_HOME (default $XDG_DATA_HOME/cellar).
In a FlatPark package, leave out <prefix>: flatpak run --command=cellar <app-id> config locale=ja_JP
`

func main() {
	if len(os.Args) < 2 {
		if store.Packaged() {
			os.Exit(exit(cmdRun(nil)))
		}
		if hasGUI {
			os.Exit(exit(runGUI("")))
		}
		fmt.Print(usage)
		os.Exit(2)
	}
	cmds := map[string]func([]string) error{
		"new": cmdNew, "create": cmdNew, "rm": cmdRm, "list": cmdList, "inspect": cmdInspect, "plan": cmdPlan,
		"apply": cmdApply, "config": cmdConfig, "deps": cmdDeps, "fonts": cmdFonts, "revert": cmdRevert, "run": cmdRun, "logs": cmdLogs, "kill": cmdKill,
		"desktop": cmdDesktop, "import": cmdImport, "app": cmdApp, "runner": cmdRunner,
		"gui": func(a []string) error {
			view := ""
			if len(a) > 0 {
				view = a[0]
			}
			return runGUI(view)
		},
		"version": func([]string) error { fmt.Println(version); return nil },
	}
	cmds["extract"], cmds["icon"] = cmdExtract, cmdIcon
	f, ok := cmds[os.Args[1]]
	if !ok && store.Packaged() {
		// Inside a package the launcher is called as `cellar [files...]`
		// (the .desktop Exec): run the default app with them.
		os.Exit(exit(cmdRun(append([]string{"--"}, os.Args[1:]...))))
	}
	if !ok {
		if os.Args[1] == "help" || os.Args[1] == "-h" || os.Args[1] == "--help" {
			fmt.Print(usage)
			return
		}
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	os.Exit(exit(f(os.Args[2:])))
}

type exitCode int

func (e exitCode) Error() string { return fmt.Sprintf("exit %d", int(e)) }

func exit(err error) int {
	var ec exitCode
	if errors.As(err, &ec) {
		return int(ec)
	}
	if err != nil {
		if jsonOut {
			emit(map[string]string{"error": err.Error()})
		} else {
			fmt.Fprintln(os.Stderr, "cellar:", err)
		}
		return 1
	}
	return 0
}

var jsonOut bool

func emit(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

// parse lets flags appear anywhere before "--"; it returns positionals and
// everything after "--".
func parse(fs *flag.FlagSet, args []string) (pos, rest []string, err error) {
	fs.BoolVar(&jsonOut, "json", false, "JSON output")
	fs.SetOutput(io.Discard)
	for i, a := range args {
		if a == "--" {
			rest = args[i+1:]
			args = args[:i]
			break
		}
	}
	for {
		if err := fs.Parse(args); err != nil {
			return nil, nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, rest, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

// target splits "prefix/app". In packaged mode the whole argument is the app.
func target(pos []string) (prefix, app string, err error) {
	if store.Packaged() {
		if len(pos) > 0 {
			app = pos[0]
		}
		return "", app, nil
	}
	if len(pos) == 0 {
		return "", "", errors.New("which prefix?")
	}
	prefix, app, _ = strings.Cut(pos[0], "/")
	return prefix, app, nil
}

func open(pos []string) (*store.Prefix, *config.Config, string, error) {
	name, app, err := target(pos)
	if err != nil {
		return nil, nil, "", err
	}
	p, err := store.Open(name)
	if err != nil {
		return nil, nil, "", err
	}
	for _, n := range p.Notes {
		fmt.Fprintln(os.Stderr, "cellar:", n)
	}
	c, err := p.Load()
	return p, c, app, err
}

func cmdNew(args []string) error {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	rn := fs.String("runner", "", "runner (default: system Wine, else newest download)")
	loc := fs.String("locale", "", "Windows region (zh_CN, ja_JP...; default: the host's)")
	fonts := fs.String("fonts", "", "font sets: core, cjk, all, comma-separated, or none (default: core, and cjk for a CJK region)")
	pos, _, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: cellar new <prefix>")
	}
	if *rn == "" {
		if *rn, err = runner.Default(); err != nil {
			return err
		}
	}
	// Unset, the region is the host's, and the fonts English plus CJK for
	// a CJK region.
	locale := *loc
	if locale == "" {
		locale = config.HostLocale()
	}
	sets := config.DefaultFontSets(locale)
	if *fonts == "none" {
		sets = []string{}
	} else if *fonts != "" {
		sets = strings.Split(*fonts, ",")
	}
	p, err := store.Create(pos[0], *rn, locale, sets)
	if err != nil {
		return err
	}
	if _, err := p.Load(); err != nil {
		os.RemoveAll(p.Dir)
		return err
	}
	if jsonOut {
		emit(p)
	} else {
		fmt.Println(p.Config)
	}
	return nil
}

func cmdRm(args []string) error {
	fs := flag.NewFlagSet("rm", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "do not ask")
	pos, _, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: cellar rm <prefix> [--yes]")
	}
	p, err := store.Open(pos[0])
	if err != nil {
		return err
	}
	if !*yes {
		fmt.Printf("Delete prefix %q and everything installed in it (%s)? [y/N] ", p.Name, p.Dir)
		var answer string
		fmt.Scanln(&answer)
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			return errors.New("aborted (--yes skips the question)")
		}
	}
	if err := appmgr.DeletePrefix(p); err != nil {
		return err
	}
	if !jsonOut {
		fmt.Println("removed", p.Dir)
	}
	return nil
}

func cmdList(args []string) error {
	if _, _, err := parse(flag.NewFlagSet("list", flag.ContinueOnError), args); err != nil {
		return err
	}
	names, err := store.List()
	if err != nil {
		return err
	}
	type row struct {
		Name    string   `json:"name"`
		Runner  string   `json:"runner"`
		Applied bool     `json:"applied"`
		Dirty   bool     `json:"dirty"`
		Apps    []string `json:"apps"`
		Error   string   `json:"error,omitempty"`
	}
	var rows []row
	for _, n := range names {
		r := row{Name: n}
		p, err := store.Open(n)
		if err == nil {
			var c *config.Config
			if c, err = p.Load(); err == nil {
				st, _ := p.State()
				r.Runner, r.Applied = c.Runner, st.Hash != ""
				r.Dirty = r.Applied && st.Hash != c.Hash()
				for _, a := range c.Apps {
					r.Apps = append(r.Apps, a.ID)
				}
			}
		}
		if err != nil {
			r.Error = err.Error()
		}
		rows = append(rows, r)
	}
	if jsonOut {
		emit(rows)
		return nil
	}
	for _, r := range rows {
		state := "applied"
		switch {
		case r.Error != "":
			state = "error: " + r.Error
		case !r.Applied:
			state = "new"
		case r.Dirty:
			state = "changed"
		}
		fmt.Printf("%-16s %-32s %-10s %s\n", r.Name, r.Runner, state, strings.Join(r.Apps, " "))
	}
	return nil
}

func cmdInspect(args []string) error {
	pos, _, err := parse(flag.NewFlagSet("inspect", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	p, c, _, err := open(pos)
	if err != nil {
		return err
	}
	st, _ := p.State()
	out := map[string]any{"prefix": p, "config": c, "hash": c.Hash(), "applied_hash": st.Hash}
	if !jsonOut {
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	emit(out)
	return nil
}

func plan(args []string, name string) (*reconcile.Plan, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	force := fs.Bool("force", false, "rewrite every managed value")
	pos, _, err := parse(fs, args)
	if err != nil {
		return nil, err
	}
	p, c, _, err := open(pos)
	if err != nil {
		return nil, err
	}
	return reconcile.Compute(p, c, *force)
}

func printPlan(pl *reconcile.Plan) {
	for _, w := range pl.Warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	if len(pl.Changes) == 0 {
		fmt.Printf("%s: up to date (%s)\n", pl.Prefix, pl.Wine)
		return
	}
	for _, c := range pl.Changes {
		fmt.Println(c)
	}
}

func cmdPlan(args []string) error {
	pl, err := plan(args, "plan")
	if err != nil {
		return err
	}
	if jsonOut {
		emit(pl)
	} else {
		printPlan(pl)
	}
	return nil
}

func cmdRevert(args []string) error {
	pos, _, err := parse(flag.NewFlagSet("revert", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: cellar revert <prefix>")
	}
	p, err := store.Open(pos[0])
	if err != nil {
		return err
	}
	if !p.ConfigEdited() {
		return fmt.Errorf("%s: no edits since the last apply", p.Name)
	}
	if err := p.RevertConfig(); err != nil {
		return err
	}
	self, _ := os.Executable()
	self, _ = filepath.EvalSymlinks(self)
	return appmgr.RefreshDesktop(self, p)
}

func cmdApply(args []string) error {
	pl, err := plan(args, "apply")
	if err != nil {
		return err
	}
	logf := func(s string) {
		if !jsonOut {
			fmt.Println("·", s)
		}
	}
	if !jsonOut {
		printPlan(pl)
	}
	if err := pl.Apply(logf); err != nil {
		return err
	}
	if jsonOut {
		emit(pl)
	}
	return nil
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	exe := fs.String("exe", "", "run this program instead of an app")
	trace := fs.String("trace", "", "WINEDEBUG channels")
	pos, rest, err := parse(fs, args)
	if err != nil {
		return err
	}
	p, c, app, err := open(pos)
	if err != nil {
		return err
	}
	st, err := p.State()
	if err != nil {
		return err
	}
	// Plan on every launch, not only when cellar.toml changed: a new cellar
	// (or a package update) can change what the same file means, and links,
	// fonts or drives can drift. The plan is cheap; apply runs only if due.
	_ = st
	pl, err := reconcile.Compute(p, c, false)
	if err != nil {
		return err
	}
	if len(pl.Changes) > 0 {
		if err := pl.Apply(func(s string) { fmt.Fprintln(os.Stderr, "cellar: apply:", s) }); err != nil {
			return fmt.Errorf("apply before run: %w", err)
		}
	}
	r, err := runner.Resolve(c.Runner)
	if err != nil {
		return err
	}
	o := launch.Options{App: app, Exe: *exe, Args: rest, Trace: *trace}
	if fi, _ := os.Stderr.Stat(); !jsonOut && fi != nil && fi.Mode()&os.ModeCharDevice != 0 {
		o.Echo = os.Stderr
	}
	m, err := launch.Run(p, c, r, o)
	if err != nil {
		return err
	}
	if jsonOut {
		emit(m)
	}
	if m.ExitCode != 0 {
		return exitCode(m.ExitCode)
	}
	return nil
}

func cmdLogs(args []string) error {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	id := fs.String("run", "", "run id (default: latest)")
	n := fs.Int("tail", 50, "log lines")
	list := fs.Bool("list", false, "list runs only")
	clear := fs.Bool("clear", false, "delete the finished runs' records")
	pos, _, err := parse(fs, args)
	if err != nil {
		return err
	}
	p, _, _, err := open(pos)
	if err != nil {
		return err
	}
	if *clear {
		return launch.ClearRuns(p)
	}
	runs, err := launch.Runs(p)
	if err != nil {
		return err
	}
	if *list {
		if jsonOut {
			emit(runs)
			return nil
		}
		for i := range runs {
			fmt.Println(launch.FormatMeta(&runs[i]))
		}
		return nil
	}
	var m *launch.Meta
	for i := range runs {
		if *id == "" || runs[i].ID == *id {
			m = &runs[i]
			break
		}
	}
	if m == nil {
		return errors.New("no such run")
	}
	b, _ := os.ReadFile(m.Log)
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	for i, l := range lines {
		lines[i] = launch.Readable(l)
	}
	if len(lines) > *n {
		lines = lines[len(lines)-*n:]
	}
	if jsonOut {
		emit(map[string]any{"run": m, "tail": lines})
		return nil
	}
	fmt.Println(launch.FormatMeta(m))
	fmt.Println(strings.Join(lines, "\n"))
	return nil
}

func cmdKill(args []string) error {
	pos, _, err := parse(flag.NewFlagSet("kill", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	p, c, _, err := open(pos)
	if err != nil {
		return err
	}
	r, err := runner.Resolve(c.Runner)
	if err != nil {
		return err
	}
	r.Kill(p.Pfx)
	return nil
}

func cmdDesktop(args []string) error {
	fs := flag.NewFlagSet("desktop", flag.ContinueOnError)
	remove := fs.Bool("remove", false, "remove the launchers")
	pos, _, err := parse(fs, args)
	if err != nil {
		return err
	}
	if store.Packaged() {
		return errors.New("packaged apps get their launcher from Flatpak")
	}
	p, c, app, err := open(pos)
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	self, _ = filepath.EvalSymlinks(self)
	var done []string
	for i := range c.Apps {
		a := &c.Apps[i]
		if app != "" && a.ID != app || app == "" && !a.Desktop && !*remove {
			continue
		}
		var path string
		if *remove {
			path, err = desktop.Remove(p, a.ID)
			if os.IsNotExist(err) {
				continue
			}
		} else {
			path, err = desktop.Install(self, p, a)
		}
		if err != nil {
			return err
		}
		done = append(done, path)
	}
	if jsonOut {
		emit(done)
	} else {
		for _, d := range done {
			fmt.Println(d)
		}
	}
	return nil
}

func cmdRunner(args []string) error {
	if len(args) == 0 {
		args = []string{"list"}
	}
	fs := flag.NewFlagSet("runner", flag.ContinueOnError)
	n := fs.Int("n", 3, "releases per source")
	pos, _, err := parse(fs, args[1:])
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		infos := runner.Infos()
		if jsonOut {
			emit(map[string]any{"flatpak": runner.InFlatpak(), "runners": infos})
			return nil
		}
		if len(infos) == 0 {
			return runner.ErrNone
		}
		for _, r := range infos {
			def := " "
			if r.Default {
				def = "*"
			}
			fmt.Printf("%s %-36s %-11s %-7s %-22s %s\n", def, r.Name, r.Source, r.Kind, r.Version, r.Dir)
			for _, p := range r.Problems {
				fmt.Println("    ! " + p)
			}
		}
	case "available":
		rels, warns, err := runner.Available(*n)
		if err != nil {
			return err
		}
		if jsonOut {
			emit(map[string]any{"releases": rels, "warnings": warns})
			return nil
		}
		for _, w := range warns {
			fmt.Fprintln(os.Stderr, "warning:", w)
		}
		for _, r := range rels {
			mark := " "
			switch {
			case r.Installed:
				mark = "✓"
			case r.Default:
				mark = "*"
			case !r.Usable:
				mark = "!"
			}
			size := "    ?"
			if r.Size > 0 {
				size = fmt.Sprintf("%4dM", r.Size>>20)
			}
			fmt.Printf("%s %-36s %-7s %s  %s\n", mark, r.ID, r.Kind, size, r.Note)
		}
		fmt.Println("\n* recommended  ✓ installed  ! needs 32-bit libraries here.  cellar runner install <id>")
	case "install":
		if len(pos) != 1 {
			return errors.New("usage: cellar runner install <source[@version]|url>")
		}
		var prog io.Writer
		if !jsonOut {
			prog = os.Stderr
		}
		name, err := runner.InstallID(pos[0], prog)
		if err != nil {
			return err
		}
		if jsonOut {
			emit(map[string]string{"installed": name})
		} else {
			fmt.Println(name)
		}
	case "rm":
		if len(pos) != 1 {
			return errors.New("usage: cellar runner rm <name>")
		}
		return appmgr.RemoveRunner(pos[0])
	default:
		return fmt.Errorf("unknown runner command %q (list, available, install, rm)", args[0])
	}
	return nil
}

func cmdImport(args []string) error {
	if len(args) == 0 || args[0] != "bottles" {
		return errors.New("usage: cellar import bottles [--list] [<bottle>...]")
	}
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	list := fs.Bool("list", false, "list bottles found")
	as := fs.String("as", "", "prefix name (one bottle only)")
	pos, _, err := parse(fs, args[1:])
	if err != nil {
		return err
	}
	found := importer.Find()
	if *list || len(pos) == 0 {
		if jsonOut {
			emit(found)
			return nil
		}
		if len(found) == 0 {
			fmt.Println("no bottles in", strings.Join(importer.BottlesDirs(), ", "))
		}
		for _, b := range found {
			fmt.Printf("%-16s %-16s %-30s %s%s\n", b.Name, b.Runner, strings.Join(b.Apps, ","), b.Dir, b.Error)
		}
		return nil
	}
	if *as != "" && len(pos) > 1 {
		return errors.New("--as takes one bottle")
	}
	var results []*importer.Result
	for _, want := range pos {
		dir := want
		if !filepath.IsAbs(want) {
			dir = ""
			for _, b := range found {
				if b.Name == want {
					dir = b.Dir
				}
			}
			if dir == "" {
				return fmt.Errorf("no bottle %q", want)
			}
		}
		r, err := importer.Import(dir, *as)
		if err != nil {
			return fmt.Errorf("%s: %w", want, err)
		}
		results = append(results, r)
		if !jsonOut {
			fmt.Printf("%s -> %s (apps: %s)\n", want, r.Config, strings.Join(r.Apps, ", "))
			for _, n := range r.Notes {
				fmt.Println("  note:", n)
			}
		}
	}
	if jsonOut {
		emit(results)
	}
	return nil
}

func cmdApp(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: cellar app discover|add|rm <prefix> ...")
	}
	fs := flag.NewFlagSet("app", flag.ContinueOnError)
	name := fs.String("name", "", "app name")
	appArgs := fs.String("args", "", "arguments")
	noDesk := fs.Bool("no-desktop", false, "no menu launcher")
	pos, _, err := parse(fs, args[1:])
	if err != nil {
		return err
	}
	p, c, _, err := open(pos)
	if err != nil {
		return err
	}
	rest := pos
	if !store.Packaged() && len(rest) > 0 {
		rest = rest[1:]
	}
	switch args[0] {
	case "discover":
		progs := appmgr.Discover(p, c)
		if jsonOut {
			emit(progs)
			return nil
		}
		for _, pr := range progs {
			fmt.Printf("%-10s %-32s %s %s\n", pr.From, pr.Name, pr.Exe, pr.Args)
		}
	case "add":
		if len(rest) != 1 {
			return errors.New(`usage: cellar app add <prefix> 'C:\path\to.exe'`)
		}
		self, _ := os.Executable()
		a, err := appmgr.Add(p, c, winapp.Program{Name: *name, Exe: rest[0], Args: *appArgs}, self, *noDesk)
		if a == nil {
			return err
		}
		if jsonOut {
			emit(a)
		} else {
			fmt.Printf("added %s (icon: %s)\n", a.ID, a.Icon)
		}
		return err
	case "rm":
		if len(rest) != 1 {
			return errors.New("usage: cellar app rm <prefix> <id>")
		}
		return appmgr.Remove(p, rest[0])
	case "rename":
		if len(rest) != 2 {
			return errors.New("usage: cellar app rename <prefix> <id> <name>")
		}
		self, _ := os.Executable()
		self, _ = filepath.EvalSymlinks(self)
		return appmgr.Rename(self, p, rest[0], rest[1])
	default:
		return fmt.Errorf("unknown app command %q", args[0])
	}
	return nil
}

// cmdExtract is a FlatPark package's apply_extra: it unpacks an
// extract-method installer into /app/extra once, at install time, offline.
func cmdExtract(args []string) error {
	if _, _, err := parse(flag.NewFlagSet("extract", flag.ContinueOnError), args); err != nil {
		return err
	}
	if !store.Packaged() && os.Getenv("CELLAR_EXTRA") == "" {
		return errors.New("extract runs inside a package (apply_extra)")
	}
	// At install time: the package's own config (there is no user yet).
	c, err := config.Load(store.PackagedConfig)
	if err != nil {
		return err
	}
	if c.Install == nil || c.Install.Method != "extract" {
		return nil // run-method installers stay as they are for first launch
	}
	src := filepath.Join(reconcile.ExtraDir(), c.Install.Source)
	fp, err := reconcile.Fingerprint(src)
	if err != nil {
		return err
	}
	if err := reconcile.Unpack(src, reconcile.Unpacked(), c.Install.Subdir); err != nil {
		return err
	}
	// Per-user writable copies compare against this to notice updates.
	if err := os.WriteFile(reconcile.UnpackedSource(), []byte(fp+"\n"), 0o644); err != nil {
		return err
	}
	// The vendor file has served its purpose; don't keep two copies.
	return os.Remove(src)
}

// cmdIcon extracts a program's icon as PNG, for packagers.
func cmdIcon(args []string) error {
	pos, _, err := parse(flag.NewFlagSet("icon", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return errors.New("usage: cellar icon <program.exe> <out.png>")
	}
	return winapp.ExtractIcon(pos[0], pos[1])
}
