package regfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "user.reg")
	os.WriteFile(path, []byte(`WINE REGISTRY Version 2

[Software\\Wine\\Explorer] 1792052398
"Desktop"="Default"

[Software\\Wine\\Explorer\\Desktops] 1792052398
#time=1dd5c7df77bc8ec
"Default"="800x600"
"q\"x"="a\\b"
"Bin"=hex:01,02
`), 0o644)
	v, err := Values(path, `Software\Wine\Explorer\Desktops`)
	if err != nil {
		t.Fatal(err)
	}
	if len(v) != 2 || v["Default"] != "800x600" || v[`q"x`] != `a\b` {
		t.Errorf("got %q", v)
	}
	if v, err := Values(filepath.Join(t.TempDir(), "none"), "X"); err != nil || len(v) != 0 {
		t.Errorf("missing file: %v %v", v, err)
	}
}

func TestUnescape(t *testing.T) {
	if s := Unescape(`a\\b\"c\x4e2d\0`); s != "a\\b\"c中\x00" {
		t.Errorf("got %q", s)
	}
}
