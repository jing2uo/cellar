// Package fontreg keeps the Fonts registry key in step with the font files
// in a prefix's C:\windows\Fonts. Wine's GDI finds those files by itself,
// but DirectWrite (WPF, CEF, Electron) knows only the fonts listed in the
// key, so a font copied into the folder, by cellar or by hand, is missing
// for those programs until it is listed.
package fontreg

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/jing2uo/cellar/internal/regfile"
	"github.com/jing2uo/cellar/internal/runner"
)

const Key = `HKEY_LOCAL_MACHINE\Software\Microsoft\Windows NT\CurrentVersion\Fonts`

// suffix ends the value names cellar writes: "<file> (TrueType)". Wine names
// its entries after the face ("Noto Sans CJK SC (TrueType)") and on startup
// deletes, by that name, any it listed for a host font that a copy in the
// prefix now shadows; an entry of cellar's under the face name went with
// it. A file name is never a face name, so these stay, and the name tells
// which entries are cellar's to remove.
const suffix = " (TrueType)"

// Change lists the files to add to the key, and the files whose entries
// (cellar's) go because the file is gone.
type Change struct {
	Add    []string `json:"add,omitempty"`
	Remove []string `json:"remove,omitempty"`
}

func (c Change) Empty() bool { return len(c.Add) == 0 && len(c.Remove) == 0 }

func (c Change) String() string {
	var parts []string
	if len(c.Add) > 0 {
		parts = append(parts, "registered "+strings.Join(c.Add, ", "))
	}
	if len(c.Remove) > 0 {
		parts = append(parts, "unregistered "+strings.Join(c.Remove, ", "))
	}
	return "fonts: " + strings.Join(parts, "; ")
}

// Diff compares the folder with the key as system.reg has it. While a
// wineserver runs, system.reg may lag a little; Sync is idempotent, so the
// worst case is writing an entry again.
func Diff(pfx string) (Change, error) {
	var ch Change
	ents, err := os.ReadDir(filepath.Join(pfx, "drive_c", "windows", "Fonts"))
	if os.IsNotExist(err) {
		return ch, nil
	}
	if err != nil {
		return ch, err
	}
	files := map[string]string{} // lower case -> as on disk; Wine ignores case
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".ttf", ".ttc", ".otf", ".otc":
			files[strings.ToLower(e.Name())] = e.Name()
		}
	}
	listed, err := regfile.Values(filepath.Join(pfx, "system.reg"), `Software\Microsoft\Windows NT\CurrentVersion\Fonts`)
	if err != nil {
		return ch, err
	}
	known := map[string]bool{}
	for name, data := range listed {
		f := fontsFile(data)
		if f == "" {
			continue
		}
		if _, ok := files[strings.ToLower(f)]; ok {
			known[strings.ToLower(f)] = true
		} else if name == f+suffix {
			ch.Remove = append(ch.Remove, f)
		}
	}
	for lower, f := range files {
		if !known[lower] {
			ch.Add = append(ch.Add, f)
		}
	}
	slices.Sort(ch.Add)
	slices.Sort(ch.Remove)
	return ch, nil
}

// Sync applies Diff with `wine reg import`. Nothing runs when the key is
// already in step.
func Sync(r *runner.Runner, pfx string, env map[string]string) (Change, error) {
	ch, err := Diff(pfx)
	if err != nil || ch.Empty() {
		return ch, err
	}
	f, err := os.CreateTemp(filepath.Dir(pfx), ".fonts-*.reg")
	if err != nil {
		return ch, err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(regFile(ch)); err != nil {
		f.Close()
		return ch, err
	}
	f.Close()
	cmd := r.Command(pfx, env, "reg", "import", f.Name())
	if out, err := cmd.CombinedOutput(); err != nil {
		return ch, fmt.Errorf("reg import: %v\n%s", err, bytes.TrimSpace(out))
	}
	return ch, nil
}

// fontsFile is the file an entry points at in C:\windows\Fonts: a bare name
// (Windows' own form) or a full path there; "" for anything else (Wine's
// \??\unix\ paths to host fonts).
func fontsFile(data string) string {
	if rest, ok := cutPrefixFold(data, `C:\windows\Fonts\`); ok {
		data = rest
	}
	if data == "" || strings.ContainsAny(data, `\/`) {
		return ""
	}
	return data
}

func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
		return s[len(prefix):], true
	}
	return s, false
}

// regFile renders the change as a UTF-16LE .reg file.
func regFile(ch Change) []byte {
	esc := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace
	var sb strings.Builder
	sb.WriteString("Windows Registry Editor Version 5.00\r\n\r\n[" + Key + "]\r\n")
	for _, f := range ch.Remove {
		sb.WriteString(`"` + esc(f+suffix) + "\"=-\r\n")
	}
	for _, f := range ch.Add {
		sb.WriteString(`"` + esc(f+suffix) + `"="` + esc(f) + "\"\r\n")
	}
	out := []byte{0xff, 0xfe}
	for _, u := range utf16.Encode([]rune(sb.String())) {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}
