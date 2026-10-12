// Package importer turns Bottles bottles into cellar prefixes.
//
// A bottle directory is itself the Wine prefix (drive_c, *.reg, dosdevices)
// plus Bottles' own files. Everything Bottles set through the registry - DLL
// overrides (DXVK's included), DPI, Windows version, winecfg tweaks - lives in
// the copied *.reg files and needs no translation. What Bottles applies at
// launch time instead is translated into cellar.toml: the language, the
// virtual desktop, and the program list.
package importer

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/jing2uo/cellar/internal/store"
)

// BottlesDirs are where Bottles keeps bottles: the Flatpak's data dir first.
func BottlesDirs() []string {
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, ".var/app/com.usebottles.bottles/data/bottles/bottles"),
		filepath.Join(home, ".local/share/bottles/bottles"),
	}
}

type bottleYML struct {
	Name     string            `yaml:"Name"`
	Arch     string            `yaml:"Arch"`
	Runner   string            `yaml:"Runner"`
	Windows  string            `yaml:"Windows"`
	Language string            `yaml:"Language"`
	Env      map[string]string `yaml:"Environment_Variables"`
	Params   struct {
		VirtualDesktop    bool   `yaml:"virtual_desktop"`
		VirtualDesktopRes string `yaml:"virtual_desktop_res"`
	} `yaml:"Parameters"`
	Programs map[string]struct {
		Name           string `yaml:"name"`
		Executable     string `yaml:"executable"`
		Path           string `yaml:"path"`
		Folder         string `yaml:"folder"`
		Arguments      string `yaml:"arguments"`
		ArgumentsOn    *bool  `yaml:"arguments_enabled"`
		VirtualDesktop *bool  `yaml:"virtual_desktop"`
		Removed        *bool  `yaml:"removed"`
	} `yaml:"External_Programs"`
}

type Bottle struct {
	Name   string   `json:"name"`
	Prefix string   `json:"prefix"` // what Import names it
	Dir    string   `json:"dir"`
	Runner string   `json:"runner"`
	Apps   []string `json:"apps"`
	Size   int64    `json:"-"`
	Error  string   `json:"error,omitempty"`
}

// Find lists the bottles on this machine.
func Find() []Bottle {
	var out []Bottle
	for _, root := range BottlesDirs() {
		ents, _ := os.ReadDir(root)
		for _, e := range ents {
			dir := filepath.Join(root, e.Name())
			b := Bottle{Name: e.Name(), Prefix: slug(e.Name()), Dir: dir}
			y, err := read(dir)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				b.Error = err.Error()
			} else {
				b.Runner = y.Runner
				for _, p := range programs(y) {
					b.Apps = append(b.Apps, p.Name)
				}
			}
			out = append(out, b)
		}
	}
	return out
}

func read(dir string) (*bottleYML, error) {
	f, err := os.ReadFile(filepath.Join(dir, "bottle.yml"))
	if err != nil {
		return nil, err
	}
	var y bottleYML
	if err := yaml.Unmarshal(f, &y); err != nil {
		return nil, fmt.Errorf("bottle.yml: %w", err)
	}
	return &y, nil
}

type program struct {
	ID, Name, Exe string
	Args          []string
	VD            *bool
}

