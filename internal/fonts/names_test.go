package fonts

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/jing2uo/cellar/internal/fonts/fontstest"
)

func TestFamilies(t *testing.T) {
	dir := t.TempDir()
	one := filepath.Join(dir, "a.ttf")
	os.WriteFile(one, fontstest.Font("Arial"), 0o644)
	if got, err := Families(one); err != nil || !slices.Equal(got, []string{"Arial"}) {
		t.Errorf("ttf: %v %v", got, err)
	}
	ttc := filepath.Join(dir, "msyh.ttc")
	os.WriteFile(ttc, fontstest.Collection([]string{"Microsoft YaHei", "微软雅黑"}, []string{"Microsoft YaHei UI", "微软雅黑"}), 0o644)
	want := []string{"Microsoft YaHei", "微软雅黑", "Microsoft YaHei UI"}
	if got, err := Families(ttc); err != nil || !slices.Equal(got, want) {
		t.Errorf("ttc: %v %v, want %v", got, err, want)
	}
	junk := filepath.Join(dir, "x.ttf")
	os.WriteFile(junk, []byte("not a font"), 0o644)
	if _, err := Families(junk); err == nil {
		t.Error("junk read as a font")
	}
}

func TestExtrasAndWindowsFonts(t *testing.T) {
	h := t.TempDir()
	t.Setenv("CELLAR_HOME", h)
	lib := filepath.Join(h, "fonts")
	os.MkdirAll(lib, 0o755)
	os.WriteFile(filepath.Join(lib, "NotoSansCJK-Regular.ttc"), []byte("pack file"), 0o644)
	os.WriteFile(filepath.Join(lib, "Yahei.TTC"), fontstest.Collection([]string{"Microsoft YaHei"}), 0o644)
	os.WriteFile(filepath.Join(lib, "notes.txt"), nil, 0o644)
	x := Extras()
	if len(x) != 1 || x[0].File != "Yahei.TTC" || !slices.Equal(x[0].Families, []string{"Microsoft YaHei"}) {
		t.Errorf("extras %+v", x)
	}
	w := WindowsFonts()
	if w[0].File != "Yahei.TTC" || w[1].File != "" {
		t.Errorf("windows fonts %+v", w)
	}
}
