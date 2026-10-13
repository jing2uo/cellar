package runner

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jing2uo/cellar/internal/store"
)

// A Source is a family of downloadable Wine builds published as GitHub
// release assets: Kron4ek's Wine and Proton builds, and Bottles' vaniglia.
// The Proton build is Valve's Wine as a plain runner, without umu or the
// Steam runtime.
type Source struct {
	ID    string // family id, e.g. kron4ek-staging-wow64
	Name  string
	Repo  string
	Asset *regexp.Regexp // matches the asset; group 1 is the version
	// Tag and AssetFor rebuild assets from release tags alone, for when the
	// GitHub API is out of reach (its anonymous quota is per IP and shared
	// proxies exhaust it): Tag matches a tag, AssetFor names its asset.
	Tag      *regexp.Regexp
	AssetFor func(tag string) string
	Kind     string // wow64 | legacy
	Note     string
	Default  bool // the one to suggest when no runner is installed
}

var Sources = []Source{
	{ID: "kron4ek-staging-wow64", Name: "Wine Staging (Kron4ek, WoW64)", Repo: "Kron4ek/Wine-Builds",
		Asset: regexp.MustCompile(`^wine-([0-9.]+(?:-rc[0-9]+)?)-staging-amd64-wow64\.tar\.xz$`), Kind: "wow64", Default: true,
		Tag: kronTag, AssetFor: func(t string) string { return "wine-" + t + "-staging-amd64-wow64.tar.xz" },
		Note: "推荐：最新 Wine Staging，无需 32 位系统库"},
	{ID: "kron4ek-wow64", Name: "Wine (Kron4ek, WoW64)", Repo: "Kron4ek/Wine-Builds",
		Asset: regexp.MustCompile(`^wine-([0-9.]+(?:-rc[0-9]+)?)-amd64-wow64\.tar\.xz$`), Kind: "wow64",
		Tag: kronTag, AssetFor: func(t string) string { return "wine-" + t + "-amd64-wow64.tar.xz" },
		Note: "上游原版 Wine"},
	{ID: "kron4ek-tkg-wow64", Name: "Wine Staging-TkG (Kron4ek, WoW64)", Repo: "Kron4ek/Wine-Builds",
		Asset: regexp.MustCompile(`^wine-([0-9.]+(?:-rc[0-9]+)?)-staging-tkg-amd64-wow64\.tar\.xz$`), Kind: "wow64",
		Tag: kronTag, AssetFor: func(t string) string { return "wine-" + t + "-staging-tkg-amd64-wow64.tar.xz" },
		Note: "Staging 加 TkG 补丁集"},
	{ID: "kron4ek-proton-wow64", Name: "Proton (Kron4ek, WoW64)", Repo: "Kron4ek/Wine-Builds",
		Asset: regexp.MustCompile(`^wine-proton-([0-9][0-9.]*-[0-9]+)-amd64-wow64\.tar\.xz$`), Kind: "wow64",
		Tag: regexp.MustCompile(`^proton-[0-9][0-9.]*-[0-9]+$`), AssetFor: func(t string) string { return "wine-" + t + "-amd64-wow64.tar.xz" },
		Note: "Valve Proton 的 Wine，带游戏相关补丁；不含 DXVK，需要时用 winetricks 装"},
	{ID: "kron4ek-staging", Name: "Wine Staging (Kron4ek, 传统 32 位)", Repo: "Kron4ek/Wine-Builds",
		Asset: regexp.MustCompile(`^wine-([0-9.]+(?:-rc[0-9]+)?)-staging-amd64\.tar\.xz$`), Kind: "legacy",
		Tag: kronTag, AssetFor: func(t string) string { return "wine-" + t + "-staging-amd64.tar.xz" },
		Note: "老式 WoW，需要 32 位系统库"},
	{ID: "vaniglia", Name: "Vaniglia (Bottles)", Repo: "bottlesdevs/wine",
		Tag: bottlesTag("vaniglia"), AssetFor: func(t string) string { return t + "-x86_64.tar.gz" },
		Asset: regexp.MustCompile(`^vaniglia-([0-9][0-9.]*)-x86_64\.tar\.gz$`), Kind: "legacy",
		Note: "Bottles 打包的原版 Wine，需要 32 位系统库"},
}

var kronTag = regexp.MustCompile(`^[0-9]+\.[0-9]+(?:-rc[0-9]+)?$`)

func bottlesTag(family string) *regexp.Regexp {
	return regexp.MustCompile(`^` + family + `-[0-9][0-9.]*(?:-[0-9]+)?$`)
}

func source(id string) (*Source, error) {
	for i := range Sources {
		if Sources[i].ID == id {
			return &Sources[i], nil
		}
	}
	ids := make([]string, len(Sources))
	for i, s := range Sources {
		ids[i] = s.ID
	}
	return nil, fmt.Errorf("unknown runner source %q (have: %s)", id, strings.Join(ids, ", "))
}

