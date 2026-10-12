package appmgr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jing2uo/cellar/internal/config"
	"github.com/jing2uo/cellar/internal/desktop"
	"github.com/jing2uo/cellar/internal/store"
)

func TestRefreshDesktop(t *testing.T) {
	t.Setenv("CELLAR_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("FLATPAK_ID", "")
	p, err := store.Create("p", "system", nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	add := func(a config.App) {
		e, _ := config.Edit(p.Config)
		e.AddApp(a)
		if err := e.Save(); err != nil {
			t.Fatal(err)
		}
	}
	add(config.App{ID: "a", Exe: `C:\a.exe`, Icon: "icons/a.png"})
	add(config.App{ID: "b", Exe: `C:\b.exe`})
	add(config.App{ID: "c", Exe: `C:\c.exe`, Desktop: true})
	c, _ := p.Load()
	for i := range c.Apps[:2] {
		desktop.Install("/bin/cellar", p, &c.Apps[i])
	}

	// Edit: a's icon changes, b is dropped.
	b, _ := os.ReadFile(p.Config)
	text := strings.Replace(string(b), "icons/a.png", "icons/a2.png", 1)
	text = strings.Replace(text, "[[apps]]\nid = \"b\"\nexe = 'C:\\b.exe'\n", "", 1)
	os.WriteFile(p.Config, []byte(text), 0o644)
	if err := RefreshDesktop("/bin/cellar", p); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(desktop.Dir(), desktop.FileName("p", "a")))
	if !strings.Contains(string(got), "icons/a2.png") {
		t.Errorf("a not refreshed:\n%s", got)
	}
	if ids := desktop.Installed(p); len(ids) != 1 || ids[0] != "a" {
		t.Errorf("launchers %v (b removed, c not added)", ids)
	}
}
