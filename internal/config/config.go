// Package config defines cellar.toml and how its layers merge.
//
// Layers, later wins: the built-in "base" profile, the profiles the file
// lists (in order), the file itself, then an optional override file. Tables
// merge deeply; apps merge by id.
package config

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

//go:embed profiles/*.toml
var profileFS embed.FS

type Config struct {
	Name           string                    `toml:"name,omitempty" json:"name,omitempty"`
	Runner         string                    `toml:"runner,omitempty" json:"runner,omitempty"`
	Profiles       []string                  `toml:"profiles,omitempty" json:"profiles,omitempty"`
	WindowsVersion string                    `toml:"windows_version,omitempty" json:"windows_version,omitempty"`
	Env            map[string]string         `toml:"env,omitempty" json:"env,omitempty"`
	DllOverrides   map[string]string         `toml:"dll_overrides,omitempty" json:"dll_overrides,omitempty"`
	Registry       map[string]map[string]any `toml:"registry,omitempty" json:"registry,omitempty"`
	Fonts          Fonts                     `toml:"fonts,omitempty" json:"fonts,omitempty"`
	Drives         map[string]string         `toml:"drives,omitempty" json:"drives,omitempty"`
	// VirtualDesktop runs apps inside a Wine desktop window of this size
	// ("1920x1080"); "" means each program gets its own windows.
	VirtualDesktop string `toml:"virtual_desktop,omitempty" json:"virtual_desktop,omitempty"`
	Init           Init   `toml:"init,omitempty" json:"init,omitempty"`
	// Dependencies are winetricks verbs (vcrun2022, corefonts, dotnet48...)
	// installed once each; removing one from the list does not uninstall it.
	Dependencies []string `toml:"dependencies,omitempty" json:"dependencies,omitempty"`
	// DPI sets the Windows font scaling (96 = 100%, 144 = 150%, 192 = 200%).
	// Unset follows the host's Xft.dpi on every launch.
	DPI     int      `toml:"dpi,omitempty" json:"dpi,omitempty"`
	Install *Install `toml:"install,omitempty" json:"install,omitempty"`
	Apps    []App    `toml:"apps,omitempty" json:"apps,omitempty"`
}

// Install is the vendor's installer, used unmodified. In a FlatPark package
// Source is its extra-data file name under /app/extra; locally it is a path
// (relative to the prefix directory).
//
//   - extract: unpack it with 7z (no Wine involved). In a package this
//     happens once at install time (apply_extra) into /app/extra, read-only
//     and shared, and each user's prefix links Target to it.
//   - run: run it with Args (silent switches) in the user's prefix on first
//     launch, and again whenever the installer file changes (an update).
type Install struct {
	Source string   `toml:"source" json:"source"`
	Method string   `toml:"method" json:"method"`
	Target string   `toml:"target,omitempty" json:"target,omitempty"` // extract: Windows folder the files appear in
	Subdir string   `toml:"subdir,omitempty" json:"subdir,omitempty"` // extract: use only this folder of the archive
	Args   []string `toml:"args,omitempty" json:"args,omitempty"`     // run
	// Writable gives each user their own copy of the unpacked files instead
	// of a link to the shared read-only one: for portable programs that save
	// settings next to their .exe. Keep lists what the program writes there
	// (relative paths), carried over when a package update refreshes the copy.
	Writable bool     `toml:"writable,omitempty" json:"writable,omitempty"`
	Keep     []string `toml:"keep,omitempty" json:"keep,omitempty"`
}

type Fonts struct {
	// Link names host font families (resolved with fc-match) to link into
	// C:\windows\Fonts and register under HKLM ...\Fonts.
	Link []string `toml:"link,omitempty" json:"link,omitempty"`
}

type Init struct {
	// Mono and Gecko let wineboot offer its .NET / HTML engine installers.
	// Off by default: the prompts block a headless first run.
	Mono  bool `toml:"mono,omitempty" json:"mono,omitempty"`
	Gecko bool `toml:"gecko,omitempty" json:"gecko,omitempty"`
}

type App struct {
	ID      string   `toml:"id" json:"id"`
	Name    string   `toml:"name,omitempty" json:"name,omitempty"`
	Exe     string   `toml:"exe" json:"exe"`
	Args    []string `toml:"args,omitempty" json:"args,omitempty"`
	Workdir string   `toml:"workdir,omitempty" json:"workdir,omitempty"`
	WMClass string   `toml:"wm_class,omitempty" json:"wm_class,omitempty"`
	Desktop bool     `toml:"desktop,omitempty" json:"desktop,omitempty"`
	// Icon is a PNG/SVG path, relative to the prefix directory.
	Icon string `toml:"icon,omitempty" json:"icon,omitempty"`
	// VirtualDesktop overrides the prefix's: a size, or "off".
	VirtualDesktop string            `toml:"virtual_desktop,omitempty" json:"virtual_desktop,omitempty"`
	Env            map[string]string `toml:"env,omitempty" json:"env,omitempty"`
}

