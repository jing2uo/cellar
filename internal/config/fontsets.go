package config

import (
	"os"
	"slices"
	"sort"
	"strings"
)

// A FontSet makes the fonts Windows programs ask for by name resolve to
// fonts that are here. Three can be chosen, each touching only its own:
// core (Arial, Times New Roman, Courier New to the metric-compatible
// Liberation fonts), cjk (every region's Windows CJK fonts to that region's
// Noto Sans CJK, all in one collection file, and the CJK UI fallback), and
// all: core and cjk, plus every script's Noto Sans in the UI fallback (the
// hidden scripts set).
type FontSet struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Note     string            `json:"note"`
	Includes []string          `json:"includes,omitempty"` // sets it brings along
	Primary  string            `json:"-"`
	Roles    map[string]Face   `json:"-"`
	Subst    map[string]string `json:"-"` // Windows family -> role; "a|b": the first there is
}

// Face is a role's font: the host's copy (any of Host, by family name),
// else the one in cellar's font library, File, whose family is Family.
// Pack is the library download that brings it.
type Face struct {
	Host   []string
	Family string
	File   string
	URL    string
	Member string // set when URL is an archive (.tar.gz, .exe cabinet): File's path in it
	Sum    string // the archive's sha256, when known
	Size   int64
	Pack   string
}

// A Pack is a download into the font library, done in the settings (or
// with cellar fonts get), never on its own.
type Pack struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Note string `json:"note"`
}

var Packs = []Pack{
	{"liberation", "英文", "Liberation Sans、Serif、Mono：Arial、Times New Roman、Courier New 的替代"},
	{"mscore", "微软核心字体", "Arial、Times New Roman、Courier New、Verdana 等原版英文字体（微软 Core fonts for the Web）"},
	{"cjk", "CJK", "Noto Sans CJK：简体、繁体、日文、韩文都在这一个文件里"},
	{"cjk-serif", "CJK 衬线", "Noto Serif CJK：给要用仿宋、楷体等衬线字体的程序"},
	{"scripts", "全语种", "150 种文字的 Noto Sans，补齐泰文、阿拉伯文、印度文字等（字体选“全部”时用）"},
}

// PackByID returns the pack, or nil.
func PackByID(id string) *Pack {
	for i := range Packs {
		if Packs[i].ID == id {
			return &Packs[i]
		}
	}
	return nil
}

const notoCJK = "https://github.com/notofonts/noto-cjk/raw/main/"

// Liberation is released only as a tarball of all its styles; the library
// takes the Regular of each family from it.
const (
	liberationURL = "https://github.com/liberationfonts/liberation-fonts/files/7261482/liberation-fonts-ttf-2.1.5.tar.gz"
	liberationDir = "liberation-fonts-ttf-2.1.5/"
)

func liberationFace(style string, size int64) Face {
	file := "Liberation" + style + "-Regular.ttf"
	return Face{Host: []string{"Liberation " + style}, Family: "Liberation " + style, File: file,
		URL: liberationURL, Member: liberationDir + file, Size: size, Pack: "liberation"}
}

// cjkFace is one region of a Noto CJK collection (Sans or Serif).
func cjkFace(style, region string) Face {
	f := Face{Host: []string{"Noto " + style + " CJK " + region}, Family: "Noto " + style + " CJK " + region,
		File: "Noto" + style + "CJK-Regular.ttc", URL: notoCJK + style + "/OTC/Noto" + style + "CJK-Regular.ttc"}
	if style == "Sans" {
		f.Size, f.Pack = 19484784, "cjk"
	} else {
		f.Size, f.Pack = 26411360, "cjk-serif"
	}
	return f
}

// cjkRegions are the Noto CJK regions; cjkRegion picks the one a locale
// reads, the one its UI fallback starts with.
var cjkRegions = []string{"SC", "TC", "HK", "JP", "KR"}

func cjkRegion(locale string) string {
	switch lang, country, _ := strings.Cut(locale, "_"); {
	case lang == "zh" && country == "TW":
		return "TC"
	case lang == "zh" && (country == "HK" || country == "MO"):
		return "HK"
	case lang == "ja":
		return "JP"
	case lang == "ko":
		return "KR"
	}
	return "SC"
}

const scriptsRepo = "https://raw.githubusercontent.com/notofonts/notofonts.github.io/main/fonts/"

type scriptFace struct {
	dir, family string
	size        int64
}

//go:generate go run ./genscripts

