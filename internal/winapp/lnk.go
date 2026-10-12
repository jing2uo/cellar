// Package winapp finds the programs an installer put into a prefix (from
// its Start Menu shortcuts) and extracts their icons.
package winapp

import (
	"encoding/binary"
	"errors"
	"unicode/utf16"
)

// Shortcut is what a .lnk points at.
type Shortcut struct {
	Target  string // Windows path of the program
	Args    string
	Workdir string
	Icon    string
}

const (
	hasIDList    = 1 << 0
	hasLinkInfo  = 1 << 1
	hasName      = 1 << 2
	hasRelPath   = 1 << 3
	hasWorkDir   = 1 << 4
	hasArgs      = 1 << 5
	hasIconLoc   = 1 << 6
	isUnicode    = 1 << 7
	linkInfoLBP  = 1 << 0 // VolumeIDAndLocalBasePath
	headerSize   = 0x4c
	lnkSignature = 0x4c
)

// ParseLnk reads the parts of an MS-SHLLINK file a launcher needs: the
// target from LinkInfo's local base path, then the string data.
func ParseLnk(b []byte) (*Shortcut, error) {
	if len(b) < headerSize || binary.LittleEndian.Uint32(b) != lnkSignature {
		return nil, errors.New("not a shell link")
	}
	flags := binary.LittleEndian.Uint32(b[0x14:])
	off := headerSize
	if flags&hasIDList != 0 {
		if off+2 > len(b) {
			return nil, errors.New("truncated id list")
		}
		off += 2 + int(binary.LittleEndian.Uint16(b[off:]))
	}
	s := &Shortcut{}
	if flags&hasLinkInfo != 0 {
		if off+0x1c > len(b) {
			return nil, errors.New("truncated link info")
		}
		li := b[off:]
		size := int(binary.LittleEndian.Uint32(li))
		hdr := binary.LittleEndian.Uint32(li[4:])
		liFlags := binary.LittleEndian.Uint32(li[8:])
		if liFlags&linkInfoLBP != 0 && size <= len(li) {
			if hdr >= 0x24 {
				if u := int(binary.LittleEndian.Uint32(li[0x1c:])); u > 0 && u < size {
					s.Target = utf16z(li[u:size])
				}
			}
			if s.Target == "" {
				if a := int(binary.LittleEndian.Uint32(li[0x10:])); a > 0 && a < size {
					s.Target = ansiz(li[a:size])
				}
			}
		}
		off += size
	}
	str := func() string {
		if off+2 > len(b) {
			return ""
		}
		n := int(binary.LittleEndian.Uint16(b[off:]))
		off += 2
		if flags&isUnicode != 0 {
			n *= 2
		}
		if off+n > len(b) {
			off = len(b)
			return ""
		}
		raw := b[off : off+n]
		off += n
		// Wine counts a terminating NUL in, Windows does not.
		if flags&isUnicode != 0 {
			return utf16z(raw)
		}
		return ansiz(raw)
	}
	var rel string
	if flags&hasName != 0 {
		str()
	}
	if flags&hasRelPath != 0 {
		rel = str()
	}
	if flags&hasWorkDir != 0 {
		s.Workdir = str()
	}
	if flags&hasArgs != 0 {
		s.Args = str()
	}
	if flags&hasIconLoc != 0 {
		s.Icon = str()
	}
	if s.Target == "" && rel != "" {
		s.Target = rel
	}
	if s.Target == "" {
		return nil, errors.New("shortcut has no target path")
	}
	return s, nil
}

func utf16s(b []byte) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[2*i:])
	}
	return string(utf16.Decode(u))
}

func utf16z(b []byte) string {
	for i := 0; i+1 < len(b); i += 2 {
		if b[i] == 0 && b[i+1] == 0 {
			return utf16s(b[:i])
		}
	}
	return utf16s(b)
}

func ansiz(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
