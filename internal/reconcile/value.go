package reconcile

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
)

// A registry value in canonical text form, "<type>:<data>" - what state.json
// stores and plans print, so equal values compare equal as strings:
//
//	sz:text  expand_sz:text  dword:00000002  multi_sz:["a","b"]  binary:0a0b
type value string

func (v value) split() (string, string) {
	t, d, _ := strings.Cut(string(v), ":")
	return t, d
}

// parseValue turns a TOML value into canonical form. expand rewrites the
// strings inside it ({font:...} templates).
func parseValue(raw any, expand func(string) (string, error)) (value, error) {
	switch x := raw.(type) {
	case string:
		s, err := expand(x)
		return value("sz:" + s), err
	case int64:
		if x < 0 || x > 0xffffffff {
			return "", fmt.Errorf("dword out of range: %d", x)
		}
		return value(fmt.Sprintf("dword:%08x", x)), nil
	case bool:
		if x {
			return "dword:00000001", nil
		}
		return "dword:00000000", nil
	case map[string]any:
		t, _ := x["type"].(string)
		switch t {
		case "sz", "expand_sz":
			s, ok := x["value"].(string)
			if !ok {
				return "", fmt.Errorf("%s wants a string value", t)
			}
			s, err := expand(s)
			return value(t + ":" + s), err
		case "dword":
			return parseValue(x["value"], expand)
		case "multi_sz":
			arr, ok := x["value"].([]any)
			if !ok {
				return "", fmt.Errorf("multi_sz wants a list")
			}
			// An entry naming a font role nothing fills is left out; the
			// value goes only when all of them are.
			ss := []string{}
			for _, a := range arr {
				s, err := expand(fmt.Sprint(a))
				if errors.Is(err, errUnresolved) {
					continue
				}
				if err != nil {
					return "", err
				}
				ss = append(ss, s)
			}
			if len(ss) == 0 && len(arr) > 0 {
				return "", errUnresolved
			}
			b, _ := json.Marshal(ss)
			return value("multi_sz:" + string(b)), nil
		case "binary":
			s, _ := x["value"].(string)
			s = strings.ReplaceAll(strings.ReplaceAll(s, ",", ""), " ", "")
			if _, err := hex.DecodeString(s); err != nil {
				return "", fmt.Errorf("binary wants hex: %w", err)
			}
			return value("binary:" + strings.ToLower(s)), nil
		}
		return "", fmt.Errorf("unknown registry type %q", t)
	}
	return "", fmt.Errorf("unsupported registry value %T", raw)
}

// regData renders the right-hand side of a .reg line.
func (v value) regData() string {
	t, d := v.split()
	switch t {
	case "sz":
		return `"` + escape(d) + `"`
	case "dword":
		return "dword:" + d
	case "expand_sz":
		return "hex(2):" + hexList(utf16z(d))
	case "multi_sz":
		var ss []string
		_ = json.Unmarshal([]byte(d), &ss)
		var b []byte
		for _, s := range ss {
			b = append(b, utf16z(s)...)
		}
		b = append(b, 0, 0)
		return "hex(7):" + hexList(b)
	case "binary":
		raw, _ := hex.DecodeString(d)
		return "hex:" + hexList(raw)
	}
	return `""`
}

func escape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}

func utf16z(s string) []byte {
	var b []byte
	for _, u := range utf16.Encode([]rune(s)) {
		b = append(b, byte(u), byte(u>>8))
	}
	return append(b, 0, 0)
}

func hexList(b []byte) string {
	parts := make([]string, len(b))
	for i, c := range b {
		parts[i] = fmt.Sprintf("%02x", c)
	}
	return strings.Join(parts, ",")
}

var hives = map[string]string{
	"HKCU": "HKEY_CURRENT_USER",
	"HKLM": "HKEY_LOCAL_MACHINE",
	"HKCR": "HKEY_CLASSES_ROOT",
	"HKU":  "HKEY_USERS",
}

func normKey(k string) string {
	k = strings.Trim(strings.ReplaceAll(k, "/", `\`), `\`)
	h, rest, _ := strings.Cut(k, `\`)
	if full, ok := hives[strings.ToUpper(h)]; ok {
		h = full
	}
	return h + `\` + rest
}

// regFile renders sets and deletes as a UTF-16LE .reg file.
func regFile(sets, dels map[string]map[string]value) []byte {
	var sb strings.Builder
	sb.WriteString("Windows Registry Editor Version 5.00\r\n")
	for _, key := range unionKeys(sets, dels) {
		sb.WriteString("\r\n[" + key + "]\r\n")
		for _, n := range sortedNames(dels[key]) {
			sb.WriteString(regName(n) + "=-\r\n")
		}
		for _, n := range sortedNames(sets[key]) {
			sb.WriteString(regName(n) + "=" + sets[key][n].regData() + "\r\n")
		}
	}
	out := []byte{0xff, 0xfe}
	for _, u := range utf16.Encode([]rune(sb.String())) {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}

func regName(n string) string {
	if n == "@" {
		return "@"
	}
	return `"` + escape(n) + `"`
}
