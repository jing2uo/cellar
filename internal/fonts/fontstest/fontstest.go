// Package fontstest builds font files small enough for tests: a name
// table and nothing else, which is all cellar reads of a font.
package fontstest

import (
	"encoding/binary"
	"unicode/utf16"
)

// Font is a one-face font file whose Windows family names are families.
func Font(families ...string) []byte {
	return face(0, families)
}

// Collection is a .ttc of one face per family list.
func Collection(faces ...[]string) []byte {
	head := 12 + 4*len(faces)
	out := make([]byte, head)
	copy(out, "ttcf")
	binary.BigEndian.PutUint32(out[4:], 0x00010000)
	binary.BigEndian.PutUint32(out[8:], uint32(len(faces)))
	for i, f := range faces {
		binary.BigEndian.PutUint32(out[12+4*i:], uint32(len(out)))
		out = append(out, face(len(out), f)...)
	}
	return out
}

// face is an sfnt with a name table, laid out to sit at offset base of the
// file (table offsets count from the file's start).
func face(base int, families []string) []byte {
	var strs []byte
	var recs []byte
	for i, f := range families {
		s := utf16.Encode([]rune(f))
		b := make([]byte, 2*len(s))
		for j, u := range s {
			binary.BigEndian.PutUint16(b[2*j:], u)
		}
		rec := make([]byte, 12)
		binary.BigEndian.PutUint16(rec[0:], 3)                  // Windows
		binary.BigEndian.PutUint16(rec[2:], 1)                  // Unicode BMP
		binary.BigEndian.PutUint16(rec[4:], uint16(0x409+i))    // a language each
		binary.BigEndian.PutUint16(rec[6:], 1)                  // family
		binary.BigEndian.PutUint16(rec[8:], uint16(len(b)))     //
		binary.BigEndian.PutUint16(rec[10:], uint16(len(strs))) //
		recs = append(recs, rec...)
		strs = append(strs, b...)
	}
	name := make([]byte, 6)
	binary.BigEndian.PutUint16(name[2:], uint16(len(families)))
	binary.BigEndian.PutUint16(name[4:], uint16(6+len(recs)))
	name = append(append(name, recs...), strs...)

	out := make([]byte, 12+16)
	binary.BigEndian.PutUint32(out[0:], 0x00010000)
	binary.BigEndian.PutUint16(out[4:], 1)
	copy(out[12:], "name")
	binary.BigEndian.PutUint32(out[12+8:], uint32(base+len(out)))
	binary.BigEndian.PutUint32(out[12+12:], uint32(len(name)))
	return append(out, name...)
}
