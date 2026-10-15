// Package fonts finds the font sets' fonts, under the font source policy
// (prefs): the host's copy, the one in cellar's font library, or one to
// download. It also manages the library's packs. reconcile, the GUI and
// `cellar fonts` share it.
package fonts

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jing2uo/cellar/internal/config"
	"github.com/jing2uo/cellar/internal/prefs"
	"github.com/jing2uo/cellar/internal/runner"
	"github.com/jing2uo/cellar/internal/store"
)

// Face is a resolved font. URL and Size are where Download fetches it from.
type Face struct {
	Family string `json:"family"`
	Path   string `json:"path"`
	URL    string `json:"url,omitempty"`
	Size   int64  `json:"size,omitempty"`
}

// ErrMissing means no font fills the role under the current policy.
var ErrMissing = errors.New("font not here")

// LibraryDir holds the fonts cellar downloads, shared by its prefixes.
func LibraryDir() string { return filepath.Join(store.Home(), "fonts") }

// The host's fonts by family, from one fc-list run, kept briefly: a plan
// asks for a couple of hundred.
var host struct {
	sync.Mutex
	at    time.Time
	files map[string]string // family -> file, the Regular style when there is one
}

func hostFonts() map[string]string {
	host.Lock()
	defer host.Unlock()
	if host.files != nil && time.Since(host.at) < 20*time.Second {
		return host.files
	}
	files := map[string]string{}
	regular := map[string]bool{}
	out, _ := exec.Command("fc-list", "-f", "%{family}\t%{style}\t%{file}\n").Output()
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.SplitN(l, "\t", 3)
		if len(f) != 3 {
			continue
		}
		reg := strings.Contains(","+f[1]+",", ",Regular,")
		for _, fam := range strings.Split(f[0], ",") {
			// Regular over other styles; then the first path, to be stable.
			old, ok := files[fam]
			if !ok || (reg && !regular[fam]) || (reg == regular[fam] && f[2] < old) {
				files[fam], regular[fam] = f[2], reg
			}
		}
	}
	host.files, host.at = files, time.Now()
	return files
}

// Rescan drops the kept host font list: the next lookup runs fc-list.
func Rescan() {
	host.Lock()
	host.files = nil
	host.Unlock()
}

// File finds the file of an installed family, by name (fc-match would
// follow host aliases, Debian's "Noto Sans CJK SC" -> "Noto Sans").
func File(family string) (string, error) {
	if f, ok := hostFonts()[family]; ok {
		return f, nil
	}
	return "", fmt.Errorf("font %q: %w", family, ErrMissing)
}

// Resolve finds the font for "<set>.<role>" (cjk.SC) under the policy.
// core's roles are the chosen English core fonts': Microsoft's, or
// Liberation's; the one not chosen never stands in.
func Resolve(ref string) (Face, error) {
	id, role, _ := strings.Cut(ref, ".")
	if ms, ok := config.MSCoreFor[role]; ok && id == "core" && CorePack() == "mscore" {
		return Resolve("mscore." + ms)
	}
	fs := config.FontSetByID(id)
	if fs == nil {
		return Face{}, fmt.Errorf("unknown font set %q", id)
	}
	want, ok := fs.Roles[role]
	if !ok {
		return Face{}, fmt.Errorf("font set %s has no %s font", id, role)
	}
	src := prefs.Load().Fonts.Source
	if src != prefs.LibraryOnly {
		for _, fam := range want.Host {
			if f, err := File(fam); err == nil {
				return Face{Family: fam, Path: f}, nil
			}
		}
	}
	if want.File != "" {
		f := Face{Family: want.Family, Path: filepath.Join(LibraryDir(), want.File)}
		if _, err := os.Stat(f.Path); err == nil {
			return f, nil
		}
	}
	return Face{}, fmt.Errorf("%s: %w", strings.Join(want.Host, ", "), ErrMissing)
}

// Download fetches a face's file into the library, atomically.
func Download(f Face, log func(string)) error {
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
		return err
	}
	if f.Size > 1<<20 {
		log(fmt.Sprintf("download %s (%.1f MB)", f.URL, float64(f.Size)/1048576))
	}
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

// downloadArchive fetches a .tar.gz once and puts the members it wants
// (member -> library path) into the library, each atomically.
func downloadArchive(url string, want map[string]string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download %s: %w", filepath.Base(url), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download %s: %s", filepath.Base(url), resp.Status)
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("download %s: %w", filepath.Base(url), err)
	}
	tr := tar.NewReader(gz)
	left := len(want)
	for left > 0 {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("download %s: %w", filepath.Base(url), err)
		}
		dest, ok := want[hdr.Name]
		if !ok {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		tmp := dest + ".part"
		out, err := os.Create(tmp)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			os.Remove(tmp)
			return fmt.Errorf("download %s: %w", filepath.Base(dest), err)
		}
		if err := out.Close(); err != nil {
			os.Remove(tmp)
			return err
		}
		if err := os.Rename(tmp, dest); err != nil {
			return err
		}
		left--
	}
	if left > 0 {
		return fmt.Errorf("download %s: %d file(s) not in the archive", filepath.Base(url), left)
	}
	return nil
}

// Status is what a font set uses on this machine. For all it is how many
// of the scripts have a font (Have of Total), the rest as for the others.
type Status struct {
	ID     string `json:"id"`
	Family string `json:"family,omitempty"`
	Source string `json:"source"`         // host | runtime | library | missing | scripts
	Pack   string `json:"pack,omitempty"` // the library pack that would fill it
	Have   int    `json:"have,omitempty"`
	Total  int    `json:"total,omitempty"`
}

// CorePack is the pack of the chosen English core fonts.
func CorePack() string {
	if prefs.Load().Fonts.Core == prefs.CoreLiberation {
		return "liberation"
	}
	return "mscore"
}

// Statuses resolves each choosable set's Primary.
func Statuses() []Status {
	var out []Status
	for _, fs := range config.FontSets {
		if fs.Includes != nil {
			st := Status{ID: fs.ID, Source: "scripts", Pack: "scripts"}
			for _, f := range config.PackFaces("scripts") {
				st.Total++
				if _, err := Resolve("scripts." + strings.TrimSuffix(f.File, "-Regular.ttf")); err == nil {
					st.Have++
				}
			}
			out = append(out, st)
			continue
		}
		st := Status{ID: fs.ID, Pack: fs.Roles[fs.Primary].Pack}
		if fs.ID == "core" {
			st.Pack = CorePack()
		}
		f, err := Resolve(fs.ID + "." + fs.Primary)
		switch {
		case err != nil:
			st.Source = "missing"
		default:
			st.Family, st.Source = f.Family, source(f.Path)
		}
		out = append(out, st)
	}
	return out
}

// source tells the host's fonts (/run/host in a Flatpak) from the ones the
// Flatpak runtime ships and the library's.
func source(path string) string {
	switch {
	case strings.HasPrefix(path, LibraryDir()+"/"):
		return "library"
	case strings.HasPrefix(path, "/usr/share/fonts/") && runner.InFlatpak():
		return "runtime"
	}
	return "host"
}
