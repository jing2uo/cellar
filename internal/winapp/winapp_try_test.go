package winapp

import (
	"os"
	"testing"
)

// Exercises Discover and ExtractIcon against a real prefix when one is
// named in CELLAR_TEST_PFX.
func TestRealPrefix(t *testing.T) {
	pfx := os.Getenv("CELLAR_TEST_PFX")
	if pfx == "" {
		t.Skip("CELLAR_TEST_PFX not set")
	}
	for _, p := range Discover(pfx) {
		t.Logf("%-10s %-30s %s %s", p.From, p.Name, p.Exe, p.Args)
		dst := t.TempDir() + "/i.png"
		if err := ExtractIcon(UnixPath(pfx, p.Exe), dst); err != nil {
			t.Logf("   icon: %v", err)
		} else {
			fi, _ := os.Stat(dst)
			t.Logf("   icon: %d bytes", fi.Size())
		}
	}
}
