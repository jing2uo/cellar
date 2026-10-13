package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/jing2uo/cellar/internal/config"
	"github.com/jing2uo/cellar/internal/fonts"
	"github.com/jing2uo/cellar/internal/prefs"
	"github.com/jing2uo/cellar/internal/reconcile"
	"github.com/jing2uo/cellar/internal/runner"
	"github.com/jing2uo/cellar/internal/store"
)

// The commands here give the CLI what the GUI's settings, dependency and
// settings-page dialogs do. In a package the prefix argument is left out:
// a package has one, defined by its user's cellar.toml.

// prefixArg opens the prefix the arguments start with (none in a package)
// and returns the arguments after it.
func prefixArg(pos []string) (*store.Prefix, []string, error) {
	name := ""
	if !store.Packaged() {
		if len(pos) == 0 {
			return nil, nil, errors.New("which prefix?")
		}
		name, pos = pos[0], pos[1:]
	}
	p, err := store.Open(name)
	if err != nil {
		return nil, nil, err
	}
	for _, n := range p.Notes {
		fmt.Fprintln(os.Stderr, "cellar:", n)
	}
	return p, pos, nil
}

// applyNow applies the prefix, as a save in the GUI does.
func applyNow(p *store.Prefix) error {
	c, err := p.Load()
	if err != nil {
		return err
	}
	pl, err := reconcile.Compute(p, c, false)
	if err != nil {
		return err
	}
	if !jsonOut {
		printPlan(pl)
	}
	if len(pl.Changes) == 0 {
		return nil
	}
	return pl.Apply(func(s string) {
		if !jsonOut {
			fmt.Println("·", s)
		}
	})
}

// cmdConfig shows or changes a prefix's settings: what the GUI's settings
// dialog edits.
func cmdConfig(args []string) error {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	noApply := fs.Bool("no-apply", false, "only write cellar.toml")
	pos, _, err := parse(fs, args)
	if err != nil {
		return err
	}
	p, sets, err := prefixArg(pos)
	if err != nil {
		return err
	}
	st, err := config.ReadSettings(p.Config)
	if err != nil {
		return err
	}
	if len(sets) == 0 {
		if jsonOut {
			emit(st)
			return nil
		}
		show := func(k, v, note string) { fmt.Printf("%-8s %-22s %s\n", k, v, note) }
		show("runner", or(st.Runner, "(default)"), "")
		loc := or(st.Locale, "(host's)")
		for _, l := range config.Locales {
			if l.ID == st.Locale {
				loc += "  " + l.Name
			}
		}
		show("locale", loc, "")
		show("fonts", or(strings.Join(st.FontSets, ","), "none"), "core cjk all (all includes the other two)")
		show("winver", or(st.WindowsVersion, "(win10)"), "win11 win10 win81 win7 winxp")
		show("desktop", or(st.VirtualDesktop, "off"), "WxH or off")
		dpi := "auto"
		if st.DPI > 0 {
			dpi = strconv.Itoa(st.DPI)
		}
		show("dpi", dpi, "96 120 144 168 192, or auto")
		fmt.Printf("\nchange: cellar config %s key=value ...  (applies at once; --no-apply only writes)\n", or(p.Name, "<prefix>"))
		return nil
	}
	for _, kv := range sets {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return fmt.Errorf("%q: want key=value", kv)
		}
		switch k {
		case "runner":
			if v != "" && v != "system" && !filepath.IsAbs(v) && !slices.Contains(runner.List(), v) {
				return fmt.Errorf("runner %q is not installed (cellar runner list)", v)
			}
			st.Runner = v
		case "locale":
			st.Locale = v
		case "fonts":
			st.FontSets = []string{}
			if v != "none" && v != "" {
				for _, s := range strings.Split(v, ",") {
					if config.FontSetByID(s) == nil || s == "scripts" {
						return fmt.Errorf("font set %q (have core, cjk, all)", s)
					}
					st.FontSets = append(st.FontSets, s)
				}
			}
		case "winver":
			st.WindowsVersion = v
		case "desktop":
			if v == "off" {
				v = ""
			}
			st.VirtualDesktop = v
		case "dpi":
			if v == "auto" || v == "" {
				st.DPI = 0
			} else if st.DPI, err = strconv.Atoi(v); err != nil || st.DPI < 48 {
				return fmt.Errorf("dpi %q: a number like 120, or auto", v)
			}
		default:
			return fmt.Errorf("unknown setting %q (runner locale fonts winver desktop dpi)", k)
		}
	}
	if err := config.WriteSettings(p.Config, st); err != nil {
		return err
	}
	if *noApply {
		return nil
	}
	return applyNow(p)
}

