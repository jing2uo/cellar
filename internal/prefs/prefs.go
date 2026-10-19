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

// English core fonts, one or the other: the metric-compatible Liberation
// fonts standing in by name (the default: nothing to download, the Flatpak
// runtime has them), or Microsoft's own (Core fonts for the Web, the
// mscore pack). WPF needs a real Arial: it falls back to it by name and
// does not read substitutes. A prefix may choose for itself ([fonts] core
// in its cellar.toml); this is the choice for the ones that do not.
const (
	CoreLiberation = "liberation"
	CoreMicrosoft  = "microsoft"
)

// UI choices for the GUI; "" follows the system.
const (
	LangZH = "zh"
	LangEN = "en"

	ThemeLight = "light"
	ThemeDark  = "dark"
)

type Prefs struct {
	Fonts   Fonts   `toml:"fonts" json:"fonts"`
	UI      UI      `toml:"ui" json:"ui"`
	Runners Runners `toml:"runners" json:"runners"`
}

type Runners struct {
	// Legacy lists legacy (non-WoW64) runners for download; off by default,
	// they need 32-bit libraries the recommended WoW64 ones do without.
	Legacy bool `toml:"legacy,omitempty" json:"legacy"`
}

type UI struct {
	Language string `toml:"language,omitempty" json:"language"` // "" | zh | en
	Theme    string `toml:"theme,omitempty" json:"theme"`       // "" | light | dark
	// Order is the sidebar's prefix order, as dragged; prefixes not in it
	// follow, by name.
	Order []string `toml:"order,omitempty" json:"order"`
}

type Fonts struct {
	Source string `toml:"source" json:"source"`
	Core   string `toml:"core,omitempty" json:"core"`
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
	if p.Fonts.Core != CoreMicrosoft {
		p.Fonts.Core = CoreLiberation
	}
	if p.UI.Language != LangZH && p.UI.Language != LangEN {
		p.UI.Language = ""
	}
	if p.UI.Theme != ThemeLight && p.UI.Theme != ThemeDark {
		p.UI.Theme = ""
	}
	return p
}

func Save(p Prefs) error {
	switch p.Fonts.Source {
	case HostFirst, LibraryOnly:
	default:
		return fmt.Errorf("font source %q (want %s or %s)", p.Fonts.Source, HostFirst, LibraryOnly)
	}
	switch p.Fonts.Core {
	case "", CoreLiberation, CoreMicrosoft:
	default:
		return fmt.Errorf("core fonts %q (want %s or %s)", p.Fonts.Core, CoreLiberation, CoreMicrosoft)
	}
	switch p.UI.Language {
	case "", LangZH, LangEN:
	default:
		return fmt.Errorf("language %q (want %s or %s, or empty for the system's)", p.UI.Language, LangZH, LangEN)
	}
	switch p.UI.Theme {
	case "", ThemeLight, ThemeDark:
	default:
		return fmt.Errorf("theme %q (want %s or %s, or empty for the system's)", p.UI.Theme, ThemeLight, ThemeDark)
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
