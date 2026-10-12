package runner

import "testing"

func TestParseVerbs(t *testing.T) {
	out := parseVerbs(`Executing cd /app/bin
riched20                 MS RichEdit Control 2.0 (riched20.dll) (Microsoft, 2004) [downloadable,cached]
vb6run                   MS Visual Basic 6 runtime sp6 (Microsoft, 2004) [downloadable]
`, "dlls")
	if len(out) != 2 {
		t.Fatalf("%+v", out)
	}
	if r := out[0]; r.Verb != "riched20" || r.Title != "MS RichEdit Control 2.0 (riched20.dll)" || r.By != "Microsoft, 2004" || !r.Cached {
		t.Errorf("%+v", r)
	}
	if out[1].Cached || out[1].Category != "dlls" {
		t.Errorf("%+v", out[1])
	}
}