var FontSets = []FontSet{
	{ID: "core", Name: "英文", Primary: "sans",
		Note: "Arial、Times New Roman、Courier New 用 设置 → 字体 里选的英文核心字体",
		Roles: map[string]Face{
			"sans":  liberationFace("Sans", 410712),
			"serif": liberationFace("Serif", 393576),
			"mono":  liberationFace("Mono", 319508),
		},
		Subst: map[string]string{"Arial": "sans", "Times New Roman": "serif", "Courier New": "mono"},
	},
	{ID: "cjk", Name: "CJK", Primary: "SC",
		Note: "宋体、细明体、MS Gothic、Gulim 等换成对应地区的 Noto Sans CJK",
		Roles: func() map[string]Face {
			m := map[string]Face{}
			for _, r := range cjkRegions {
				m[r] = cjkFace("Sans", r)
			}
			for _, r := range []string{"SC", "TC", "JP", "KR"} {
				m["serif-"+r] = cjkFace("Serif", r)
			}
			return m
		}(),
		Subst: map[string]string{
			"SimSun": "SC", "NSimSun": "SC", "SimSun-ExtB": "SC", "宋体": "SC", "新宋体": "SC",
			"SimHei": "SC", "黑体": "SC", "Microsoft YaHei": "SC", "Microsoft YaHei UI": "SC", "微软雅黑": "SC",
			"DengXian": "SC", "等线": "SC",
			"FangSong": "serif-SC|SC", "仿宋": "serif-SC|SC", "KaiTi": "serif-SC|SC", "SimKai": "serif-SC|SC", "楷体": "serif-SC|SC",
			"MingLiU": "TC", "PMingLiU": "TC", "MingLiU-ExtB": "TC", "PMingLiU-ExtB": "TC", "細明體": "TC", "新細明體": "TC",
			"MingLiU_HKSCS": "HK", "MingLiU_HKSCS-ExtB": "HK",
			"Microsoft JhengHei": "TC", "Microsoft JhengHei UI": "TC", "微軟正黑體": "TC",
			"DFKai-SB": "serif-TC|TC", "標楷體": "serif-TC|TC",
			"MS Gothic": "JP", "MS PGothic": "JP", "MS UI Gothic": "JP", "Meiryo": "JP", "Meiryo UI": "JP",
			"Yu Gothic": "JP", "Yu Gothic UI": "JP", "メイリオ": "JP", "ＭＳ ゴシック": "JP", "ＭＳ Ｐゴシック": "JP",
			"MS Mincho": "serif-JP|JP", "MS PMincho": "serif-JP|JP", "Yu Mincho": "serif-JP|JP", "ＭＳ 明朝": "serif-JP|JP", "ＭＳ Ｐ明朝": "serif-JP|JP",
			"Gulim": "KR", "GulimChe": "KR", "Dotum": "KR", "DotumChe": "KR", "Malgun Gothic": "KR",
			"굴림": "KR", "굴림체": "KR", "돋움": "KR", "돋움체": "KR", "맑은 고딕": "KR",
			"Batang": "serif-KR|KR", "BatangChe": "serif-KR|KR", "Gungsuh": "serif-KR|KR", "GungsuhChe": "serif-KR|KR",
			"바탕": "serif-KR|KR", "바탕체": "serif-KR|KR",
		}},
	{ID: "all", Name: "全部", Includes: []string{"core", "cjk"},
		Note: "英文和 CJK，再加 150 种文字的 Noto Sans"},
}

// EffectiveFontSets expands all into the sets it includes; scripts tells
// whether the scripts' fallback is on (all is chosen).
func EffectiveFontSets(sets []string) (out []string, scripts bool) {
	for _, id := range sets {
		fs := FontSetByID(id)
		if fs == nil {
			out = appendNew(out, id) // reconcile says it is unknown
			continue
		}
		if fs.Includes != nil {
			scripts = true
			for _, inc := range fs.Includes {
				out = appendNew(out, inc)
			}
			continue
		}
		out = appendNew(out, id)
	}
	return out, scripts
}

// scripts is the hidden set behind the fallback chain: one role per Noto
// Sans script family, its name.
var scripts = func() FontSet {
	fs := FontSet{ID: "scripts", Name: "全语种", Roles: map[string]Face{}}
	for _, f := range scriptFaces {
		fs.Roles[f.dir] = Face{Host: []string{f.family}, Family: f.family, File: f.dir + "-Regular.ttf",
			URL: scriptsRepo + f.dir + "/hinted/ttf/" + f.dir + "-Regular.ttf", Size: f.size, Pack: "scripts"}
	}
	return fs
}()

