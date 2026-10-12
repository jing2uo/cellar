// Package store locates prefixes, their config layers and their state on
// disk, in either local mode or packaged (inside a Flatpak) mode.
package store

import (
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
const PackagedConfig = "/app/share/cellar/cellar.toml"

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
	Override string `json:"override,omitempty"`
	Pfx      string `json:"wineprefix"`
	Packaged bool   `json:"packaged"`
}

var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Open finds a prefix by name. In packaged mode there is exactly one prefix
// and name is ignored.
func Open(name string) (*Prefix, error) {
	if Packaged() {
		dir := Home()
		return &Prefix{
			Name:     os.Getenv("FLATPAK_ID"),
			Dir:      dir,
			Config:   PackagedConfig,
			Override: filepath.Join(xdg("XDG_CONFIG_HOME", ".config"), "cellar", "override.toml"),
			Pfx:      filepath.Join(dir, "pfx"),
			Packaged: true,
		}, nil
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

func Create(name, runner string, profiles []string, locale string, fontSets []string) (*Prefix, error) {
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
	if err := os.WriteFile(filepath.Join(dir, "cellar.toml"), []byte(config.Template(name, runner, profiles, locale, fontSets)), 0o644); err != nil {
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
	c, err := config.Load(p.Config, p.Override)
	if err != nil {
		return nil, err
	}
	if c.Name == "" {
		c.Name = p.Name
	}
	return c, nil
}

func (p *Prefix) RunsDir() string { return filepath.Join(p.Dir, "runs") }

// State is what the last successful apply did. Registry holds only values
// cellar itself wrote, so a later apply knows what it may delete.
type State struct {
	Hash      string                       `json:"hash"`
	Runner    string                       `json:"runner"`
	Wine      string                       `json:"wine_version"`
	Registry  map[string]map[string]string `json:"registry"`
	Fonts     map[string]string            `json:"fonts"`
	Drives    map[string]string            `json:"drives"`
	Install   string                       `json:"install,omitempty"` // fingerprint of the installer last run/unpacked
	AppliedAt time.Time                    `json:"applied_at"`
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
