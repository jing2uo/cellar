package runner

import (
	"os"
	"os/exec"
	"path/filepath"
)

// InFlatpak reports whether cellar runs inside a Flatpak sandbox.
func InFlatpak() bool {
	_, err := os.Stat("/.flatpak-info")
	return err == nil
}

// systemDirs lists where a distribution or vendor Wine may live, best first.
// In a Flatpak the only one is the base app's (org.winehq.Wine) under /app;
// on the host it is whatever wine is on PATH, then WineHQ's /opt packages.
func systemDirs() []string {
	if InFlatpak() {
		return []string{"/app"}
	}
	var dirs []string
	if w, err := exec.LookPath("wine"); err == nil {
		if real, err := filepath.EvalSymlinks(w); err == nil {
			w = real
		}
		dirs = append(dirs, filepath.Dir(filepath.Dir(w)))
	}
	for _, d := range []string{"/opt/wine-staging", "/opt/wine-devel", "/opt/wine-stable", "/usr"} {
		dirs = append(dirs, d)
	}
	seen := map[string]bool{}
	var out []string
	for _, d := range dirs {
		if seen[d] {
			continue
		}
		seen[d] = true
		if fi, err := os.Stat(filepath.Join(d, "bin", "wine")); err == nil && !fi.IsDir() {
			out = append(out, d)
		}
	}
	return out
}

// Kind tells WoW64 builds, whose unix side is x86_64 only, from legacy
// builds that also need a 32-bit unix side (i386 libraries and loader).
func kindOf(dir string) string {
	for _, p := range []string{"lib/wine/i386-unix", "lib32/wine", "lib/i386-linux-gnu/wine", "lib/wine/i386-linux-gnu"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err == nil {
			return "legacy"
		}
	}
	return "wow64"
}

// problems lists what keeps a runner from working here, with the fix.
// Legacy builds load a 32-bit unix side, so they need the 32-bit loader.
// In the Flatpak /lib/ld-linux.so.2 resolves through /app/lib/i386-linux-gnu
// only when the Compat.i386 extension is installed; on the host it comes
// with i386 multiarch.
func problems(kind string) []string {
	if kind != "legacy" {
		return nil
	}
	if _, err := os.Stat("/lib/ld-linux.so.2"); err == nil {
		return nil
	}
	if InFlatpak() {
		return []string{"legacy (non-WoW64) build needs the 32-bit runtime: " +
			"flatpak install flathub org.freedesktop.Platform.Compat.i386//26.08 org.freedesktop.Platform.GL32.default//26.08"}
	}
	return []string{"legacy (non-WoW64) build needs 32-bit host libraries: " +
		"enable i386 multiarch (Debian: dpkg --add-architecture i386 && apt install libc6:i386 ...) or use a -wow64 build"}
}
