package reconcile

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jing2uo/cellar/internal/config"
	"github.com/jing2uo/cellar/internal/store"
)

// ExtraDir is where a FlatPark package's extra-data lands; Unpacked is the
// folder `cellar extract` unpacks an extract-method installer into.
func ExtraDir() string {
	if v := os.Getenv("CELLAR_EXTRA"); v != "" { // for testing outside a package
		return v
	}
	return "/app/extra"
}

func Unpacked() string { return filepath.Join(ExtraDir(), "cellar-install") }

// UnpackedSource holds the fingerprint of the installer `cellar extract`
// unpacked; the installer itself is deleted afterwards.
func UnpackedSource() string { return Unpacked() + ".source" }

// Fingerprint is exported for `cellar extract`.
func Fingerprint(path string) (string, error) { return fingerprint(path) }

// sourcePath is the installer file: under /app/extra in a package, else
// relative to the prefix directory.
func sourcePath(p *store.Prefix, in *config.Install) string {
	if filepath.IsAbs(in.Source) {
		return in.Source
	}
	if p.Packaged {
		return filepath.Join(ExtraDir(), in.Source)
	}
	return filepath.Join(p.Dir, in.Source)
}

// Unpack extracts an installer (NSIS / 7z SFX / Inno-less exe, .msi, zip...)
// with 7z into dst, atomically. 7z ships with the Wine base app; the
// vendor's file is not changed.
func Unpack(src, dst, subdir string) error {
	sz, err := exec.LookPath("7z")
	if err != nil {
		return errors.New("7z not found (the org.winehq.Wine base ships it)")
	}
	tmp := dst + ".tmp"
	os.RemoveAll(tmp)
	out, err := exec.Command(sz, "x", "-y", "-bd", "-o"+tmp, src).CombinedOutput()
	if err != nil {
		os.RemoveAll(tmp)
		return fmt.Errorf("7z x %s: %v\n%s", filepath.Base(src), err, tail(string(out), 15))
	}
	from := tmp
	if subdir != "" {
		from = filepath.Join(tmp, filepath.FromSlash(subdir))
		if _, err := os.Stat(from); err != nil {
			os.RemoveAll(tmp)
			return fmt.Errorf("archive has no %q", subdir)
		}
	}
	os.RemoveAll(dst)
	if err := os.Rename(from, dst); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	os.RemoveAll(tmp)
	return nil
}

