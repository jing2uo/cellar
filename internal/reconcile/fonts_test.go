package reconcile

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jing2uo/cellar/internal/config"
	"github.com/jing2uo/cellar/internal/fonts"
)

// fakeFcList puts an fc-list on PATH that lists the given families (family
// -> file), as `fc-list -f '%{family}\t%{style}\t%{file}\n'` does.
func fakeFcList(t *testing.T, families map[string]string) {
	t.Helper()
	dir := t.TempDir()
	var b strings.Builder
	b.WriteString("#!/bin/sh\ncat <<'E'\n")
	for fam, file := range families {
		b.WriteString(fam + "\tRegular\t" + file + "\n")
	}
	b.WriteString("E\n")
	os.WriteFile(filepath.Join(dir, "fc-list"), []byte(b.String()), 0o755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	fonts.Rescan()
}

// home is a cellar home with the font source policy set ("" leaves the
// default, host-first).
func home(t *testing.T, policy string) string {
	t.Helper()
	h := t.TempDir()
	t.Setenv("CELLAR_HOME", h)
	if policy != "" {
		os.WriteFile(filepath.Join(h, "settings.toml"), []byte("[fonts]\nsource = \""+policy+"\"\n"), 0o644)
	}
	return h
}

func loadSets(t *testing.T, sets string) *config.Config {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cellar.toml")
	os.WriteFile(p, []byte("locale = \"zh_CN\"\nfont_sets = ["+sets+"]\n"), 0o644)
	c, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

const substK = `HKEY_LOCAL_MACHINE\Software\Microsoft\Windows NT\CurrentVersion\FontSubstitutes`
const linkK = `HKEY_LOCAL_MACHINE\Software\Microsoft\Windows NT\CurrentVersion\FontLink\SystemLink`

const otc = "/f/NotoSansCJK-Regular.ttc"

func TestAllFromHost(t *testing.T) {
	home(t, "")
	fakeFcList(t, map[string]string{"Noto Sans CJK SC": otc, "Noto Sans CJK JP": otc, "Noto Sans CJK TC": otc,
		"Noto Sans CJK KR": otc, "Noto Sans CJK HK": otc, "Noto Sans Thai": "/f/NotoSansThai-Regular.ttf"})
	d, warns, err := want(loadSets(t, `"all"`))
	if err != nil {
		t.Fatal(err)
	}
	sub := d.reg[normKey(substK)]
	// No serif here: KaiTi takes the sans.
	if sub["SimSun"] != "sz:Noto Sans CJK SC" || sub["MS Gothic"] != "sz:Noto Sans CJK JP" || sub["KaiTi"] != "sz:Noto Sans CJK SC" {
		t.Errorf("subst %v", sub)
	}
	// The fallback chain is what is here: the CJK regions, Thai, nothing more.
	link := string(d.reg[normKey(linkK)]["Tahoma"])
	if !strings.HasPrefix(link, `multi_sz:["NotoSansCJK-Regular.ttc,Noto Sans CJK SC"`) ||
		!strings.Contains(link, "NotoSansThai-Regular.ttf,Noto Sans Thai") || strings.Contains(link, "Arabic") {
		t.Errorf("SystemLink %v", link)
	}
	// all brings in core, which, missing Liberation here, installs corefonts.
	if len(d.downloads) != 0 || len(warns) != 0 || !slices.Equal(d.deps, []string{"corefonts"}) {
		t.Errorf("downloads %v warns %v deps %v", d.downloads, warns, d.deps)
	}
}

func TestCJKMissing(t *testing.T) {
	// host-first: nothing downloaded, one warning saying how to get it.
	home(t, "")
	fakeFcList(t, map[string]string{"WenQuanYi Zen Hei": "/f/wqy-zenhei.ttc"})
	d, warns, _ := want(loadSets(t, `"cjk"`))
	if len(d.downloads) != 0 || d.reg[normKey(substK)]["SimSun"] != "" || len(warns) != 1 || !strings.Contains(warns[0], "cellar fonts get cjk") {
		t.Errorf("downloads %v subst %v warns %v", d.downloads, d.reg[normKey(substK)], warns)
	}

	// auto: the collection is downloaded into the library, once for all regions.
	h := home(t, "auto")
	d, warns, _ = want(loadSets(t, `"cjk"`))
	lib := filepath.Join(h, "fonts", "NotoSansCJK-Regular.ttc")
	if len(d.downloads) != 1 || !strings.HasSuffix(d.downloads[lib].URL, "/Sans/OTC/NotoSansCJK-Regular.ttc") || len(warns) != 0 {
		t.Errorf("downloads %v warns %v", d.downloads, warns)
	}
	if d.reg[normKey(substK)]["SimSun"] != "sz:Noto Sans CJK SC" || d.fonts["NotoSansCJK-Regular.ttc"] != lib {
		t.Errorf("subst %v fonts %v", d.reg[normKey(substK)], d.fonts)
	}
	// Once there, it is used as is.
	os.MkdirAll(filepath.Dir(lib), 0o755)
	os.WriteFile(lib, []byte("otc"), 0o644)
	if d, _, _ = want(loadSets(t, `"cjk"`)); len(d.downloads) != 0 || d.fonts["NotoSansCJK-Regular.ttc"] != lib {
		t.Errorf("downloads %v fonts %v", d.downloads, d.fonts)
	}
}

func TestLibraryOnly(t *testing.T) {
	// The host's Noto CJK is not used; the library's is.
	h := home(t, "library-only")
	fakeFcList(t, map[string]string{"Noto Sans CJK SC": otc})
	if d, warns, _ := want(loadSets(t, `"cjk"`)); len(d.fonts) != 0 || len(warns) != 1 {
		t.Errorf("fonts %v warns %v", d.fonts, warns)
	}
	lib := filepath.Join(h, "fonts", "NotoSansCJK-Regular.ttc")
	os.MkdirAll(filepath.Dir(lib), 0o755)
	os.WriteFile(lib, []byte("otc"), 0o644)
	if d, warns, _ := want(loadSets(t, `"cjk"`)); d.fonts["NotoSansCJK-Regular.ttc"] != lib || len(warns) != 0 {
		t.Errorf("fonts %v warns %v", d.fonts, warns)
	}
}

func TestCoreFontsWithoutLiberation(t *testing.T) {
	home(t, "")
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

func TestPackNeeds(t *testing.T) {
	// What a pack still needs: what the host lacks, or, library-only,
	// everything not downloaded.
	missing := func(id string) (int, int) {
		for _, p := range fonts.Packs() {
			if p.ID == id {
				return p.Missing, p.Files
			}
		}
		t.Fatal("no pack", id)
		return 0, 0
	}
	home(t, "")
	fakeFcList(t, map[string]string{"Noto Sans CJK SC": otc, "Noto Sans Thai": "/f/NotoSansThai-Regular.ttf"})
	if m, _ := missing("cjk"); m != 0 {
		t.Errorf("cjk needs %d with the host's", m)
	}
	if m, n := missing("scripts"); m != n-1 {
		t.Errorf("scripts needs %d of %d, the host having Thai", m, n)
	}
	home(t, "library-only")
	if m, _ := missing("cjk"); m != 1 {
		t.Errorf("library-only: cjk needs %d", m)
	}
}
