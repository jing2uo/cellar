package reconcile

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jing2uo/cellar/internal/config"
)

// fakeFcList puts an fc-list on PATH that knows the given families, one
// file each.
func fakeFcList(t *testing.T, families map[string]string) {
	t.Helper()
	dir := t.TempDir()
	var b strings.Builder
	// fc-list -f FORMAT PATTERN
	b.WriteString("#!/bin/sh\ncase \"$1$3\" in\n")
	for fam, file := range families {
		b.WriteString("  '-f" + fam + ":style=Regular'|'-f" + fam + "') echo " + file + " ;;\n")
	}
	b.WriteString("esac\n")
	os.WriteFile(filepath.Join(dir, "fc-list"), []byte(b.String()), 0o755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func loadSets(t *testing.T, sets string) *config.Config {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cellar.toml")
	os.WriteFile(p, []byte("font_sets = ["+sets+"]\n"), 0o644)
	c, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

const substK = `HKEY_LOCAL_MACHINE\Software\Microsoft\Windows NT\CurrentVersion\FontSubstitutes`
const linkK = `HKEY_LOCAL_MACHINE\Software\Microsoft\Windows NT\CurrentVersion\FontLink\SystemLink`

func TestNotoFromHost(t *testing.T) {
	t.Setenv("CELLAR_HOME", t.TempDir())
	fakeFcList(t, map[string]string{"Noto Sans CJK SC": "/f/NotoSansCJK-Regular.ttc", "Noto Serif CJK SC": "/f/NotoSerifCJK-Regular.ttc"})
	d, warns, err := want(loadSets(t, `"zh-hans"`))
	if err != nil {
		t.Fatal(err)
	}
	if d.reg[normKey(substK)]["SimSun"] != "sz:Noto Sans CJK SC" || d.reg[normKey(substK)]["KaiTi"] != "sz:Noto Serif CJK SC" {
		t.Errorf("subst %v", d.reg[normKey(substK)])
	}
	if d.reg[normKey(linkK)]["Tahoma"] != `multi_sz:["NotoSansCJK-Regular.ttc,Noto Sans CJK SC"]` {
		t.Errorf("SystemLink %v", d.reg[normKey(linkK)]["Tahoma"])
	}
	if len(d.downloads) != 0 || len(warns) != 0 {
		t.Errorf("downloads %v warns %v", d.downloads, warns)
	}
}

func TestNotoDownloadedWhenMissing(t *testing.T) {
	// No Noto on the host (WenQuanYi does not count): the region's subset
	// OTF goes into the cache, and a cached one is used as is.
	home := t.TempDir()
	t.Setenv("CELLAR_HOME", home)
	fakeFcList(t, map[string]string{"WenQuanYi Zen Hei": "/f/wqy-zenhei.ttc"})
	d, _, err := want(loadSets(t, `"zh-hans"`))
	if err != nil {
		t.Fatal(err)
	}
	sans := filepath.Join(home, "fonts", "NotoSansSC-Regular.otf")
	if d.reg[normKey(substK)]["SimSun"] != "sz:Noto Sans SC" || d.fonts["NotoSansSC-Regular.otf"] != sans {
		t.Errorf("subst %v fonts %v", d.reg[normKey(substK)], d.fonts)
	}
	if len(d.downloads) != 2 || !strings.HasSuffix(d.downloads[sans].URL, "/Sans/SubsetOTF/SC/NotoSansSC-Regular.otf") {
		t.Errorf("downloads %v", d.downloads)
	}

	os.MkdirAll(filepath.Dir(sans), 0o755)
	os.WriteFile(sans, []byte("otf"), 0o644)
	d, _, _ = want(loadSets(t, `"zh-hans"`))
	if _, ok := d.downloads[sans]; ok || len(d.downloads) != 1 {
		t.Errorf("downloads %v", d.downloads)
	}
}

func TestCoreFontsWithoutLiberation(t *testing.T) {
	fakeFcList(t, nil)
	d, _, err := want(loadSets(t, `"core"`))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(d.deps, []string{"corefonts"}) || d.reg[normKey(substK)] != nil {
		t.Errorf("deps %v subst %v", d.deps, d.reg[normKey(substK)])
	}

	fakeFcList(t, map[string]string{"Liberation Sans": "/f/LiberationSans-Regular.ttf"})
	d, _, err = want(loadSets(t, `"core"`))
	if err != nil {
		t.Fatal(err)
	}
	// Arial maps to Liberation; Times and Courier, missing theirs, keep
	// their own names rather than borrowing a font with other metrics.
	if len(d.deps) != 0 || d.reg[normKey(substK)]["Arial"] != "sz:Liberation Sans" || len(d.reg[normKey(substK)]) != 1 {
		t.Errorf("deps %v subst %v", d.deps, d.reg[normKey(substK)])
	}
}
