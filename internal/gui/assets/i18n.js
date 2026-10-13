// The GUI's text is written in Chinese; t() looks a string up in EN when
// the GUI speaks English, falling back to the Chinese. {0}, {1}, ... in the
// text are filled from the arguments, so a sentence stays one key:
//   t("已删除 {0}", name)
// Text from the backend (runner notes, font set names) goes through t()
// too: it is fixed text, so it has its entry here like the page's own.

let lang = "zh";

export function setLang(l) {
  lang = l === "en" ? "en" : "zh";
  document.documentElement.lang = lang === "en" ? "en" : "zh-CN";
}

export const getLang = () => lang;

// LANG_NAMES are the languages on offer, each named in itself.
export const LANG_NAMES = { zh: "简体中文", en: "English" };

export function t(s, ...args) {
  let out = s;
  if (lang === "en") {
    if (Object.hasOwn(EN, s)) out = EN[s];
    else console.warn("i18n: no English for", JSON.stringify(s));
  }
  return args.length ? out.replace(/\{(\d+)\}/g, (m, i) => (i < args.length ? String(args[i]) : m)) : out;
}

// EN maps each Chinese text to its English. Keep the keys exactly as they
// appear in t() calls (and in the backend's data).
export const EN = {
  "设置": "Settings",
  "通用": "General",
  "字体": "Fonts",
  "外观与语言": "Appearance and language",
  "语言": "Language",
  "主题": "Theme",
  "跟随系统": "Follow the system",
  "浅色": "Light",
  "深色": "Dark",
  "已保存": "Saved",
};
