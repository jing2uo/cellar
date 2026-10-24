package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestEditorKeepsComments(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cellar.toml")
	os.WriteFile(p, []byte(`# my notes
runner = "system"  # pinned
locale = "zh_CN"

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
	c, err := Load(p)
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
	c, _ = Load(p)
	if c.VirtualDesktop != "" || len(c.Apps) != 1 || c.Apps[0].ID != "b" || c.DllOverrides["taisafe"] != "" {
		t.Errorf("bad reload after removal: %+v", c)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cellar.toml")
	os.WriteFile(p, []byte(Template("x", "system", "zh_CN", []string{"zh-hans"})+"\n[dll_overrides]\ntaisafe = \"disabled\"\n"), 0o644)
	st, err := ReadSettings(p)
	if err != nil {
		t.Fatal(err)
	}
	st.VirtualDesktop, st.DPI, st.WindowsVersion = "1920x1080", 144, "win7"
	if err := WriteDependencies(p, []string{"vcrun2022", "riched20"}); err != nil {
		t.Fatal(err)
	}
	if err := WriteSettings(p, st); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.VirtualDesktop != "1920x1080" || c.DPI != 144 || c.WindowsVersion != "win7" ||
		len(c.Dependencies) != 2 || c.Locale != "zh_CN" {
		t.Fatalf("not applied: %+v", c)
	}
	// DLL overrides are not a setting: the file's own and the defaults'
	// stay as they were.
	if c.DllOverrides["taisafe"] != "disabled" || c.DllOverrides["winemenubuilder.exe"] != "disabled" {
		t.Fatalf("dll overrides %v", c.DllOverrides)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "# [[apps]]") {
		t.Fatal("template comments lost")
	}
	// Clearing.
	st, _ = ReadSettings(p)
	st.VirtualDesktop, st.DPI, st.WindowsVersion = "", 0, ""
	if err := WriteSettings(p, st); err != nil {
		t.Fatal(err)
	}
	c, _ = Load(p)
	if c.VirtualDesktop != "" || c.DPI != 0 || c.WindowsVersion != "win10" {
		t.Fatalf("not cleared: %+v", c)
	}
}

func TestAddAppControlChars(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cellar.toml")
	os.WriteFile(p, []byte("runner = \"system\"\n"), 0o644)
	e, err := Edit(p)
	if err != nil {
		t.Fatal(err)
	}
	e.AddApp(App{ID: "x", Exe: `C:\x.exe`, Args: []string{"a\x00\"b\\"}, Workdir: "C:\\dir\x00"})
	if err := e.Save(); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if a := c.Apps[0]; a.Args[0] != "a\x00\"b\\" || a.Workdir != "C:\\dir\x00" {
		t.Errorf("%q %q", a.Args, a.Workdir)
	}
}

func TestRenameApp(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cellar.toml")
	os.WriteFile(p, []byte("runner = \"system\"\n"), 0o644)
	e, _ := Edit(p)
	e.AddApp(App{ID: "a", Exe: `C:\a.exe`})
	e.AddApp(App{ID: "b", Name: "B", Exe: `C:\b.exe`})
	if !e.RenameApp("a", "Alpha \"1\"") || !e.RenameApp("b", "Beta") || e.RenameApp("c", "x") {
		t.Fatal("rename result")
	}
	if err := e.Save(); err != nil {
		t.Fatal(err)
	}
	c, _ := Load(p)
	if c.Apps[0].Name != "Alpha \"1\"" || c.Apps[0].ID != "a" || c.Apps[1].Name != "Beta" {
		t.Errorf("%+v", c.Apps)
	}
}

func TestDependencies(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cellar.toml")
	os.WriteFile(p, []byte(Template("x", "system", "", nil)), 0o644)
	if d, err := ReadDependencies(p); err != nil || len(d) != 0 {
		t.Fatal(d, err)
	}
	WriteDependencies(p, []string{"vcrun2022", "riched20"})
	// Settings saves leave them alone.
	st, _ := ReadSettings(p)
	st.DPI = 120
	WriteSettings(p, st)
	if d, _ := ReadDependencies(p); !slices.Equal(d, []string{"vcrun2022", "riched20"}) {
		t.Fatal(d)
	}
	WriteDependencies(p, nil)
	if b, _ := os.ReadFile(p); strings.Contains(string(b), "\ndependencies") {
		t.Fatalf("not removed:\n%s", b)
	}
}

// A new prefix may choose its English core fonts; the file still loads.
func TestWriteCoreFonts(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cellar.toml")
	os.WriteFile(p, []byte(Template("x", "system", "zh_CN", []string{"core"})), 0o644)
	if err := WriteCoreFonts(p, "microsoft"); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil || c.Fonts.Core != "microsoft" {
		b, _ := os.ReadFile(p)
		t.Fatalf("%v %q\n%s", err, c.Fonts.Core, b)
	}
	WriteCoreFonts(p, "")
	if st, _ := ReadSettings(p); st.CoreFonts != "" {
		t.Fatal(st.CoreFonts)
	}
}

func TestWriteAddons(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cellar.toml")
	os.WriteFile(p, []byte(Template("x", "system", "zh_CN", []string{"core"})), 0o644)
	if err := WriteAddons(p, true, false); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil || !c.Addons.Gecko || c.Addons.Mono {
		b, _ := os.ReadFile(p)
		t.Fatalf("%v %+v\n%s", err, c.Addons, b)
	}
	st, _ := ReadSettings(p)
	st.Gecko, st.Mono = false, true
	WriteSettings(p, st)
	if st, _ := ReadSettings(p); st.Gecko || !st.Mono {
		t.Fatalf("%+v", st)
	}
	WriteAddons(p, false, false)
	if b, _ := os.ReadFile(p); strings.Contains(string(b), "[addons]") {
		t.Fatalf("empty table kept:\n%s", b)
	}
}
