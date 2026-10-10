package winapp

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Program is a launchable program found in a prefix.
type Program struct {
	Name    string `json:"name"`
	Exe     string `json:"exe"` // Windows path
	Args    string `json:"args,omitempty"`
	Workdir string `json:"workdir,omitempty"`
	From    string `json:"from"` // start-menu | scan
	depth   int
	size    int64
}

// skipName drops uninstallers, updaters and crash reporters by substring:
// vendors glue the words into names like TdxUnInstall or TxBugReport.
var skipName = regexp.MustCompile(`(?i)(uninst|unins\d|卸载|setup|install|update|upgrade|crash|report|helper|repair|launcherhelper|elevat|service|daemon|host$)`)

// Discover lists the programs installed in a prefix: Start Menu shortcuts
// first (what the installer meant to be launched), then executables under
// Program Files not already covered.
func Discover(pfx string) []Program {
	driveC := filepath.Join(pfx, "drive_c")
	var out []Program
	seen := map[string]bool{}
	add := func(p Program) {
		k := strings.ToLower(p.Exe)
		if seen[k] || skipName.MatchString(p.Name) || skipName.MatchString(filepath.Base(winToSlash(p.Exe))) {
			return
		}
		seen[k] = true
		out = append(out, p)
	}

	var menus []string
	menus = append(menus, filepath.Join(driveC, "ProgramData/Microsoft/Windows/Start Menu"))
	users, _ := filepath.Glob(filepath.Join(driveC, "users/*/AppData/Roaming/Microsoft/Windows/Start Menu"))
	menus = append(menus, users...)
	users, _ = filepath.Glob(filepath.Join(driveC, "users/*/Desktop"))
	menus = append(menus, users...)
	for _, m := range menus {
		filepath.WalkDir(m, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".lnk") {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			s, err := ParseLnk(b)
			if err != nil || !strings.EqualFold(filepath.Ext(winToSlash(s.Target)), ".exe") {
				return nil
			}
			add(Program{Name: strings.TrimSuffix(d.Name(), filepath.Ext(d.Name())), Exe: s.Target, Args: s.Args, Workdir: s.Workdir, From: "start-menu"})
			return nil
		})
	}

	// Installers without shortcuts, and portable apps unpacked anywhere in
	// drive_c (e.g. C:\zd_xxx for the Tongdaxin family). Shallow first: the
	// main program usually sits at the top of its folder.
	roots := []string{"Program Files", "Program Files (x86)"}
	if ents, err := os.ReadDir(driveC); err == nil {
		for _, e := range ents {
			switch strings.ToLower(e.Name()) {
			case "windows", "users", "programdata", "program files", "program files (x86)":
				continue
			}
			if e.IsDir() {
				roots = append(roots, e.Name())
			}
		}
	}
	var scanned []Program
	for _, root := range roots {
		base := filepath.Join(driveC, root)
		filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(base, path)
			depth := strings.Count(rel, string(filepath.Separator))
			if d.IsDir() {
				if depth >= 2 || isWineDir(rel) {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.EqualFold(filepath.Ext(path), ".exe") {
				return nil
			}
			win := `C:\` + strings.ReplaceAll(filepath.Join(root, rel), "/", `\`)
			var size int64
			if fi, err := d.Info(); err == nil {
				size = fi.Size()
			}
			scanned = append(scanned, Program{Name: strings.TrimSuffix(d.Name(), filepath.Ext(d.Name())), Exe: win, From: "scan", depth: depth, size: size})
			return nil
		})
	}
	// The main program is usually the biggest executable at its level.
	sort.SliceStable(scanned, func(i, j int) bool {
		if scanned[i].depth != scanned[j].depth {
			return scanned[i].depth < scanned[j].depth
		}
		return scanned[i].size > scanned[j].size
	})
	for _, p := range scanned {
		add(p)
	}
	return out
}

// isWineDir skips what wineboot itself puts under Program Files.
func isWineDir(rel string) bool {
	top := strings.ToLower(strings.SplitN(rel, string(filepath.Separator), 2)[0])
	switch top {
	case "common files", "internet explorer", "windows media player", "windows nt", "windows photo viewer", "windowspowershell", "msbuild", "reference assemblies":
		return true
	}
	return false
}

func winToSlash(p string) string { return strings.ReplaceAll(p, `\`, "/") }

// UnixPath maps a C:\ path into the prefix.
func UnixPath(pfx, win string) string {
	if len(win) >= 2 && win[1] == ':' {
		return filepath.Join(pfx, "dosdevices", strings.ToLower(win[:1])+":", winToSlash(strings.TrimLeft(win[2:], `\/`)))
	}
	return win
}

// WinPath maps a path inside the prefix's drive_c to C:\...; "" if outside.
func WinPath(pfx, unix string) string {
	driveC := filepath.Join(pfx, "drive_c") + "/"
	if rest, ok := strings.CutPrefix(unix, driveC); ok {
		return `C:\` + strings.ReplaceAll(rest, "/", `\`)
	}
	return ""
}
