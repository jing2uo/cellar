// Package runner resolves, lists and installs Wine builds.
//
// A runner is a directory with bin/wine (the layout of Kron4ek, Bottles and
// WineHQ builds). Two kinds:
//
//   - "system": the Wine the environment provides. Inside the Flatpak that
//     is org.winehq.Wine installed with flatpak (or a packaged Wine app's
//     base under /app); on the host it is the distribution's or WineHQ's,
//     if any.
//   - downloaded builds under <cellar home>/runners/<name>.
//
// A prefix names one in cellar.toml; an absolute directory works too.
package runner

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jing2uo/cellar/internal/store"
	"github.com/ulikunitz/xz"
)

type Runner struct {
	Name       string            `json:"name"`
	Dir        string            `json:"dir"`
	Wine       string            `json:"wine"`
	Wineserver string            `json:"wineserver"`
	Env        map[string]string `json:"env,omitempty"` // under the prefix's env (runnerEnv)
}

// ErrNone means no runner is available at all; the fix is to download one.
var ErrNone = errors.New("no Wine found: install your distribution's wine (in the Flatpak: flatpak install " + FlatpakWine + "), or download one with `cellar runner available` / `cellar runner install " + defaultSource() + "`")

// FlatpakWine is the org.winehq.Wine branch built on the Flatpak's runtime.
const FlatpakWine = "flathub org.winehq.Wine//stable-26.08"

func defaultSource() string {
	for _, s := range Sources {
		if s.Default {
			return s.ID
		}
	}
	return Sources[0].ID
}

func Resolve(name string) (*Runner, error) {
	switch {
	case name == "":
		d, err := Default()
		if err != nil {
			return nil, err
		}
		return Resolve(d)
	case name == "system":
		dirs := systemDirs()
		if len(dirs) == 0 {
			if InFlatpak() {
				return nil, errors.New(`runner "system": no Wine found; flatpak install ` + FlatpakWine + `, or pick a downloaded runner`)
			}
			return nil, errors.New(`runner "system": no Wine installed on this system; pick a downloaded runner instead`)
		}
		return at("system", dirs[0])
	case filepath.IsAbs(name):
		return at(name, name)
	}
	return at(name, filepath.Join(store.RunnersDir(), name))
}

// wineRoot is where a runner's bin/wine is: the directory itself, or its
// files/ in Proton's layout (GE-Proton, a Steam Proton copied over).
func wineRoot(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "bin", "wine")); err != nil {
		if _, err := os.Stat(filepath.Join(dir, "files", "bin", "wine")); err == nil {
			return filepath.Join(dir, "files")
		}
	}
	return dir
}

func at(name, dir string) (*Runner, error) {
	dir = wineRoot(dir)
	r := &Runner{Name: name, Dir: dir, Wine: filepath.Join(dir, "bin", "wine"), Wineserver: wineserver(dir), Env: runnerEnv(dir)}
	if _, err := os.Stat(r.Wine); err != nil {
		return nil, fmt.Errorf("runner %q: %s missing (have: %s)", name, r.Wine, strings.Join(Names(), ", "))
	}
	return r, nil
}

// wineserver is next to wine in portable builds; Debian keeps it in
// /usr/lib/wine.
func wineserver(dir string) string {
	for _, p := range []string{"bin/wineserver", "lib/wine/wineserver", "lib/wine/wineserver64"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err == nil {
			return filepath.Join(dir, p)
		}
	}
	if p, err := exec.LookPath("wineserver"); err == nil {
		return p
	}
	return filepath.Join(dir, "bin", "wineserver")
}

