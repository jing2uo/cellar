package importer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanRootOrBottle(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"zj", "wxwork"} {
		os.MkdirAll(filepath.Join(root, n), 0o755)
		os.WriteFile(filepath.Join(root, n, "bottle.yml"), []byte("Name: "+n+"\nRunner: sys-wine-11.0\n"), 0o644)
	}
	os.MkdirAll(filepath.Join(root, "not-a-bottle"), 0o755)
	if got := Scan(root); len(got) != 2 || got[0].Name != "wxwork" || got[0].Runner != "sys-wine-11.0" {
		t.Errorf("root: %+v", got)
	}
	if got := Scan(filepath.Join(root, "zj")); len(got) != 1 || got[0].Prefix != "zj" {
		t.Errorf("bottle: %+v", got)
	}
}

func TestWinPathFromPortal(t *testing.T) {
	// Bottles wrote its own path; the bottle is read through the portal.
	p := "/home/u/.var/app/com.usebottles.bottles/data/bottles/bottles/zj/drive_c/Program Files/zj/zj.exe"
	if got := winPath("/run/user/1000/doc/ab12/zj", p); got != `C:\Program Files\zj\zj.exe` {
		t.Errorf("got %q", got)
	}
}
