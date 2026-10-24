package runner

import (
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"
)

func utf16le(s string) []byte {
	var b []byte
	for _, u := range utf16.Encode([]rune(s)) {
		b = append(b, byte(u), byte(u>>8))
	}
	return b
}

func mkdirs(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestParseAddonVersions(t *testing.T) {
	b := append(utf16le("x\x00wine-mono-10.4.1-x86.msi\x00"), utf16le("wine-gecko-2.47.4-x86_64.msi")...)
	if g, m := parseAddonVersions(b); g != "2.47.4" || m != "10.4.1" {
		t.Errorf("got gecko %q mono %q", g, m)
	}
}

func TestFlatpakWines(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	apps := filepath.Join(home, ".local/share/flatpak/app/org.winehq.Wine", flatpakArch())
	mkdirs(t, filepath.Join(apps, "stable-25.08/active/files"), filepath.Join(apps, "stable-26.08/active/files"))
	got := flatpakWines()
	if len(got) != 2 || filepath.Base(filepath.Dir(filepath.Dir(got[0]))) != "stable-26.08" {
		t.Fatalf("got %v", got)
	}
	env := runnerEnv(got[0])
	if env["LD_LIBRARY_PATH"] == "" || filepath.Dir(filepath.SplitList(env["PATH"])[0]) != got[0] {
		t.Errorf("env %v", env)
	}
	if runnerEnv("/opt/wine-staging") != nil {
		t.Error("env for a non-flatpak runner")
	}
}

func TestAddons(t *testing.T) {
	root := t.TempDir()
	defer func(f func() []string) { addonRoots = f }(addonRoots)
	addonRoots = func() []string { return []string{root} }
	dir := filepath.Join(t.TempDir(), "wine")
	mkdirs(t, filepath.Join(dir, "lib/wine/x86_64-windows"))
	os.WriteFile(filepath.Join(dir, "lib/wine/x86_64-windows/appwiz.cpl"), utf16le("wine-gecko-2.47.4-x86_64.msi wine-mono-10.4.1-x86.msi"), 0o644)
	ext := filepath.Join(root, "gecko")
	// The runner's own data dir wins; Mono of another version does not count.
	mkdirs(t, filepath.Join(ext, "wine-gecko-2.47.4-x86_64"), filepath.Join(ext, "wine-gecko-2.47.4-x86"),
		filepath.Join(dir, "share/wine/gecko/wine-gecko-2.47.4-x86_64"), filepath.Join(dir, "share/wine/mono/wine-mono-9.0.0"))
	r := &Runner{Dir: dir}
	a := r.Addons()
	if a.Gecko != "2.47.4" || a.Gecko64 == nil || a.Gecko64.Dir != filepath.Join(dir, "share/wine/gecko/wine-gecko-2.47.4-x86_64") {
		t.Errorf("gecko64 %+v", a.Gecko64)
	}
	if a.Gecko32 == nil || a.Gecko32.Dir != filepath.Join(ext, "wine-gecko-2.47.4-x86") {
		t.Errorf("gecko32 %+v", a.Gecko32)
	}
	if a.Mono != nil {
		t.Errorf("mono %+v", a.Mono)
	}
}
