package display

import (
	"slices"
	"testing"
)

func TestSizes(t *testing.T) {
	got := Sizes(1920, 1080)
	want := []string{"640x480", "800x600", "960x540", "1024x768", "1152x864", "1280x720", "1280x768",
		"1280x960", "1280x1024", "1440x900", "1440x960", "1600x900", "1680x1050", "1920x800", "1920x1080"}
	if !slices.Equal(got, want) {
		t.Errorf("1920x1080:\n got %v\nwant %v", got, want)
	}
	// The screen's own size, odd as it may be, comes last.
	if got := Sizes(2560, 1600); got[len(got)-1] != "2560x1600" || !slices.Contains(got, "2560x1440") || slices.Contains(got, "2880x1620") {
		t.Errorf("2560x1600: %v", got)
	}
	if got := Sizes(1080, 1920); !slices.Contains(got, "1080x1920") || !slices.Contains(got, "600x800") {
		t.Errorf("portrait: %v", got)
	}
	if got := Sizes(0, 0); len(got) != len(sizes) {
		t.Errorf("no screen: %d sizes", len(got))
	}
}

func TestParse(t *testing.T) {
	if w, h, ok := Parse(" 2000X1080 "); !ok || w != 2000 || h != 1080 {
		t.Errorf("got %d %d %v", w, h, ok)
	}
	for _, s := range []string{"", "2000", "0x10", "ax1"} {
		if _, _, ok := Parse(s); ok {
			t.Errorf("%q parsed", s)
		}
	}
}
