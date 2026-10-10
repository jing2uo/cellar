// Package appmgr adds and removes a prefix's apps: the [[apps]] entry, its
// icon and its menu launcher. The GUI and `cellar app` share it.
package appmgr

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jing2uo/cellar/internal/config"
	"github.com/jing2uo/cellar/internal/desktop"
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
