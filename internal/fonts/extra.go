package fonts

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jing2uo/cellar/internal/config"
)

// Extra is a font put into the library by hand (any file the packs do not
// bring). Every prefix with a font set gets it, and the font sets stop
// substituting its families: a real Microsoft YaHei dropped in wins over
// the Noto it otherwise stands for.
type Extra struct {
	File     string   `json:"file"`
	Path     string   `json:"path"`
	Families []string `json:"families"`
}

// Extras lists the library's extra fonts; one whose names cannot be read
// is still listed, with none.
func Extras() []Extra {
	ents, err := os.ReadDir(LibraryDir())
	if err != nil {
		return nil
	}
	packed := config.FileFamilies()
	var out []Extra
	for _, e := range ents {
		name := e.Name()
		switch strings.ToLower(filepath.Ext(name)) {
		case ".ttf", ".ttc", ".otf", ".otc":
		default:
			continue
		}
		if _, ok := packed[name]; ok || e.IsDir() {
			continue
		}
		x := Extra{File: name, Path: filepath.Join(LibraryDir(), name)}
		x.Families, _ = Families(x.Path)
		out = append(out, x)
	}
	return out
}

// WindowsFont is a Windows font programs ask for by name that no free font
// can be: whether the library has it (File), else what stands in for it.
type WindowsFont struct {
	Name     string   `json:"name"`
	Families []string `json:"families"`
	File     string   `json:"file,omitempty"`
}

// WindowsFonts checks the library for Microsoft YaHei and SimSun. Without
// them cjk maps their names to Noto Sans CJK, which looks the same to most
// programs but not to ones that check the font itself.
func WindowsFonts() []WindowsFont {
	out := []WindowsFont{
		{Name: "微软雅黑", Families: []string{"Microsoft YaHei", "Microsoft YaHei UI", "微软雅黑"}},
		{Name: "宋体", Families: []string{"SimSun", "NSimSun", "宋体", "新宋体"}},
	}
	extras := Extras()
	for i := range out {
		for _, x := range extras {
			if slices.ContainsFunc(x.Families, func(f string) bool {
				return slices.ContainsFunc(out[i].Families, func(w string) bool { return strings.EqualFold(f, w) })
			}) {
				out[i].File = x.File
				break
			}
		}
	}
	return out
}
