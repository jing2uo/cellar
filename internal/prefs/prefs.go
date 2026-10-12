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
	HostFirst   = "host-first"   // the host's, else the library's; nothing downloaded
	LibraryOnly = "library-only" // only the library's: the same on every machine
	Auto        = "auto"         // as host-first, and an apply downloads what is missing
)

type Prefs struct {
	Fonts Fonts `toml:"fonts" json:"fonts"`
}

type Fonts struct {
	Source string `toml:"source" json:"source"`
}

func path() string { return filepath.Join(store.Home(), "settings.toml") }

// Load reads the settings, filling in defaults. A package downloads what it
// misses on its own: nobody is there to do it on its first launch.
func Load() Prefs {
	p := Prefs{}
	if b, err := os.ReadFile(path()); err == nil {
		toml.Unmarshal(b, &p)
	}
	switch p.Fonts.Source {
	case HostFirst, LibraryOnly, Auto:
	default:
		p.Fonts.Source = HostFirst
		if store.Packaged() {
			p.Fonts.Source = Auto
		}
	}
	return p
}

func Save(p Prefs) error {
	switch p.Fonts.Source {
	case HostFirst, LibraryOnly, Auto:
	default:
		return fmt.Errorf("font source %q (want %s, %s or %s)", p.Fonts.Source, HostFirst, LibraryOnly, Auto)
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