// cmdDeps lists, adds and removes a prefix's winetricks verbs, or searches
// winetricks' list: the GUI's dependency dialog.
func cmdDeps(args []string) error {
	fs := flag.NewFlagSet("deps", flag.ContinueOnError)
	noApply := fs.Bool("no-apply", false, "only write cellar.toml")
	pos, _, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 && pos[0] == "search" {
		return depsSearch(pos[1:])
	}
	p, rest, err := prefixArg(pos)
	if err != nil {
		return err
	}
	want, err := config.ReadDependencies(p.Config)
	if err != nil {
		return err
	}
	b, _ := os.ReadFile(filepath.Join(p.Pfx, "winetricks.log"))
	done := strings.Fields(string(b))
	if len(rest) == 0 {
		if jsonOut {
			emit(map[string]any{"want": want, "installed": done})
			return nil
		}
		for _, v := range want {
			mark := "pending"
			if slices.Contains(done, v) {
				mark = "installed"
			}
			fmt.Printf("%-20s %s\n", v, mark)
		}
		var extra []string
		for _, v := range done {
			if !slices.Contains(want, v) && !slices.Contains(extra, v) {
				extra = append(extra, v)
			}
		}
		if len(extra) > 0 {
			fmt.Println("\nalso installed (not asked for any more, or by hand):", strings.Join(extra, " "))
		}
		if len(want) == 0 && len(extra) == 0 {
			fmt.Println("none. cellar deps search <word> finds verbs; cellar deps", or(p.Name, "<prefix>"), "add <verb>...")
		}
		return nil
	}
	op, verbs := rest[0], rest[1:]
	if len(verbs) == 0 || (op != "add" && op != "rm") {
		return errors.New("usage: cellar deps [<prefix>] [add|rm <verb>...] | cellar deps search <word>")
	}
	if op == "add" {
		known, err := runner.Verbs()
		if err == nil {
			for _, v := range verbs {
				if !slices.ContainsFunc(known, func(k runner.Verb) bool { return k.Verb == v }) {
					fmt.Fprintf(os.Stderr, "note: %s is not in winetricks' dll/font lists (cellar deps search); kept anyway\n", v)
				}
			}
		}
		for _, v := range verbs {
			if !slices.Contains(want, v) {
				want = append(want, v)
			}
		}
	} else {
		want = slices.DeleteFunc(want, func(v string) bool { return slices.Contains(verbs, v) })
		if inst := slices.DeleteFunc(slices.Clone(verbs), func(v string) bool { return !slices.Contains(done, v) }); len(inst) > 0 {
			fmt.Fprintln(os.Stderr, "note: winetricks cannot uninstall; still installed:", strings.Join(inst, " "))
		}
	}
	if err := config.WriteDependencies(p.Config, want); err != nil {
		return err
	}
	if *noApply || op == "rm" {
		return nil
	}
	return applyNow(p)
}

func depsSearch(words []string) error {
	verbs, err := runner.Verbs()
	if err != nil {
		return err
	}
	var hits []runner.Verb
	for _, v := range verbs {
		text := strings.ToLower(v.Verb + " " + v.Title + " " + v.By)
		if !slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(text, strings.ToLower(w)) }) {
			hits = append(hits, v)
		}
	}
	if jsonOut {
		emit(hits)
		return nil
	}
	for _, v := range hits {
		fmt.Printf("%-22s %-6s %s (%s)\n", v.Verb, v.Category, v.Title, v.By)
	}
	if len(hits) == 0 {
		fmt.Println("no verb matches; https://github.com/Winetricks/winetricks lists them all")
	}
	return nil
}

// cmdFonts is the settings page's font section: the library's packs and
// the font source policy.
func cmdFonts(args []string) error {
	pos, _, err := parse(flag.NewFlagSet("fonts", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		pos = []string{"list"}
	}
	switch pos[0] {
	case "list":
		pr, packs, sets := prefs.Load(), fonts.Packs(), fonts.Statuses()
		if jsonOut {
			emit(map[string]any{"source": pr.Fonts.Source, "packs": packs, "sets": sets, "library": fonts.LibraryDir()})
			return nil
		}
		fmt.Printf("source: %s   (cellar fonts policy host-first|library-only)\n\n", pr.Fonts.Source)
		for _, p := range packs {
			state := "not downloaded"
			switch {
			case p.Have == p.Files:
				state = "downloaded"
			case p.Have > 0:
				state = fmt.Sprintf("downloaded %d/%d", p.Have, p.Files)
			}
			if p.Host > 0 {
				state += fmt.Sprintf(", host has %d/%d", p.Host, p.Files)
			}
			if p.Missing > 0 && p.Have < p.Files {
				state += fmt.Sprintf("; get: %d (%.1f MB)", p.Missing, float64(p.MissSize)/1048576)
			}
			fmt.Printf("%-10s %5.1f MB  %-48s %s\n", p.ID, float64(p.Size)/1048576, state, p.Note)
		}
		fmt.Println()
		for _, s := range sets {
			use := s.Family + " (" + s.Source + ")"
			switch {
			case s.Source == "missing":
				use = "missing: cellar fonts get " + s.Pack
			case s.Source == "scripts":
				use = fmt.Sprintf("%d of %d scripts have a font", s.Have, s.Total)
			}
			fmt.Printf("set %-6s -> %s\n", s.ID, use)
		}
		fmt.Println("\nlibrary:", fonts.LibraryDir())
		return nil
	case "get", "rm":
		if len(pos) != 2 || config.PackByID(pos[1]) == nil {
			return fmt.Errorf("usage: cellar fonts %s cjk|cjk-serif|scripts", pos[0])
		}
		if pos[0] == "rm" {
			return fonts.RemovePack(pos[1])
		}
		return fonts.GetPack(pos[1], func(done, total int, file string) {
			if !jsonOut {
				fmt.Fprintf(os.Stderr, "\r%d/%d %-50s", done, total, file)
			}
		})
	case "policy":
		pr := prefs.Load()
		if len(pos) == 1 {
			fmt.Println(pr.Fonts.Source)
			return nil
		}
		pr.Fonts.Source = pos[1]
		return prefs.Save(pr)
	}
	return fmt.Errorf("unknown fonts command %q (list, get, rm, policy)", pos[0])
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
