package runner

import (
	"bufio"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// Verb is one winetricks verb as `winetricks <category> list` prints it.
type Verb struct {
	Verb     string `json:"verb"`
	Title    string `json:"title"`
	By       string `json:"by,omitempty"` // publisher, year
	Category string `json:"category"`     // dlls | fonts
	Cached   bool   `json:"cached,omitempty"`
}

// verbLine is a list row: the verb, then its title in a column (a log line
// such as "Executing cd ..." has one space); verbBy splits off the
// "(publisher, year)" at the end of the title.
var (
	verbLine = regexp.MustCompile(`^(\S+)\s{2,}(.*?)(?:\s+\[([^\]]*)\])?$`)
	verbBy   = regexp.MustCompile(`^(.*) \(([^()]*)\)$`)
)

// Verbs asks winetricks for its DLL / runtime and font verbs. It takes a
// couple of seconds: callers keep the result.
func Verbs() ([]Verb, error) {
	wt, err := Winetricks()
	if err != nil {
		return nil, err
	}
	var out []Verb
	for _, cat := range []string{"dlls", "fonts"} {
		cmd := exec.Command(wt, cat, "list")
		// English, and no prefix touched: listing needs none.
		cmd.Env = append(os.Environ(), "LANG=C", "LC_ALL=C", "WINEPREFIX="+os.DevNull)
		b, err := cmd.Output()
		if err != nil {
			return nil, err
		}
		out = append(out, parseVerbs(string(b), cat)...)
	}
	return out, nil
}

func parseVerbs(text, cat string) []Verb {
	var out []Verb
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		m := verbLine.FindStringSubmatch(strings.TrimSpace(sc.Text()))
		if m == nil || m[2] == "" {
			continue
		}
		v := Verb{Verb: m[1], Title: m[2], Category: cat, Cached: strings.Contains(m[3], "cached")}
		if b := verbBy.FindStringSubmatch(v.Title); b != nil {
			v.Title, v.By = b[1], b[2]
		}
		out = append(out, v)
	}
	return out
}
