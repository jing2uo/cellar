package main

import (
	"errors"
	"flag"
	"fmt"
	"strconv"

	"github.com/jing2uo/cellar/internal/appmgr"
	"github.com/jing2uo/cellar/internal/config"
	"github.com/jing2uo/cellar/internal/reconcile"
	"github.com/jing2uo/cellar/internal/runner"
	"github.com/jing2uo/cellar/internal/store"
)

// The GUI's process manager on the command line: ps lists, kill ends,
// block keeps a program from running.

// afterPrefix is what follows the prefix among the positionals (inside a
// package, where the prefix is left out, all of them).
func afterPrefix(pos []string) []string {
	if !store.Packaged() && len(pos) > 0 {
		return pos[1:]
	}
	return pos
}

func cmdPs(args []string) error {
	fs := flag.NewFlagSet("ps", flag.ContinueOnError)
	all := fs.Bool("all", false, "include Wine's own processes")
	pos, _, err := parse(fs, args)
	if err != nil {
		return err
	}
	p, c, _, err := open(pos)
	if err != nil {
		return err
	}
	r, err := runner.Resolve(c.Runner)
	if err != nil {
		return err
	}
	ps, err := r.Tasks(p.Pfx, c.Env)
	if err != nil {
		return err
	}
	shown := []runner.Process{}
	for _, pr := range ps {
		if *all || !pr.System {
			shown = append(shown, pr)
		}
	}
	if jsonOut {
		emit(shown)
		return nil
	}
	if len(shown) == 0 {
		fmt.Println("nothing running")
		return nil
	}
	for _, pr := range shown {
		sys := ""
		if pr.System {
			sys = "  (wine)"
		}
		fmt.Printf("%7d  %8d KB  %s%s\n", pr.PID, pr.Memory, pr.Name, sys)
	}
	return nil
}

// cmdKill ends everything in the prefix, or the processes named: pids or
// .exe names (every process of that name).
func cmdKill(args []string) error {
	pos, _, err := parse(flag.NewFlagSet("kill", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	p, c, _, err := open(pos)
	if err != nil {
		return err
	}
	r, err := runner.Resolve(c.Runner)
	if err != nil {
		return err
	}
	which := afterPrefix(pos)
	if len(which) == 0 {
		r.Kill(p.Pfx, c.Env)
		return nil
	}
	var pids []int
	var names []string
	for _, w := range which {
		if n, err := strconv.Atoi(w); err == nil {
			pids = append(pids, n)
		} else {
			names = append(names, w)
		}
	}
	if err := r.TaskKill(p.Pfx, c.Env, pids); err != nil {
		return err
	}
	if len(names) > 0 {
		ended, err := appmgr.EndByName(p, c, names)
		if err != nil {
			return err
		}
		if len(ended) == 0 {
			return fmt.Errorf("none of %v is running", names)
		}
	}
	return nil
}

// cmdBlock lists the blocked programs, blocks .exe names (ending what runs
// of them) or, with --rm, lets them run again.
func cmdBlock(args []string) error {
	fs := flag.NewFlagSet("block", flag.ContinueOnError)
	rm := fs.Bool("rm", false, "let them run again")
	pos, _, err := parse(fs, args)
	if err != nil {
		return err
	}
	p, _, _, err := open(pos)
	if err != nil {
		return err
	}
	names := afterPrefix(pos)
	if len(names) > 0 {
		if err := appmgr.Block(p, names, !*rm); err != nil {
			return busyHint(err, p.Name)
		}
	}
	blocked, err := config.ReadBlocked(p.Config)
	if err != nil {
		return err
	}
	if jsonOut {
		emit(blocked)
		return nil
	}
	if len(names) == 0 && len(blocked) == 0 {
		fmt.Println("nothing blocked")
	}
	for _, b := range blocked {
		fmt.Println(b)
	}
	return nil
}

// busyHint says how to get past reconcile.ErrBusy.
func busyHint(err error, prefix string) error {
	if !errors.Is(err, reconcile.ErrBusy) {
		return err
	}
	if store.Packaged() {
		return fmt.Errorf("%w (see `cellar ps`; `cellar apply --kill` ends them and applies)", err)
	}
	return fmt.Errorf("%w (see `cellar ps %s`; `cellar apply %s --kill` ends them and applies)", err, prefix, prefix)
}
