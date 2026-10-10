//go:build !nogui

package gui

import (
	"fmt"
	"sync"
	"time"
)

// Long work (wineboot, winetricks downloads) runs as a task the page polls,
// so the window never blocks and the log is visible as it happens.
type Task struct {
	ID     string    `json:"id"`
	Prefix string    `json:"prefix"`
	Title  string    `json:"title"`
	Done   bool      `json:"done"`
	Error  string    `json:"error,omitempty"`
	Lines  []string  `json:"lines"`
	Start  time.Time `json:"start"`
}

type tasks struct {
	mu   sync.Mutex
	n    int
	byID map[string]*Task
}

func (t *tasks) start(prefix, title string, fn func(log func(string)) error) string {
	t.mu.Lock()
	t.n++
	id := fmt.Sprintf("t%d", t.n)
	task := &Task{ID: id, Prefix: prefix, Title: title, Lines: []string{}, Start: time.Now()}
	if t.byID == nil {
		t.byID = map[string]*Task{}
	}
	t.byID[id] = task
	t.mu.Unlock()
	go func() {
		err := fn(func(line string) {
			t.mu.Lock()
			task.Lines = append(task.Lines, line)
			if len(task.Lines) > 400 {
				task.Lines = task.Lines[len(task.Lines)-400:]
			}
			t.mu.Unlock()
		})
		t.mu.Lock()
		task.Done = true
		if err != nil {
			task.Error = err.Error()
		}
		t.mu.Unlock()
	}()
	return id
}

func (t *tasks) get(id string) (Task, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	task, ok := t.byID[id]
	if !ok {
		return Task{}, false
	}
	cp := *task
	cp.Lines = append([]string{}, task.Lines...)
	return cp, true
}

// running is the unfinished task of a prefix, if any: one at a time, since
// two applies or winetricks runs on one prefix would race.
func (t *tasks) running(prefix string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	for id, task := range t.byID {
		if task.Prefix == prefix && !task.Done {
			return id
		}
	}
	return ""
}
