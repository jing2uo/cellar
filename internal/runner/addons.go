package runner

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
)

// Wine's HTML engine (Gecko) and its .NET runtime (Mono) are add-ons, each
// Wine release pinned to one version of each. Unpacked, Wine loads them in
// place from a directory the registry names, with no install into the
// prefix; it finds one by itself only through a drive letter covering it,
// and cellar's prefixes have no Z:. Otherwise it offers to download the
// .msi when the prefix is made.
//
// Addons finds the unpacked ones a runner can use: in its own data dir
// (share/wine/{gecko,mono}), and in the Flatpak in /app/share/wine too,
// where the app mounts org.winehq.Wine's extensions (org.winehq.Wine.gecko,
// .mono) when they are installed; they serve any runner wanting the same
// version. Distributions keep theirs each in their own place and form
// (often the .msi), so on the host a runner without them gets Wine's
// download.

// Addon is one unpacked add-on: the directory name Wine expects and where
// it is on the host.
type Addon struct {
	Name string `json:"name"` // wine-gecko-2.47.4-x86_64, wine-mono-10.4.1
	Dir  string `json:"dir"`
}

type Addons struct {
	Gecko                  string // the version, which names its registry key
	Gecko64, Gecko32, Mono *Addon
}

// addonVersions reads the versions the runner wants from appwiz.cpl, which
// names the .msi files it would download (UTF-16 strings).
func (r *Runner) addonVersions() (gecko, mono string) {
	var files []string
	for _, pat := range []string{"lib*/wine/*-windows/appwiz.cpl", "lib*/*/wine/*-windows/appwiz.cpl"} {
		m, _ := filepath.Glob(filepath.Join(r.Dir, pat))
		files = append(files, m...)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if gecko, mono = parseAddonVersions(b); gecko != "" || mono != "" {
			return
		}
	}
	return "", ""
}

var addonRe = regexp.MustCompile(`wine-(gecko|mono)-([0-9]+(?:\.[0-9]+)+)`)

func parseAddonVersions(b []byte) (gecko, mono string) {
	for _, m := range addonRe.FindAllSubmatch(bytes.ReplaceAll(b, []byte{0}, nil), -1) {
		switch v := string(m[2]); string(m[1]) {
		case "gecko":
			if gecko == "" {
				gecko = v
			}
		case "mono":
			if mono == "" {
				mono = v
			}
		}
	}
	return
}

// addonRoots are the environment's data dirs for add-ons beside the
// runner's own (a var for the tests).
var addonRoots = func() []string {
	if InFlatpak() {
		return []string{"/app/share/wine"}
	}
	return nil
}

// addonDirs are where unpacked add-ons may be, by subdirectory (gecko,
// mono), best first.
func (r *Runner) addonDirs(sub string) []string {
	dirs := []string{filepath.Join(r.Dir, "share/wine", sub)}
	for _, root := range addonRoots() {
		dirs = append(dirs, filepath.Join(root, sub))
	}
	return dirs
}

func (r *Runner) findAddon(sub, name string) *Addon {
	for _, d := range r.addonDirs(sub) {
		p := filepath.Join(d, name)
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			return &Addon{Name: name, Dir: p}
		}
	}
	return nil
}

// Addons finds the runner's add-ons unpacked; a field is nil where none of
// the wanted version is.
func (r *Runner) Addons() Addons {
	var a Addons
	gecko, mono := r.addonVersions()
	if gecko != "" {
		a.Gecko = gecko
		a.Gecko64 = r.findAddon("gecko", "wine-gecko-"+gecko+"-x86_64")
		a.Gecko32 = r.findAddon("gecko", "wine-gecko-"+gecko+"-x86")
	}
	if mono != "" {
		a.Mono = r.findAddon("mono", "wine-mono-"+mono)
	}
	return a
}