// fingerprint identifies the installer file by content: Flatpak gives
// deployed files a fixed mtime, so size and time say nothing.
func fingerprint(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

// installChange works out the install step, if one is due.
func (pl *Plan) installChange() (*Change, error) {
	in := pl.c.Install
	if in == nil {
		return nil, nil
	}
	src := sourcePath(pl.p, in)
	switch in.Method {
	case "extract":
		if in.Target == "" {
			return nil, errors.New("install: extract needs target (a Windows folder)")
		}
		dst := winToUnix(pl.p.Pfx, in.Target)
		if pl.p.Packaged && !in.Writable {
			// Unpacked once at install time; link the user's prefix to it.
			if cur, err := os.Readlink(dst); err == nil && cur == Unpacked() {
				return nil, nil
			}
			return &Change{Op: "install", Name: "link", Key: in.Target, Value: Unpacked()}, nil
		}
		if pl.p.Packaged {
			// A per-user copy, refreshed when the package brings a new
			// installer.
			b, err := os.ReadFile(UnpackedSource())
			if err != nil {
				return nil, fmt.Errorf("install: %s missing: the package's apply_extra did not unpack the installer", UnpackedSource())
			}
			fp := strings.TrimSpace(string(b))
			if isDir(dst) && pl.st.Install == fp {
				return nil, nil
			}
			return &Change{Op: "install", Name: "copy", Key: in.Target, Value: Unpacked(), Old: fp}, nil
		}
		fp, err := fingerprint(src)
		if err != nil {
			return nil, fmt.Errorf("install source: %w", err)
		}
		if isDir(dst) && pl.st.Install == fp {
			return nil, nil
		}
		return &Change{Op: "install", Name: "extract", Key: in.Target, Value: src, Old: fp}, nil
	case "run":
		fp, err := fingerprint(src)
		if err != nil {
			return nil, fmt.Errorf("install source: %w", err)
		}
		if pl.st.Install == fp {
			return nil, nil
		}
		return &Change{Op: "install", Name: "run", Value: src + " " + strings.Join(in.Args, " "), Old: fp}, nil
	}
	return nil, fmt.Errorf("install: method %q (want extract or run)", in.Method)
}

func winToUnix(pfx, win string) string {
	if len(win) < 2 || win[1] != ':' {
		return filepath.Join(pfx, "drive_c", strings.ReplaceAll(win, `\`, "/"))
	}
	rest := strings.ReplaceAll(strings.TrimLeft(win[2:], `\/`), `\`, "/")
	return filepath.Join(pfx, "dosdevices", strings.ToLower(win[:1])+":", rest)
}

// doInstall performs it; returns the fingerprint to record.
func (pl *Plan) doInstall(c *Change, run func(exe string, args []string) error, log func(string)) (string, error) {
	in := pl.c.Install
	switch c.Name {
	case "link":
		dst := winToUnix(pl.p.Pfx, in.Target)
		if _, err := os.Stat(Unpacked()); err != nil {
			return "", fmt.Errorf("%s missing: the package's apply_extra did not unpack the installer", Unpacked())
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", err
		}
		os.RemoveAll(dst)
		log("link " + in.Target + " -> " + Unpacked())
		return pl.st.Install, os.Symlink(Unpacked(), dst)
	case "extract":
		dst := winToUnix(pl.p.Pfx, in.Target)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", err
		}
		log("extract " + filepath.Base(c.Value) + " -> " + in.Target)
		tmp := dst + ".cellar-unpack"
		if err := Unpack(c.Value, tmp, in.Subdir); err != nil {
			return "", err
		}
		defer os.RemoveAll(tmp)
		return c.Old, refresh(tmp, dst, in.Keep, log)
	case "copy":
		dst := winToUnix(pl.p.Pfx, in.Target)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", err
		}
		log("copy " + Unpacked() + " -> " + in.Target)
		return c.Old, refresh(Unpacked(), dst, in.Keep, log)
	case "run":
		log("run installer " + filepath.Base(sourcePath(pl.p, in)) + " " + strings.Join(in.Args, " "))
		return c.Old, run(sourcePath(pl.p, in), in.Args)
	}
	return "", nil
}

func isDir(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.IsDir()
}

// refresh replaces dst with a copy of src, moving the keep entries the
// program wrote into the old dst across. The swap is a rename, so a failed
// copy leaves the old files in place.
func refresh(src, dst string, keep []string, log func(string)) error {
	next := dst + ".cellar-new"
	old := dst + ".cellar-old"
	os.RemoveAll(next)
	os.RemoveAll(old)
	if err := copyTree(src, next); err != nil {
		os.RemoveAll(next)
		return err
	}
	if isDir(dst) {
		for _, k := range keep {
			k = filepath.Clean(filepath.FromSlash(k))
			if filepath.IsAbs(k) || strings.HasPrefix(k, "..") {
				continue
			}
			from := filepath.Join(dst, k)
			if _, err := os.Lstat(from); err != nil {
				continue
			}
			to := filepath.Join(next, k)
			os.RemoveAll(to)
			os.MkdirAll(filepath.Dir(to), 0o755)
			if err := os.Rename(from, to); err != nil {
				os.RemoveAll(next)
				return fmt.Errorf("keep %s: %w", k, err)
			}
			log("keep " + k)
		}
	}
	if _, err := os.Lstat(dst); err == nil {
		if err := os.Rename(dst, old); err != nil {
			os.RemoveAll(next)
			return err
		}
	}
	if err := os.Rename(next, dst); err != nil {
		os.Rename(old, dst)
		return err
	}
	return os.RemoveAll(old)
}

// copyTree copies files with io.Copy, which uses copy_file_range: a
// reflink on btrfs when source and target share the filesystem.
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		to := filepath.Join(dst, rel)
		switch {
		case fi.Mode()&os.ModeSymlink != 0:
			l, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(l, to)
		case fi.IsDir():
			return os.MkdirAll(to, 0o755)
		case fi.Mode().IsRegular():
			in, err := os.Open(path)
			if err != nil {
				return err
			}
			defer in.Close()
			// Writable for the user even though /app/extra is read-only.
			out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fi.Mode().Perm()|0o600)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, in); err != nil {
				out.Close()
				return err
			}
			return out.Close()
		}
		return nil
	})
}