type Release struct {
	ID        string    `json:"id"` // <source>@<version>: what `runner install` takes
	Source    string    `json:"source"`
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	Kind      string    `json:"kind"`
	URL       string    `json:"url"`
	Size      int64     `json:"size"`
	Published time.Time `json:"published"`
	Installed bool      `json:"installed"`
	Usable    bool      `json:"usable"` // kind works in this environment as-is
	Note      string    `json:"note,omitempty"`
	Default   bool      `json:"default,omitempty"`
}

// InstallName is the runners/ directory a release unpacks to.
func (r Release) InstallName() string { return r.Source + "-" + r.Version }

type ghRelease struct {
	Published time.Time `json:"published_at"`
	Assets    []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// Available lists the newest releases of every source (perSource each),
// newest first within a source. GitHub answers are cached for an hour.
func Available(perSource int) ([]Release, []string, error) {
	installed := map[string]bool{}
	for _, n := range List() {
		installed[n] = true
	}
	legacyOK := len(problems("legacy")) == 0
	var out []Release
	var warns []string
	byRepo := map[string][]ghRelease{}
	for _, s := range Sources {
		rels, ok := byRepo[s.Repo]
		if !ok {
			var err error
			if rels, err = releases(s.Repo); err != nil {
				warns = append(warns, err.Error())
			}
			byRepo[s.Repo] = rels
		}
		n := 0
		for _, gr := range rels {
			for _, a := range gr.Assets {
				m := s.Asset.FindStringSubmatch(a.Name)
				if m == nil {
					continue
				}
				r := Release{
					ID: s.ID + "@" + m[1], Source: s.ID, Name: s.Name, Version: m[1], Kind: s.Kind,
					URL: a.URL, Size: a.Size, Published: gr.Published, Note: s.Note,
					Default: s.Default && n == 0, Usable: s.Kind == "wow64" || legacyOK,
				}
				r.Installed = installed[r.InstallName()]
				out = append(out, r)
				n++
			}
			if n >= perSource {
				break
			}
		}
	}
	if len(out) == 0 && len(warns) > 0 {
		return nil, warns, fmt.Errorf("no runner releases reachable: %s", strings.Join(warns, "; "))
	}
	return out, warns, nil
}

// Find resolves "<source>" (newest) or "<source>@<version>".
func Find(id string) (*Release, error) {
	src, ver, _ := strings.Cut(id, "@")
	if _, err := source(src); err != nil {
		return nil, err
	}
	rels, _, err := Available(1000)
	if err != nil {
		return nil, err
	}
	for _, r := range rels {
		if r.Source == src && (ver == "" || r.Version == ver) {
			return &r, nil
		}
	}
	return nil, fmt.Errorf("no release %q", id)
}

func releases(repo string) ([]ghRelease, error) {
	cache := filepath.Join(store.Home(), "cache", strings.ReplaceAll(repo, "/", "_")+".json")
	if fi, err := os.Stat(cache); err == nil && time.Since(fi.ModTime()) < time.Hour {
		if b, err := os.ReadFile(cache); err == nil {
			var rels []ghRelease
			if json.Unmarshal(b, &rels) == nil {
				return rels, nil
			}
		}
	}
	rels, err := apiReleases(repo)
	if err != nil {
		var ferr error
		if rels, ferr = feedReleases(repo); ferr != nil {
			return nil, fmt.Errorf("%v; release feed: %v", err, ferr)
		}
	}
	sort.SliceStable(rels, func(i, j int) bool { return rels[i].Published.After(rels[j].Published) })
	if b, err := json.Marshal(rels); err == nil {
		os.MkdirAll(filepath.Dir(cache), 0o755)
		os.WriteFile(cache, b, 0o644)
	}
	return rels, nil
}

func token() string {
	for _, k := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	if out, err := exec.Command("gh", "auth", "token").Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}

func apiReleases(repo string) ([]ghRelease, error) {
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/"+repo+"/releases?per_page=100", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	if tok := token(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API %s: %s", repo, resp.Status)
	}
	var rels []ghRelease
	return rels, json.NewDecoder(resp.Body).Decode(&rels)
}

var feedEntry = regexp.MustCompile(`(?s)<entry>.*?<updated>([^<]+)</updated>.*?/releases/tag/([^"]+)"`)

// feedReleases reads the releases Atom feed (the newest ten, no quota) and
// rebuilds each source's asset from the tag. Sizes are unknown here.
func feedReleases(repo string) ([]ghRelease, error) {
	resp, err := http.Get("https://github.com/" + repo + "/releases.atom")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s", resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var rels []ghRelease
	for _, m := range feedEntry.FindAllStringSubmatch(string(b), -1) {
		tag := html.UnescapeString(m[2])
		var gr ghRelease
		gr.Published, _ = time.Parse(time.RFC3339, m[1])
		for _, s := range Sources {
			if s.Repo != repo || s.Tag == nil || !s.Tag.MatchString(tag) {
				continue
			}
			name := s.AssetFor(tag)
			gr.Assets = append(gr.Assets, ghAsset{Name: name, URL: "https://github.com/" + repo + "/releases/download/" + tag + "/" + name})
		}
		if len(gr.Assets) > 0 {
			rels = append(rels, gr)
		}
	}
	if len(rels) == 0 {
		return nil, fmt.Errorf("no releases in feed")
	}
	return rels, nil
}
