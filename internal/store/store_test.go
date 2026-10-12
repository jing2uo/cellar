package store

import (
	"os"
	"testing"
)

func TestRevertConfig(t *testing.T) {
	t.Setenv("CELLAR_HOME", t.TempDir())
	p, err := Create("p", "system", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Never applied: nothing to go back to.
	if p.ConfigEdited() || p.RevertConfig() == nil {
		t.Fatal("revert without an applied copy")
	}
	applied, _ := os.ReadFile(p.Config)
	os.WriteFile(p.AppliedConfig(), applied, 0o644)
	if p.ConfigEdited() {
		t.Fatal("edited right after apply")
	}
	os.WriteFile(p.Config, append(applied, "dpi = 144\n"...), 0o644)
	if !p.ConfigEdited() {
		t.Fatal("edit not seen")
	}
	if err := p.RevertConfig(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p.Config); string(b) != string(applied) || p.ConfigEdited() {
		t.Errorf("not reverted:\n%s", b)
	}
}
