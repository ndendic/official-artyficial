// Package herdr talks to a herdr server: the CLI for every request
// (JSON output, HERDR_SOCKET_PATH set), and the socket API's
// events.subscribe for live status, with a polling fallback.
package herdr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Agent is one live agent as herdr reports it.
type Agent struct {
	Pane        string `json:"pane_id"`
	Tab         string `json:"tab_id"`
	Workspace   string `json:"workspace_id"`
	Name        string `json:"name"`
	Title       string `json:"terminal_title_stripped"`
	Kind        string `json:"agent"`
	Status      string `json:"agent_status"`
	Cwd         string `json:"cwd"`
	Focused     bool   `json:"focused"`
	SessionID   string `json:"-"`
	SessionKind string `json:"-"`
}

// Label is the name a person knows the agent by: its herdr name, else its
// terminal title, else its pane id.
func (a Agent) Label() string {
	switch {
	case a.Name != "":
		return a.Name
	case strings.TrimSpace(a.Title) != "":
		return strings.TrimSpace(a.Title)
	}
	return a.Pane
}

// Workspace is one herdr workspace.
type Workspace struct {
	ID      string `json:"workspace_id"`
	Label   string `json:"label"`
	Focused bool   `json:"focused"`
}

// Opened is what creating a place for a new agent returned.
type Opened struct {
	Workspace string
	Tab       string
	Pane      string
	// NewWorkspace is true when the workspace was created for it.
	NewWorkspace bool
}

// Client is everything the console needs from herdr. The CLI implements
// it; tests use Fake.
type Client interface {
	List(ctx context.Context) ([]Agent, error)
	Get(ctx context.Context, pane string) (Agent, error)
	Read(ctx context.Context, pane string, lines int) (string, error)
	Prompt(ctx context.Context, pane, text string) error
	SendKeys(ctx context.Context, pane string, keys []string) error
	Workspaces(ctx context.Context) ([]Workspace, error)
	// OpenTab makes a shell pane for a new agent in the workspace labelled
	// label (created when missing), never taking focus.
	OpenTab(ctx context.Context, label, cwd, tabLabel string) (Opened, error)
	Start(ctx context.Context, name, kind, pane string) error
	// ClosePane closes one pane; herdr removes its tab and workspace
	// when they are left empty.
	ClosePane(ctx context.Context, pane string) error
}

// Error is a herdr server error (JSON on stderr) or a CLI failure.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

// Code returns the herdr error code of err, or "".
func Code(err error) string {
	var he *Error
	if errors.As(err, &he) {
		return he.Code
	}
	return ""
}

// CLI runs the herdr binary.
type CLI struct {
	Bin    string
	Socket string
	// Timeout bounds one command that does not wait on an agent.
	Timeout time.Duration
}

