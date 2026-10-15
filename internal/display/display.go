// Package display answers which virtual desktop sizes Wine takes on this
// machine. Wine sizes a virtual desktop by switching it to a display mode,
// and refuses a size that is not one of the desktop's modes: a fixed list of
// common sizes no larger than the host's primary screen, the screen's own
// size, and the size in HKCU\Software\Wine\Explorer\Desktops "Default"
// (win32u add_virtual_source). cellar writes any other size there, so the
// list here is what works with no help.
package display

import (
	"cmp"
	"fmt"
	"io"
	"log"
	"slices"
	"strconv"
	"strings"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/randr"
	"github.com/jezek/xgb/xproto"
)

// sizes is Wine 11's list (win32u/sysparams.c get_screen_sizes), landscape.
var sizes = [][2]int{
	{640, 480}, {800, 600}, {1024, 768}, {1152, 864}, {1280, 960}, {1600, 1200}, // 4:3
	{960, 540}, {1280, 720}, {1600, 900}, {1920, 1080}, {2560, 1440}, {2880, 1620}, {3200, 1800}, // 16:9
	{1440, 900}, {1680, 1050}, {1920, 1200}, {2560, 1600}, // 16:10
	{1440, 960}, {1920, 1280}, // 3:2
	{2560, 1080},              // 21:9
	{1920, 800}, {3840, 1600}, // 12:5
	{1280, 1024}, // 5:4
	{1280, 768},  // 5:3
}

// Screen is the host's primary screen size in pixels, as Wine's X11 driver
// sees it (RandR: the primary output's CRTC, else the first lit one, else
// the root window). 0, 0 with no display.
func Screen() (int, int) {
	xgb.Logger = log.New(io.Discard, "", 0)
	conn, err := xgb.NewConn()
	if err != nil {
		return 0, 0
	}
	defer conn.Close()
	scr := xproto.Setup(conn).DefaultScreen(conn)
	if randr.Init(conn) == nil {
		if res, err := randr.GetScreenResourcesCurrent(conn, scr.Root).Reply(); err == nil {
			outputs := res.Outputs
			if p, err := randr.GetOutputPrimary(conn, scr.Root).Reply(); err == nil && p.Output != 0 {
				outputs = append([]randr.Output{p.Output}, outputs...)
			}
			for _, o := range outputs {
				info, err := randr.GetOutputInfo(conn, o, res.ConfigTimestamp).Reply()
				if err != nil || info.Crtc == 0 {
					continue
				}
				crtc, err := randr.GetCrtcInfo(conn, info.Crtc, res.ConfigTimestamp).Reply()
				if err == nil && crtc.Width > 0 && crtc.Height > 0 {
					return int(crtc.Width), int(crtc.Height)
				}
			}
		}
	}
	return int(scr.WidthInPixels), int(scr.HeightInPixels)
}

// Sizes lists the sizes Wine takes for a screen of w x h, smallest first,
// the screen's own size last. A portrait screen gets portrait sizes. With
// no screen known (0) the whole list is given.
func Sizes(w, h int) []string {
	portrait := h > w
	if portrait {
		w, h = h, w
	}
	var out []string
	add := func(x, y int) {
		if portrait {
			x, y = y, x
		}
		if s := Format(x, y); !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	fits := func(x, y int) bool { return w == 0 || x <= w && y <= h }
	var keep [][2]int
	for _, s := range sizes {
		if fits(s[0], s[1]) && !(s[0] == w && s[1] == h) {
			keep = append(keep, s)
		}
	}
	// By width, then height: the order people scan a size list in.
	slices.SortFunc(keep, func(a, b [2]int) int { return cmp.Or(a[0]-b[0], a[1]-b[1]) })
	for _, s := range keep {
		add(s[0], s[1])
	}
	if w > 0 {
		add(w, h)
	}
	return out
}

func Format(w, h int) string { return fmt.Sprintf("%dx%d", w, h) }

// Parse reads "WxH".
func Parse(s string) (int, int, bool) {
	a, b, ok := strings.Cut(strings.ToLower(strings.TrimSpace(s)), "x")
	if !ok {
		return 0, 0, false
	}
	w, err1 := strconv.Atoi(a)
	h, err2 := strconv.Atoi(b)
	return w, h, err1 == nil && err2 == nil && w > 0 && h > 0
}
