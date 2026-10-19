// Package store locates prefixes, their config layers and their state on
// disk, in either local mode or packaged (inside a Flatpak) mode.
package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/jing2uo/cellar/internal/config"
)

// PackagedConfig is where a Flatpak wine app ships its cellar.toml. Its
// presence switches cellar into packaged mode.
var PackagedConfig = "/app/share/cellar/cellar.toml" // a var for the tests

func Packaged() bool {
	_, err := os.Stat(PackagedConfig)
	return err == nil
}

func xdg(env, fallback string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, fallback)
}

// Home is cellar's data root.
func Home() string {
	if v := os.Getenv("CELLAR_HOME"); v != "" {
		return v
	}
	return filepath.Join(xdg("XDG_DATA_HOME", ".local/share"), "cellar")
}

func RunnersDir() string  { return filepath.Join(Home(), "runners") }
func PrefixesDir() string { return filepath.Join(Home(), "prefixes") }

type Prefix struct {
	Name     string `json:"name"`
	Dir      string `json:"dir"`
	Config   string `json:"config"`
	Pfx      string `json:"wineprefix"`
	Packaged bool   `json:"packaged"`
	// Notes are what opening it did worth telling: a package update merged
	// into the user's cellar.toml, keys the user's changes kept.
	Notes []string `json:"notes,omitempty"`
}

var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// CheckName vets a prefix name: it becomes a directory under the prefixes.
func CheckName(name string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("bad prefix name %q", name)
	}
	return nil
}

// Open finds a prefix by name. In packaged mode there is exactly one prefix
// and name is ignored.
func Open(name string) (*Prefix, error) {
	if Packaged() {
		dir := Home()
		p := &Prefix{Name: os.Getenv("FLATPAK_ID"), Dir: dir, Config: filepath.Join(dir, "cellar.toml"),
			Pfx: filepath.Join(dir, "pfx"), Packaged: true}
		return p, p.syncPackage()
	}
	if !validName.MatchString(name) {
		return nil, fmt.Errorf("bad prefix name %q", name)
	}
	dir := filepath.Join(PrefixesDir(), name)
	p := &Prefix{Name: name, Dir: dir, Config: filepath.Join(dir, "cellar.toml"), Pfx: filepath.Join(dir, "pfx")}
	if _, err := os.Stat(p.Config); err != nil {
		return nil, fmt.Errorf("no prefix %q (%s)", name, p.Config)
	}
	return p, nil
}

func Create(name, runner, locale string, fontSets []string) (*Prefix, error) {
	if !validName.MatchString(name) {
		return nil, fmt.Errorf("bad prefix name %q", name)
	}
	dir := filepath.Join(PrefixesDir(), name)
	if _, err := os.Stat(dir); err == nil {
		return nil, fmt.Errorf("prefix %q already exists", name)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "cellar.toml"), []byte(config.Template(name, runner, locale, fontSets)), 0o644); err != nil {
		return nil, err
	}
	return Open(name)
}

func List() ([]string, error) {
	ents, err := os.ReadDir(PrefixesDir())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	var out []string
	for _, e := range ents {
		if _, err := os.Stat(filepath.Join(PrefixesDir(), e.Name(), "cellar.toml")); err == nil {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

func (p *Prefix) Load() (*config.Config, error) {
	c, err := config.Load(p.Config)
	if err != nil {
		return nil, err
	}
	if c.Name == "" {
		c.Name = p.Name
	}
	return c, nil
}

// packageBase is the package's cellar.toml the user's copy last took in.
func (p *Prefix) packageBase() string { return filepath.Join(p.Dir, "package.toml") }

// syncPackage keeps a package's prefix defined by one cellar.toml, the
// user's: the first launch copies the package's in; a package update is
// merged into it key by key (config.MergeUpstream), the user's changes
// winning.
func (p *Prefix) syncPackage() error {
	next, err := os.ReadFile(PackagedConfig)
	if err != nil {
		return err
	}
	if _, err := os.Stat(p.Config); err != nil {
		if err := os.MkdirAll(p.Dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p.Config, next, 0o644); err != nil {
			return err
		}
		return os.WriteFile(p.packageBase(), next, 0o644)
	}
	base, err := os.ReadFile(p.packageBase())
	if err != nil {
		base, _ = os.ReadFile(p.Config)
	}
	if bytes.Equal(base, next) {
		return nil
	}
	conflicts, err := config.MergeUpstream(p.Config, base, next)
	if err != nil {
		p.Notes = append(p.Notes, "package update not merged into "+p.Config+": "+err.Error())
		return nil
	}
	for _, k := range conflicts {
		p.Notes = append(p.Notes, fmt.Sprintf("package update changed %s; your own setting of it stays", k))
	}
	return os.WriteFile(p.packageBase(), next, 0o644)
}

func (p *Prefix) RunsDir() string { return filepath.Join(p.Dir, "runs") }

// AppliedConfig is cellar.toml as of the last apply.
func (p *Prefix) AppliedConfig() string { return filepath.Join(p.Dir, "applied.toml") }

// ConfigEdited reports whether cellar.toml differs from the applied copy:
// whether there are edits RevertConfig can undo.
func (p *Prefix) ConfigEdited() bool {
	a, err := os.ReadFile(p.AppliedConfig())
	if err != nil {
		return false
	}
	b, err := os.ReadFile(p.Config)
	return err == nil && !bytes.Equal(a, b)
}

// RevertConfig puts cellar.toml back as it was at the last apply.
func (p *Prefix) RevertConfig() error {
	b, err := os.ReadFile(p.AppliedConfig())
	if err != nil {
		return fmt.Errorf("no applied copy of %s to go back to", p.Config)
	}
	tmp := p.Config + ".revert"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p.Config)
}

// State is what the last successful apply did. Registry holds only values
// cellar itself wrote, so a later apply knows what it may delete.
type State struct {
	Hash     string                       `json:"hash"`
	Runner   string                       `json:"runner"`
	Wine     string                       `json:"wine_version"`
	Registry map[string]map[string]string `json:"registry"`
	Fonts    map[string]string            `json:"fonts"`
	Drives   map[string]string            `json:"drives"`
	Install  string                       `json:"install,omitempty"` // fingerprint of the installer last run/unpacked
	// CorePack is the English core fonts' pack applied (mscore or
	// liberation), "" without the core set: what the prefix followed when
	// cellar's settings, not its cellar.toml, chose it.
	CorePack  string    `json:"core_pack,omitempty"`
	AppliedAt time.Time `json:"applied_at"`
}

func (p *Prefix) statePath() string { return filepath.Join(p.Dir, "state.json") }

// State returns the zero State when the prefix was never applied.
func (p *Prefix) State() (*State, error) {
	s := &State{}
	b, err := os.ReadFile(p.statePath())
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	return s, json.Unmarshal(b, s)
}

func (p *Prefix) SaveState(s *State) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := p.statePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p.statePath())
}

// Initialized reports whether wineboot has created the prefix.
func (p *Prefix) Initialized() bool {
	_, err := os.Stat(filepath.Join(p.Pfx, "system.reg"))
	return err == nil
}