func programs(y *bottleYML) []program {
	var out []program
	seen := map[string]int{}
	for _, p := range y.Programs {
		if p.Removed != nil && *p.Removed {
			continue
		}
		pr := program{Name: p.Name, Exe: p.Path, VD: p.VirtualDesktop}
		if p.Arguments != "" && (p.ArgumentsOn == nil || *p.ArgumentsOn) {
			pr.Args = strings.Fields(p.Arguments)
		}
		id := slug(p.Name)
		if n := seen[id]; n > 0 {
			id = fmt.Sprintf("%s-%d", id, n+1)
		}
		seen[slug(p.Name)]++
		pr.ID = id
		out = append(out, pr)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

var nonSlug = regexp.MustCompile(`[^a-z0-9._-]+`)

func slug(s string) string {
	s = strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if s == "" {
		return "app"
	}
	return s
}

// winPath turns a program path Bottles stored as a host path inside the
// bottle into a C:\ path, so it survives the move.
func winPath(bottleDir, p string) string {
	if len(p) >= 2 && p[1] == ':' {
		return p
	}
	driveC := filepath.Join(bottleDir, "drive_c") + "/"
	if rest, ok := strings.CutPrefix(p, driveC); ok {
		return `C:\` + strings.ReplaceAll(rest, "/", `\`)
	}
	return p
}

// skip lists the bottle entries that are Bottles' own, not the prefix's.
var skip = map[string]bool{"bottle.yml": true, "template.yml": true, "icons": true, "cache": true, "states": true, ".fvs": true}

type Result struct {
	Prefix string   `json:"prefix"`
	Config string   `json:"config"`
	Apps   []string `json:"apps"`
	Notes  []string `json:"notes,omitempty"`
}

// Import copies bottleDir into a new prefix called name. The copy goes
// through copy_file_range, which btrfs turns into a reflink: no data is
// duplicated and a multi-GB bottle takes seconds.
func Import(bottleDir, name string) (*Result, error) {
	y, err := read(bottleDir)
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = slug(filepath.Base(bottleDir))
	}
	dir := filepath.Join(store.PrefixesDir(), name)
	if _, err := os.Stat(dir); err == nil {
		return nil, fmt.Errorf("prefix %q already exists", name)
	}
	tmp := dir + ".importing"
	os.RemoveAll(tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "pfx"), 0o755); err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(bottleDir)
	if err != nil {
		return nil, err
	}
	for _, e := range ents {
		if skip[e.Name()] {
			continue
		}
		if err := copyTree(filepath.Join(bottleDir, e.Name()), filepath.Join(tmp, "pfx", e.Name())); err != nil {
			os.RemoveAll(tmp)
			return nil, fmt.Errorf("copy %s: %w", e.Name(), err)
		}
	}
	if _, err := os.Stat(filepath.Join(bottleDir, "icons")); err == nil {
		copyTree(filepath.Join(bottleDir, "icons"), filepath.Join(tmp, "icons"))
	}

	res := &Result{Prefix: name}
	toml, notes := render(bottleDir, y, tmp, res)
	res.Notes = notes
	if err := os.WriteFile(filepath.Join(tmp, "cellar.toml"), []byte(toml), 0o644); err != nil {
		os.RemoveAll(tmp)
		return nil, err
	}
	if err := os.Rename(tmp, dir); err != nil {
		os.RemoveAll(tmp)
		return nil, err
	}
	res.Config = filepath.Join(dir, "cellar.toml")
	return res, nil
}

func render(bottleDir string, y *bottleYML, dst string, res *Result) (string, []string) {
	var b strings.Builder
	var notes []string
	q := func(s string) string { return fmt.Sprintf("%q", s) }
	lit := func(s string) string {
		if strings.Contains(s, "'") {
			return q(s)
		}
		return "'" + s + "'"
	}

	fmt.Fprintf(&b, "# Imported from Bottles %s on %s.\n", bottleDir, time.Now().Format("2006-01-02"))
	b.WriteString("# The registry (DLL overrides incl. DXVK, DPI, Windows version, winecfg settings)\n")
	b.WriteString("# came along with the prefix; this file only carries what Bottles applied at launch.\n\n")

	runner := y.Runner
	if strings.HasPrefix(runner, "sys-wine") {
		// Bottles' Flatpak "system" Wine is org.winehq.Wine - the same base
		// cellar's Flatpak is built on.
		runner = "system"
	} else {
		notes = append(notes, fmt.Sprintf("Bottles runner %q: install a matching runner (cellar runner available) and set runner", y.Runner))
	}
	fmt.Fprintf(&b, "runner = %s  # Bottles: %s\n", q(runner), y.Runner)
	b.WriteString("profiles = []  # the prefix is already set up; add \"zh-cn\" only if fonts need it\n")
	if y.Params.VirtualDesktop && y.Params.VirtualDesktopRes != "" {
		fmt.Fprintf(&b, "virtual_desktop = %s\n", q(y.Params.VirtualDesktopRes))
	}

	env := map[string]string{}
	for k, v := range y.Env {
		env[k] = v
	}
	if y.Language != "" && y.Language != "sys" {
		loc := y.Language
		if !strings.Contains(loc, ".") {
			loc += ".UTF-8"
		}
		env["LANG"], env["LC_ALL"] = loc, loc
	}
	if len(env) > 0 {
		b.WriteString("\n[env]\n")
		keys := make([]string, 0, len(env))
		for k := range env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "%s = %s\n", k, q(env[k]))
		}
	}

	// Keep Z: exactly as the bottle had it; cellar's base profile would
	// otherwise remove it.
	if t, err := os.Readlink(filepath.Join(bottleDir, "dosdevices", "z:")); err == nil {
		fmt.Fprintf(&b, "\n[drives]\nz = %s  # as in Bottles; \"\" hides the host filesystem from Windows programs\n", q(t))
	} else {
		b.WriteString("\n[drives]\nz = \"\"  # the bottle had no Z: drive\n")
	}

	icons, _ := os.ReadDir(filepath.Join(dst, "icons"))
	for _, p := range programs(y) {
		fmt.Fprintf(&b, "\n[[apps]]\nid = %s\nname = %s\nexe = %s\n", q(p.ID), q(p.Name), lit(winPath(bottleDir, p.Exe)))
		if len(p.Args) > 0 {
			parts := make([]string, len(p.Args))
			for i, a := range p.Args {
				parts[i] = q(a)
			}
			fmt.Fprintf(&b, "args = [%s]\n", strings.Join(parts, ", "))
		}
		if p.VD != nil && !*p.VD && y.Params.VirtualDesktop {
			b.WriteString("virtual_desktop = \"off\"\n")
		}
		if icon := pickIcon(icons, p); icon != "" {
			fmt.Fprintf(&b, "icon = %s\n", q("icons/"+icon))
		}
		b.WriteString("desktop = true\n")
		res.Apps = append(res.Apps, p.ID)
	}
	return b.String(), notes
}

// pickIcon finds the icon Bottles extracted for a program: named after the
// program, or after its executable.
func pickIcon(icons []os.DirEntry, p program) string {
	exe := strings.TrimSuffix(strings.ToLower(filepath.Base(strings.ReplaceAll(p.Exe, `\`, "/"))), ".exe")
	for _, want := range []string{strings.ToLower(p.Name), exe} {
		for _, e := range icons {
			if strings.EqualFold(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())), want) {
				return e.Name()
			}
		}
	}
	return ""
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		to := filepath.Join(dst, rel)
		switch {
		case fi.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, to)
		case fi.IsDir():
			return os.MkdirAll(to, fi.Mode().Perm()|0o700)
		case fi.Mode().IsRegular():
			return copyFile(path, to, fi.Mode().Perm())
		}
		return nil // sockets, fifos: not part of a prefix
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode|0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
