package runner

import "testing"

func TestParseTasklist(t *testing.T) {
	out := `"services.exe","56","Console","1","12,772 K"
"WXWork.exe","248","Console","1","808,648 K"
"WeMailNode_x64.exe","728","Console","1","153,664 K"
"tasklist.exe","1232","Console","1","63,800 K"
`
	ps, err := parseTasklist([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 3 {
		t.Fatalf("got %d processes: %+v", len(ps), ps)
	}
	if p := ps[0]; p.Name != "services.exe" || !p.System || p.PID != 56 {
		t.Errorf("services: %+v", p)
	}
	if p := ps[1]; p.Name != "WXWork.exe" || p.System || p.PID != 248 || p.Memory != 808648 {
		t.Errorf("wxwork: %+v", p)
	}
}
