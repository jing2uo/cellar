package reconcile

import (
	"encoding/json"
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
	link := string(d.reg[normKey(linkK)][config.LinkFont])
	if !strings.HasPrefix(link, `multi_sz:["NotoSansCJK-Regular.ttc,Noto Sans CJK SC"`) ||
		!strings.Contains(link, `"NotoSansThai-Regular.ttf"`) || strings.Contains(link, "Arabic") {
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

func TestLongChainFitsWine(t *testing.T) {
	// auto: the whole chain resolves, far more than one entry holds.
	home(t, "auto")
	fakeFcList(t, nil)
	d, warns, _ := want(loadSets(t, `"all"`))
	sl := d.reg[normKey(linkK)]
	if len(sl) < 3 || len(warns) != 0 {
		t.Fatalf("entries %d warns %v", len(sl), warns)
	}
	if !strings.HasPrefix(string(sl[config.LinkFont]), `multi_sz:["NotoSansCJK-Regular.ttc,Noto Sans CJK SC"`) || sl["Tahoma"] == "" {
		t.Errorf("SystemLink %v", sl)
	}
	all := map[string]bool{}
	for name, v := range sl {
		var entries []string
		json.Unmarshal([]byte(strings.TrimPrefix(string(v), "multi_sz:")), &entries)
		if linkSize(name, entries) > linkRoom {
			t.Errorf("%s: %d bytes", name, linkSize(name, entries))
		}
		for _, e := range entries {
			all[e] = true
		}
	}
	if !all["NotoSansZanabazarSquare-Regular.ttf"] || !all["NotoSansThai-Regular.ttf"] || !all["NotoSansCJK-Regular.ttc,Noto Sans CJK KR"] {
		t.Errorf("chain lost fonts: %d", len(all))
	}
}

func TestLinkDrift(t *testing.T) {
	pfx := t.TempDir()
	os.WriteFile(filepath.Join(pfx, "system.reg"), []byte(`WINE REGISTRY Version 2

[Software\\Microsoft\\Windows NT\\CurrentVersion\\FontLink\\SystemLink] 1791844608
#time=1dd5a9a2b0c88ee
"Microsoft Sans Serif"=str(7):"NotoSansCJK-Regular.ttc,Noto Sans CJK SC\0NotoSansThai-Regular.ttf\0"
"Tahoma"=str(7):"SIMSUN.TTC,SimSun\0MSGOTHIC.TTC,MS UI Gothic\0"

[Software\\Other] 1791844608
"Segoe UI"=str(7):"NotoSansThai-Regular.ttf\0"
`), 0o644)
	applied := map[string]string{
		"Microsoft Sans Serif": `multi_sz:["NotoSansCJK-Regular.ttc,Noto Sans CJK SC","NotoSansThai-Regular.ttf"]`,
		"Tahoma":               `multi_sz:["NotoSansLao-Regular.ttf"]`,  // Wine put its own back
		"Segoe UI":             `multi_sz:["NotoSansThai-Regular.ttf"]`, // gone from the key
	}
	if d := linkDrift(pfx, applied); d["Microsoft Sans Serif"] || !d["Tahoma"] || !d["Segoe UI"] {
		t.Errorf("drift %v", d)
	}
	if s := unescapeReg(`a\\b\"c\x4e2d\0`); s != "a\\b\"c中\x00" {
		t.Errorf("unescape %q", s)
	}
}
