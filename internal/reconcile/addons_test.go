package reconcile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jing2uo/cellar/internal/config"
	"github.com/jing2uo/cellar/internal/runner"
	"github.com/jing2uo/cellar/internal/store"
)

func TestWantAddons(t *testing.T) {
	a := runner.Addons{Gecko: "2.47.4",
		Gecko64: &runner.Addon{Name: "wine-gecko-2.47.4-x86_64", Dir: "/g64"},
		Gecko32: &runner.Addon{Name: "wine-gecko-2.47.4-x86", Dir: "/g32"},
		Mono:    &runner.Addon{Name: "wine-mono-10.4.1", Dir: "/m"}}
	d := &desired{reg: map[string]map[string]value{}}
	wantAddons(d, config.Addons{Gecko: true}, a)
	if len(d.addons) != 2 || d.addons["wine-gecko-2.47.4-x86"] != "/g32" {
		t.Errorf("links %v", d.addons)
	}
	if v := d.reg[geckoKey+"2.47.4"]["GeckoPath"]; v != `sz:C:\windows\cellar-addons\wine-gecko-2.47.4-x86_64` {
		t.Errorf("GeckoPath %q", v)
	}
	if g, m := addonsPending(d.reg); !g || m {
		t.Errorf("pending gecko %v mono %v", g, m)
	}
	d = &desired{reg: map[string]map[string]value{}}
	wantAddons(d, config.Addons{}, a)
	if len(d.addons)+len(d.reg) != 0 {
		t.Errorf("off: %v %v", d.addons, d.reg)
	}
}

func TestLinkAddons(t *testing.T) {
	pfx := t.TempDir()
	dir := filepath.Join(pfx, "drive_c/windows/cellar-addons")
	if err := linkAddons(pfx, map[string]string{"wine-mono-9": "/old", "wine-gecko-1": "/g"}); err != nil {
		t.Fatal(err)
	}
	if err := linkAddons(pfx, map[string]string{"wine-mono-10": "/new", "wine-gecko-1": "/g"}); err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 2 {
		t.Errorf("links %v", ents)
	}
	if l, _ := os.Readlink(filepath.Join(dir, "wine-mono-10")); l != "/new" {
		t.Errorf("mono link %q", l)
	}
	linkAddons(pfx, nil)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("dir left behind")
	}
}

func TestAddonsTurnedOn(t *testing.T) {
	pfx := t.TempDir()
	os.WriteFile(filepath.Join(pfx, "system.reg"), nil, 0o644)
	plan := func(applied []string, c config.Addons) []Change {
		pl := &Plan{Wine: "wine-11.0", p: &store.Prefix{Pfx: pfx},
			c:    &config.Config{Addons: c},
			st:   &store.State{Wine: "wine-11.0", Addons: applied},
			want: &desired{reg: map[string]map[string]value{}}}
		pl.diff(false)
		return pl.Changes
	}
	if ch := plan(nil, config.Addons{Gecko: true, Mono: true}); len(ch) != 1 || ch[0].Op != "addons" || ch[0].Value != "gecko mono" {
		t.Errorf("turned on: %v", ch)
	}
	if ch := plan([]string{"gecko"}, config.Addons{Gecko: true}); len(ch) != 0 {
		t.Errorf("already on: %v", ch)
	}
	if ch := plan([]string{"gecko", "mono"}, config.Addons{}); len(ch) != 0 {
		t.Errorf("turned off: %v", ch)
	}
}
