package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestMergeUpstream(t *testing.T) {
	base := `font_sets = ["cjk"]

[install]
source = "app-1.0.exe"
method = "run"
args = ["/S"]

[drives]
h = "~"

[[apps]]
id = "app"
exe = 'C:\Program Files\App\app.exe'
`
	// The user: other font sets, a DPI, their own args, a comment.
	user := `# mine
font_sets = ["core", "cjk"]  # both
dpi = 120

[install]
source = "app-1.0.exe"
method = "run"
args = ["/S", "/D=C:\\App"]

[drives]
h = "~"

[[apps]]
id = "app"
exe = 'C:\Program Files\App\app.exe'
`
	// The update: a new installer and args, a moved exe, a Windows
	// version, no H: drive.
	next := `font_sets = ["cjk"]
windows_version = "win7"

[install]
source = "app-2.0.exe"
method = "run"
args = ["/S", "/NCRC"]

[[apps]]
id = "app"
exe = 'C:\Program Files\App2\app.exe'
`
	p := filepath.Join(t.TempDir(), "cellar.toml")
	os.WriteFile(p, []byte(user), 0o644)
	conflicts, err := MergeUpstream(p, []byte(base), []byte(next))
	if err != nil {
		t.Fatal(err)
	}
	// args changed on both sides: the user's stays, and it is said.
	if !slices.Equal(conflicts, []string{"install.args"}) {
		t.Errorf("conflicts %v", conflicts)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(c.FontSets, []string{"core", "cjk"}) || c.DPI != 120 || c.WindowsVersion != "win7" {
		t.Errorf("sets %v dpi %d winver %q", c.FontSets, c.DPI, c.WindowsVersion)
	}
	if c.Install.Source != "app-2.0.exe" || !slices.Equal(c.Install.Args, []string{"/S", `/D=C:\App`}) {
		t.Errorf("install %+v", c.Install)
	}
	if _, ok := c.Drives["h"]; ok || c.Apps[0].Exe != `C:\Program Files\App2\app.exe` {
		t.Errorf("drives %v apps %+v", c.Drives, c.Apps)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "# mine") || !strings.Contains(string(b), "# both") {
		t.Errorf("comments lost:\n%s", b)
	}
}
