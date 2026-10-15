// Package launch runs programs in a prefix and keeps a record of every run:
// runs/<id>/meta.json (what ran, how, and how it ended) and runs/<id>/wine.log
// (everything wine printed). The record is what `cellar logs` and AI
// diagnosis read.
package launch

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"

	"github.com/jing2uo/cellar/internal/config"
	"github.com/jing2uo/cellar/internal/fontreg"
	"github.com/jing2uo/cellar/internal/regfile"
	"github.com/jing2uo/cellar/internal/runner"
	"github.com/jing2uo/cellar/internal/store"
)

const keepRuns = 20

type Meta struct {
	ID       string            `json:"id"`
	Prefix   string            `json:"prefix"`
	App      string            `json:"app,omitempty"`
	Runner   string            `json:"runner"`
	Wine     string            `json:"wine_version"`
	Argv     []string          `json:"argv"`
	Workdir  string            `json:"workdir"`
	Env      map[string]string `json:"env"` // what cellar set, not the inherited rest
	Start    time.Time         `json:"start"`
	End      time.Time         `json:"end"`
	Seconds  float64           `json:"seconds"`
	ExitCode int               `json:"exit_code"`
	Signal   string            `json:"signal,omitempty"`
	Log      string            `json:"log"`
}

type Options struct {
	App   string   // app id from cellar.toml
	Exe   string   // or an ad-hoc program: Windows path, unix path, or a builtin like winecfg
	Args  []string // extra arguments
	Trace string   // WINEDEBUG channels, e.g. "+loaddll,+seh"
	Echo  io.Writer
}

// Run starts the program and waits for it. The returned Meta is also
// written to disk.
func Run(p *store.Prefix, c *config.Config, r *runner.Runner, o Options) (*Meta, error) {
	var exe string
	var args []string
	env := maps.Clone(c.Env)
	if env == nil {
		env = map[string]string{}
	}
	env["WINEDEBUG"] = "fixme-all"
	workdir := ""
	appID := ""
	vd := c.VirtualDesktop
	if o.Exe != "" {
		exe = o.Exe
		// A Windows Installer package is not a program: msiexec runs it.
		if strings.EqualFold(filepath.Ext(exe), ".msi") {
			exe, args = "msiexec", []string{"/i", o.Exe}
		}
	} else {
		app, err := c.App(o.App)
		if err != nil {
			return nil, err
		}
		appID, exe, args, workdir = app.ID, app.Exe, app.Args, app.Workdir
		maps.Copy(env, app.Env)
		if app.VirtualDesktop != "" {
			vd = app.VirtualDesktop
		}
	}
	args = append(append([]string{}, args...), o.Args...)
	// Host paths become Windows paths: without a Z: drive (cellar's default)
	// wine cannot start a program given by its unix path at all.
	exe = winPath(p.Pfx, exe)
	for i, a := range args {
		if strings.HasPrefix(a, "/") {
			if _, err := os.Stat(a); err == nil {
				args[i] = winPath(p.Pfx, a)
			}
		}
	}
	if o.Trace != "" {
		env["WINEDEBUG"] = o.Trace
	}
	if workdir == "" {
		workdir = exeDir(p.Pfx, exe)
	} else {
		workdir = unixPath(p.Pfx, workdir)
	}

	now := time.Now()
	id := now.Format("20060102-150405.000")
	dir := filepath.Join(p.RunsDir(), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	logf, err := os.Create(filepath.Join(dir, "wine.log"))
	if err != nil {
		return nil, err
	}
	defer logf.Close()

	// Fonts added to or deleted from C:\windows\Fonts since the last run,
	// by hand or by a program, get listed for DirectWrite.
	if ch, err := fontreg.Sync(r, p.Pfx, env); err != nil {
		fmt.Fprintf(logf, "cellar: fonts registry: %v\n", err)
	} else if !ch.Empty() {
		fmt.Fprintf(logf, "cellar: %s\n", ch)
	}

	argv := append([]string{exe}, args...)
	if vd != "" && vd != "off" {
		if err := desktopSize(r, p.Pfx, env, vd); err != nil {
			fmt.Fprintf(logf, "cellar: virtual desktop size: %v\n", err)
		}
		// One named desktop per app, so launching it again joins the window
		// already open (Bottles makes a new one every launch).
		name := p.Name
		if appID != "" {
			name += "-" + appID
		}
		argv = append([]string{"explorer", "/desktop=" + name + "," + vd}, argv...)
	}
	cmd := r.Command(p.Pfx, env, argv...)
	cmd.Dir = workdir
	out := io.Writer(logf)
	if o.Echo != nil {
		out = io.MultiWriter(logf, o.Echo)
	}
	cmd.Stdout, cmd.Stderr = out, out

	m := &Meta{
		ID: id, Prefix: p.Name, App: appID, Runner: r.Name, Wine: r.Version(),
		Argv: append([]string{r.Wine}, argv...), Workdir: workdir, Env: env, Start: now,
		Log: logf.Name(),
	}
	m.Env["WINEPREFIX"] = p.Pfx

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigs)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		for s := range sigs {
			cmd.Process.Signal(s)
		}
	}()
	err = cmd.Wait()
	m.End = time.Now()
	m.Seconds = m.End.Sub(m.Start).Round(time.Millisecond).Seconds()
	m.ExitCode = cmd.ProcessState.ExitCode()
	if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		m.Signal = ws.Signal().String()
	}
	if err != nil && !errors.As(err, new(*exec.ExitError)) {
		return m, err
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), b, 0o644); err != nil {
		return m, err
	}
	prune(p.RunsDir())
	return m, nil
}

