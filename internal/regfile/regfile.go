// Package regfile reads Wine's registry files, as left on disk by the
// wineserver (which writes them out a little after a change, and on exit).
package regfile

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Values reads a key's string values from a Wine registry file
// (system.reg, user.reg). key is relative to the file's hive, with single
// backslashes: Software\Wine\DllOverrides. A missing file reads as empty.
func Values(path, key string) (map[string]string, error) {
	header := "[" + strings.ReplaceAll(key, `\`, `\\`) + "]"
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]string{}
	in := false
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "[") {
			in = strings.HasPrefix(line, header)
			continue
		}
		if !in {
			continue
		}
		name, rest, ok := cutQuoted(line)
		if !ok || !strings.HasPrefix(rest, "=") {
			continue
		}
		data, rest, ok := cutQuoted(rest[1:])
		if !ok || rest != "" {
			continue // not a plain string value
		}
		out[Unescape(name)] = Unescape(data)
	}
	return out, sc.Err()
}

// cutQuoted splits a leading "..." (escapes kept) off s.
func cutQuoted(s string) (q, rest string, ok bool) {
	if !strings.HasPrefix(s, `"`) {
		return "", s, false
	}
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return s[1:i], s[i+1:], true
		}
	}
	return "", s, false
}

// Unescape undoes the escapes of a Wine .reg string: \\ \" \0 \n \r \t
// and \x followed by up to four hex digits.
func Unescape(s string) string {
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
