package reconcile

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/jing2uo/cellar/internal/config"
)

const linkKey = `HKEY_LOCAL_MACHINE\Software\Microsoft\Windows NT\CurrentVersion\FontLink\SystemLink`

// Wine reads each SystemLink entry into a 4 KB buffer, name and all, and
// one that doesn't fit ends the read of the whole key: no font links at
// all. linkRoom is what an entry may take, with room to spare.
const linkRoom = 4096 - 64

func linkSize(name string, entries []string) int {
	n := 20 + 2*len(utf16.Encode([]rune(name)))
	n = (n+7)&^7 + 2
	for _, e := range entries {
		n += 2 * (len(utf16.Encode([]rune(e))) + 1)
	}
	return n
}

// spreadLinks splits a fallback chain too long for one SystemLink entry.
// Every font falls back to Microsoft Sans Serif's links, then Tahoma's,
// so the chain fills those two in turn; what is left goes on the fonts
// apps end up with (the substitutes' targets) and Segoe UI, which read
// their own entry first. The CJK regions lead the chain, so they land in
// the first part; no script font has Han, so they still win for it.
// It returns how many fonts did not fit anywhere.
func spreadLinks(reg map[string]map[string]value) int {
	sl := reg[linkKey]
	v, ok := sl[config.LinkFont]
	if !ok {
		return 0
	}
	t, d := v.split()
	var chain []string
	if t != "multi_sz" || json.Unmarshal([]byte(d), &chain) != nil || linkSize(config.LinkFont, chain) <= linkRoom {
		return 0
	}
	take := func(name string, rest []string) ([]string, []string) {
		var part []string
		for len(rest) > 0 && linkSize(name, append(part, rest[0])) <= linkRoom {
			part, rest = append(part, rest[0]), rest[1:]
		}
		return part, rest
	}
	set := func(name string, part []string) {
		b, _ := json.Marshal(part)
		sl[name] = value("multi_sz:" + string(b))
	}
	first, rest := take(config.LinkFont, chain)
	set(config.LinkFont, first)
	if _, user := sl["Tahoma"]; !user && len(rest) > 0 {
		var part []string
		part, rest = take("Tahoma", rest)
		set("Tahoma", part)
	}
	if len(rest) == 0 {
		return 0
	}
	subst := reg[normKey(`HKLM\Software\Microsoft\Windows NT\CurrentVersion\FontSubstitutes`)]
	holders := []string{"Segoe UI"}
	for _, to := range subst {
		if t, fam := to.split(); t == "sz" && subst[fam] == "" {
			holders = append(holders, fam)
		}
	}
	slices.Sort(holders)
	holders = slices.Compact(holders)
	left := 0
	for _, h := range holders {
		if _, user := sl[h]; user {
			continue
		}
		part, r := take(h, rest)
		set(h, part)
		left = max(left, len(r))
	}
	return left
}

func linkWarning(n int) string {
	return fmt.Sprintf("font fallback: %d script fonts left out, more than Wine reads of the SystemLink entries", n)
}

// linkDrift names the applied SystemLink entries system.reg no longer
// holds. Wine puts its own Tahoma and Microsoft Sans Serif entries back
// whenever it starts under another code page (a locale change, or a wine
// run outside cellar), behind state.json's back.
func linkDrift(pfx string, applied map[string]string) map[string]bool {
	if len(applied) == 0 {
		return nil
	}
	f, err := os.Open(filepath.Join(pfx, "system.reg"))
	if err != nil {
		return nil
	}
	defer f.Close()
	have := map[string][]string{}
	in := false
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "[") {
			in = strings.HasPrefix(line, `[Software\\Microsoft\\Windows NT\\CurrentVersion\\FontLink\\SystemLink]`)
			continue
		}
		if !in || !strings.HasPrefix(line, `"`) {
			continue
		}
		name, rest, ok := strings.Cut(line[1:], `"=str(7):"`)
		if !ok || !strings.HasSuffix(rest, `"`) {
			continue
		}
		var entries []string
		for _, e := range strings.Split(unescapeReg(strings.TrimSuffix(rest, `"`)), "\x00") {
			if e != "" {
				entries = append(entries, e)
			}
		}
		have[unescapeReg(name)] = entries
	}
	drift := map[string]bool{}
	for n, v := range applied {
		var want []string
		if t, d := value(v).split(); t != "multi_sz" || json.Unmarshal([]byte(d), &want) != nil {
			continue
		}
		if !slices.Equal(have[n], want) {
			drift[n] = true
		}
	}
	return drift
}

// unescapeReg undoes the escapes of a Wine .reg string: \\ \" \0 \n
// \r \t and \x followed by up to four hex digits.
func unescapeReg(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch c := s[i]; c {
		case '0':
			b.WriteByte(0)
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'x':
			j := i + 1
			for j < len(s) && j < i+5 && strings.IndexByte("0123456789abcdefABCDEF", s[j]) >= 0 {
				j++
			}
			n, _ := strconv.ParseUint(s[i+1:j], 16, 16)
			b.WriteString(string(utf16.Decode([]uint16{uint16(n)})))
			i = j - 1
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
