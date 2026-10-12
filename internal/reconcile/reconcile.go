// Package reconcile turns a cellar.toml into a Wine prefix: it computes the
// desired state, diffs it against what cellar applied last time (and, for
// files, against the disk), and applies the difference.
package reconcile

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jing2uo/cellar/internal/config"
	"github.com/jing2uo/cellar/internal/launch"
	"github.com/jing2uo/cellar/internal/runner"
	"github.com/jing2uo/cellar/internal/store"
)

type Change struct {
	Op    string `json:"op"` // init update set delete link(copy font) unlink map unmap install
	Key   string `json:"key,omitempty"`
	Name  string `json:"name,omitempty"`
	Value string `json:"value,omitempty"`
	Old   string `json:"old,omitempty"`
}

func (c Change) String() string {
	switch c.Op {
	case "init", "update":
		return fmt.Sprintf("%-6s prefix  %s", c.Op, c.Value)
	case "set":
		s := fmt.Sprintf("set    %s\\%s = %s", c.Key, c.Name, c.Value)
		if c.Old != "" {
			s += "  (was " + c.Old + ")"
		}
		return s
	case "delete":
		return fmt.Sprintf("delete %s\\%s  (was %s)", c.Key, c.Name, c.Old)
	case "link", "unlink":
		return fmt.Sprintf("%-6s font    %s <- %s", c.Op, c.Name, c.Value)
	case "map", "unmap":
		return fmt.Sprintf("%-6s drive   %s: -> %s", c.Op, c.Name, c.Value)
	case "install":
		switch c.Name {
		case "link", "extract":
			return fmt.Sprintf("install %-7s %s <- %s", c.Name, c.Key, c.Value)
		case "run":
			return "install run     " + c.Value
		}
		return fmt.Sprintf("install winetricks %s", c.Name)
	}
	return c.Op
}

type Plan struct {
	Prefix   string   `json:"prefix"`
	Hash     string   `json:"hash"`
	Runner   string   `json:"runner"`
	Wine     string   `json:"wine_version"`
	Changes  []Change `json:"changes"`
	Warnings []string `json:"warnings,omitempty"`

	p       *store.Prefix
	c       *config.Config
	r       *runner.Runner
	st      *store.State
	want    *desired
	sets    map[string]map[string]value
	dels    map[string]map[string]value
	links   map[string]string // font file name -> host path
	unlink  []string
	drives  map[string]string // letter -> host path, "" removes
	deps    []string          // winetricks verbs to install
	install *Change           // vendor installer step

	// RunProgram runs a Windows program in the prefix and waits; set by the
	// caller (it lives in package launch, which records the run).
	RunProgram func(exe string, args []string) error `json:"-"`
}

type desired struct {
	reg    map[string]map[string]value
	fonts  map[string]string // file name -> host path
	drives map[string]string
}

// Compute builds the plan. force re-writes every managed registry value even
// if state.json says it is already there.
func Compute(p *store.Prefix, c *config.Config, force bool) (*Plan, error) {
	r, err := runner.Resolve(c.Runner)
	if err != nil {
		return nil, err
	}
	st, err := p.State()
	if err != nil {
		return nil, err
	}
	pl := &Plan{Prefix: p.Name, Hash: c.Hash(), Runner: r.Name, Wine: r.Version(), p: p, c: c, r: r, st: st}
	pl.want, pl.Warnings, err = want(c)
	if err != nil {
		return nil, err
	}
	if c.DPI == 0 {
		// Automatic: follow the host's Xft.dpi; with no display to ask (a
		// headless apply), keep whatever was applied last.
		key := normKey(`HKCU\Control Panel\Desktop`)
		if dpi := hostDPI(); dpi > 0 {
			put(pl.want.reg, key, "LogPixels", value(fmt.Sprintf("dword:%08x", dpi)))
		} else if old, ok := st.Registry[key]["LogPixels"]; ok {
			put(pl.want.reg, key, "LogPixels", value(old))
		}
	}
	// Installers run like any program, so the run is recorded in runs/.
	pl.RunProgram = func(exe string, args []string) error {
		m, err := launch.Run(p, c, r, launch.Options{Exe: exe, Args: args})
		if err != nil {
			return err
		}
		if m.ExitCode != 0 {
			return fmt.Errorf("installer exited %d (log: %s)", m.ExitCode, m.Log)
		}
		return nil
	}
	pl.diff(force)
	return pl, nil
}

