package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func load(t *testing.T, text string) *Config {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cellar.toml")
	os.WriteFile(p, []byte(text), 0o644)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

const substKey = `HKLM\Software\Microsoft\Windows NT\CurrentVersion\FontSubstitutes`
const linkKey = `HKLM\Software\Microsoft\Windows NT\CurrentVersion\FontLink\SystemLink`

func TestLocaleAndFontSet(t *testing.T) {
	c := load(t, "locale = \"zh_CN\"\nfont_sets = [\"cjk\"]\n")
	if c.Locale != "zh_CN" || !slices.Equal(c.FontSets, []string{"cjk"}) || c.Env["LC_ALL"] != "zh_CN.UTF-8" {
		t.Fatalf("locale %q sets %v env %v", c.Locale, c.FontSets, c.Env)
	}
	// Each region's Windows names go to that region of the one collection.
	sub := c.Registry[substKey]
	if sub["SimSun"] != "{family:@cjk.SC}" || sub["MS Gothic"] != "{family:@cjk.JP}" || sub["Gulim"] != "{family:@cjk.KR}" ||
		sub["KaiTi"] != "{family:@cjk.serif-SC|@cjk.SC}" {
		t.Errorf("subst %v", sub)
	}
	// cjk alone: no English substitution, no scripts.
	if !slices.Contains(c.Fonts.Link, "@cjk.serif-SC") || slices.Contains(c.Fonts.Link, "@scripts.NotoSansThai") || sub["Arial"] != nil {
		t.Errorf("fonts %v", c.Fonts.Link)
	}
}

func TestFallbackFollowsLocale(t *testing.T) {
	first := func(text string) string {
		c := load(t, text)
		return c.Registry[linkKey][LinkFont].(map[string]any)["value"].([]any)[0].(string)
	}
	for locale, region := range map[string]string{"zh_CN": "SC", "zh_TW": "TC", "zh_HK": "HK", "ja_JP": "JP", "ko_KR": "KR", "en_US": "SC"} {
		if got := first("locale = \"" + locale + "\"\nfont_sets = [\"core\", \"cjk\"]\n"); got != "{font:@cjk."+region+"},{family:@cjk."+region+"}" {
			t.Errorf("%s: %s", locale, got)
		}
	}
	// English alone writes no fallback chain.
	if c := load(t, "font_sets = [\"core\"]\n"); c.Registry[linkKey] != nil {
		t.Errorf("core only: %v", c.Registry[linkKey])
	}
}

func TestAllIncludesTheOthers(t *testing.T) {
	c := load(t, "locale = \"ja_JP\"\nfont_sets = [\"all\"]\n")
	// One entry: every font falls back to it.
	if len(c.Registry[linkKey]) != 1 {
		t.Errorf("SystemLink entries %v", c.Registry[linkKey])
	}
	link := c.Registry[linkKey][LinkFont].(map[string]any)["value"].([]any)
	// The locale's CJK region, the other regions, then the scripts, which
	// name only their file.
	if link[0] != "{font:@cjk.JP},{family:@cjk.JP}" || link[5] != "{font:@scripts.NotoSans}" {
		t.Errorf("SystemLink %v", link[:6])
	}
	if c.Registry[substKey]["Arial"] != "{family:@core.sans}" || c.Registry[substKey]["MS Gothic"] != "{family:@cjk.JP}" {
		t.Errorf("subst %v", c.Registry[substKey])
	}
	if !slices.Equal(DefaultFontSets("zh_TW"), []string{"core", "cjk"}) || !slices.Equal(DefaultFontSets("de_DE"), []string{"core"}) {
		t.Error("defaults")
	}
}

func TestFileOverridesLocaleEnv(t *testing.T) {
	// The file's env wins over what its locale sets.
	c := load(t, `locale = "ja_JP"
font_sets = ["core", "cjk"]
[env]
LC_ALL = "C.UTF-8"
`)
	if c.Locale != "ja_JP" || c.Env["LANG"] != "ja_JP.UTF-8" || c.Env["LC_ALL"] != "C.UTF-8" {
		t.Fatalf("locale %q env %v", c.Locale, c.Env)
	}
	if c.Registry[substKey]["Arial"] != "{family:@core.sans}" {
		t.Errorf("subst %v", c.Registry[substKey])
	}
	// No winetricks fallback: Liberation comes from the host or the library.
	if slices.Contains(c.Dependencies, "corefonts") || !slices.Contains(c.Fonts.Link, "@core.mono") {
		t.Errorf("deps %v fonts %v", c.Dependencies, c.Fonts.Link)
	}
}

func TestPacks(t *testing.T) {
	// The four regions share one file: the pack is that file once.
	if f := PackFaces("cjk"); len(f) != 1 || f[0].File != "NotoSansCJK-Regular.ttc" {
		t.Errorf("cjk %v", f)
	}
	if f := PackFaces("scripts"); len(f) < 100 || f[0].Family != "Noto Sans" {
		t.Errorf("scripts %d first %v", len(f), f[0])
	}
}

func TestNoLocaleNoFonts(t *testing.T) {
	c := load(t, `runner = "system"`)
	if c.Locale != "" || len(c.FontSets) != 0 || c.Env["LANG"] != "" || c.Registry[substKey] != nil {
		t.Fatalf("got %+v", c)
	}
}

func TestExamplesLoad(t *testing.T) {
	files, _ := filepath.Glob("../../packaging/examples/*/cellar.toml")
	if len(files) == 0 {
		t.Fatal("no examples")
	}
	for _, f := range files {
		if _, err := Load(f); err != nil {
			t.Error(err)
		}
	}
}
