package store

import (
	"os"
	"testing"
)

func TestRevertConfig(t *testing.T) {
	t.Setenv("CELLAR_HOME", t.TempDir())
	p, err := Create("p", "system", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Never applied: nothing to go back to.
	if p.ConfigEdited() || p.RevertConfig() == nil {
		t.Fatal("revert without an applied copy")
	}
	applied, _ := os.ReadFile(p.Config)
	os.WriteFile(p.AppliedConfig(), applied, 0o644)
	if p.ConfigEdited() {
		t.Fatal("edited right after apply")
	}
	os.WriteFile(p.Config, append(applied, "dpi = 144\n"...), 0o644)
	if !p.ConfigEdited() {
		t.Fatal("edit not seen")
	}
	if err := p.RevertConfig(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p.Config); string(b) != string(applied) || p.ConfigEdited() {
		t.Errorf("not reverted:\n%s", b)
	}
}

func TestPackagedOneFile(t *testing.T) {
	dir := t.TempDir()
	PackagedConfig = dir + "/package/cellar.toml"
	t.Cleanup(func() { PackagedConfig = "/app/share/cellar/cellar.toml" })
	os.MkdirAll(dir+"/package", 0o755)
	t.Setenv("CELLAR_HOME", dir+"/data")
	t.Setenv("FLATPAK_ID", "org.example.App")
	write := func(p, s string) { os.WriteFile(p, []byte(s), 0o644) }

	// First launch: the package's config becomes the user's.
	write(PackagedConfig, "font_sets = [\"cjk\"]\n\n[install]\nsource = \"app-1.exe\"\nmethod = \"run\"\n")
	p, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p.Config); string(b) != "font_sets = [\"cjk\"]\n\n[install]\nsource = \"app-1.exe\"\nmethod = \"run\"\n" {
		t.Fatalf("first copy:\n%s", b)
	}
	// The user changes the fonts; an update brings a new installer.
	write(p.Config, "font_sets = [\"core\", \"cjk\"]\n\n[install]\nsource = \"app-1.exe\"\nmethod = \"run\"\n")
	write(PackagedConfig, "font_sets = [\"cjk\"]\n\n[install]\nsource = \"app-2.exe\"\nmethod = \"run\"\n")
	if p, err = Open(""); err != nil || len(p.Notes) != 0 {
		t.Fatal(err, p.Notes)
	}
	c, err := p.Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Install.Source != "app-2.exe" || len(c.FontSets) != 2 {
		t.Errorf("merged: install %+v sets %v", c.Install, c.FontSets)
	}
	// Nothing new: nothing touched.
	before, _ := os.ReadFile(p.Config)
	Open("")
	if after, _ := os.ReadFile(p.Config); string(after) != string(before) {
		t.Error("rewritten without an update")
	}
}
