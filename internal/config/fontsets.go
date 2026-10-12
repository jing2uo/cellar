package config

import (
	"os"
	"sort"
	"strings"
)

// A FontSet makes the fonts one script's Windows programs ask for resolve
// to one fixed font per role: Noto CJK for the CJK sets (consistent glyph
// coverage and metrics on every machine), Liberation for the Latin core
// fonts. The Windows names are substituted to a role (the lists follow
// winetricks' fake* verbs), the font is copied in and registered, and the
// UI fonts fall back to Fallback for missing glyphs. Deps are winetricks
// verbs installed when Primary is neither installed nor downloadable.
type FontSet struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Primary  string            `json:"-"`
	Roles    map[string]Face   `json:"-"`
	Subst    map[string]string `json:"-"` // Windows family -> role
	Fallback string            `json:"-"` // role the UI fonts fall back to
	Deps     []string          `json:"-"`
}

// Face is a role's font: the host's copy (any of Host, by family name),
// else the one cellar downloads from URL into its font cache as File,
// whose family is Family.
type Face struct {
	Host   []string
	Family string
	File   string
	URL    string
	Size   int64 // bytes, for the GUI
}

const notoCJK = "https://github.com/notofonts/noto-cjk/raw/main/"

// notoFace is a Noto CJK region: the host's OTC family, else the region's
// subset OTF (only that region's glyphs, a fraction of the size).
func notoFace(style, region, cjkRegion string, size int64) Face {
	return Face{
		Host:   []string{"Noto " + style + " CJK " + cjkRegion, "Noto " + style + " " + region},
		Family: "Noto " + style + " " + region,
		File:   "Noto" + style + region + "-Regular.otf",
		URL:    notoCJK + style + "/SubsetOTF/" + region + "/Noto" + style + region + "-Regular.otf",
		Size:   size,
	}
}

var FontSets = []FontSet{
	{ID: "zh-hans", Name: "简体中文", Primary: "sans", Fallback: "sans",
		Roles: map[string]Face{
			"sans":  notoFace("Sans", "SC", "SC", 8331336),
			"serif": notoFace("Serif", "SC", "SC", 11625800),
		},
		Subst: map[string]string{
			"SimSun": "sans", "NSimSun": "sans", "SimSun-ExtB": "sans", "宋体": "sans", "新宋体": "sans",
			"SimHei": "sans", "黑体": "sans", "Microsoft YaHei": "sans", "Microsoft YaHei UI": "sans", "微软雅黑": "sans",
			"DengXian": "sans", "等线": "sans",
			"FangSong": "serif", "仿宋": "serif", "KaiTi": "serif", "SimKai": "serif", "楷体": "serif",
		}},
	{ID: "zh-hant", Name: "繁體中文", Primary: "sans", Fallback: "sans",
		Roles: map[string]Face{"sans": notoFace("Sans", "TC", "TC", 5683368)},
		Subst: map[string]string{
			"MingLiU": "sans", "PMingLiU": "sans", "MingLiU-ExtB": "sans", "PMingLiU-ExtB": "sans", "細明體": "sans", "新細明體": "sans",
			"Microsoft JhengHei": "sans", "Microsoft JhengHei UI": "sans", "微軟正黑體": "sans",
			"DFKai-SB": "sans", "標楷體": "sans",
		}},
	{ID: "ja", Name: "日本語", Primary: "sans", Fallback: "sans",
		Roles: map[string]Face{"sans": notoFace("Sans", "JP", "JP", 4533028)},
		Subst: map[string]string{
			"MS Gothic": "sans", "MS PGothic": "sans", "MS UI Gothic": "sans", "MS Mincho": "sans", "MS PMincho": "sans",
			"Meiryo": "sans", "Meiryo UI": "sans", "Yu Gothic": "sans", "Yu Gothic UI": "sans", "Yu Mincho": "sans",
			"メイリオ": "sans", "ＭＳ ゴシック": "sans", "ＭＳ Ｐゴシック": "sans", "ＭＳ 明朝": "sans", "ＭＳ Ｐ明朝": "sans",
		}},
	{ID: "ko", Name: "한국어", Primary: "sans", Fallback: "sans",
		Roles: map[string]Face{"sans": notoFace("Sans", "KR", "KR", 4644748)},
		Subst: map[string]string{
			"Gulim": "sans", "GulimChe": "sans", "Dotum": "sans", "DotumChe": "sans", "Batang": "sans", "BatangChe": "sans",
			"Gungsuh": "sans", "GungsuhChe": "sans", "Malgun Gothic": "sans",
			"굴림": "sans", "굴림체": "sans", "돋움": "sans", "돋움체": "sans", "바탕": "sans", "바탕체": "sans", "맑은 고딕": "sans",
		}},
	// Arial, Times New Roman, Courier New: the metric-compatible Liberation
	// fonts (the Flatpak runtime ships them), else Microsoft's own through
	// winetricks.
	{ID: "core", Name: "西文核心字体", Primary: "sans", Deps: []string{"corefonts"},
		Roles: map[string]Face{
			"sans":  {Host: []string{"Liberation Sans"}},
			"serif": {Host: []string{"Liberation Serif"}},
			"mono":  {Host: []string{"Liberation Mono"}},
		},
		Subst: map[string]string{"Arial": "sans", "Times New Roman": "serif", "Courier New": "mono"},
	},
}

