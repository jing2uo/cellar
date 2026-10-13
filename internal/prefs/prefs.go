// Package prefs is cellar's own settings, as opposed to a prefix's: one
// settings.toml per cellar home (the manager's, or each package's), what
// the GUI's settings page and `cellar fonts policy` edit.
package prefs

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"

	"github.com/jing2uo/cellar/internal/store"
)

// Font sources: where the font sets' fonts may come from.
const (
	HostFirst   = "host-first"   // the host's, else the library's
	LibraryOnly = "library-only" // only the library's: the same on every machine
)

type Prefs struct {
	Fonts Fonts `toml:"fonts" json:"fonts"`
}

type Fonts struct {
	Source string `toml:"source" json:"source"`
}

func path() string { return filepath.Join(store.Home(), "settings.toml") }

// Load reads the settings, filling in defaults. Nothing is ever downloaded
// on its own: the library is filled in the settings or with cellar fonts get.
func Load() Prefs {
	p := Prefs{}
	if b, err := os.ReadFile(path()); err == nil {
		toml.Unmarshal(b, &p)
	}
	switch p.Fonts.Source {
	case HostFirst, LibraryOnly:
	default:
		p.Fonts.Source = HostFirst
	}
	return p
}

func Save(p Prefs) error {
	switch p.Fonts.Source {
	case HostFirst, LibraryOnly:
	default:
		return fmt.Errorf("font source %q (want %s or %s)", p.Fonts.Source, HostFirst, LibraryOnly)
	}
	b, err := toml.Marshal(p)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(store.Home(), 0o755); err != nil {
		return err
	}
	tmp := path() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path())
}
