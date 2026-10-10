package config

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Editor changes cellar.toml in place, line by line, so the comments and
// layout people (and AI) put in the file survive GUI edits. It handles the
// shapes cellar writes: top-level key = value, flat [tables] of
// key = "string", and appending [[apps]] blocks.
type Editor struct {
	path  string
	lines []string
}

func Edit(path string) (*Editor, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return &Editor{path: path, lines: strings.Split(strings.TrimRight(string(b), "\n"), "\n")}, nil
}

var (
	headerRe = regexp.MustCompile(`^\s*\[\[?\s*([^\]]+?)\s*\]\]?\s*(#.*)?$`)
	keyRe    = regexp.MustCompile(`^\s*("(?:[^"\\]|\\.)*"|'[^']*'|[A-Za-z0-9_.-]+)\s*=`)
)

func unquoteKey(k string) string {
	if strings.HasPrefix(k, `"`) {
		if s, err := strconv.Unquote(k); err == nil {
			return s
		}
	}
	return strings.Trim(k, "'")
}

// section returns [start, end) of a table's body; start = -1 if absent. The
// top level is name "".
func (e *Editor) section(name string) (int, int) {
	start := -1
	if name == "" {
		start = 0
	}
	for i, l := range e.lines {
		m := headerRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		if start >= 0 {
			return start, i
		}
		if !strings.HasPrefix(strings.TrimSpace(l), "[[") && unquoteKey(m[1]) == name {
			start = i + 1
		}
	}
	if start < 0 {
		return -1, -1
	}
	return start, len(e.lines)
}

func (e *Editor) findKey(from, to int, key string) int {
	for i := from; i < to; i++ {
		if m := keyRe.FindStringSubmatch(e.lines[i]); m != nil && unquoteKey(m[1]) == key {
			return i
		}
	}
	return -1
}

// Set sets a top-level key to a TOML literal, or removes it when literal is
// "". A trailing comment on the line is kept.
func (e *Editor) Set(key, literal string) {
	start, end := e.section("")
	i := e.findKey(start, end, key)
	if literal == "" {
		if i >= 0 {
			e.lines = append(e.lines[:i], e.lines[i+1:]...)
		}
		return
	}
	line := tomlKey(key) + " = " + literal
	if i >= 0 {
		if c := trailingComment(e.lines[i]); c != "" {
			line += "  " + c
		}
		e.lines[i] = line
		return
	}
	// After the last top-level key, before the first table.
	at := start
	for j := start; j < end; j++ {
		if keyRe.MatchString(e.lines[j]) {
			at = j + 1
		}
	}
	e.lines = append(e.lines[:at], append([]string{line}, e.lines[at:]...)...)
}

// SetTable replaces a flat table's entries with values (string values),
// keeping comment lines inside it; an empty map removes the table.
func (e *Editor) SetTable(name string, values map[string]string) {
	start, end := e.section(name)
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var body []string
	for _, k := range keys {
		body = append(body, tomlKey(k)+" = "+strconv.Quote(values[k]))
	}
	if start < 0 {
		if len(values) == 0 {
			return
		}
		// Before the first [[apps]] so tables stay together.
		at := len(e.lines)
		for i, l := range e.lines {
			if strings.HasPrefix(strings.TrimSpace(l), "[[") {
				at = i
				break
			}
		}
		block := append([]string{"", "[" + tomlKey(name) + "]"}, body...)
		e.lines = append(e.lines[:at], append(block, e.lines[at:]...)...)
		return
	}
	var kept []string
	for i := start; i < end; i++ {
		if !keyRe.MatchString(e.lines[i]) {
			kept = append(kept, e.lines[i])
		}
	}
	for len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
		kept = kept[:len(kept)-1]
	}
	if len(values) == 0 {
		// Drop the header too, and the blank line before it.
		h := start - 1
		if h > 0 && strings.TrimSpace(e.lines[h-1]) == "" {
			h--
		}
		e.lines = append(e.lines[:h], e.lines[end:]...)
		return
	}
	repl := append(append(kept, body...), "")
	if end == len(e.lines) {
		repl = repl[:len(repl)-1]
	}
	e.lines = append(e.lines[:start], append(repl, e.lines[end:]...)...)
}

// AddApp appends an [[apps]] block.
func (e *Editor) AddApp(a App) {
	b := []string{"", "[[apps]]", "id = " + strconv.Quote(a.ID)}
	if a.Name != "" {
		b = append(b, "name = "+strconv.Quote(a.Name))
	}
	b = append(b, "exe = "+literal(a.Exe))
	if len(a.Args) > 0 {
		parts := make([]string, len(a.Args))
		for i, x := range a.Args {
			parts[i] = strconv.Quote(x)
		}
		b = append(b, "args = ["+strings.Join(parts, ", ")+"]")
	}
	if a.Workdir != "" {
		b = append(b, "workdir = "+literal(a.Workdir))
	}
	if a.Icon != "" {
		b = append(b, "icon = "+strconv.Quote(a.Icon))
	}
	if a.Desktop {
		b = append(b, "desktop = true")
	}
	e.lines = append(e.lines, b...)
}

// RemoveApp deletes the [[apps]] block with this id.
func (e *Editor) RemoveApp(id string) bool {
	for i, l := range e.lines {
		if strings.TrimSpace(l) != "[[apps]]" {
			continue
		}
		end := len(e.lines)
		for j := i + 1; j < len(e.lines); j++ {
			if headerRe.MatchString(e.lines[j]) {
				end = j
				break
			}
		}
		if k := e.findKey(i+1, end, "id"); k >= 0 && strings.Contains(e.lines[k], strconv.Quote(id)) {
			from := i
			if from > 0 && strings.TrimSpace(e.lines[from-1]) == "" {
				from--
			}
			e.lines = append(e.lines[:from], e.lines[end:]...)
			return true
		}
	}
	return false
}

// Save writes the file if it still loads; otherwise nothing changes.
func (e *Editor) Save() error {
	tmp := e.path + ".edit"
	if err := os.WriteFile(tmp, []byte(strings.Join(e.lines, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	if _, err := Load(tmp, ""); err != nil {
		b, _ := os.ReadFile(tmp)
		os.Remove(tmp)
		return fmt.Errorf("edit would break cellar.toml: %w\n%s", err, b)
	}
	return os.Rename(tmp, e.path)
}

func tomlKey(k string) string {
	if regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(k) {
		return k
	}
	return strconv.Quote(k)
}

// literal prefers a TOML literal string, which needs no escaping for
// Windows paths.
func literal(s string) string {
	if strings.ContainsAny(s, "'\n") {
		return strconv.Quote(s)
	}
	return "'" + s + "'"
}

// Literal renders v as a TOML value for Set.
func Literal(v any) string {
	switch x := v.(type) {
	case string:
		return strconv.Quote(x)
	case int:
		return strconv.Itoa(x)
	case bool:
		return strconv.FormatBool(x)
	case []string:
		parts := make([]string, len(x))
		for i, s := range x {
			parts[i] = strconv.Quote(s)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}
	return fmt.Sprint(v)
}

func trailingComment(line string) string {
	inStr := byte(0)
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case inStr != 0 && c == '\\' && inStr == '"':
			i++
		case inStr != 0 && c == inStr:
			inStr = 0
		case inStr == 0 && (c == '"' || c == '\''):
			inStr = c
		case inStr == 0 && c == '#':
			return line[i:]
		}
	}
	return ""
}
