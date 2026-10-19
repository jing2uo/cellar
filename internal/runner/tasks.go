package runner

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Process is one Windows process of a prefix, as tasklist reports it.
type Process struct {
	Name   string `json:"name"`
	PID    int    `json:"pid"`
	Memory int64  `json:"memory"` // KB
	// System marks Wine's own processes (services, explorer and the like);
	// ending one takes the prefix down with it.
	System bool `json:"system,omitempty"`
}

var systemProcs = map[string]bool{
	"start.exe": true, "services.exe": true, "winedevice.exe": true, "svchost.exe": true,
	"plugplay.exe": true, "rpcss.exe": true, "explorer.exe": true, "conhost.exe": true,
	"wineboot.exe": true, "winedbg.exe": true, "tabtip.exe": true,
}

// Tasks lists the prefix's processes. It asks the wineserver (tasklist), so
// processes started from another Flatpak instance, out of this one's sight
// in /proc, are there too. With no wineserver up nothing runs, and none is
// started just to say so. env is the prefix's (its locale above all: see
// Command).
func (r *Runner) Tasks(prefix string, env map[string]string) ([]Process, error) {
	if !ServerUp(prefix) {
		return []Process{}, nil
	}
	out, err := r.Command(prefix, env, "tasklist", "/fo", "csv", "/nh").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, cmdError("tasklist", err, ee.Stderr)
		}
		return nil, fmt.Errorf("tasklist: %w", err)
	}
	return parseTasklist(out)
}

// parseTasklist reads `tasklist /fo csv /nh`:
// "WXWork.exe","248","Console","1","808,648 K". The tasklist.exe that wrote
// it is left out.
func parseTasklist(out []byte) ([]Process, error) {
	rd := csv.NewReader(bytes.NewReader(out))
	rd.FieldsPerRecord = -1
	recs, err := rd.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("tasklist: %w", err)
	}
	ps := []Process{}
	for _, rec := range recs {
		if len(rec) < 2 {
			continue
		}
		pid, err := strconv.Atoi(rec[1])
		if err != nil {
			continue
		}
		name := strings.ToLower(rec[0])
		if name == "tasklist.exe" {
			continue
		}
		p := Process{Name: rec[0], PID: pid, System: systemProcs[name]}
		if len(rec) >= 5 {
			mem := strings.NewReplacer(",", "", ".", "", " ", "", "K", "").Replace(rec[4])
			p.Memory, _ = strconv.ParseInt(mem, 10, 64)
		}
		ps = append(ps, p)
	}
	return ps, nil
}

// TaskKill ends processes by pid (taskkill /f).
func (r *Runner) TaskKill(prefix string, env map[string]string, pids []int) error {
	if len(pids) == 0 {
		return nil
	}
	args := []string{"taskkill", "/f"}
	for _, p := range pids {
		args = append(args, "/pid", strconv.Itoa(p))
	}
	if out, err := r.Command(prefix, env, args...).CombinedOutput(); err != nil {
		return cmdError("taskkill", err, out)
	}
	return nil
}

// cmdError names a failed wine command with its output, or is ErrNoAgent.
func cmdError(name string, err error, out []byte) error {
	out = bytes.TrimSpace(out)
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == proxyNoAgent && bytes.Contains(out, []byte(ErrNoAgent.Error())) {
		return ErrNoAgent
	}
	if len(out) == 0 {
		return fmt.Errorf("%s: %w", name, err)
	}
	return fmt.Errorf("%s: %v\n%s", name, err, out)
}