func (pl *Plan) diff(force bool) {
	pl.Changes = []Change{}
	initialized := pl.p.Initialized()
	applied := pl.st.Registry
	switch {
	case !initialized:
		pl.Changes = append(pl.Changes, Change{Op: "init", Value: pl.Wine})
		applied = nil
	case pl.st.Wine != "" && (pl.st.Wine != pl.Wine || pl.st.Runner != pl.Runner):
		pl.Changes = append(pl.Changes, Change{Op: "update", Value: pl.st.Wine + " -> " + pl.Wine})
	}

	pl.sets, pl.dels = map[string]map[string]value{}, map[string]map[string]value{}
	for key, vals := range pl.want.reg {
		for n, v := range vals {
			old, had := applied[key][n]
			if had && value(old) == v && !force {
				continue
			}
			put(pl.sets, key, n, v)
			ch := Change{Op: "set", Key: key, Name: n, Value: string(v)}
			if had && value(old) != v {
				ch.Old = old
			}
			pl.Changes = append(pl.Changes, ch)
		}
	}
	for key, vals := range applied {
		for n, old := range vals {
			if _, ok := pl.want.reg[key][n]; !ok {
				put(pl.dels, key, n, value(old))
				pl.Changes = append(pl.Changes, Change{Op: "delete", Key: key, Name: n, Old: old})
			}
		}
	}

	fontsDir := filepath.Join(pl.p.Pfx, "drive_c", "windows", "Fonts")
	pl.links, pl.unlink = map[string]string{}, nil
	for f, src := range pl.want.fonts {
		if !sameFile(filepath.Join(fontsDir, f), src) {
			pl.links[f] = src
			pl.Changes = append(pl.Changes, Change{Op: "link", Name: f, Value: src})
		}
	}
	for f, src := range pl.st.Fonts {
		if _, ok := pl.want.fonts[f]; !ok {
			pl.unlink = append(pl.unlink, f)
			pl.Changes = append(pl.Changes, Change{Op: "unlink", Name: f, Value: src})
		}
	}

	dos := filepath.Join(pl.p.Pfx, "dosdevices")
	pl.drives = map[string]string{}
	for letter, target := range pl.want.drives {
		cur, err := os.Readlink(filepath.Join(dos, letter+":"))
		exists := err == nil
		switch {
		case target == "" && exists:
			pl.drives[letter] = ""
			pl.Changes = append(pl.Changes, Change{Op: "unmap", Name: letter, Value: cur})
		case target != "" && cur != target:
			pl.drives[letter] = target
			pl.Changes = append(pl.Changes, Change{Op: "map", Name: letter, Value: target})
		}
	}
	pl.deps = nil
	done := winetricksDone(pl.p.Pfx)
	for _, v := range pl.c.Dependencies {
		if !done[v] {
			pl.deps = append(pl.deps, v)
			pl.Changes = append(pl.Changes, Change{Op: "install", Name: v})
		}
	}

	pl.install = nil
	if ch, err := pl.installChange(); err != nil {
		pl.Warnings = appendOnce(pl.Warnings, err.Error())
	} else if ch != nil {
		pl.install = ch
		pl.Changes = append(pl.Changes, *ch)
	}

	// Before init there is no dosdevices yet; wineboot creates z:, so plan
	// its removal now and let Apply recompute after wineboot.
	if !initialized && pl.want.drives["z"] == "" {
		if _, ok := pl.want.drives["z"]; ok {
			pl.Changes = append(pl.Changes, Change{Op: "unmap", Name: "z", Value: "/"})
		}
	}
	sort.SliceStable(pl.Changes, func(i, j int) bool { return opOrder(pl.Changes[i]) < opOrder(pl.Changes[j]) })
}

func opOrder(c Change) string {
	o := map[string]string{"init": "0", "update": "0", "delete": "1", "set": "2", "unlink": "3", "link": "4", "unmap": "5", "map": "6", "install": "7"}[c.Op]
	if c.Op == "install" {
		return o // dependencies in config order, the app's installer last
	}
	return o + c.Key + "\x00" + c.Name
}

