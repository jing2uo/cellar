// Package appmgr adds and removes a prefix's apps (the [[apps]] entry, its
// icon and its menu launcher) and whole prefixes. The GUI and the CLI share
// it.
package appmgr

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/jing2uo/cellar/internal/config"
	"github.com/jing2uo/cellar/internal/desktop"
	"github.com/jing2uo/cellar/internal/runner"
	"github.com/jing2uo/cellar/internal/store"
	"github.com/jing2uo/cellar/internal/winapp"
)

var nonID = regexp.MustCompile(`[^a-z0-9._-]+`)

// Discover lists programs installed in the prefix that are not apps yet.
func Discover(p *store.Prefix, c *config.Config) []winapp.Program {
	have := map[string]bool{}
	for _, a := range c.Apps {
		have[strings.ToLower(a.Exe)] = true
	}
	out := []winapp.Program{}
	for _, prog := range winapp.Discover(p.Pfx) {
		if !have[strings.ToLower(prog.Exe)] {
			out = append(out, prog)
		}
	}
	return out
}

// Add records prog as an app, extracts its icon into <prefix>/icons and,
// unless noDesktop, installs the menu launcher calling back cellarBin.
func Add(p *store.Prefix, c *config.Config, prog winapp.Program, cellarBin string, noDesktop bool) (*config.App, error) {
	if !strings.Contains(prog.Exe, `\`) {
		return nil, fmt.Errorf("exe must be a Windows path inside the prefix (C:\\...), got %q", prog.Exe)
	}
	if _, err := os.Stat(winapp.UnixPath(p.Pfx, prog.Exe)); err != nil {
		return nil, fmt.Errorf("%s: %w", prog.Exe, err)
	}
	if prog.Name == "" {
		prog.Name = strings.TrimSuffix(filepath.Base(strings.ReplaceAll(prog.Exe, `\`, "/")), filepath.Ext(prog.Exe))
	}
	id := strings.Trim(nonID.ReplaceAllString(strings.ToLower(prog.Name), "-"), "-")
	if id == "" {
		id = strings.ToLower(strings.TrimSuffix(filepath.Base(strings.ReplaceAll(prog.Exe, `\`, "/")), ".exe"))
	}
	base := id
	for i := 2; ; i++ {
		if _, err := c.App(id); err != nil {
			break
		}
		id = fmt.Sprintf("%s-%d", base, i)
	}
	a := config.App{ID: id, Name: prog.Name, Exe: prog.Exe, Workdir: prog.Workdir, Desktop: !noDesktop}
	if prog.Args != "" {
		a.Args = strings.Fields(prog.Args)
	}
	if err := os.MkdirAll(filepath.Join(p.Dir, "icons"), 0o755); err == nil {
		rel := filepath.Join("icons", id+".png")
		if winapp.ExtractIcon(winapp.UnixPath(p.Pfx, prog.Exe), filepath.Join(p.Dir, rel)) == nil {
			a.Icon = rel
		}
	}
	e, err := config.Edit(p.Config)
	if err != nil {
		return nil, err
	}
	e.AddApp(a)
	if err := e.Save(); err != nil {
		return nil, err
	}
	if a.Desktop {
		if _, err := desktop.Install(cellarBin, p, &a); err != nil {
			return &a, err
		}
	}
	return &a, nil
}

// RefreshDesktop rewrites the prefix's menu launchers after cellar.toml
// changed: the ones there get the apps' current name, icon and command, and
// the ones whose app is gone are removed. It adds none; that stays the
// user's choice.
func RefreshDesktop(cellarBin string, p *store.Prefix) error {
	if p.Packaged {
		return nil
	}
	c, err := p.Load()
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for i := range c.Apps {
		a := &c.Apps[i]
		have[a.ID] = true
		if _, err := os.Stat(filepath.Join(desktop.Dir(), desktop.FileName(p.Name, a.ID))); err != nil {
			continue
		}
		if _, err := desktop.Install(cellarBin, p, a); err != nil {
			return err
		}
	}
	for _, id := range desktop.Installed(p) {
		if !have[id] {
			desktop.Remove(p, id)
		}
	}
	return nil
}

// Remove drops the app and its launcher; program files stay.
func Remove(p *store.Prefix, id string) error {
	desktop.Remove(p, id)
	os.Remove(filepath.Join(p.Dir, "icons", id+".png"))
	e, err := config.Edit(p.Config)
	if err != nil {
		return err
	}
	if !e.RemoveApp(id) {
		return fmt.Errorf("no app %q", id)
	}
	return e.Save()
}

// DeletePrefix removes a prefix: its processes, its apps' menu launchers
// and its directory, the Windows programs installed in it included.
// RunnerUsers lists the prefixes whose cellar.toml names this runner.
func RunnerUsers(name string) []string {
	names, _ := store.List()
	def, _ := runner.Default()
	var out []string
	for _, n := range names {
		p, err := store.Open(n)
		if err != nil {
			continue
		}
		c, err := p.Load()
		if err != nil {
			continue
		}
		if c.Runner == name || c.Runner == "" && def == name {
			out = append(out, n)
		}
	}
	return out
}

// RemoveRunner deletes a downloaded runner no prefix uses. The system Wine
// is not cellar's to remove.
func RemoveRunner(name string) error {
	if !slices.Contains(runner.List(), name) {
		return fmt.Errorf("runner %q: not a downloaded runner", name)
	}
	if users := RunnerUsers(name); len(users) > 0 {
		return fmt.Errorf("runner %s is used by %s; switch them to another runner first", name, strings.Join(users, ", "))
	}
	return os.RemoveAll(filepath.Join(store.RunnersDir(), name))
}

func DeletePrefix(p *store.Prefix) error {
	if p.Packaged {
		return fmt.Errorf("packaged prefixes are removed with their package")
	}
	if c, err := p.Load(); err == nil {
		if r, err := runner.Resolve(c.Runner); err == nil {
			r.Server(p.Pfx, "-k") // fails when no server runs
			r.Server(p.Pfx, "-w")
		}
		for _, a := range c.Apps {
			desktop.Remove(p, a.ID)
		}
	}
	return os.RemoveAll(p.Dir)
}
