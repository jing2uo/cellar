// Package desktop writes host launchers (.desktop files) for prefix apps,
// the way Bottles does: Exec runs `cellar run`, so launching never needs the
// GUI.
package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jing2uo/cellar/internal/config"
	"github.com/jing2uo/cellar/internal/runner"
	"github.com/jing2uo/cellar/internal/store"
)

// Dir is the host's menu directory. Inside the Flatpak XDG_DATA_HOME is the
// app's own data dir, so the host's is derived from $HOME (the Flatpak has
// xdg-data/applications granted for this).
func Dir() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" || runner.InFlatpak() {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local/share")
	}
	return filepath.Join(base, "applications")
}

func FileName(prefix, app string) string {
	return fmt.Sprintf("cellar-%s-%s.desktop", prefix, app)
}

// WMClass guesses the X11 class Wine gives a program's windows: the
// executable name, lower-cased.
func WMClass(exe string) string {
	i := strings.LastIndexAny(exe, `\/`)
	return strings.ToLower(exe[i+1:])
}

// Entry renders the launcher. cellarBin is the absolute path to call back.
func Entry(cellarBin string, p *store.Prefix, a *config.App) string {
	name := a.Name
	if name == "" {
		name = a.ID
	}
	class := a.WMClass
	if class == "" {
		class = WMClass(a.Exe)
	}
	icon := "wine"
	if a.Icon != "" {
		icon = filepath.Join(p.Dir, a.Icon)
	}
	files := "%f"
	switch id := os.Getenv("FLATPAK_ID"); {
	case runner.InFlatpak() && id != "":
		// Launch through flatpak; --file-forwarding hands opened files in
		// through the document portal, as Bottles' launchers do.
		cellarBin = "flatpak run --command=cellar --file-forwarding " + id
		files = "@@ %f @@"
		if h := os.Getenv("CELLAR_HOME"); h != "" {
			cellarBin = "flatpak run --command=cellar --file-forwarding --env=CELLAR_HOME=" + quote(h) + " " + id
		}
	case os.Getenv("CELLAR_HOME") != "":
		cellarBin = "env CELLAR_HOME=" + quote(os.Getenv("CELLAR_HOME")) + " " + quote(cellarBin)
	default:
		cellarBin = quote(cellarBin)
	}
	return fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=%s
Comment=%s in cellar prefix %s
Exec=%s run %s/%s -- %s
Icon=%s
Terminal=false
StartupWMClass=%s
Categories=Utility;X-Wine;
X-Cellar-Prefix=%s
X-Cellar-App=%s
`, name, name, p.Name, cellarBin, p.Name, a.ID, files, icon, class, p.Name, a.ID)
}

// ErrPackaged: a FlatPark package exports its own .desktop at build time;
// cellar never writes host menus from inside one.
var ErrPackaged = fmt.Errorf("packaged app: launchers come from the Flatpak's exported .desktop file")

func Install(cellarBin string, p *store.Prefix, a *config.App) (string, error) {
	if p.Packaged {
		return "", ErrPackaged
	}
	path := filepath.Join(Dir(), FileName(p.Name, a.ID))
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, []byte(Entry(cellarBin, p, a)), 0o644)
}

func Remove(p *store.Prefix, app string) (string, error) {
	path := filepath.Join(Dir(), FileName(p.Name, app))
	return path, os.Remove(path)
}

func quote(s string) string {
	if strings.ContainsAny(s, " \t\"'\\") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}
