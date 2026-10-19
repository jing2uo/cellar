package importer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/jing2uo/cellar/internal/config"
	"github.com/jing2uo/cellar/internal/runner"
	"github.com/jing2uo/cellar/internal/store"
)

// CellarDirs are where the other cellar keeps its prefixes: the
// system-installed one's when this is the Flatpak, the Flatpak's when this is
// installed on the system.
func CellarDirs() []string {
	home, _ := os.UserHomeDir()
	if runner.InFlatpak() {
		return []string{filepath.Join(home, ".local/share/cellar/prefixes")}
	}
	return []string{filepath.Join(home, ".var/app/org.flatpark.Cellar/data/cellar/prefixes")}
}

// FindCellar lists the prefixes of the other cellar.
func FindCellar() []Bottle {
	var out []Bottle
	for _, root := range CellarDirs() {
		out = append(out, ScanCellar(root)...)
	}
	return out
}

// ScanCellar lists the cellar prefixes in dir: dir itself when it is one, the
// ones directly in it (a prefixes directory), or those of the cellar home it
// is.
func ScanCellar(dir string) []Bottle {
	if p, ok := cellarPrefix(dir); ok {
		return []Bottle{p}
	}
	if _, err := os.Stat(filepath.Join(dir, "prefixes")); err == nil {
		dir = filepath.Join(dir, "prefixes")
	}
	var out []Bottle
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if p, ok := cellarPrefix(filepath.Join(dir, e.Name())); ok {
			out = append(out, p)
		}
	}
	return out
}

func cellarPrefix(dir string) (Bottle, bool) {
	name := filepath.Base(dir)
	p := Bottle{Name: name, Prefix: name, Dir: dir}
	c, err := config.Load(filepath.Join(dir, "cellar.toml"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			return p, false
		}
		p.Error = err.Error()
		return p, true
	}
	if store.CheckName(name) != nil {
		p.Prefix = slug(name)
	}
	p.Runner = c.Runner
	for _, a := range c.Apps {
		if a.Name != "" {
			p.Apps = append(p.Apps, a.Name)
		} else {
			p.Apps = append(p.Apps, a.ID)
		}
	}
	return p, true
}

// cellarSkip lists what stays behind: the run logs, and a package's copy of
// its own cellar.toml.
var cellarSkip = map[string]bool{"runs": true, "package.toml": true}

// ImportCellar copies a prefix of another cellar into a new prefix called
// name. cellar.toml comes along unchanged, and so do the state of the last
// apply (state.json, applied.toml): the next apply here knows what cellar
// wrote and changes only what differs.
func ImportCellar(srcDir, name string) (*Result, error) {
	c, err := config.Load(filepath.Join(srcDir, "cellar.toml"))
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = filepath.Base(srcDir)
		if store.CheckName(name) != nil {
			name = slug(name)
		}
	}
	if err := store.CheckName(name); err != nil {
		return nil, err
	}
	dir := filepath.Join(store.PrefixesDir(), name)
	if _, err := os.Stat(dir); err == nil {
		return nil, fmt.Errorf("prefix %q already exists", name)
	}
	tmp := dir + ".importing"
	os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(srcDir)
	if err != nil {
		os.RemoveAll(tmp)
		return nil, err
	}
	for _, e := range ents {
		if cellarSkip[e.Name()] {
			continue
		}
		if err := copyTree(filepath.Join(srcDir, e.Name()), filepath.Join(tmp, e.Name())); err != nil {
			os.RemoveAll(tmp)
			return nil, fmt.Errorf("copy %s: %w", e.Name(), err)
		}
	}
	if err := os.Rename(tmp, dir); err != nil {
		os.RemoveAll(tmp)
		return nil, err
	}
	res := &Result{Prefix: name, Config: filepath.Join(dir, "cellar.toml")}
	for _, a := range c.Apps {
		res.Apps = append(res.Apps, a.ID)
	}
	// Runners are per cellar, and "system" is the host's Wine to a
	// system-installed cellar but the Flatpak's own inside it.
	if c.Runner != "" {
		if _, err := runner.Resolve(c.Runner); err != nil {
			res.Notes = append(res.Notes, fmt.Sprintf("runner %q is not installed here: install it, or choose another runner in the prefix's settings", c.Runner))
		}
	}
	return res, nil
}