func (c *Config) App(id string) (*App, error) {
	if id == "" {
		if len(c.Apps) == 1 {
			return &c.Apps[0], nil
		}
		return nil, fmt.Errorf("prefix has %d apps, name one", len(c.Apps))
	}
	for i := range c.Apps {
		if c.Apps[i].ID == id {
			return &c.Apps[i], nil
		}
	}
	return nil, fmt.Errorf("no app %q", id)
}

// Hash covers everything apply acts on. Apps are left out: changing how a
// program is launched needs no reconcile.
func (c *Config) Hash() string {
	cp := *c
	cp.Apps = nil
	cp.Name = ""
	cp.VirtualDesktop = ""
	b, _ := json.Marshal(cp) // map keys marshal sorted
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// Profiles lists the built-in profile names.
func Profiles() []string {
	ents, _ := fs.ReadDir(profileFS, "profiles")
	var out []string
	for _, e := range ents {
		out = append(out, strings.TrimSuffix(e.Name(), ".toml"))
	}
	return out
}

func profile(name string) (map[string]any, error) {
	b, err := profileFS.ReadFile("profiles/" + name + ".toml")
	if err != nil {
		return nil, fmt.Errorf("unknown profile %q (have: %s)", name, strings.Join(Profiles(), ", "))
	}
	return parse(b, "profile "+name)
}

// expand resolves profile includes (a profile's `include = [...]`), each
// included profile placed before the one including it, once.
func expand(names []string) ([]map[string]any, []string, error) {
	var layers []map[string]any
	var order []string
	seen := map[string]bool{}
	var visit func(n string, depth int) error
	visit = func(n string, depth int) error {
		if seen[n] {
			return nil
		}
		if depth > 8 {
			return fmt.Errorf("profile include loop at %q", n)
		}
		seen[n] = true
		p, err := profile(n)
		if err != nil {
			return err
		}
		if inc, ok := p["include"].([]any); ok {
			for _, i := range inc {
				if err := visit(fmt.Sprint(i), depth+1); err != nil {
					return err
				}
			}
		}
		delete(p, "include")
		layers = append(layers, p)
		order = append(order, n)
		return nil
	}
	for _, n := range names {
		if err := visit(n, 0); err != nil {
			return nil, nil, err
		}
	}
	return layers, order, nil
}

func parse(b []byte, what string) (map[string]any, error) {
	m := map[string]any{}
	if err := toml.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	return m, nil
}

func readLayer(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parse(b, path)
}

// Load merges the layers into the effective config. override may be "" or
// missing.
func Load(path, override string) (*Config, error) {
	user, err := readLayer(path)
	if err != nil {
		return nil, err
	}
	var ov map[string]any
	if override != "" {
		ov, err = readLayer(override)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}

	names := []string{"base"}
	for _, l := range []map[string]any{user, ov} {
		if ps, ok := l["profiles"].([]any); ok {
			names = names[:1]
			for _, p := range ps {
				names = append(names, fmt.Sprint(p))
			}
		}
	}
	merged := map[string]any{}
	layers, _, err := expand(names)
	if err != nil {
		return nil, err
	}
	for _, p := range layers {
		merge(merged, p)
	}
	merge(merged, user)
	if ov != nil {
		merge(merged, ov)
	}

	b, err := toml.Marshal(merged)
	if err != nil {
		return nil, err
	}
	var c Config
	dec := toml.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	c.Profiles = names[1:]
	return &c, nil
}

func merge(dst, src map[string]any) {
	for k, v := range src {
		if k == "apps" {
			dst[k] = mergeApps(dst[k], v)
			continue
		}
		sm, sok := v.(map[string]any)
		dm, dok := dst[k].(map[string]any)
		if sok && dok {
			merge(dm, sm)
			continue
		}
		if sok {
			cp := map[string]any{}
			merge(cp, sm)
			dst[k] = cp
			continue
		}
		dst[k] = v
	}
}

func mergeApps(dst, src any) any {
	d, _ := dst.([]any)
	s, _ := src.([]any)
	idx := map[string]int{}
	for i, a := range d {
		if m, ok := a.(map[string]any); ok {
			idx[fmt.Sprint(m["id"])] = i
		}
	}
	for _, a := range s {
		m, ok := a.(map[string]any)
		if !ok {
			continue
		}
		id := fmt.Sprint(m["id"])
		if i, ok := idx[id]; ok {
			merge(d[i].(map[string]any), m)
			continue
		}
		idx[id] = len(d)
		d = append(d, m)
	}
	return d
}

// Template is what `cellar new` writes.
func Template(name, runner string, profiles []string) string {
	q := make([]string, len(profiles))
	for i, p := range profiles {
		q[i] = fmt.Sprintf("%q", p)
	}
	sort.Strings(q)
	return fmt.Sprintf(`# cellar prefix %q. Edit, then `+"`cellar plan %s`"+` / `+"`cellar apply %s`"+`.
runner = %q
profiles = [%s]

# [dll_overrides]
# taisafe = "disabled"

# [drives]
# d = "xdg:DOWNLOAD"

# [[apps]]
# id = "app"
# exe = 'C:\Program Files\App\app.exe'
# desktop = true
`, name, name, name, runner, strings.Join(q, ", "))
}
