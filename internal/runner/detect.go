package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// InFlatpak reports whether cellar runs inside a Flatpak sandbox.
func InFlatpak() bool {
	_, err := os.Stat("/.flatpak-info")
	return err == nil
}

// systemDirs lists where a distribution or vendor Wine may live, best first.
// In a Flatpak these are a Wine base under /app (packaged Wine apps keep
// org.winehq.Wine as theirs), then org.winehq.Wine as the user installed it
// with flatpak, read-only through the app's filesystem permissions. On the
// host it is whatever wine is on PATH, then WineHQ's /opt packages.
func systemDirs() []string {
	var dirs []string
	if InFlatpak() {
		dirs = append([]string{"/app"}, flatpakWines()...)
	} else {
		if w, err := exec.LookPath("wine"); err == nil {
			if real, err := filepath.EvalSymlinks(w); err == nil {
				w = real
			}
			dirs = append(dirs, filepath.Dir(filepath.Dir(w)))
		}
		dirs = append(dirs, "/opt/wine-staging", "/opt/wine-devel", "/opt/wine-stable", "/usr")
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

// flatpakRoots are the user's and the system's flatpak installations. The
// user's is the real ~/.local/share/flatpak: inside the sandbox
// XDG_DATA_HOME points at the app's own data dir.
func flatpakRoots() []string {
	home, _ := os.UserHomeDir()
	return []string{filepath.Join(home, ".local/share/flatpak"), "/var/lib/flatpak"}
}

func flatpakArch() string {
	return map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH]
}

// flatpakWines lists the installed branches of org.winehq.Wine, newest
// first, the user's installation before the system's for the same branch.
// The path goes through active/, which a flatpak update repoints, so a
// prefix keeps its runner across updates.
func flatpakWines() []string {
	type wine struct {
		dir, branch string
		root        int
	}
	var ws []wine
	for i, root := range flatpakRoots() {
		m, _ := filepath.Glob(filepath.Join(root, "app/org.winehq.Wine", flatpakArch(), "*/active/files"))
		for _, d := range m {
			ws = append(ws, wine{d, filepath.Base(filepath.Dir(filepath.Dir(d))), i})
		}
	}
	sort.SliceStable(ws, func(i, j int) bool {
		if ws[i].branch != ws[j].branch {
			return ws[i].branch > ws[j].branch
		}
		return ws[i].root < ws[j].root
	})
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = w.dir
	}
	return out
}

// flatpakWine tells whether dir is an org.winehq.Wine installed with
// flatpak and run from outside its own sandbox.
func flatpakWine(dir string) bool {
	return strings.Contains(dir, "/flatpak/app/org.winehq.Wine/")
}

// runnerEnv is what a runner needs in its environment beyond the prefix's.
// org.winehq.Wine keeps libraries the runtime lacks in its own lib/ (krb5,
// unixODBC; samba's under lib/samba, for ntlm_auth), found through
// /app/lib in its own sandbox but not from here; its bin/ has ntlm_auth.
func runnerEnv(dir string) map[string]string {
	if !flatpakWine(dir) {
		return nil
	}
	return map[string]string{
		"LD_LIBRARY_PATH": prepend(os.Getenv("LD_LIBRARY_PATH"), filepath.Join(dir, "lib"), filepath.Join(dir, "lib/samba")),
		"PATH":            prepend(os.Getenv("PATH"), filepath.Join(dir, "bin")),
	}
}

func prepend(list string, dirs ...string) string {
	if list != "" {
		dirs = append(dirs, list)
	}
	return strings.Join(dirs, ":")
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
// FlatpakI386 installs the 32-bit runtime the Flatpak's extension points take.
const FlatpakI386 = "flatpak install flathub org.freedesktop.Platform.Compat.i386//26.08 org.freedesktop.Platform.GL32.default//26.08"

// Legacy32 tells whether legacy (non-WoW64) runners can run here.
func Legacy32() bool { return len(problems("legacy")) == 0 }

func problems(kind string) []string {
	if kind != "legacy" {
		return nil
	}
	if _, err := os.Stat("/lib/ld-linux.so.2"); err == nil {
		return nil
	}
	if InFlatpak() {
		return []string{"legacy (non-WoW64) build needs the 32-bit runtime: " + FlatpakI386}
	}
	return []string{"legacy (non-WoW64) build needs 32-bit host libraries: " +
		"enable i386 multiarch (Debian: dpkg --add-architecture i386 && apt install libc6:i386 ...) or use a -wow64 build"}
}