func put(m map[string]map[string]value, key, name string, v value) {
	if m[key] == nil {
		m[key] = map[string]value{}
	}
	m[key][name] = v
}

var fontTmpl = regexp.MustCompile(`\{font:([^}]+)\}`)

func want(c *config.Config) (*desired, []string, error) {
	d := &desired{reg: map[string]map[string]value{}, fonts: map[string]string{}, drives: map[string]string{}}
	var warns []string

	resolved := map[string]string{} // family -> host path
	resolve := func(family string) (string, bool) {
		if f, ok := resolved[family]; ok {
			return f, f != ""
		}
		f, err := fcMatch(family)
		if err != nil {
			warns = append(warns, err.Error())
		}
		resolved[family] = f
		return f, f != ""
	}

	for _, fam := range c.Fonts.Link {
		path, ok := resolve(fam)
		if !ok {
			continue
		}
		name := filepath.Base(path)
		d.fonts[name] = path
		put(d.reg, normKey(`HKLM\Software\Microsoft\Windows NT\CurrentVersion\Fonts`), fam+" (TrueType)", value("sz:"+name))
	}

	expand := func(s string) (string, error) {
		var missing string
		out := fontTmpl.ReplaceAllStringFunc(s, func(m string) string {
			fam := fontTmpl.FindStringSubmatch(m)[1]
			path, ok := resolve(fam)
			if !ok {
				missing = fam
				return m
			}
			return filepath.Base(path)
		})
		if missing != "" {
			return "", fmt.Errorf("font %q not found", missing)
		}
		return out, nil
	}

	for key, vals := range c.Registry {
		nk := normKey(key)
		for n, raw := range vals {
			v, err := parseValue(raw, expand)
			if err != nil {
				warns = append(warns, fmt.Sprintf("skip %s\\%s: %v", key, n, err))
				continue
			}
			put(d.reg, nk, n, v)
		}
	}
	for dll, mode := range c.DllOverrides {
		if mode == "disabled" {
			mode = ""
		}
		put(d.reg, normKey(`HKCU\Software\Wine\DllOverrides`), dll, value("sz:"+mode))
	}
	if dpi := c.DPI; dpi > 0 {
		put(d.reg, normKey(`HKCU\Control Panel\Desktop`), "LogPixels", value(fmt.Sprintf("dword:%08x", dpi)))
	}
	if c.WindowsVersion != "" {
		put(d.reg, normKey(`HKCU\Software\Wine`), "Version", value("sz:"+c.WindowsVersion))
	}

	for letter, target := range c.Drives {
		letter = strings.ToLower(strings.TrimSuffix(letter, ":"))
		if len(letter) != 1 || letter[0] < 'a' || letter[0] > 'y' && letter != "z" {
			return nil, nil, fmt.Errorf("bad drive letter %q", letter)
		}
		if letter == "c" {
			return nil, nil, fmt.Errorf("drive c: belongs to the prefix")
		}
		if target == "" {
			d.drives[letter] = ""
			continue
		}
		path, err := hostPath(target)
		if err != nil {
			warns = append(warns, fmt.Sprintf("drive %s: %v", letter, err))
			continue
		}
		d.drives[letter] = path
	}
	return d, warns, nil
}

// fcMatch finds the file of an installed family. fc-list, not fc-match:
// host rules may alias a family to another (Debian's do, "Noto Sans CJK SC"
// -> "Noto Sans"), and fc-match follows them; fc-list matches the name.
func fcMatch(family string) (string, error) {
	for _, pat := range []string{family + ":style=Regular", family} {
		out, err := exec.Command("fc-list", "-f", "%{file}\n", pat).Output()
		if err != nil {
			return "", fmt.Errorf("fc-list %q: %v", family, err)
		}
		files := strings.Fields(string(out))
		if len(files) > 0 {
			sort.Strings(files)
			return files[0], nil
		}
	}
	return "", fmt.Errorf("font %q not installed", family)
}

