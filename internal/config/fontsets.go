package config

import (
	"os"
	"strings"
)

// A FontSet makes the fonts one script's Windows programs ask for resolve
// to host fonts: the families are copied in and registered, the Windows
// names substituted to them (the lists follow winetricks' fake* verbs), and
// the UI fonts fall back to them for missing glyphs. Deps are winetricks
// verbs for sets with no host font to use.
type FontSet struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Families []string          `json:"-"`
	Fallback string            `json:"-"` // family the UI fonts fall back to
	Subst    map[string]string `json:"-"`
	Deps     []string          `json:"-"`
}

const (
	sansSC = "Noto Sans CJK SC"
	serfSC = "Noto Serif CJK SC"
	sansTC = "Noto Sans CJK TC"
	sansJP = "Noto Sans CJK JP"
	sansKR = "Noto Sans CJK KR"
)

var FontSets = []FontSet{
	{ID: "zh-hans", Name: "简体中文", Families: []string{sansSC, serfSC}, Fallback: sansSC, Subst: map[string]string{
		"SimSun": sansSC, "NSimSun": sansSC, "SimSun-ExtB": sansSC, "宋体": sansSC, "新宋体": sansSC,
		"SimHei": sansSC, "黑体": sansSC, "Microsoft YaHei": sansSC, "Microsoft YaHei UI": sansSC, "微软雅黑": sansSC,
		"DengXian": sansSC, "等线": sansSC,
		"FangSong": serfSC, "仿宋": serfSC, "KaiTi": serfSC, "SimKai": serfSC, "楷体": serfSC,
	}},
	{ID: "zh-hant", Name: "繁體中文", Families: []string{sansTC}, Fallback: sansTC, Subst: map[string]string{
		"MingLiU": sansTC, "PMingLiU": sansTC, "MingLiU-ExtB": sansTC, "PMingLiU-ExtB": sansTC, "細明體": sansTC, "新細明體": sansTC,
		"Microsoft JhengHei": sansTC, "Microsoft JhengHei UI": sansTC, "微軟正黑體": sansTC,
		"DFKai-SB": sansTC, "標楷體": sansTC,
	}},
	{ID: "ja", Name: "日本語", Families: []string{sansJP}, Fallback: sansJP, Subst: map[string]string{
		"MS Gothic": sansJP, "MS PGothic": sansJP, "MS UI Gothic": sansJP, "MS Mincho": sansJP, "MS PMincho": sansJP,
		"Meiryo": sansJP, "Meiryo UI": sansJP, "Yu Gothic": sansJP, "Yu Gothic UI": sansJP, "Yu Mincho": sansJP,
		"メイリオ": sansJP, "ＭＳ ゴシック": sansJP, "ＭＳ Ｐゴシック": sansJP, "ＭＳ 明朝": sansJP, "ＭＳ Ｐ明朝": sansJP,
	}},
	{ID: "ko", Name: "한국어", Families: []string{sansKR}, Fallback: sansKR, Subst: map[string]string{
		"Gulim": sansKR, "GulimChe": sansKR, "Dotum": sansKR, "DotumChe": sansKR, "Batang": sansKR, "BatangChe": sansKR,
		"Gungsuh": sansKR, "GungsuhChe": sansKR, "Malgun Gothic": sansKR,
		"굴림": sansKR, "굴림체": sansKR, "돋움": sansKR, "돋움체": sansKR, "바탕": sansKR, "바탕체": sansKR, "맑은 고딕": sansKR,
	}},
	// Arial, Times New Roman, Courier New...: hosts rarely have the
	// metric-compatible Liberation fonts, so install Microsoft's own.
	{ID: "core", Name: "西文核心字体", Deps: []string{"corefonts"}},
}

func fontSet(id string) *FontSet {
	for i := range FontSets {
		if FontSets[i].ID == id {
			return &FontSets[i]
		}
	}
	return nil
}

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
		fs := fontSet(id)
		if fs == nil {
			continue
		}
		for win, fam := range fs.Subst {
			subst[win] = fam
		}
		if fs.Fallback != "" {
			link = append(link, "{font:"+fs.Fallback+"},"+fs.Fallback)
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

// applyFontSets adds the sets' families and winetricks verbs to the config.
func (c *Config) applyFontSets() {
	for _, id := range c.FontSets {
		fs := fontSet(id)
		if fs == nil {
			continue
		}
		for _, f := range fs.Families {
			c.Fonts.Link = appendNew(c.Fonts.Link, f)
		}
		for _, d := range fs.Deps {
			c.Dependencies = appendNew(c.Dependencies, d)
		}
	}
}

func appendNew(xs []string, x string) []string {
	for _, y := range xs {
		if y == x {
			return xs
		}
	}
	return append(xs, x)
}
