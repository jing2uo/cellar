package appmgr

import (
	"strings"

	"github.com/jing2uo/cellar/internal/config"
	"github.com/jing2uo/cellar/internal/reconcile"
	"github.com/jing2uo/cellar/internal/runner"
	"github.com/jing2uo/cellar/internal/store"
)

// Block keeps programs (by .exe name) from running: they go into
// cellar.toml's dll_overrides as disabled, which applies at once (live,
// with programs running), and the running copies are ended. block false
// lets them run again. reconcile.ErrBusy: written, but other pending
// changes need the prefix's programs ended before anything applies.
func Block(p *store.Prefix, names []string, block bool) error {
	if err := config.SetBlocked(p.Config, names, block); err != nil {
		return err
	}
	c, err := p.Load()
	if err != nil {
		return err
	}
	pl, err := reconcile.Compute(p, c, false)
	if err != nil {
		return err
	}
	if err := pl.Apply(func(string) {}); err != nil || !block {
		return err
	}
	_, err = EndByName(p, c, names)
	return err
}

// EndByName ends the prefix's processes with these .exe names (any case)
// and gives their pids.
func EndByName(p *store.Prefix, c *config.Config, names []string) ([]int, error) {
	r, err := runner.Resolve(c.Runner)
	if err != nil {
		return nil, err
	}
	ps, err := r.Tasks(p.Pfx, c.Env)
	if err != nil {
		return nil, err
	}
	var pids []int
	for _, pr := range ps {
		for _, n := range names {
			if strings.EqualFold(pr.Name, n) {
				pids = append(pids, pr.PID)
				break
			}
		}
	}
	return pids, r.TaskKill(p.Pfx, c.Env, pids)
}
