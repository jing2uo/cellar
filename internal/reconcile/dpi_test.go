package reconcile

import "testing"

func TestParseXftDPI(t *testing.T) {
	for db, want := range map[string]int{
		"Xft.antialias:\t1\nXft.dpi:\t180\nXcursor.size:\t24\n": 180,
		"Xft.dpi: 96.0\n":    96,
		"Xcursor.size: 24\n": 0,
		"Xft.dpi: 9000\n":    0,
	} {
		if got := parseXftDPI(db); got != want {
			t.Errorf("%q: got %d want %d", db, got, want)
		}
	}
}
