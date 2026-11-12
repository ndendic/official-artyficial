package console

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// OpenedSession is a herdr session the console opened. Only these may be
// closed, taught, or sent to without a confirm (unless crew).
type OpenedSession struct {
	Pane      string    `json:"pane"`
	Tab       string    `json:"tab"`
	Workspace string    `json:"workspace"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Cwd       string    `json:"cwd"`
	OpenedAt  time.Time `json:"opened_at"`
	OpenedBy  string    `json:"opened_by"` // the browser session (unverified id)
	Taught    bool      `json:"taught"`
	// Companions are the other panes in Tab when the agent had started: the
	// ones herdr or a herdr plugin opened beside it (a sidebar). Close
	// closes the tab when only these are left; a pane not listed here is
	// one the person made, and the tab stays for it.
	Companions []string `json:"companions,omitempty"`
}

// Registry is the set of opened sessions, kept in the Plugin's data
// directory so a restart does not forget them. A lost file only makes the
// console more careful: every session then needs a confirm.
type Registry struct {
	mu   sync.Mutex
	path string
	m    map[string]OpenedSession
}

// LoadRegistry reads path ("" keeps it in memory only).
func LoadRegistry(path string) *Registry {
	r := &Registry{path: path, m: map[string]OpenedSession{}}
	if path == "" {
		return r
	}
	raw, err := os.ReadFile(path)
	if err == nil {
		var list []OpenedSession
		if json.Unmarshal(raw, &list) == nil {
			for _, o := range list {
				if o.Pane != "" {
					r.m[o.Pane] = o
				}
			}
		}
	}
	return r
}

// Get returns the opened session of a pane.
func (r *Registry) Get(pane string) (OpenedSession, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.m[pane]
	return o, ok
}

// Has reports whether the console opened pane.
func (r *Registry) Has(pane string) bool {
	_, ok := r.Get(pane)
	return ok
}

// All returns every opened session.
func (r *Registry) All() []OpenedSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]OpenedSession, 0, len(r.m))
	for _, o := range r.m {
		out = append(out, o)
	}
	return out
}

// Put adds or replaces an opened session.
func (r *Registry) Put(o OpenedSession) {
	r.mu.Lock()
	r.m[o.Pane] = o
	r.saveLocked()
	r.mu.Unlock()
}

// Update changes the opened session of pane in place; it does nothing
// when the pane is no longer registered (it was closed meanwhile).
func (r *Registry) Update(pane string, change func(*OpenedSession)) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.m[pane]
	if !ok {
		return false
	}
	change(&o)
	r.m[pane] = o
	r.saveLocked()
	return true
}

// Delete forgets a pane.
func (r *Registry) Delete(pane string) {
	r.mu.Lock()
	delete(r.m, pane)
	r.saveLocked()
	r.mu.Unlock()
}

func (r *Registry) saveLocked() {
	if r.path == "" {
		return
	}
	list := make([]OpenedSession, 0, len(r.m))
	for _, o := range r.m {
		list = append(list, o)
	}
	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(r.path), 0o700)
	tmp := r.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, r.path)
	}
}