// mscore is the hidden set of Microsoft's core fonts, a role per file:
// what core uses in place of Liberation when the settings say so (the
// roles MSCoreFor names), and all of it is copied in then. They come as
// the self-extracting cabinets of Core fonts for the Web, which may be
// passed on unmodified; winetricks' corefonts takes them from the same
// mirror.
var mscore = func() FontSet {
	fs := FontSet{ID: "mscore", Name: "微软核心字体", Roles: map[string]Face{}}
	const mirror = "https://github.com/pushcx/corefonts/raw/master/"
	cabs := []struct {
		exe, sum string
		files    map[string]string // file (lower case) -> family
	}{
		{"andale32.exe", "0524fe42951adc3a7eb870e32f0920313c71f170c859b5f770d82b4ee111e970", map[string]string{"andalemo.ttf": "Andale Mono"}},
		{"arial32.exe", "85297a4d146e9c87ac6f74822734bdee5f4b2a722d7eaa584b7f2cbf76f478f6", map[string]string{
			"arial.ttf": "Arial", "arialbd.ttf": "Arial", "ariali.ttf": "Arial", "arialbi.ttf": "Arial"}},
		{"arialb32.exe", "a425f0ffb6a1a5ede5b979ed6177f4f4f4fdef6ae7c302a7b7720ef332fec0a8", map[string]string{"ariblk.ttf": "Arial Black"}},
		{"comic32.exe", "9c6df3feefde26d4e41d4a4fe5db2a89f9123a772594d7f59afd062625cd204e", map[string]string{
			"comic.ttf": "Comic Sans MS", "comicbd.ttf": "Comic Sans MS"}},
		{"courie32.exe", "bb511d861655dde879ae552eb86b134d6fae67cb58502e6ff73ec5d9151f3384", map[string]string{
			"cour.ttf": "Courier New", "courbd.ttf": "Courier New", "couri.ttf": "Courier New", "courbi.ttf": "Courier New"}},
		{"georgi32.exe", "2c2c7dcda6606ea5cf08918fb7cd3f3359e9e84338dc690013f20cd42e930301", map[string]string{
			"georgia.ttf": "Georgia", "georgiab.ttf": "Georgia", "georgiai.ttf": "Georgia", "georgiaz.ttf": "Georgia"}},
		{"impact32.exe", "6061ef3b7401d9642f5dfdb5f2b376aa14663f6275e60a51207ad4facf2fccfb", map[string]string{"impact.ttf": "Impact"}},
		{"times32.exe", "db56595ec6ef5d3de5c24994f001f03b2a13e37cee27bc25c58f6f43e8f807ab", map[string]string{
			"times.ttf": "Times New Roman", "timesbd.ttf": "Times New Roman", "timesi.ttf": "Times New Roman", "timesbi.ttf": "Times New Roman"}},
		{"trebuc32.exe", "5a690d9bb8510be1b8b4fe49f1f2319651fe51bbe54775ddddd8ef0bd07fdac9", map[string]string{
			"trebuc.ttf": "Trebuchet MS", "trebucbd.ttf": "Trebuchet MS", "trebucit.ttf": "Trebuchet MS", "trebucbi.ttf": "Trebuchet MS"}},
		{"verdan32.exe", "c1cb61255e363166794e47664e2f21af8e3a26cb6346eb8d2ae2fa85dd5aad96", map[string]string{
			"verdana.ttf": "Verdana", "verdanab.ttf": "Verdana", "verdanai.ttf": "Verdana", "verdanaz.ttf": "Verdana"}},
		{"webdin32.exe", "64595b5abc1080fba8610c5c34fab5863408e806aafe84653ca8575bed17d75a", map[string]string{"webdings.ttf": "Webdings"}},
	}
	// A family's Regular file is what the host's copy of the family stands
	// in for; the other styles come with the pack only.
	regular := map[string]bool{"andalemo.ttf": true, "arial.ttf": true, "ariblk.ttf": true, "comic.ttf": true,
		"cour.ttf": true, "georgia.ttf": true, "impact.ttf": true, "times.ttf": true, "trebuc.ttf": true,
		"verdana.ttf": true, "webdings.ttf": true}
	for _, c := range cabs {
		for file, family := range c.files {
			var host []string
			if regular[file] {
				host = []string{family}
			}
			fs.Roles[strings.TrimSuffix(file, ".ttf")] = Face{Host: host, Family: family, File: file,
				URL: mirror + c.exe, Member: file, Sum: c.sum, Pack: "mscore"}
		}
	}
	return fs
}()

