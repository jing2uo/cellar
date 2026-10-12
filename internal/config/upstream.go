package config

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// MergeUpstream folds a package update into the user's cellar.toml at
// path: base is the package's cellar.toml the user's copy last took, next
// the new one. Per key - top-level keys, each table's keys, [[apps]] by id
// - a change upstream is taken when the user left the key as base had it;
// when the user changed it too, the user's value stays and the key is
// returned as a conflict. Edits go through the Editor, so the user's
// comments stay.
func MergeUpstream(path string, base, next []byte) ([]string, error) {
	var b, n, u map[string]any
	if err := toml.Unmarshal(base, &b); err != nil {
		return nil, fmt.Errorf("package config (old): %w", err)
	}
	if err := toml.Unmarshal(next, &n); err != nil {
		return nil, fmt.Errorf("package config (new): %w", err)
	}
	e, err := Edit(path)
	if err != nil {
		return nil, err
	}
	if err := toml.Unmarshal([]byte(strings.Join(e.lines, "\n")), &u); err != nil {
		return nil, err
	}
	var conflicts []string
	// take decides one key: true when next's value is to be written.
	take := func(name string, bv, nv, uv any) bool {
		if reflect.DeepEqual(bv, nv) {
			return false
		}
		if reflect.DeepEqual(uv, bv) {
			return true
		}
		if !reflect.DeepEqual(uv, nv) {
			conflicts = append(conflicts, name)
		}
		return false
	}
	for _, k := range keysOf(b, n) {
		bv, nv, uv := b[k], n[k], u[k]
		switch {
		case k == "apps":
			bm, nm, um := appsByID(bv), appsByID(nv), appsByID(uv)
			for _, id := range keysOf(bm, nm) {
				if !take("apps."+id, bm[id], nm[id], um[id]) {
					continue
				}
				e.RemoveApp(id)
				if app, ok := nm[id].(map[string]any); ok {
					block := []string{"", "[[apps]]"}
					// id first, the way cellar writes apps.
					for _, ak := range append([]string{"id"}, keysOf(app)...) {
						if ak == "id" && len(block) > 2 {
							continue
						}
						lit, err := literalOf(app[ak])
						if err != nil {
							return nil, err
						}
						block = append(block, tomlKey(ak)+" = "+lit)
					}
					e.lines = append(e.lines, block...)
				}
			}
		case isTable(bv) || isTable(nv):
			bt, nt, ut := table(bv), table(nv), table(uv)
			for _, sk := range keysOf(bt, nt) {
				if isTable(bt[sk]) || isTable(nt[sk]) {
					// Deeper tables ([registry."HKCU\..."]) are not merged
					// by line: the user's stays, flagged when upstream moved.
					if !reflect.DeepEqual(bt[sk], nt[sk]) {
						conflicts = append(conflicts, k+"."+sk)
					}
					continue
				}
				if take(k+"."+sk, bt[sk], nt[sk], ut[sk]) {
					lit, err := literalOf(nt[sk])
					if err != nil {
						return nil, err
					}
					e.SetIn(k, sk, lit)
				}
			}
		default:
			if take(k, bv, nv, uv) {
				lit, err := literalOf(nv)
				if err != nil {
					return nil, err
				}
				e.Set(k, lit)
			}
		}
	}
	return conflicts, e.Save()
}

// literalOf renders a value as a TOML literal for Set; nil is "" (remove).
func literalOf(v any) (string, error) {
	if v == nil {
		return "", nil
	}
	b, err := toml.Marshal(map[string]any{"v": v})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(b)), "v =")), nil
}

func isTable(v any) bool { _, ok := v.(map[string]any); return ok }

func table(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func appsByID(v any) map[string]any {
	out := map[string]any{}
	list, _ := v.([]any)
	for _, a := range list {
		if m, ok := a.(map[string]any); ok {
			out[fmt.Sprint(m["id"])] = m
		}
	}
	return out
}

func keysOf[V any](ms ...map[string]V) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range ms {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}