// desktopSize makes Wine take the virtual desktop's size. Wine sizes it by
// switching to that display mode, and its modes are a list of common sizes
// up to the screen's (internal/display) plus the Desktops "Default" size:
// any other size is refused and the desktop keeps Default's. Default is set
// to the size every time it differs, so one an app overrides works too.
func desktopSize(r *runner.Runner, pfx string, env map[string]string, size string) error {
	cur, err := regfile.Values(filepath.Join(pfx, "user.reg"), `Software\Wine\Explorer\Desktops`)
	if err != nil || cur["Default"] == size {
		return err
	}
	out, err := r.Command(pfx, env, "reg", "add", `HKCU\Software\Wine\Explorer\Desktops`,
		"/v", "Default", "/t", "REG_SZ", "/d", size, "/f").CombinedOutput()
	if err != nil {
		return fmt.Errorf("reg add: %v\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// InstallDrive is the drive letter cellar points at the folder of a program
// started from outside the prefix (an installer in ~/Downloads, say). The
// whole folder, not a copy of the file: multi-file installers look for their
// data next to setup.exe.
const InstallDrive = "y"

// winPath turns an absolute host path into one Wine can use: C:\ inside the
// prefix, an existing drive that covers it, or else InstallDrive pointed at
// its folder. Anything else is returned unchanged.
func winPath(pfx, path string) string {
	if !filepath.IsAbs(path) {
		return path
	}
	path = filepath.Clean(path)
	dos := filepath.Join(pfx, "dosdevices")
	best, bestLen := "", -1
	ents, _ := os.ReadDir(dos)
	for _, e := range ents {
		n := e.Name()
		if len(n) != 2 || n[1] != ':' {
			continue
		}
		target, err := filepath.EvalSymlinks(filepath.Join(dos, n))
		if err != nil {
			continue
		}
		if (path == target || strings.HasPrefix(path, target+"/") || target == "/") && len(target) > bestLen {
			best, bestLen = n, len(target)
		}
	}
	if best == "" {
		// Inside the prefix, through its own (possibly symlinked) path.
		if real, err := filepath.EvalSymlinks(filepath.Join(pfx, "drive_c")); err == nil && strings.HasPrefix(path, real+"/") {
			return `C:\` + strings.ReplaceAll(strings.TrimPrefix(path, real+"/"), "/", `\`)
		}
		link := filepath.Join(dos, InstallDrive+":")
		os.Remove(link)
		if os.Symlink(filepath.Dir(path), link) != nil {
			return path
		}
		return strings.ToUpper(InstallDrive) + `:\` + filepath.Base(path)
	}
	target, _ := filepath.EvalSymlinks(filepath.Join(dos, best))
	rest := strings.TrimPrefix(strings.TrimPrefix(path, target), "/")
	return strings.ToUpper(best[:1]) + `:\` + strings.ReplaceAll(rest, "/", `\`)
}

// unixPath maps C:\... into the prefix; other letters go through dosdevices.
func unixPath(pfx, p string) string {
	if len(p) >= 2 && p[1] == ':' {
		letter := strings.ToLower(p[:1])
		rest := strings.ReplaceAll(strings.TrimLeft(p[2:], `\/`), `\`, "/")
		return filepath.Join(pfx, "dosdevices", letter+":", rest)
	}
	return p
}

func exeDir(pfx, exe string) string {
	if strings.ContainsAny(exe, `\/`) {
		return filepath.Dir(unixPath(pfx, exe))
	}
	return filepath.Join(pfx, "drive_c")
}

func Runs(p *store.Prefix) ([]Meta, error) {
	ents, err := os.ReadDir(p.RunsDir())
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	var out []Meta
	for _, e := range ents {
		b, err := os.ReadFile(filepath.Join(p.RunsDir(), e.Name(), "meta.json"))
		if err != nil {
			continue // still running, or killed before it wrote meta
		}
		var m Meta
		if json.Unmarshal(b, &m) == nil {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// ClearRuns deletes the finished runs' records; running ones, which have no
// meta.json yet, stay.
func ClearRuns(p *store.Prefix) error {
	runs, err := Runs(p)
	if err != nil {
		return err
	}
	for _, m := range runs {
		if err := os.RemoveAll(filepath.Join(p.RunsDir(), m.ID)); err != nil {
			return err
		}
	}
	return nil
}

func prune(dir string) {
	ents, _ := os.ReadDir(dir)
	if len(ents) <= keepRuns {
		return
	}
	names := make([]string, len(ents))
	for i, e := range ents {
		names[i] = e.Name()
	}
	sort.Strings(names)
	for _, n := range names[:len(names)-keepRuns] {
		os.RemoveAll(filepath.Join(dir, n))
	}
}

func FormatMeta(m *Meta) string {
	sig := ""
	if m.Signal != "" {
		sig = " (" + m.Signal + ")"
	}
	return fmt.Sprintf("%s  %-12s exit %d%s  %.1fs  %s", m.ID, orDash(m.App), m.ExitCode, sig, m.Seconds, strings.Join(m.Argv[1:], " "))
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// Readable decodes a log line for display. Wine's own messages are UTF-8,
// but console programs write their output in the OEM code page, which under
// zh_CN is GBK; a line that isn't valid UTF-8 is read as GB18030.
func Readable(line string) string {
	if utf8.ValidString(line) {
		return line
	}
	if s, err := simplifiedchinese.GB18030.NewDecoder().String(line); err == nil {
		return s
	}
	return line
}