// FindBin returns bin when it is on PATH or a path that exists, else
// ~/.local/bin/herdr when that exists, else bin.
func FindBin(bin string) string {
	if bin == "" {
		bin = "herdr"
	}
	if p, err := exec.LookPath(bin); err == nil {
		return p
	}
	if home := Home(); home != "" {
		p := filepath.Join(home, ".local", "bin", "herdr")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return bin
}

func (c *CLI) run(ctx context.Context, timeout time.Duration, args ...string) ([]byte, error) {
	if timeout == 0 {
		timeout = c.Timeout
	}
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Bin, args...)
	cmd.Env = append(os.Environ(), "HERDR_SOCKET_PATH="+c.Socket)
	// The Plugin is not a pane: never let an inherited pane context
	// retarget a command.
	cmd.Env = withoutPaneContext(cmd.Env)
	if os.Getenv("HOME") == "" {
		if h := Home(); h != "" {
			cmd.Env = append(cmd.Env, "HOME="+h)
		}
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil {
		return nil, cliError(args, stderr.Bytes(), err)
	}
	return stdout.Bytes(), nil
}

func withoutPaneContext(env []string) []string {
	out := env[:0:0]
	for _, kv := range env {
		if strings.HasPrefix(kv, "HERDR_PANE_ID=") || strings.HasPrefix(kv, "HERDR_TAB_ID=") || strings.HasPrefix(kv, "HERDR_WORKSPACE_ID=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func cliError(args []string, stderr []byte, err error) error {
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	text := strings.TrimSpace(string(stderr))
	if i := strings.Index(text, "{"); i >= 0 && json.Unmarshal([]byte(text[i:]), &body) == nil && (body.Error.Code != "" || body.Error.Message != "") {
		return &Error{Code: body.Error.Code, Message: body.Error.Message}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Code: "timeout", Message: "herdr " + args[0] + " did not answer in time"}
	}
	var ee *exec.Error
	if errors.As(err, &ee) {
		return &Error{Code: "herdr_missing", Message: "cannot run herdr: " + ee.Error()}
	}
	if text == "" {
		text = err.Error()
	}
	if len(text) > 300 {
		text = text[:300] + "…"
	}
	return &Error{Message: "herdr " + strings.Join(args[:min(2, len(args))], " ") + ": " + text}
}

type rawAgent struct {
	Agent
	Session *struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
	} `json:"agent_session"`
	NameP *string `json:"name"`
}

func (r rawAgent) agent() Agent {
	a := r.Agent
	if r.NameP != nil {
		a.Name = *r.NameP
	}
	if r.Session != nil {
		a.SessionKind, a.SessionID = r.Session.Kind, r.Session.Value
	}
	return a
}

// List returns every live agent.
func (c *CLI) List(ctx context.Context) ([]Agent, error) {
	out, err := c.run(ctx, 0, "agent", "list")
	if err != nil {
		return nil, err
	}
	return parseList(out)
}

func parseList(out []byte) ([]Agent, error) {
	var body struct {
		Result struct {
			Agents []rawAgent `json:"agents"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		return nil, fmt.Errorf("herdr agent list: %w", err)
	}
	agents := make([]Agent, 0, len(body.Result.Agents))
	for _, r := range body.Result.Agents {
		agents = append(agents, r.agent())
	}
	return agents, nil
}

// Get returns one agent by pane id.
func (c *CLI) Get(ctx context.Context, pane string) (Agent, error) {
	out, err := c.run(ctx, 0, "agent", "get", pane)
	if err != nil {
		return Agent{}, err
	}
	var body struct {
		Result struct {
			Agent rawAgent `json:"agent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		return Agent{}, fmt.Errorf("herdr agent get: %w", err)
	}
	return body.Result.Agent.agent(), nil
}

// Read returns the pane's recent text with soft wraps joined.
func (c *CLI) Read(ctx context.Context, pane string, lines int) (string, error) {
	out, err := c.run(ctx, 0, "agent", "read", pane, "--source", "recent-unwrapped", "--lines", fmt.Sprint(lines))
	return string(out), err
}

// Prompt submits text to the agent (no wait: the console follows the
// reply itself).
func (c *CLI) Prompt(ctx context.Context, pane, text string) error {
	_, err := c.run(ctx, 30*time.Second, "agent", "prompt", pane, text)
	return err
}

// SendKeys writes logical keys to the agent.
func (c *CLI) SendKeys(ctx context.Context, pane string, keys []string) error {
	_, err := c.run(ctx, 0, append([]string{"agent", "send-keys", pane}, keys...)...)
	return err
}

// Workspaces lists herdr's workspaces.
func (c *CLI) Workspaces(ctx context.Context) ([]Workspace, error) {
	out, err := c.run(ctx, 0, "workspace", "list")
	if err != nil {
		return nil, err
	}
	var body struct {
		Result struct {
			Workspaces []Workspace `json:"workspaces"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		return nil, fmt.Errorf("herdr workspace list: %w", err)
	}
	return body.Result.Workspaces, nil
}

// OpenTab finds or creates the workspace labelled label and makes a new
// tab in it, never taking focus.
func (c *CLI) OpenTab(ctx context.Context, label, cwd, tabLabel string) (Opened, error) {
	spaces, err := c.Workspaces(ctx)
	if err != nil {
		return Opened{}, err
	}
	// workspace create can move focus despite --no-focus (seen with herdr 0.9),
	// so the focused workspace is put back, as in Start.
	var console string
	defer func() { c.keepFocus(ctx, spaces, &console)() }()
	for _, w := range spaces {
		if w.Label == label {
			out, err := c.run(ctx, 0, "tab", "create", "--workspace", w.ID, "--cwd", cwd, "--label", tabLabel, "--no-focus")
			if err != nil {
				return Opened{}, err
			}
			o, err := parseCreated(out)
			o.Workspace = w.ID
			console = w.ID
			return o, err
		}
	}
	out, err := c.run(ctx, 0, "workspace", "create", "--cwd", cwd, "--label", label, "--no-focus")
	if err != nil {
		return Opened{}, err
	}
	o, err := parseCreated(out)
	if err != nil {
		return o, err
	}
	o.NewWorkspace = true
	console = o.Workspace
	if tabLabel != "" && o.Tab != "" {
		_, _ = c.run(ctx, 0, "tab", "rename", o.Tab, tabLabel)
	}
	return o, nil
}

func parseCreated(out []byte) (Opened, error) {
	var body struct {
		Result struct {
			Workspace struct {
				ID string `json:"workspace_id"`
			} `json:"workspace"`
			Tab struct {
				ID          string `json:"tab_id"`
				WorkspaceID string `json:"workspace_id"`
			} `json:"tab"`
			RootPane struct {
				ID string `json:"pane_id"`
			} `json:"root_pane"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		return Opened{}, fmt.Errorf("herdr create: %w", err)
	}
	o := Opened{Workspace: body.Result.Workspace.ID, Tab: body.Result.Tab.ID, Pane: body.Result.RootPane.ID}
	if o.Workspace == "" {
		o.Workspace = body.Result.Tab.WorkspaceID
	}
	if o.Pane == "" || o.Tab == "" {
		return o, &Error{Message: "herdr did not return the new tab and pane"}
	}
	return o, nil
}

// Start starts an agent of kind in pane under name. A pane whose shell is
// not yet at its prompt is retried for a few seconds.
func (c *CLI) Start(ctx context.Context, name, kind, pane string) error {
	// herdr's agent start focuses the workspace of the pane it starts in
	// (seen with herdr 0.9). The console must never take the user's focus, so the
	// workspace focused before the start is focused again after it.
	if spaces, err := c.Workspaces(ctx); err == nil {
		ws, _, _ := strings.Cut(pane, ":")
		defer c.keepFocus(ctx, spaces, &ws)()
	}
	var err error
	for i := 0; i < 5; i++ {
		_, err = c.run(ctx, 70*time.Second, "agent", "start", name, "--kind", kind, "--pane", pane, "--timeout", "60000")
		if err == nil || Code(err) == "agent_not_ready" || ctx.Err() != nil {
			return err
		}
		if code := Code(err); code != "" && !strings.Contains(code, "not_available") && !strings.Contains(code, "busy") && !strings.Contains(code, "shell") {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return err
}

// keepFocus returns a func that puts focus back on the workspace focused in
// spaces when herdr moved it to the console's own workspace (*console, read
// when the func runs). herdr moves focus a moment after a create or an agent
// start returns, so it watches for focusWatch. It never moves focus away
// from any other workspace, so a change the user makes is left alone.
func (c *CLI) keepFocus(ctx context.Context, spaces []Workspace, console *string) func() {
	prev := ""
	for _, w := range spaces {
		if w.Focused {
			prev = w.ID
		}
	}
	return func() {
		if prev == "" || console == nil || *console == "" || *console == prev {
			return
		}
		ctx := context.WithoutCancel(ctx)
		for end := time.Now().Add(focusWatch); ; {
			if c.focusedWorkspace(ctx) == *console {
				_, _ = c.run(ctx, 10*time.Second, "workspace", "focus", prev)
			}
			if time.Now().After(end) {
				return
			}
			time.Sleep(250 * time.Millisecond)
		}
	}
}

// focusWatch is how long keepFocus watches for herdr's late focus move.
var focusWatch = 2 * time.Second

// focusedWorkspace is the id of herdr's focused workspace, or "" when it
// cannot be read.
func (c *CLI) focusedWorkspace(ctx context.Context) string {
	spaces, err := c.Workspaces(ctx)
	if err != nil {
		return ""
	}
	for _, w := range spaces {
		if w.Focused {
			return w.ID
		}
	}
	return ""
}

// ClosePane closes one pane, never the rest of its tab.
func (c *CLI) ClosePane(ctx context.Context, pane string) error {
	_, err := c.run(ctx, 0, "pane", "close", pane)
	return err
}

// NamePattern is herdr's rule for agent names.
var NamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// Kinds are the agent kinds herdr 0.9 can start (herdr agent).
var Kinds = []string{"claude", "codex", "pi", "gemini", "cursor", "devin", "agy", "cline", "omp", "mastracode", "opencode", "copilot", "kimi", "kiro", "droid", "amp", "grok", "hermes", "kilo", "qodercli", "qwen", "letta", "maki", "muse"}

// KnownKind reports whether kind is one herdr can start.
func KnownKind(kind string) bool {
	for _, k := range Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// Raw runs any herdr command and returns its stdout (tests and tools).
func (c *CLI) Raw(ctx context.Context, args ...string) ([]byte, error) {
	return c.run(ctx, 0, args...)
}
