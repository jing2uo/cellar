package reconcile

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/jing2uo/cellar/internal/config"
	"github.com/jing2uo/cellar/internal/runner"
)

// addonsDir holds a prefix's links to the unpacked Gecko and Mono it uses
// ([init] gecko / mono): on C:, so the registry can name them without a
// drive letter covering the host path. Mshtml and mscoree load them from
// there. Where the runner's version is not found unpacked, Wine offers its
// download when the prefix is made, as before.
const addonsDir = `windows\cellar-addons`

// Gecko's path goes under its version; Mono's is checked only for being
// there, so it must go when the runner wants another version.
const (
	geckoKey   = `HKEY_LOCAL_MACHINE\Software\Wine\MSHTML\`
	geckoKey32 = `HKEY_LOCAL_MACHINE\Software\Wow6432Node\Wine\MSHTML\`
	monoKey    = `HKEY_CURRENT_USER\Software\Wine\Mono`
)

// wantAddons adds the add-ons' links and registry values to d.
func wantAddons(d *desired, c config.Init, a runner.Addons) {
	d.addons = map[string]string{}
	add := func(ad *runner.Addon, key, name string) {
		if ad == nil {
			return
		}
		d.addons[ad.Name] = ad.Dir
		put(d.reg, key, name, value(`sz:C:\`+addonsDir+`\`+ad.Name))
	}
	if c.Gecko {
		add(a.Gecko64, geckoKey+a.Gecko, "GeckoPath")
		add(a.Gecko32, geckoKey32+a.Gecko, "GeckoPath")
	}
	if c.Mono {
		add(a.Mono, monoKey, "RuntimePath")
	}
}

// addonsPending tells which add-on paths sets write. Wineboot must not
// register that add-on's dll before they are in the registry, or Wine
// offers the download; it registers it after, which takes the path up.
func addonsPending(sets map[string]map[string]value) (gecko, mono bool) {
	for key := range sets {
		switch {
		case strings.HasPrefix(key, geckoKey), strings.HasPrefix(key, geckoKey32):
			gecko = true
		case key == monoKey:
			mono = true
		}
	}
	return
}

// linkAddons makes the prefix's add-on links exactly want.
func linkAddons(pfx string, want map[string]string) error {
	dir := filepath.Join(pfx, "drive_c", filepath.FromSlash(strings.ReplaceAll(addonsDir, `\`, "/")))
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if _, ok := want[e.Name()]; !ok {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	if len(want) == 0 {
		os.Remove(dir)
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for name, target := range want {
		link := filepath.Join(dir, name)
		if cur, err := os.Readlink(link); err == nil && cur == target {
			continue
		}
		os.Remove(link)
		if err := os.Symlink(target, link); err != nil {
			return err
		}
	}
	return nil
}
