// Package about describes cellar and the system it runs on, for a bug
// report: the GUI's General tab and `cellar about`.
package about

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/jing2uo/cellar/internal/config"
	"github.com/jing2uo/cellar/internal/runner"
	"github.com/jing2uo/cellar/internal/store"
)

type Info struct {
	Version string `json:"version"`
	Install string `json:"install"` // flatpak | host
	Home    string `json:"home"`
	Runner  string `json:"runner"` // the default, with its version
	OS      string `json:"os"`
	Runtime string `json:"runtime,omitempty"` // the flatpak runtime
	Kernel  string `json:"kernel"`
	Arch    string `json:"arch"`
	Desktop string `json:"desktop"`
	Locale  string `json:"locale"`
	Go      string `json:"go"`
}

func Get(version string) Info {
	a := Info{Version: version, Install: "host", Home: store.Home(), Arch: runtime.GOARCH, Go: runtime.Version(),
		Locale: config.HostLocale()}
	a.OS = osName("/etc/os-release")
	if runner.InFlatpak() {
		a.Install, a.Runtime, a.OS = "flatpak", a.OS, osName("/run/host/os-release")
	}
	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		a.Kernel = strings.TrimSpace(string(b))
	}
	if def, err := runner.Default(); err == nil {
		a.Runner = def
		for _, r := range runner.Infos() {
			if r.Name == def && r.Version != "" {
				a.Runner += " (" + r.Version + ")"
			}
		}
	}
	a.Desktop = strings.TrimSpace(os.Getenv("XDG_CURRENT_DESKTOP") + " " + os.Getenv("XDG_SESSION_TYPE"))
	return a
}

// String is the report as the GUI copies it: one "key: value" a line,
// empty values left out.
func (a Info) String() string {
	var b strings.Builder
	for _, kv := range [][2]string{
		{"Version", a.Version}, {"Install", a.Install}, {"Data", a.Home}, {"Default runner", a.Runner},
		{"OS", a.OS}, {"Flatpak runtime", a.Runtime}, {"Kernel", a.Kernel + " · " + a.Arch},
		{"Desktop", a.Desktop}, {"Locale", a.Locale}, {"Go", a.Go},
	} {
		if kv[1] != "" {
			fmt.Fprintf(&b, "%s: %s\n", kv[0], kv[1])
		}
	}
	return b.String()
}

// osName is an os-release file's PRETTY_NAME.
func osName(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(l, "PRETTY_NAME="); ok {
			return strings.Trim(v, `"'`)
		}
	}
	return ""
}
