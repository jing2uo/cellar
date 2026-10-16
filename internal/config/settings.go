package config

import (
	"os"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Settings are the knobs the GUI's settings dialog edits. They are read
// from the file alone (not merged with the defaults), so writing them back
// records only what the user set.
type Settings struct {
	Runner         string   `json:"runner"`
	Locale         string   `json:"locale"`
	FontSets       []string `json:"font_sets"`
	WindowsVersion string   `json:"windows_version"`
	VirtualDesktop string   `json:"virtual_desktop"`
	DPI            int      `json:"dpi"`
	CoreFonts      string   `json:"core_fonts"` // [fonts] core; "" follows cellar's settings
}

func ReadSettings(path string) (Settings, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Settings{}, err
	}
	var c Config
	if err := toml.Unmarshal(b, &c); err != nil {
		return Settings{}, err
	}
	st := Settings{Runner: c.Runner, WindowsVersion: c.WindowsVersion,
		VirtualDesktop: c.VirtualDesktop, DPI: c.DPI, CoreFonts: c.Fonts.Core}
	file, err := parse(b, path)
	if err != nil {
		return Settings{}, err
	}
	st.Locale, st.FontSets = localeOf([]map[string]any{file})
	if st.FontSets == nil {
		st.FontSets = []string{}
	}
	return st, nil
}

// ReadDependencies is the file's own dependency list.
func ReadDependencies(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := toml.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	if c.Dependencies == nil {
		return []string{}, nil
	}
	return c.Dependencies, nil
}

// WriteDependencies sets the file's dependency list; empty removes it.
func WriteDependencies(path string, deps []string) error {
	e, err := Edit(path)
	if err != nil {
		return err
	}
	if len(deps) == 0 {
		e.Set("dependencies", "")
	} else {
		e.Set("dependencies", Literal(deps))
	}
	return e.Save()
}

// WriteSettings edits the file in place; empty values remove their key.
func WriteSettings(path string, st Settings) error {
	e, err := Edit(path)
	if err != nil {
		return err
	}
	opt := func(key string, v any, empty bool) {
		if empty {
			e.Set(key, "")
		} else {
			e.Set(key, Literal(v))
		}
	}
	opt("runner", st.Runner, st.Runner == "")
	opt("locale", st.Locale, st.Locale == "")
	opt("font_sets", st.FontSets, false)
	opt("windows_version", st.WindowsVersion, st.WindowsVersion == "")
	opt("virtual_desktop", st.VirtualDesktop, st.VirtualDesktop == "")
	opt("dpi", st.DPI, st.DPI == 0)
	core := ""
	if st.CoreFonts != "" {
		core = Literal(st.CoreFonts)
	}
	e.SetIn("fonts", "core", core)
	return e.Save()
}

// WriteCoreFonts sets [fonts] core; empty removes it, following cellar's
// settings.
func WriteCoreFonts(path, core string) error {
	e, err := Edit(path)
	if err != nil {
		return err
	}
	v := ""
	if core != "" {
		v = Literal(core)
	}
	e.SetIn("fonts", "core", v)
	return e.Save()
}

// ReadBlocked lists the programs the file keeps from running: dll overrides
// of an .exe set to disabled, which Wine refuses to start.
func ReadBlocked(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := toml.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	out := []string{}
	for name, mode := range c.DllOverrides {
		if strings.HasSuffix(strings.ToLower(name), ".exe") && (mode == "disabled" || mode == "") {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out, nil
}

// SetBlocked blocks programs by file name (written in lower case; Wine
// ignores case), or lets them run again (names as ReadBlocked gives them).
func SetBlocked(path string, names []string, block bool) error {
	e, err := Edit(path)
	if err != nil {
		return err
	}
	for _, n := range names {
		if block {
			e.SetIn("dll_overrides", strings.ToLower(n), Literal("disabled"))
		} else {
			e.SetIn("dll_overrides", n, "")
		}
	}
	return e.Save()
}
