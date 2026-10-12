package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func load(t *testing.T, text string) *Config {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cellar.toml")
	os.WriteFile(p, []byte(text), 0o644)
	c, err := Load(p, "")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

const substKey = `HKLM\Software\Microsoft\Windows NT\CurrentVersion\FontSubstitutes`
const linkKey = `HKLM\Software\Microsoft\Windows NT\CurrentVersion\FontLink\SystemLink`

func TestZhCNProfileIsLocaleAndFontSet(t *testing.T) {
	c := load(t, `profiles = ["zh-cn"]`)
	if c.Locale != "zh_CN" || !slices.Equal(c.FontSets, []string{"zh-hans"}) {
		t.Fatalf("locale %q sets %v", c.Locale, c.FontSets)
	}
	if c.Env["LC_ALL"] != "zh_CN.UTF-8" {
		t.Errorf("env %v", c.Env)
	}
	if c.Registry[substKey]["SimSun"] != "{family:@zh-hans.sans}" || !slices.Contains(c.Fonts.Link, "@zh-hans.serif") {
		t.Errorf("fonts %v subst %v", c.Fonts.Link, c.Registry[substKey])
	}
}

func TestFileOverridesProfile(t *testing.T) {
	// The file's own locale and sets win, its env still wins over the locale's.
	c := load(t, `profiles = ["zh-cn"]
locale = "ja_JP"
font_sets = ["zh-hans", "ja", "core"]
[env]
LC_ALL = "C.UTF-8"
`)
	if c.Locale != "ja_JP" || c.Env["LANG"] != "ja_JP.UTF-8" || c.Env["LC_ALL"] != "C.UTF-8" {
		t.Fatalf("locale %q env %v", c.Locale, c.Env)
	}
	// The locale's set leads the UI fallbacks.
	if !slices.Equal(c.FontSets, []string{"ja", "zh-hans", "core"}) {
		t.Errorf("sets %v", c.FontSets)
	}
	tahoma := c.Registry[linkKey]["Tahoma"].(map[string]any)["value"].([]any)
	if !strings.Contains(tahoma[0].(string), "@ja.sans") || len(tahoma) != 2 {
		t.Errorf("SystemLink Tahoma %v", tahoma)
	}
	if c.Registry[substKey]["MS Gothic"] != "{family:@ja.sans}" || c.Registry[substKey]["Arial"] != "{family:@core.sans}" {
		t.Errorf("subst %v", c.Registry[substKey])
	}
	// corefonts is reconcile's call: only when no Liberation is installed.
	if slices.Contains(c.Dependencies, "corefonts") || !slices.Contains(c.Fonts.Link, "@core.mono") {
		t.Errorf("deps %v fonts %v", c.Dependencies, c.Fonts.Link)
	}
}

func TestNoLocaleNoFonts(t *testing.T) {
	c := load(t, `profiles = []`)
	if c.Locale != "" || len(c.FontSets) != 0 || c.Env["LANG"] != "" || c.Registry[substKey] != nil {
		t.Fatalf("got %+v", c)
	}
}

func TestSettingsMigrateProfiles(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cellar.toml")
	os.WriteFile(p, []byte(Template("x", "system", []string{"zh-cn"}, "", nil)), 0o644)
	st, err := ReadSettings(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Locale != "zh_CN" || !slices.Equal(st.FontSets, []string{"zh-hans"}) {
		t.Fatalf("read %q %v", st.Locale, st.FontSets)
	}
	// Turning the region back to the host's must not leave zh-cn behind.
	st.Locale = ""
	if err := WriteSettings(p, st); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Locale != "" || len(c.Profiles) != 0 || !slices.Equal(c.FontSets, []string{"zh-hans"}) {
		t.Errorf("locale %q profiles %v sets %v", c.Locale, c.Profiles, c.FontSets)
	}
}
