package config

import (
	"os"

	"github.com/pelletier/go-toml/v2"
)

// Settings are the knobs the GUI's settings dialog edits. They are read
// from the file alone (not merged with profiles), so writing them back
// records only what the user set.
type Settings struct {
	Runner         string            `json:"runner"`
	Profiles       []string          `json:"profiles"`
	WindowsVersion string            `json:"windows_version"`
	VirtualDesktop string            `json:"virtual_desktop"`
	DPI            int               `json:"dpi"`
	DllOverrides   map[string]string `json:"dll_overrides"`
	Dependencies   []string          `json:"dependencies"`
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
	st := Settings{Runner: c.Runner, Profiles: c.Profiles, WindowsVersion: c.WindowsVersion,
		VirtualDesktop: c.VirtualDesktop, DPI: c.DPI, DllOverrides: c.DllOverrides, Dependencies: c.Dependencies}
	if st.Profiles == nil {
		st.Profiles = []string{}
	}
	if st.DllOverrides == nil {
		st.DllOverrides = map[string]string{}
	}
	if st.Dependencies == nil {
		st.Dependencies = []string{}
	}
	return st, nil
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
	opt("profiles", st.Profiles, false)
	opt("windows_version", st.WindowsVersion, st.WindowsVersion == "")
	opt("virtual_desktop", st.VirtualDesktop, st.VirtualDesktop == "")
	opt("dpi", st.DPI, st.DPI == 0)
	opt("dependencies", st.Dependencies, len(st.Dependencies) == 0)
	e.SetTable("dll_overrides", st.DllOverrides)
	return e.Save()
}