// MSCoreFor maps core's roles to the mscore roles that fill them when
// Microsoft's fonts are chosen.
var MSCoreFor = map[string]string{"sans": "arial", "serif": "times", "mono": "cour"}

// MSCoreRoles are mscore's roles, sorted.
func MSCoreRoles() []string { return sortedKeys(roleSet(mscore.Roles)) }

// FileFamilies are the families each pack file holds (a CJK collection
// holds a region each): having any of them means having the file.
func FileFamilies() map[string][]string {
	out := map[string][]string{}
	for _, fs := range append(slices.Clone(FontSets), scripts, mscore) {
		for _, r := range sortedKeys(roleSet(fs.Roles)) {
			if f := fs.Roles[r]; f.File != "" {
				out[f.File] = appendNew(out[f.File], f.Family)
			}
		}
	}
	return out
}

// PackFaces are the files a pack downloads, one face per file.
func PackFaces(pack string) []Face {
	var out []Face
	seen := map[string]bool{}
	for _, fs := range append(slices.Clone(FontSets), scripts, mscore) {
		for _, r := range sortedKeys(roleSet(fs.Roles)) {
			f := fs.Roles[r]
			if f.Pack == pack && !seen[f.File] {
				seen[f.File] = true
				out = append(out, f)
			}
		}
	}
	return out
}

func roleSet(m map[string]Face) map[string]bool {
	out := map[string]bool{}
	for k := range m {
		out[k] = true
	}
	return out
}

// FontSetByID returns the set, or nil.
func FontSetByID(id string) *FontSet {
	switch id {
	case scripts.ID:
		return &scripts
	case mscore.ID:
		return &mscore
	}
	for i := range FontSets {
		if FontSets[i].ID == id {
			return &FontSets[i]
		}
	}
	return nil
}

// A font role reference, "@cjk.SC": in fonts.link it stands for the
// resolved family, in registry strings {family:REF} and {font:REF} for its
// family name and file name. reconcile resolves them.
func RoleRef(set, role string) string { return "@" + set + "." + role }

//go:generate go run ./genlocales

// Locale is a Windows region: Wine reads it from LANG/LC_ALL itself (UI
// language, ANSI code page), whether or not the host has the glibc locale.
type Locale struct {
	ID     string `json:"id"` // zh_CN
	Name   string `json:"name"`
	Common bool   `json:"common,omitempty"`
	CJK    bool   `json:"cjk,omitempty"` // a new prefix in it gets the cjk set
}

// common are listed first, with names of our own where glibc's are long.
var common = []Locale{
	{ID: "zh_CN", Name: "简体中文（中国）"},
	{ID: "zh_SG", Name: "简体中文（新加坡）"},
	{ID: "zh_TW", Name: "繁體中文（台灣）"},
	{ID: "zh_HK", Name: "繁體中文（香港）"},
	{ID: "ja_JP"}, {ID: "ko_KR"}, {ID: "en_US"}, {ID: "en_GB"}, {ID: "de_DE"}, {ID: "fr_FR"},
	{ID: "es_ES"}, {ID: "es_MX"}, {ID: "pt_BR"}, {ID: "pt_PT"}, {ID: "it_IT"}, {ID: "nl_NL"},
	{ID: "pl_PL"}, {ID: "ru_RU"}, {ID: "uk_UA"}, {ID: "tr_TR"}, {ID: "vi_VN"}, {ID: "th_TH"},
	{ID: "id_ID"}, {ID: "ar_SA"}, {ID: "he_IL"},
}

// Locales is the region list: the common ones, then all of glibc's.
var Locales = func() []Locale {
	names := map[string]string{}
	for _, l := range allLocales {
		names[l.ID] = l.Name
	}
	var out []Locale
	seen := map[string]bool{}
	for _, l := range common {
		if l.Name == "" {
			l.Name = names[l.ID]
		}
		l.Common = true
		out = append(out, l)
		seen[l.ID] = true
	}
	for _, l := range allLocales {
		if !seen[l.ID] {
			out = append(out, l)
		}
	}
	for i := range out {
		out[i].CJK = IsCJK(out[i].ID)
	}
	return out
}()

// DefaultFontSets are what a new prefix starts with: English, and CJK for
// a Chinese, Japanese or Korean region. "all" is the user's to choose.
func DefaultFontSets(locale string) []string {
	if IsCJK(locale) {
		return []string{"core", "cjk"}
	}
	return []string{"core"}
}

// IsCJK reports whether a region reads Chinese, Japanese or Korean.
func IsCJK(locale string) bool {
	lang, _, _ := strings.Cut(locale, "_")
	return lang == "zh" || lang == "ja" || lang == "ko"
}

