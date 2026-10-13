package fonts

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/jing2uo/cellar/internal/config"
	"github.com/jing2uo/cellar/internal/prefs"
)

// PackStatus is a library pack: how much of it is downloaded, how much the
// host already has, and what is left to download: what neither has, or
// under library-only everything not downloaded (host fonts do not count).
type PackStatus struct {
	config.Pack
	Size     int64 `json:"size"`  // all its files
	Files    int   `json:"files"` //
	Have     int   `json:"have"`  // downloaded
	Host     int   `json:"host"`  // families the host has
	Missing  int   `json:"missing"`
	MissSize int64 `json:"missing_size"`
}

// needed lists a pack's faces the library still has to get under the
// policy: not downloaded and, unless it is library-only, not on the host.
func needed(id string) []config.Face {
	useHost := prefs.Load().Fonts.Source != prefs.LibraryOnly
	var out []config.Face
	for _, f := range config.PackFaces(id) {
		if _, err := os.Stat(filepath.Join(LibraryDir(), f.File)); err == nil {
			continue
		}
		if useHost && onHost(f.File) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// onHost reports whether the host has a pack file's font: any family in it.
func onHost(file string) bool {
	hf := hostFonts()
	for _, fam := range config.FileFamilies()[file] {
		if _, ok := hf[fam]; ok {
			return true
		}
	}
	return false
}

func Packs() []PackStatus {
	var out []PackStatus
	for _, p := range config.Packs {
		st := PackStatus{Pack: p}
		for _, f := range config.PackFaces(p.ID) {
			st.Files++
			st.Size += f.Size
			if _, err := os.Stat(filepath.Join(LibraryDir(), f.File)); err == nil {
				st.Have++
			}
			if onHost(f.File) {
				st.Host++
			}
		}
		for _, f := range needed(p.ID) {
			st.Missing++
			st.MissSize += f.Size
		}
		out = append(out, st)
	}
	return out
}

// GetPack downloads what the pack still needs under the policy (needed),
// a few at a time. progress gets each file as it lands; a failed file is
// reported and the rest go on.
func GetPack(id string, progress func(done, total int, file string)) error {
	if config.PackByID(id) == nil {
		return fmt.Errorf("no font pack %q", id)
	}
	faces := needed(id)
	var mu sync.Mutex
	var failed []string
	var firstErr error
	done := 0
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	// Faces packed in one archive come out of a single download.
	archives := map[string][]config.Face{}
	for _, f := range faces {
		if f.Member != "" {
			archives[f.URL] = append(archives[f.URL], f)
		}
	}
	for url, group := range archives {
		want := map[string]string{}
		for _, f := range group {
			want[f.Member] = filepath.Join(LibraryDir(), f.File)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			err := downloadArchive(url, want)
			mu.Lock()
			defer mu.Unlock()
			for _, f := range group {
				if err != nil {
					failed = append(failed, f.File)
					continue
				}
				done++
				if progress != nil {
					progress(done, len(faces), f.File)
				}
			}
			if err != nil && firstErr == nil {
				firstErr = err
			}
		}()
	}
	for _, f := range faces {
		if f.Member != "" {
			continue
		}
		path := filepath.Join(LibraryDir(), f.File)
		if _, err := os.Stat(path); err == nil {
			mu.Lock()
			done++
			mu.Unlock()
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			err := Download(Face{Family: f.Family, Path: path, URL: f.URL, Size: f.Size}, func(string) {})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed = append(failed, f.File)
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			done++
			if progress != nil {
				progress(done, len(faces), f.File)
			}
		}()
	}
	wg.Wait()
	if len(failed) > 0 {
		return fmt.Errorf("%d of %d files failed (%v); run it again to retry them", len(failed), len(faces), firstErr)
	}
	return nil
}

// RemovePack deletes the pack's files from the library. Prefixes keep the
// copies they have.
func RemovePack(id string) error {
	faces := config.PackFaces(id)
	if len(faces) == 0 {
		return fmt.Errorf("no font pack %q", id)
	}
	for _, f := range faces {
		if err := os.Remove(filepath.Join(LibraryDir(), f.File)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