// hostPath expands ~, $VARS and xdg:NAME (a user-dirs.dirs entry such as
// xdg:DOWNLOAD).
func hostPath(s string) (string, error) {
	home, _ := os.UserHomeDir()
	if name, ok := strings.CutPrefix(s, "xdg:"); ok {
		out, err := exec.Command("xdg-user-dir", strings.ToUpper(name)).Output()
		if err != nil {
			return "", fmt.Errorf("xdg-user-dir %s: %v", name, err)
		}
		s = strings.TrimSpace(string(out))
		if s == home || s == "" {
			return "", fmt.Errorf("xdg dir %s is not set", name)
		}
		return s, nil
	}
	if s == "~" {
		s = home
	} else if rest, ok := strings.CutPrefix(s, "~/"); ok {
		s = filepath.Join(home, rest)
	}
	s = os.ExpandEnv(s)
	if !filepath.IsAbs(s) {
		return "", fmt.Errorf("%q is not absolute", s)
	}
	return s, nil
}

// Apply executes the plan. Steps run in order; state.json is written only
// when all of them succeed.
func (pl *Plan) Apply(log func(string)) error {
	if len(pl.Changes) > 0 && (pl.Changes[0].Op == "init" || pl.Changes[0].Op == "update") {
		arg := "-i"
		if pl.Changes[0].Op == "update" {
			arg = "-u"
		}
		log(fmt.Sprintf("wineboot %s (%s)", arg, pl.Wine))
		if err := pl.wineboot(arg); err != nil {
			return err
		}
		if pl.Changes[0].Op == "init" {
			pl.st.Registry = nil
		}
		pl.diff(false) // dosdevices and Fonts exist now
	}

	if len(pl.sets)+len(pl.dels) > 0 {
		log(fmt.Sprintf("registry: %d set, %d delete", count(pl.sets), count(pl.dels)))
		if err := pl.importReg(); err != nil {
			return err
		}
	}

	fontsDir := filepath.Join(pl.p.Pfx, "drive_c", "windows", "Fonts")
	for _, f := range pl.unlink {
		dst := filepath.Join(fontsDir, f)
		os.Remove(dst)
	}
	for f, src := range pl.links {
		dst := filepath.Join(fontsDir, f)
		if err := copyFile(src, dst); err != nil {
			return fmt.Errorf("font %s: %w", f, err)
		}
		log("font " + f)
	}

	dos := filepath.Join(pl.p.Pfx, "dosdevices")
	for letter, target := range pl.drives {
		link := filepath.Join(dos, letter+":")
		if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
			return err
		}
		if target != "" {
			if err := os.Symlink(target, link); err != nil {
				return err
			}
		}
		log(fmt.Sprintf("drive %s: -> %s", letter, orNone(target)))
	}

	// A verb that fails (often a download) does not stop the rest: the
	// others still install and the state is still recorded. winetricks.log
	// decides what is pending, so the failed ones stay in the next plan.
	var failed []string
	var details []string
	for _, v := range pl.deps {
		log("winetricks " + v)
		if err := pl.winetricks(v, log); err != nil {
			log("  ✗ " + v + " failed, skipped")
			failed = append(failed, v)
			details = append(details, err.Error())
		}
	}

	installFP := pl.st.Install
	if pl.install != nil && len(failed) > 0 {
		// The installer may need what failed; it runs on the next apply.
		log("install skipped: dependencies failed")
	} else if pl.install != nil {
		run := pl.RunProgram
		if run == nil {
			return errors.New("install: no program runner set")
		}
		fp, err := pl.doInstall(pl.install, run, log)
		if err != nil {
			return fmt.Errorf("install: %w", err)
		}
		installFP = fp
	}

	// Make wineserver flush the registry before we record success.
	if err := pl.r.Server(pl.p.Pfx, "-w"); err != nil {
		return fmt.Errorf("wineserver -w: %w", err)
	}

	st := &store.State{
		Hash: pl.Hash, Runner: pl.Runner, Wine: pl.Wine,
		Registry: map[string]map[string]string{}, Fonts: maps.Clone(pl.want.fonts),
		Drives: maps.Clone(pl.want.drives), Install: installFP, AppliedAt: time.Now().UTC(),
	}
	for key, vals := range pl.want.reg {
		st.Registry[key] = map[string]string{}
		for n, v := range vals {
			st.Registry[key][n] = string(v)
		}
	}
	if len(failed) > 0 {
		// Not the config's hash, so the prefix still reads as changed.
		st.Hash = "partial:" + pl.Hash
	}
	if err := pl.p.SaveState(st); err != nil {
		return err
	}
	if len(failed) > 0 {
		return fmt.Errorf("winetricks failed: %s (the rest was applied)\n%s",
			strings.Join(failed, " "), strings.Join(details, "\n"))
	}
	return nil
}

