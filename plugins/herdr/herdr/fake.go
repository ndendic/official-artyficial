package herdr

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Fake is an in-memory herdr for tests. It records every input it is given
// so a test can assert that nothing reached a pane.
type Fake struct {
	mu      sync.Mutex
	agents  map[string]Agent
	screens map[string]string
	spaces  []Workspace
	nextTab int

	Prompts []Input // every Prompt, in order
	Keys    []Input // every SendKeys, in order
	Closed  []string
	Started []string
}

// Input is one recorded input.
type Input struct {
	Pane string
	Text string
}

// NewFake returns a Fake holding agents.
func NewFake(agents ...Agent) *Fake {
	f := &Fake{agents: map[string]Agent{}, screens: map[string]string{}}
	for _, a := range agents {
		f.agents[a.Pane] = a
	}
	return f
}

// Put adds or replaces an agent.
func (f *Fake) Put(a Agent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agents[a.Pane] = a
}

// SetScreen sets what Read returns for pane.
func (f *Fake) SetScreen(pane, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.screens[pane] = text
}

// SetWorkspaces sets the workspaces Workspaces returns, in herdr's order.
func (f *Fake) SetWorkspaces(spaces ...Workspace) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spaces = append([]Workspace(nil), spaces...)
}

func (f *Fake) List(context.Context) ([]Agent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []Agent{}
	for _, a := range f.agents {
		out = append(out, a)
	}
	return out, nil
}

func (f *Fake) Get(_ context.Context, pane string) (Agent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.agents[pane]
	if !ok {
		return Agent{}, &Error{Code: "agent_not_found", Message: "agent target " + pane + " not found"}
	}
	return a, nil
}

func (f *Fake) Read(_ context.Context, pane string, _ int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.screens[pane], nil
}

func (f *Fake) Prompt(_ context.Context, pane, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.agents[pane]; !ok {
		return &Error{Code: "agent_not_found", Message: pane}
	}
	f.Prompts = append(f.Prompts, Input{pane, text})
	return nil
}

func (f *Fake) SendKeys(_ context.Context, pane string, keys []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Keys = append(f.Keys, Input{pane, strings.Join(keys, " ")})
	return nil
}

func (f *Fake) Workspaces(context.Context) ([]Workspace, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Workspace(nil), f.spaces...), nil
}

func (f *Fake) OpenTab(_ context.Context, label, cwd, tabLabel string) (Opened, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ws := ""
	for _, w := range f.spaces {
		if w.Label == label {
			ws = w.ID
		}
	}
	created := false
	if ws == "" {
		ws = fmt.Sprintf("w%d", 90+len(f.spaces))
		f.spaces = append(f.spaces, Workspace{ID: ws, Label: label})
		created = true
	}
	f.nextTab++
	return Opened{Workspace: ws, Tab: fmt.Sprintf("%s:t%d", ws, f.nextTab), Pane: fmt.Sprintf("%s:p%d", ws, f.nextTab), NewWorkspace: created}, nil
}

func (f *Fake) Start(_ context.Context, name, kind, pane string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	ws, _, _ := strings.Cut(pane, ":")
	f.agents[pane] = Agent{Pane: pane, Tab: strings.Replace(pane, ":p", ":t", 1), Workspace: ws, Name: name, Kind: kind, Status: "idle"}
	f.Started = append(f.Started, name)
	return nil
}

func (f *Fake) ClosePane(_ context.Context, pane string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Closed = append(f.Closed, pane)
	delete(f.agents, pane)
	return nil
}
