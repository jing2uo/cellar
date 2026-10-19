package importer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImportCellar(t *testing.T) {
	t.Setenv("CELLAR_HOME", t.TempDir())
	other := t.TempDir()
	src := filepath.Join(other, "prefixes", "wxwork")
	os.MkdirAll(filepath.Join(src, "pfx", "drive_c"), 0o755)
	os.MkdirAll(filepath.Join(src, "runs"), 0o755)
	os.Symlink("../drive_c", filepath.Join(src, "pfx", "c-link"))
	os.WriteFile(filepath.Join(src, "cellar.toml"), []byte("runner = \"no-such-runner\"\n\n[[apps]]\nid = \"wx\"\nname = \"WeCom\"\nexe = 'C:\\wx.exe'\n"), 0o644)
	os.WriteFile(filepath.Join(src, "state.json"), []byte("{}"), 0o644)

	// A cellar home, its prefixes directory, or one prefix.
	for _, d := range []string{other, filepath.Dir(src), src} {
		if got := ScanCellar(d); len(got) != 1 || got[0].Prefix != "wxwork" || got[0].Apps[0] != "WeCom" {
			t.Errorf("%s: %+v", d, got)
		}
	}
	res, err := ImportCellar(src, "")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(res.Config)
	if res.Prefix != "wxwork" || len(res.Apps) != 1 || len(res.Notes) != 1 {
		t.Errorf("%+v", res)
	}
	for _, f := range []string{"cellar.toml", "state.json", "pfx/drive_c"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Error(err)
		}
	}
	if l, _ := os.Readlink(filepath.Join(dir, "pfx", "c-link")); l != "../drive_c" {
		t.Errorf("symlink: %q", l)
	}
	if _, err := os.Stat(filepath.Join(dir, "runs")); err == nil {
		t.Error("run logs came along")
	}
	if _, err := ImportCellar(src, ""); err == nil {
		t.Error("imported twice")
	}
}