func (r *Runner) Version() string {
	cmd := exec.Command(r.Wine, "--version")
	cmd.Env = r.Environ("", nil)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// List returns the downloaded runners' names.
func List() []string {
	ents, _ := os.ReadDir(store.RunnersDir())
	var out []string
	for _, e := range ents {
		if _, err := os.Stat(filepath.Join(wineRoot(filepath.Join(store.RunnersDir(), e.Name())), "bin", "wine")); err == nil {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// Names is every usable runner name: "system" when present, then downloads.
func Names() []string {
	var out []string
	if len(systemDirs()) > 0 {
		out = append(out, "system")
	}
	return append(out, List()...)
}

// Default is what a new prefix gets: the system Wine when there is one,
// else the newest download.
func Default() (string, error) {
	n := Names()
	if len(n) == 0 {
		return "", ErrNone
	}
	if n[0] == "system" {
		return "system", nil
	}
	return n[len(n)-1], nil
}

type Info struct {
	Name     string   `json:"name"`
	Source   string   `json:"source"` // system | downloaded
	Dir      string   `json:"dir"`
	Version  string   `json:"version"`
	Kind     string   `json:"kind"` // wow64 | legacy
	Problems []string `json:"problems,omitempty"`
	Default  bool     `json:"default,omitempty"`
}

// Infos describes every runner. On the host, every system Wine found is
// listed (the first is "system", others by directory).
func Infos() []Info {
	def, _ := Default()
	var out []Info
	add := func(name, src, dir string) {
		r, err := at(name, dir)
		if err != nil {
			return
		}
		k := kindOf(dir)
		out = append(out, Info{Name: name, Source: src, Dir: dir, Version: r.Version(), Kind: k, Problems: problems(k), Default: name == def})
	}
	for i, d := range systemDirs() {
		name := d
		if i == 0 {
			name = "system"
		}
		add(name, "system", d)
	}
	for _, n := range List() {
		add(n, "downloaded", filepath.Join(store.RunnersDir(), n))
	}
	return out
}

// Install downloads a .tar.xz / .tar.gz build and unpacks it as runners/<name>.
// name defaults to the archive's top-level directory.
func Install(url, name string, progress io.Writer) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	var body io.Reader = resp.Body
	if progress != nil {
		body = io.TeeReader(body, &counter{w: progress, total: resp.ContentLength})
	}
	switch {
	case strings.HasSuffix(url, ".tar.xz"):
		body, err = xz.NewReader(body)
	case strings.HasSuffix(url, ".tar.gz"):
		body, err = gzip.NewReader(body)
	default:
		return "", fmt.Errorf("unsupported archive %s", path.Base(url))
	}
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(store.RunnersDir(), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(store.RunnersDir(), ".install-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	top, err := untar(tar.NewReader(body), tmp)
	if err != nil {
		return "", err
	}
	if name == "" {
		name = top
	}
	src := filepath.Join(tmp, top)
	if _, err := os.Stat(filepath.Join(src, "bin", "wine")); err != nil {
		return "", fmt.Errorf("archive has no %s/bin/wine", top)
	}
	dst := filepath.Join(store.RunnersDir(), name)
	if _, err := os.Stat(dst); err == nil {
		return "", fmt.Errorf("runner %q already installed", name)
	}
	return name, os.Rename(src, dst)
}

// untar extracts into dir, rejecting paths that escape it, and returns the
// single top-level directory name.
func untar(tr *tar.Reader, dir string) (string, error) {
	top := ""
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		clean := filepath.Clean(h.Name)
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
			return "", fmt.Errorf("unsafe path in archive: %s", h.Name)
		}
		first := strings.SplitN(clean, "/", 2)[0]
		if top == "" {
			top = first
		} else if first != top {
			return "", errors.New("archive has more than one top-level directory")
		}
		dst := filepath.Join(dir, clean)
		switch h.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(dst, 0o755)
		case tar.TypeReg:
			err = writeFile(dst, tr, os.FileMode(h.Mode)&0o777)
		case tar.TypeSymlink:
			err = os.Symlink(h.Linkname, dst)
		case tar.TypeLink:
			err = os.Link(filepath.Join(dir, filepath.Clean(h.Linkname)), dst)
		}
		if err != nil {
			return "", err
		}
	}
	return top, nil
}

func writeFile(dst string, r io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

type counter struct {
	w           io.Writer
	total, n    int64
	lastPercent int64
}

func (c *counter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	if c.total > 0 {
		if pc := c.n * 100 / c.total; pc/10 != c.lastPercent/10 {
			c.lastPercent = pc
			fmt.Fprintf(c.w, "  %d%%\n", pc)
		}
	}
	return len(p), nil
}

// Command builds a wine invocation against prefix. env is layered over the
// caller's environment; WINEDEBUG defaults to quiet. Pass the prefix's env
// (config.Env, which holds its locale) even for a tool: Wine started under
// another code page puts its own Tahoma and Microsoft Sans Serif font links
// back, over cellar's, and the GUI's own locale is not the prefix's (in the
// Flatpak, LC_CTYPE=C.UTF-8). With the prefix's wineserver in another
// Flatpak instance, the command goes through an agent there (agent.go).
func (r *Runner) Command(prefix string, env map[string]string, args ...string) *exec.Cmd {
	cmd := exec.Command(r.Wine, args...)
	cmd.Env = r.Environ(prefix, env)
	if serverOwner(prefix) == foreignServer {
		if self, err := os.Executable(); err == nil {
			cmd.Path, cmd.Args = self, append([]string{self, ProxyCommand, r.Wine}, args...)
		}
	}
	return cmd
}

// Server runs wineserver with args (-w waits for it to exit, -k kills it).
func (r *Runner) Server(prefix string, args ...string) error {
	cmd := exec.Command(r.Wineserver, args...)
	cmd.Env = r.Environ(prefix, nil)
	return cmd.Run()
}

// Kill ends every process in the prefix and waits for its wineserver to go.
// wineboot -k asks the server itself (-s: the desktop process too), through
// an agent for a server in another flatpak instance. wineserver -k signals
// the pid holding the server's lock, which such a server keeps out of
// sight, so it is left out then; with no agent there it fails and ends
// nothing.
func (r *Runner) Kill(prefix string, env map[string]string) error {
	if !ServerUp(prefix) {
		return nil
	}
	if serverOwner(prefix) == foreignServer {
		if out, err := r.Command(prefix, env, "wineboot", "-k", "-s").CombinedOutput(); err != nil {
			return cmdError("wineboot -k", err, out)
		}
	} else {
		r.Command(prefix, env, "wineboot", "-k", "-s").Run()
		r.Server(prefix, "-k")
	}
	r.Server(prefix, "-w")
	return nil
}

// ServerUp tells whether a wineserver serves the prefix: its socket is in
// /tmp/.wine-<uid>/server-<dev>-<inode>, as Wine names it.
func ServerUp(prefix string) bool {
	dir := serverDir(prefix)
	if dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, "socket"))
	return err == nil
}

// Environ is the environment for the runner's programs in prefix: the
// caller's, the runner's own, then env. An empty prefix leaves WINEPREFIX
// as it is.
func (r *Runner) Environ(prefix string, env map[string]string) []string {
	m := map[string]string{"WINEDEBUG": "-all"}
	for k, v := range r.Env {
		m[k] = v
	}
	for k, v := range env {
		m[k] = v
	}
	if prefix != "" {
		m["WINEPREFIX"] = prefix
	}
	out := make([]string, 0, len(os.Environ())+len(m))
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if _, ok := m[k]; !ok {
			out = append(out, kv)
		}
	}
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

// InstallID installs "<source>[@<version>]" from the catalog, or a URL.
func InstallID(id string, progress io.Writer) (string, error) {
	if strings.Contains(id, "://") {
		return Install(id, "", progress)
	}
	r, err := Find(id)
	if err != nil {
		return "", err
	}
	if r.Installed {
		return r.InstallName(), nil
	}
	return Install(r.URL, r.InstallName(), progress)
}

const winetricksURL = "https://raw.githubusercontent.com/Winetricks/winetricks/master/src/winetricks"

// Winetricks finds winetricks: the system's (a packaged Wine app's base
// ships one), else a copy cellar downloads once into its data dir.
func Winetricks() (string, error) {
	if p, err := exec.LookPath("winetricks"); err == nil {
		return p, nil
	}
	p := filepath.Join(store.Home(), "tools", "winetricks")
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	resp, err := http.Get(winetricksURL)
	if err != nil {
		return "", fmt.Errorf("download winetricks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download winetricks: %s", resp.Status)
	}
	if err := writeFile(p, resp.Body, 0o755); err != nil {
		return "", err
	}
	return p, nil
}
