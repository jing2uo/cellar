package winapp

import (
	"os"
	"testing"
)

func TestWineLnkNul(t *testing.T) {
	// Wine's shortcut counts the terminating NUL into its strings: the
	// workdir ends in one and the args are nothing but one.
	b, err := os.ReadFile("testdata/wine-notepadpp.lnk")
	if err != nil {
		t.Fatal(err)
	}
	s, err := ParseLnk(b)
	if err != nil {
		t.Fatal(err)
	}
	if s.Target != `C:\Program Files\Notepad++\notepad++.exe` || s.Workdir != `C:\Program Files\Notepad++` || s.Args != "" {
		t.Errorf("%+v", s)
	}
}