// HostLocale is the host's locale (zh_CN), the default for new prefixes.
func HostLocale() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := os.Getenv(k)
		v, _, _ = strings.Cut(v, ".")
		v, _, _ = strings.Cut(v, "@")
		if v != "" && v != "C" && v != "POSIX" {
			return v
		}
	}
	return ""
}

// LinkFont is the SystemLink entry the fallback chain goes on: for glyphs
// it lacks, every font falls back to Microsoft Sans Serif's links (and
// Tahoma's), so one entry serves all. The chain is the CJK region the
// locale reads and the other regions (cjk), then every script's Noto Sans
// (all): whichever of these are here. Reconcile spreads it over more
// entries when it outgrows what Wine reads of one.
const LinkFont = "Microsoft Sans Serif"

// subst renders a Subst role spec ("serif-SC|SC") of a set as a template.
func subst(set, spec string) string {
	var refs []string
	for _, r := range strings.Split(spec, "|") {
		refs = append(refs, RoleRef(set, r))
	}
	return "{family:" + strings.Join(refs, "|") + "}"
}

// fallback lists the role references of the fallback chain.
func fallback(locale string, sets []string) []string {
	sets, all := EffectiveFontSets(sets)
	var refs []string
	if slices.Contains(sets, "cjk") {
		first := cjkRegion(locale)
		refs = append(refs, RoleRef("cjk", first))
		for _, r := range cjkRegions {
			if r != first {
				refs = append(refs, RoleRef("cjk", r))
			}
		}
	}
	if all {
		for _, f := range scriptFaces {
			refs = append(refs, RoleRef(scripts.ID, f.dir))
		}
	}
	return refs
}

// localeLayer is what locale and font_sets add, as a config layer placed
// after the defaults and before the file, so the file can still override.
func localeLayer(locale string, sets []string) map[string]any {
	l := map[string]any{}
	if locale != "" {
		lang := locale + ".UTF-8"
		l["env"] = map[string]any{"LANG": lang, "LC_ALL": lang}
	}
	reg := map[string]any{}
	subs := map[string]any{}
	eff, _ := EffectiveFontSets(sets)
	for _, id := range eff {
		fs := FontSetByID(id)
		if fs == nil {
			continue
		}
		for win, spec := range fs.Subst {
			subs[win] = subst(id, spec)
		}
	}
	if len(subs) > 0 {
		reg[`HKLM\Software\Microsoft\Windows NT\CurrentVersion\FontSubstitutes`] = subs
	}
	if refs := fallback(locale, sets); len(refs) > 0 {
		var link []any
		for _, ref := range refs {
			if strings.HasPrefix(ref, "@"+scripts.ID+".") {
				// One family per file: the file alone names it, and the
				// chain is long.
				link = append(link, "{font:"+ref+"}")
				continue
			}
			link = append(link, "{font:"+ref+"},{family:"+ref+"}")
		}
		reg[`HKLM\Software\Microsoft\Windows NT\CurrentVersion\FontLink\SystemLink`] = map[string]any{
			LinkFont: map[string]any{"type": "multi_sz", "value": link},
		}
		reg[`HKCU\Control Panel\Desktop`] = map[string]any{
			"FontSmoothing": "2", "FontSmoothingType": int64(2), "FontSmoothingGamma": int64(1400), "FontSmoothingOrientation": int64(1),
		}
	}
	if len(reg) > 0 {
		l["registry"] = reg
	}
	return l
}

// applyFontSets adds what the sets and the fallback chain use to
// fonts.link; reconcile copies in whatever of it is here.
func (c *Config) applyFontSets() {
	eff, _ := EffectiveFontSets(c.FontSets)
	for _, id := range eff {
		fs := FontSetByID(id)
		if fs == nil {
			continue
		}
		roles := map[string]bool{fs.Primary: true}
		for _, spec := range fs.Subst {
			for _, r := range strings.Split(spec, "|") {
				roles[r] = true
			}
		}
		for _, r := range sortedKeys(roles) {
			c.Fonts.Link = appendNew(c.Fonts.Link, RoleRef(id, r))
		}
	}
	for _, ref := range fallback(c.Locale, c.FontSets) {
		c.Fonts.Link = appendNew(c.Fonts.Link, ref)
	}
}

func sortedKeys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func appendNew(xs []string, x string) []string {
	for _, y := range xs {
		if y == x {
			return xs
		}
	}
	return append(xs, x)
}
