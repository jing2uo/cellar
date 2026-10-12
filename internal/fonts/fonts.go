// Package fonts finds the font sets' fonts: the host's copy, else the one
// cellar downloaded into its font cache, else a download to do. reconcile
// and the GUI's status line share it.
package fonts

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jing2uo/cellar/internal/config"
	"github.com/jing2uo/cellar/internal/runner"
	"github.com/jing2uo/cellar/internal/store"
)

// Face is a resolved font. URL is set when the file is not there yet and
// has to be downloaded to Path first.
type Face struct {
	Family string `json:"family"`
	Path   string `json:"path"`
	URL    string `json:"url,omitempty"`
	Size   int64  `json:"size,omitempty"`
}

// ErrMissing means the role's font is neither installed nor downloadable.
var ErrMissing = errors.New("font not installed")

// CacheDir holds the fonts cellar downloads, shared by all prefixes.
func CacheDir() string { return filepath.Join(store.Home(), "fonts") }

// File finds the file of an installed family. fc-list, not fc-match: host
// rules may alias a family to another (Debian's do, "Noto Sans CJK SC" ->
// "Noto Sans"), and fc-match follows them; fc-list matches the name.
func File(family string) (string, error) {
	for _, pat := range []string{family + ":style=Regular", family} {
		out, err := exec.Command("fc-list", "-f", "%{file}\n", pat).Output()
		if err != nil {
			return "", fmt.Errorf("fc-list %q: %v", family, err)
		}
		files := strings.Fields(string(out))
		if len(files) > 0 {
			sort.Strings(files)
			return files[0], nil
		}
	}
	return "", fmt.Errorf("font %q: %w", family, ErrMissing)
}

// Resolve finds the font for "<set>.<role>" (zh-hans.sans).
func Resolve(ref string) (Face, error) {
	id, role, _ := strings.Cut(ref, ".")
	fs := config.FontSetByID(id)
	if fs == nil {
		return Face{}, fmt.Errorf("unknown font set %q", id)
	}
	want, ok := fs.Roles[role]
	if !ok {
		return Face{}, fmt.Errorf("font set %s has no %s font", id, role)
	}
	for _, fam := range want.Host {
		if f, err := File(fam); err == nil {
			return Face{Family: fam, Path: f}, nil
		}
	}
	if want.File == "" {
		return Face{}, fmt.Errorf("%s: %w", strings.Join(want.Host, ", "), ErrMissing)
	}
	f := Face{Family: want.Family, Path: filepath.Join(CacheDir(), want.File)}
	if _, err := os.Stat(f.Path); err != nil {
		f.URL, f.Size = want.URL, want.Size
	}
	return f, nil
}

// Download fetches a face's file into the cache, atomically.
func Download(f Face, log func(string)) error {
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
		return err
	}
	log(fmt.Sprintf("download %s (%.1f MB)", f.URL, float64(f.Size)/1048576))
	resp, err := http.Get(f.URL)
	if err != nil {
		return fmt.Errorf("download %s: %w", filepath.Base(f.Path), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download %s: %s", filepath.Base(f.Path), resp.Status)
	}
	tmp := f.Path + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		os.Remove(tmp)
		return fmt.Errorf("download %s: %w", filepath.Base(f.Path), err)
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, f.Path)
}

// Status is what a font set would use on this machine.
type Status struct {
	ID     string   `json:"id"`
	Family string   `json:"family,omitempty"`
	Source string   `json:"source,omitempty"` // host | runtime | cache | download
	Size   int64    `json:"size,omitempty"`   // bytes to download, for every role
	Deps   []string `json:"deps,omitempty"`   // installed instead, when the font is missing
}

// Statuses resolves each set's fonts.
func Statuses() []Status {
	var out []Status
	for _, fs := range config.FontSets {
		st := Status{ID: fs.ID}
		f, err := Resolve(fs.ID + "." + fs.Primary)
		switch {
		case err != nil:
			st.Deps = fs.Deps
		case f.URL != "":
			st.Family, st.Source = f.Family, "download"
		default:
			st.Family, st.Source = f.Family, source(f.Path)
		}
		for role := range fs.Roles {
			if f, err := Resolve(fs.ID + "." + role); err == nil {
				st.Size += f.Size
			}
		}
		out = append(out, st)
	}
	return out
}

// source tells the host's fonts (/run/host in a Flatpak) from the ones the
// Flatpak runtime ships and the ones cellar downloaded.
func source(path string) string {
	switch {
	case strings.HasPrefix(path, CacheDir()+"/"):
		return "cache"
	case strings.HasPrefix(path, "/usr/share/fonts/") && runner.InFlatpak():
		return "runtime"
	}
	return "host"
}
