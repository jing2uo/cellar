#!/usr/bin/env python3
"""Check the GUI's translations.

The GUI is written in Chinese and translated through a dictionary keyed by
the Chinese text (internal/gui/assets/i18n.js). This checks that:

  1. every Chinese text in index.html's script is the first argument of
     t(), as a plain "..." or '...' literal (no template literal, no
     concatenation), and the markup outside the script has none;
  2. every such key, and every Chinese text the backend sends for display
     (runner notes, font set and pack names and notes), has an EN entry;
  3. every Chinese string literal in internal/gui/*.go is the first
     argument of tr(zh, en);
  4. EN has no entries nothing uses.

Run from the repository root: scripts/i18n-check.py
"""
import re
import sys

HAN = re.compile(r"[㐀-鿿豈-﫿　-〿＀-￯]")
ROOT = "internal/gui/assets/"
problems = []


def line_of(text, pos):
    return text.count("\n", 0, pos) + 1


def js_strings(src):
    """Yield (kind, value, start, prev) for each string or template literal
    part in JS source; prev is the code just before it. Skips comments and
    regex literals."""
    i, n = 0, len(src)
    tmpl = []  # brace depths of open template ${...}
    depth = 0
    last = ""  # last significant code char, to tell a regex from a division
    while i < n:
        c = src[i]
        if c == "/" and src.startswith("//", i):
            i = src.find("\n", i)
            i = n if i < 0 else i
            continue
        if c == "/" and src.startswith("/*", i):
            i = src.find("*/", i + 2) + 2
            continue
        if c == "/" and (last == "" or last in "(,=:[!&|?{};+-*%<>~^" or src[max(0, i - 7):i].rstrip().endswith("return")):
            j = i + 1
            cls = False
            while j < n:
                if src[j] == "\\":
                    j += 2
                    continue
                if src[j] == "[":
                    cls = True
                elif src[j] == "]":
                    cls = False
                elif src[j] == "/" and not cls:
                    break
                j += 1
            i = j + 1
            last = "a"
            continue
        if c in "\"'":
            j = i + 1
            while j < n and src[j] != c:
                j += 2 if src[j] == "\\" else 1
            yield ("str", src[i + 1:j], i, src[max(0, i - 40):i])
            i = j + 1
            last = "a"
            continue
        if c == "`" or (c == "}" and tmpl and tmpl[-1] == depth):
            if c == "}":
                tmpl.pop()
            j = i + 1
            start = j
            while j < n and src[j] != "`" and not src.startswith("${", j):
                j += 2 if src[j] == "\\" else 1
            yield ("tmpl", src[start:j], start, src[max(0, i - 40):i])
            if j < n and src[j] == "`":
                i = j + 1
                last = "a"
            else:
                tmpl.append(depth)
                i = j + 2
                last = "("
            continue
        if c == "{":
            depth += 1
        elif c == "}":
            depth -= 1
        if not c.isspace():
            last = c
        i += 1


EN_KEYS = {}


def load_en():
    src = open(ROOT + "i18n.js", encoding="utf-8").read()
    body = src[src.index("export const EN"):]
    for kind, val, pos, prev in js_strings(body):
        if kind == "str" and HAN.search(val):
            # a key is followed by ':'
            after = body[pos + len(val) + 2:pos + len(val) + 4].lstrip()
            if after.startswith(":"):
                EN_KEYS[val.encode().decode("unicode_escape") if "\\u" in val else val] = line_of(body, pos)


def check_html():
    src = open(ROOT + "index.html", encoding="utf-8").read()
    s0 = src.index("<script type=\"module\">")
    s1 = src.index("</script>", s0)
    style_end = src.index("</style>")
    markup = src[style_end:s0]
    for m in HAN.finditer(markup):
        problems.append(f"index.html:{line_of(src, style_end + m.start())}: Chinese in the markup; render it from the script with t()")
    script = src[s0:s1]
    used = set()
    for kind, val, pos, prev in js_strings(script):
        if not HAN.search(val):
            continue
        ln = line_of(src, s0 + pos)
        if kind == "tmpl":
            problems.append(f"index.html:{ln}: Chinese in a template literal: {val[:40]!r}; use t(\"...\", args)")
        elif not re.search(r"\bt\(\s*$", prev):
            problems.append(f"index.html:{ln}: Chinese string not passed to t(): {val[:40]!r}")
        else:
            used.add(val)
    return used


def go_literals(path):
    for ln, line in enumerate(open(path, encoding="utf-8"), 1):
        code = line
        # drop a // comment outside strings
        out, q = [], None
        k = 0
        while k < len(code):
            ch = code[k]
            if q:
                out.append(ch)
                if ch == "\\":
                    out.append(code[k + 1:k + 2])
                    k += 2
                    continue
                if ch == q:
                    q = None
            elif ch in "\"`":
                q = ch
                out.append(ch)
            elif code.startswith("//", k):
                break
            else:
                out.append(ch)
            k += 1
        code = "".join(out)
        for m in re.finditer(r'"((?:[^"\\]|\\.)*)"|`([^`]*)`', code):
            val = m.group(1) if m.group(1) is not None else m.group(2)
            if HAN.search(val):
                yield ln, val, code[:m.start()], code[m.end():]


def check_go_gui():
    import glob
    for path in sorted(glob.glob("internal/gui/*.go")):
        for ln, val, before, after in go_literals(path):
            if not re.search(r"\btr\(\s*$", before):
                problems.append(f"{path}:{ln}: Chinese string not in tr(zh, en): {val[:40]!r}")


def backend_texts():
    """Chinese texts the backend sends for display."""
    out = {}
    for path in ["internal/runner/catalog.go", "internal/config/fontsets.go"]:
        in_common = False
        for ln, line in enumerate(open(path, encoding="utf-8"), 1):
            if line.startswith("var common"):
                in_common = True  # locale names: each in its own language
            elif in_common and line.startswith("}"):
                in_common = False
        for ln, val, before, after in go_literals(path):
            if after.lstrip().startswith(":"):
                continue  # a map key: a Windows font name, not shown
            out[val] = f"{path}:{ln}"
    # drop the common locales' names
    src = open("internal/config/fontsets.go", encoding="utf-8").read()
    i = src.find("var common")
    block = src[i:src.find("\n}\n", i)] if i >= 0 else ""
    for name in re.findall(r'Name:\s*"([^"]*)"', block):
        out.pop(name, None)
    return out


def main():
    load_en()
    used = check_html()
    check_go_gui()
    back = backend_texts()
    for key in sorted(used):
        if key not in EN_KEYS:
            problems.append(f"i18n.js: no EN entry for {key!r}")
    for key, where in sorted(back.items()):
        if key not in EN_KEYS:
            problems.append(f"i18n.js: no EN entry for backend text {key!r} ({where})")
    for key, ln in sorted(EN_KEYS.items(), key=lambda kv: kv[1]):
        if key not in used and key not in back:
            problems.append(f"i18n.js:{ln}: EN entry nothing uses: {key!r}")
    for p in problems:
        print(p)
    print(f"{len(problems)} problem(s); {len(used)} keys in the page, {len(back)} from the backend, {len(EN_KEYS)} in EN")
    sys.exit(1 if problems else 0)


main()
