package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditorKeepsComments(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cellar.toml")
	os.WriteFile(p, []byte(`# my notes
runner = "system"  # pinned
profiles = ["zh-cn"]

[dll_overrides]
# TAisafe kills the process on start
taisafe = "disabled"

[[apps]]
id = "a"
exe = 'C:\a.exe'
`), 0o644)
	e, err := Edit(p)
	if err != nil {
		t.Fatal(err)
	}
	e.Set("virtual_desktop", Literal("1920x1080"))
	e.Set("runner", Literal("wine-x"))
	e.SetTable("dll_overrides", map[string]string{"taisafe": "disabled", "mfc42": "native,builtin"})
	e.Set("dependencies", Literal([]string{"vcrun2022"}))
	e.AddApp(App{ID: "b", Name: "B", Exe: `C:\Program Files\B\b.exe`, Desktop: true})
	if err := e.Save(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	s := string(b)
	for _, want := range []string{"# my notes", `runner = "wine-x"  # pinned`, "# TAisafe kills", `mfc42 = "native,builtin"`,
		`virtual_desktop = "1920x1080"`, `dependencies = ["vcrun2022"]`, `exe = 'C:\Program Files\B\b.exe'`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	c, err := Load(p, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.VirtualDesktop != "1920x1080" || len(c.Apps) != 2 || c.DllOverrides["mfc42"] != "native,builtin" {
		t.Errorf("bad reload: %+v", c)
	}

	e, _ = Edit(p)
	e.SetTable("dll_overrides", nil)
	e.Set("virtual_desktop", "")
	if !e.RemoveApp("a") {
		t.Fatal("RemoveApp a")
	}
	if err := e.Save(); err != nil {
		t.Fatal(err)
	}
	c, _ = Load(p, "")
	if c.VirtualDesktop != "" || len(c.Apps) != 1 || c.Apps[0].ID != "b" || c.DllOverrides["taisafe"] != "" {
		t.Errorf("bad reload after removal: %+v", c)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cellar.toml")
	os.WriteFile(p, []byte(Template("x", "system", []string{"zh-cn"})), 0o644)
	st, err := ReadSettings(p)
	if err != nil {
		t.Fatal(err)
	}
	st.VirtualDesktop, st.DPI, st.WindowsVersion = "1920x1080", 144, "win7"
	st.DllOverrides["mfc42"] = "native,builtin"
	st.Dependencies = []string{"vcrun2022", "riched20"}
	if err := WriteSettings(p, st); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.VirtualDesktop != "1920x1080" || c.DPI != 144 || c.WindowsVersion != "win7" || c.DllOverrides["mfc42"] != "native,builtin" ||
		len(c.Dependencies) != 2 || len(c.Profiles) != 1 {
		t.Fatalf("not applied: %+v", c)
	}
	// Base profile's override survives next to the user's.
	if c.DllOverrides["winemenubuilder.exe"] != "disabled" {
		t.Fatal("lost base dll override")
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "# [[apps]]") {
		t.Fatal("template comments lost")
	}
	// Clearing.
	st, _ = ReadSettings(p)
	st.VirtualDesktop, st.DPI, st.WindowsVersion, st.Dependencies = "", 0, "", nil
	st.DllOverrides = map[string]string{}
	st.Profiles = []string{}
	if err := WriteSettings(p, st); err != nil {
		t.Fatal(err)
	}
	c, _ = Load(p, "")
	if c.VirtualDesktop != "" || c.DPI != 0 || c.WindowsVersion != "win10" || len(c.Dependencies) != 0 || len(c.Profiles) != 0 || c.DllOverrides["mfc42"] != "" {
		t.Fatalf("not cleared: %+v", c)
	}
}
