package fonts

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jing2uo/cellar/internal/runner"
)

// downloadCabinet fetches a self-extracting cabinet (Microsoft's core
// fonts come as these), checks it against sum, unpacks it with cabextract
// (7z if that is missing) and puts the members it wants (member -> library
// path; names in any case) into the library.
func downloadCabinet(url, sum string, want map[string]string) error {
	name := filepath.Base(url)
	if err := os.MkdirAll(LibraryDir(), 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(LibraryDir(), ".cab-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	exe := filepath.Join(tmp, name)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download %s: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download %s: %s", name, resp.Status)
	}
	out, err := os.Create(exe)
	if err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(out, h), resp.Body); err != nil {
		out.Close()
		return fmt.Errorf("download %s: %w", name, err)
	}
	if err := out.Close(); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); sum != "" && got != sum {
		return fmt.Errorf("download %s: checksum %s, want %s", name, got, sum)
	}
	dir := filepath.Join(tmp, "x")
	var cmd *exec.Cmd
	if p, err := exec.LookPath("cabextract"); err == nil {
		cmd = exec.Command(p, "-q", "-d", dir, exe)
	} else if p, err := runner.SevenZip(); err == nil {
		cmd = exec.Command(p, "e", "-y", "-o"+dir, exe)
	} else {
		return fmt.Errorf("%s: unpacking needs cabextract or 7z", name)
	}
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("unpack %s: %v\n%s", name, err, strings.TrimSpace(string(b)))
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	left := len(want)
	for _, e := range ents {
		dest, ok := want[strings.ToLower(e.Name())]
		if !ok {
			continue
		}
		if err := copyFile(filepath.Join(dir, e.Name()), dest); err != nil {
			return err
		}
		left--
	}
	if left > 0 {
		return fmt.Errorf("unpack %s: %d file(s) not in it", name, left)
	}
	return nil
}

// copyFile copies into the library atomically.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
