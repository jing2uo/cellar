package fontreg

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"
)

func prefix(t *testing.T, files []string, reg string) string {
	t.Helper()
	pfx := t.TempDir()
	dir := filepath.Join(pfx, "drive_c", "windows", "Fonts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(pfx, "system.reg"), []byte(reg), 0o644); err != nil {
		t.Fatal(err)
	}
	return pfx
}

const reg = `WINE REGISTRY Version 2

[Software\\Microsoft\\Windows NT\\CurrentVersion\\Fonts] 1792045491
#time=1dd5c6de2d740f2
"Arial (TrueType)"="arial.ttf"
"Courier"="coure.fon"
"Liberation Sans Bold (TrueType)"="\\??\\unix\\run\\host\\fonts\\truetype\\liberation\\LiberationSans-Bold.ttf"
"SimSun & NSimSun (TrueType)"="C:\\windows\\Fonts\\SIMSUN.TTC"
"gone.ttf (TrueType)"="gone.ttf"
"Old Font (TrueType)"="old.ttf"

[Software\\Microsoft\\Windows NT\\CurrentVersion\\FontSubstitutes] 1792044648
"x.ttf (TrueType)"="x.ttf"
`

func TestDiff(t *testing.T) {
	pfx := prefix(t, []string{"arial.ttf", "simsun.ttc", "msyh.ttc", "NotoSansCJK-Regular.ttc", "corefonts.installed", "a.ttf.cellar-tmp"}, reg)
	ch, err := Diff(pfx)
	if err != nil {
		t.Fatal(err)
	}
	// simsun.ttc is listed by full path, in another case; the substitutes
	// key's value is not an entry; old.ttf is gone but not cellar's.
	if want := []string{"NotoSansCJK-Regular.ttc", "msyh.ttc"}; !slices.Equal(ch.Add, want) {
		t.Errorf("add = %v, want %v", ch.Add, want)
	}
	if want := []string{"gone.ttf"}; !slices.Equal(ch.Remove, want) {
		t.Errorf("remove = %v, want %v", ch.Remove, want)
	}
}

func TestDiffInSync(t *testing.T) {
	pfx := prefix(t, []string{"arial.ttf"}, "[Software\\\\Microsoft\\\\Windows NT\\\\CurrentVersion\\\\Fonts]\n\"Arial (TrueType)\"=\"arial.ttf\"\n")
	if ch, err := Diff(pfx); err != nil || !ch.Empty() {
		t.Errorf("diff = %+v, %v; want empty", ch, err)
	}
	if ch, err := Diff(filepath.Join(t.TempDir(), "none")); err != nil || !ch.Empty() {
		t.Errorf("no prefix: %+v, %v", ch, err)
	}
}

func TestRegFile(t *testing.T) {
	b := regFile(Change{Add: []string{"微软雅黑.ttf"}, Remove: []string{"gone.ttf"}})
	if b[0] != 0xff || b[1] != 0xfe {
		t.Fatal("no BOM")
	}
	u := make([]uint16, (len(b)-2)/2)
	for i := range u {
		u[i] = uint16(b[2+2*i]) | uint16(b[3+2*i])<<8
	}
	s := string(utf16.Decode(u))
	for _, want := range []string{
		"[" + Key + "]\r\n",
		"\"gone.ttf (TrueType)\"=-\r\n",
		"\"微软雅黑.ttf (TrueType)\"=\"微软雅黑.ttf\"\r\n",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
}
