package winapp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"

	"github.com/tc-hib/winres"
)

// ExtractIcon writes the largest image of an executable's first icon group
// as a PNG. Large icons are usually PNG inside the resource already; smaller
// ones are 32-bit DIBs, decoded here.
func ExtractIcon(exe, dst string) error {
	f, err := os.Open(exe)
	if err != nil {
		return err
	}
	defer f.Close()
	rs, err := winres.LoadFromEXE(f)
	if err != nil {
		return err
	}
	var first winres.Identifier
	rs.WalkType(winres.RT_GROUP_ICON, func(id winres.Identifier, _ uint16, _ []byte) bool {
		first = id
		return false
	})
	if first == nil {
		return errors.New("no icon in executable")
	}
	icon, err := rs.GetIcon(first)
	if err != nil {
		return err
	}
	var ico bytes.Buffer
	if err := icon.SaveICO(&ico); err != nil {
		return err
	}
	img, raw, err := largest(ico.Bytes())
	if err != nil {
		return err
	}
	if raw != nil {
		return os.WriteFile(dst, raw, 0o644)
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return err
	}
	return os.WriteFile(dst, out.Bytes(), 0o644)
}

// largest picks the biggest entry of an .ico; raw is set when it is a PNG.
func largest(ico []byte) (image.Image, []byte, error) {
	if len(ico) < 6 {
		return nil, nil, errors.New("bad ico")
	}
	n := int(binary.LittleEndian.Uint16(ico[4:]))
	best, bestArea, bestBpp := -1, 0, 0
	for i := 0; i < n; i++ {
		e := ico[6+16*i:]
		w, h := int(e[0]), int(e[1])
		if w == 0 {
			w = 256
		}
		if h == 0 {
			h = 256
		}
		bpp := int(binary.LittleEndian.Uint16(e[6:]))
		if w*h > bestArea || w*h == bestArea && bpp > bestBpp {
			best, bestArea, bestBpp = i, w*h, bpp
		}
	}
	if best < 0 {
		return nil, nil, errors.New("empty ico")
	}
	e := ico[6+16*best:]
	size := int(binary.LittleEndian.Uint32(e[8:]))
	off := int(binary.LittleEndian.Uint32(e[12:]))
	if off+size > len(ico) {
		return nil, nil, errors.New("bad ico entry")
	}
	data := ico[off : off+size]
	if bytes.HasPrefix(data, []byte("\x89PNG")) {
		return nil, data, nil
	}
	img, err := decodeDIB(data)
	return img, nil, err
}

// decodeDIB handles the 32-bit BGRA bitmaps modern icons use, 24-bit, and
// the 1/4/8-bit palette ones of older programs; below 32-bit, transparency
// comes from the AND mask that follows the pixels.
func decodeDIB(d []byte) (image.Image, error) {
	if len(d) < 40 {
		return nil, errors.New("short dib")
	}
	hdr := int(binary.LittleEndian.Uint32(d))
	w := int(int32(binary.LittleEndian.Uint32(d[4:])))
	h := int(int32(binary.LittleEndian.Uint32(d[8:]))) / 2 // XOR + AND masks
	bpp := int(binary.LittleEndian.Uint16(d[14:]))
	if w <= 0 || h <= 0 || hdr > len(d) {
		return nil, errors.New("bad icon bitmap")
	}
	var palette [][4]byte
	switch bpp {
	case 1, 4, 8:
		n := int(binary.LittleEndian.Uint32(d[32:]))
		if n == 0 {
			n = 1 << bpp
		}
		if hdr+4*n > len(d) {
			return nil, errors.New("short palette")
		}
		for i := 0; i < n; i++ {
			e := d[hdr+4*i:]
			palette = append(palette, [4]byte{e[2], e[1], e[0], 255})
		}
		hdr += 4 * n
	case 24, 32:
	default:
		return nil, errors.New("unsupported icon bitmap")
	}
	px := d[hdr:]
	stride := ((w*bpp + 31) / 32) * 4
	maskStride := ((w + 31) / 32) * 4
	if len(px) < stride*h {
		return nil, errors.New("short pixel data")
	}
	mask := px[stride*h:]
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		row := px[(h-1-y)*stride:]
		for x := 0; x < w; x++ {
			var c color.NRGBA
			switch bpp {
			case 32:
				p := row[x*4:]
				c = color.NRGBA{p[2], p[1], p[0], p[3]}
			case 24:
				p := row[x*3:]
				c = color.NRGBA{p[2], p[1], p[0], 255}
			default:
				bit := x * bpp
				idx := int(row[bit/8]>>(8-bpp-bit%8)) & (1<<bpp - 1)
				if idx < len(palette) {
					e := palette[idx]
					c = color.NRGBA{e[0], e[1], e[2], 255}
				}
			}
			if bpp != 32 {
				if mr := (h - 1 - y) * maskStride; mr+x/8 < len(mask) && mask[mr+x/8]&(0x80>>(x%8)) != 0 {
					c.A = 0
				}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img, nil
}
