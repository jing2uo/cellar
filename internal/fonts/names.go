package fonts

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"slices"
	"unicode/utf16"
)

// Families reads the family names a font file holds, every face of a
// collection and every language: the family (name ID 1) and typographic
// family (16) of the Windows records, which is how Windows programs ask
// for a font ("Microsoft YaHei", "微软雅黑").
func Families(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var tag [4]byte
	if _, err := f.ReadAt(tag[:], 0); err != nil {
		return nil, err
	}
	offsets := []int64{0}
	if string(tag[:]) == "ttcf" {
		var hdr [12]byte
		if _, err := f.ReadAt(hdr[:], 0); err != nil {
			return nil, err
		}
		n := binary.BigEndian.Uint32(hdr[8:])
		if n == 0 || n > 256 {
			return nil, errors.New("bad font collection")
		}
		buf := make([]byte, 4*n)
		if _, err := f.ReadAt(buf, 12); err != nil {
			return nil, err
		}
		offsets = offsets[:0]
		for i := range n {
			offsets = append(offsets, int64(binary.BigEndian.Uint32(buf[4*i:])))
		}
	}
	var out []string
	for _, off := range offsets {
		names, err := faceFamilies(f, off)
		if err != nil {
			return nil, err
		}
		for _, n := range names {
			if !slices.Contains(out, n) {
				out = append(out, n)
			}
		}
	}
	return out, nil
}

// faceFamilies reads one face's family names from its name table.
func faceFamilies(r io.ReaderAt, off int64) ([]string, error) {
	var hdr [12]byte
	if _, err := r.ReadAt(hdr[:], off); err != nil {
		return nil, err
	}
	numTables := int(binary.BigEndian.Uint16(hdr[4:]))
	dir := make([]byte, 16*numTables)
	if _, err := r.ReadAt(dir, off+12); err != nil {
		return nil, err
	}
	for i := range numTables {
		rec := dir[16*i:]
		if string(rec[:4]) != "name" {
			continue
		}
		start := int64(binary.BigEndian.Uint32(rec[8:]))
		length := binary.BigEndian.Uint32(rec[12:])
		if length > 1<<20 {
			return nil, errors.New("name table too large")
		}
		tbl := make([]byte, length)
		if _, err := r.ReadAt(tbl, start); err != nil {
			return nil, err
		}
		return nameFamilies(tbl), nil
	}
	return nil, errors.New("font has no name table")
}

func nameFamilies(tbl []byte) []string {
	if len(tbl) < 6 {
		return nil
	}
	count := int(binary.BigEndian.Uint16(tbl[2:]))
	strs := int(binary.BigEndian.Uint16(tbl[4:]))
	var out []string
	for i := range count {
		at := 6 + 12*i
		if at+12 > len(tbl) {
			break
		}
		rec := tbl[at:]
		platform, id := binary.BigEndian.Uint16(rec), binary.BigEndian.Uint16(rec[6:])
		length, offset := int(binary.BigEndian.Uint16(rec[8:])), int(binary.BigEndian.Uint16(rec[10:]))
		if platform != 3 || (id != 1 && id != 16) || strs+offset+length > len(tbl) {
			continue
		}
		raw := tbl[strs+offset : strs+offset+length]
		u := make([]uint16, len(raw)/2)
		for j := range u {
			u[j] = binary.BigEndian.Uint16(raw[2*j:])
		}
		if s := string(utf16.Decode(u)); s != "" && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}
