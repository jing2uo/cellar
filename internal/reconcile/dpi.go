package reconcile

import (
	"io"
	"log"
	"strconv"
	"strings"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// hostDPI reads Xft.dpi from the X server's resource database (the root
// window's RESOURCE_MANAGER, what `xrdb -query` prints). Desktops that scale
// X11 apps themselves set it; under Hyprland's xwayland force_zero_scaling it
// is the only thing that makes Windows programs come out at the right size.
// Pure Go: no xrdb in the Flatpak runtime, no cgo in the launcher. 0 when
// there is no display or no setting.
func hostDPI() int {
	xgb.Logger = log.New(io.Discard, "", 0) // it narrates auth fallbacks on stderr
	conn, err := xgb.NewConn()
	if err != nil {
		return 0
	}
	defer conn.Close()
	root := xproto.Setup(conn).DefaultScreen(conn).Root
	atom, err := xproto.InternAtom(conn, true, uint16(len("RESOURCE_MANAGER")), "RESOURCE_MANAGER").Reply()
	if err != nil || atom.Atom == 0 {
		return 0
	}
	prop, err := xproto.GetProperty(conn, false, root, atom.Atom, xproto.AtomString, 0, 1<<20).Reply()
	if err != nil {
		return 0
	}
	return parseXftDPI(string(prop.Value))
}

func parseXftDPI(db string) int {
	for _, line := range strings.Split(db, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(k) != "Xft.dpi" {
			continue
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err == nil && f >= 48 && f <= 480 {
			return int(f + 0.5)
		}
	}
	return 0
}