func (pl *Plan) wineboot(arg string) error {
	if err := os.MkdirAll(pl.p.Pfx, 0o755); err != nil {
		return err
	}
	var skip []string
	if !pl.c.Init.Mono {
		skip = append(skip, "mscoree")
	}
	if !pl.c.Init.Gecko {
		skip = append(skip, "mshtml")
	}
	env := maps.Clone(pl.c.Env)
	if env == nil {
		env = map[string]string{}
	}
	if len(skip) > 0 {
		env["WINEDLLOVERRIDES"] = strings.Join(skip, ",") + "="
	}
	cmd := pl.r.Command(pl.p.Pfx, env, "wineboot", arg)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("wineboot %s: %v\n%s", arg, err, tail(out.String(), 20))
	}
	return pl.r.Server(pl.p.Pfx, "-w")
}

func (pl *Plan) importReg() error {
	f, err := os.CreateTemp(pl.p.Dir, ".apply-*.reg")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(regFile(pl.sets, pl.dels)); err != nil {
		return err
	}
	f.Close()
	cmd := pl.r.Command(pl.p.Pfx, pl.c.Env, "reg", "import", f.Name())
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("reg import: %v\n%s", err, tail(out.String(), 20))
	}
	return nil
}

// Fonts are copied, not linked: the same family lives at /usr/share/fonts on
// the host and /run/host/fonts in a Flatpak, and a link would flip between
// the two whenever the prefix is used from the other side. A copy with the
// same size counts as the same font wherever it came from.
func sameFile(dst, src string) bool {
	d, err := os.Lstat(dst)
	if err != nil || !d.Mode().IsRegular() {
		return false
	}
	s, err := os.Stat(src)
	return err == nil && s.Size() == d.Size()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	os.Remove(dst)
	tmp := dst + ".cellar-tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

func count(m map[string]map[string]value) int {
	n := 0
	for _, v := range m {
		n += len(v)
	}
	return n
}

func unionKeys(a, b map[string]map[string]value) []string {
	ks := slices.Collect(maps.Keys(a))
	for k := range b {
		if _, ok := a[k]; !ok {
			ks = append(ks, k)
		}
	}
	sort.Strings(ks)
	return ks
}

func sortedNames(m map[string]value) []string {
	ns := slices.Collect(maps.Keys(m))
	sort.Strings(ns)
	return ns
}

func orNone(s string) string {
	if s == "" {
		return "(removed)"
	}
	return s
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// winetricksDone reads the verbs winetricks itself logged as installed in
// the prefix, so the prefix - not cellar's state - is the truth.
func winetricksDone(pfx string) map[string]bool {
	b, _ := os.ReadFile(filepath.Join(pfx, "winetricks.log"))
	done := map[string]bool{}
	for _, l := range strings.Fields(string(b)) {
		done[l] = true
	}
	return done
}

func (pl *Plan) winetricks(verb string, log func(string)) error {
	wt, err := runner.Winetricks()
	if err != nil {
		return err
	}
	env := maps.Clone(pl.c.Env)
	if env == nil {
		env = map[string]string{}
	}
	env["WINE"], env["WINESERVER"] = pl.r.Wine, pl.r.Wineserver
	env["WINETRICKS_LATEST_VERSION_CHECK"] = "disabled"
	cmd := exec.Command(wt, "-q", verb)
	cmd.Env = runner.Environ(pl.p.Pfx, env)
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	var tailBuf []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			l := sc.Text()
			tailBuf = append(tailBuf, l)
			if len(tailBuf) > 30 {
				tailBuf = tailBuf[1:]
			}
			log("  " + l)
		}
	}()
	err = cmd.Run()
	pw.Close()
	<-done
	if err != nil {
		return fmt.Errorf("winetricks %s: %v\n%s", verb, err, strings.Join(tailBuf, "\n"))
	}
	if !winetricksDone(pl.p.Pfx)[verb] {
		return fmt.Errorf("winetricks %s finished but did not record the verb", verb)
	}
	return nil
}

func appendOnce(xs []string, x string) []string {
	for _, y := range xs {
		if y == x {
			return xs
		}
	}
	return append(xs, x)
}