// FontSetByID returns the set, or nil.
func FontSetByID(id string) *FontSet {
	for i := range FontSets {
		if FontSets[i].ID == id {
			return &FontSets[i]
		}
	}
	return nil
}

// A font role reference, "@zh-hans.sans": in fonts.link it stands for the
// resolved family, in registry strings {family:REF} and {font:REF} for its
// family name and file name. reconcile resolves them.
func RoleRef(set, role string) string { return "@" + set + "." + role }

// Locale is a Windows region: Wine reads it from LANG/LC_ALL itself (UI
// language, ANSI code page), whether or not the host has the glibc locale.
type Locale struct {
	ID      string `json:"id"` // zh_CN
	Name    string `json:"name"`
	FontSet string `json:"font_set,omitempty"` // suggested with it
}

var Locales = []Locale{
	{"zh_CN", "简体中文（中国）", "zh-hans"},
	{"zh_SG", "简体中文（新加坡）", "zh-hans"},
	{"zh_TW", "繁體中文（台灣）", "zh-hant"},
	{"zh_HK", "繁體中文（香港）", "zh-hant"},
	{"ja_JP", "日本語（日本）", "ja"},
	{"ko_KR", "한국어（대한민국）", "ko"},
	{"en_US", "English (United States)", ""},
	{"en_GB", "English (United Kingdom)", ""},
	{"de_DE", "Deutsch (Deutschland)", ""},
	{"fr_FR", "Français (France)", ""},
	{"es_ES", "Español (España)", ""},
	{"es_MX", "Español (México)", ""},
	{"pt_BR", "Português (Brasil)", ""},
	{"pt_PT", "Português (Portugal)", ""},
	{"it_IT", "Italiano (Italia)", ""},
	{"nl_NL", "Nederlands (Nederland)", ""},
	{"pl_PL", "Polski (Polska)", ""},
	{"ru_RU", "Русский (Россия)", ""},
	{"uk_UA", "Українська (Україна)", ""},
	{"tr_TR", "Türkçe (Türkiye)", ""},
	{"vi_VN", "Tiếng Việt (Việt Nam)", ""},
	{"th_TH", "ไทย (ไทย)", ""},
	{"id_ID", "Bahasa Indonesia (Indonesia)", ""},
	{"ar_SA", "العربية (السعودية)", ""},
	{"he_IL", "עברית (ישראל)", ""},
}

// SuggestedFontSets are the sets a new prefix in locale starts with.
func SuggestedFontSets(locale string) []string {
	for _, l := range Locales {
		if l.ID == locale && l.FontSet != "" {
			return []string{l.FontSet}
		}
	}
	return []string{}
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

// The fonts Windows UI text is drawn with; each falls back to the selected
// sets' fonts for glyphs it lacks.
var uiFonts = []string{"Tahoma", "Segoe UI", "Microsoft Sans Serif", "MS Shell Dlg", "MS Shell Dlg 2", "Arial"}

// localeLayer is what locale and font_sets add, as a config layer placed
// after the profiles and before the file, so the file can still override.
func localeLayer(locale string, sets []string) map[string]any {
	l := map[string]any{}
	if locale != "" {
		lang := locale + ".UTF-8"
		l["env"] = map[string]any{"LANG": lang, "LC_ALL": lang}
	}
	reg := map[string]any{}
	subst := map[string]any{}
	var link []any
	for _, id := range sets {
		fs := FontSetByID(id)
		if fs == nil {
			continue
		}
		for win, role := range fs.Subst {
			subst[win] = "{family:" + RoleRef(id, role) + "}"
		}
		if fs.Fallback != "" {
			ref := RoleRef(id, fs.Fallback)
			link = append(link, "{font:"+ref+"},{family:"+ref+"}")
		}
	}
	if len(subst) > 0 {
		reg[`HKLM\Software\Microsoft\Windows NT\CurrentVersion\FontSubstitutes`] = subst
	}
	if len(link) > 0 {
		sl := map[string]any{}
		for _, f := range uiFonts {
			sl[f] = map[string]any{"type": "multi_sz", "value": link}
		}
		reg[`HKLM\Software\Microsoft\Windows NT\CurrentVersion\FontLink\SystemLink`] = sl
		reg[`HKCU\Control Panel\Desktop`] = map[string]any{
			"FontSmoothing": "2", "FontSmoothingType": int64(2), "FontSmoothingGamma": int64(1400), "FontSmoothingOrientation": int64(1),
		}
	}
	if len(reg) > 0 {
		l["registry"] = reg
	}
	return l
}

// applyFontSets adds the role references the sets use to fonts.link;
// reconcile copies in what they resolve to. A set's Deps are added by
// reconcile, which knows whether its fonts are installed.
func (c *Config) applyFontSets() {
	for _, id := range c.FontSets {
		fs := FontSetByID(id)
		if fs == nil {
			continue
		}
		roles := map[string]bool{}
		for _, r := range fs.Subst {
			roles[r] = true
		}
		if fs.Fallback != "" {
			roles[fs.Fallback] = true
		}
		for _, r := range sortedKeys(roles) {
			c.Fonts.Link = appendNew(c.Fonts.Link, RoleRef(id, r))
		}
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
