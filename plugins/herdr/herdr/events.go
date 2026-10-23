package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

// Watcher keeps a live view of every agent's status. It subscribes to the
// herdr socket (events.subscribe: pane created, closed, agent detected, and
// pane.agent_status_changed for each known pane) and refreshes the agent
// list on each event. When the socket cannot be used it polls the list.
// Listeners are woken on every change.
type Watcher struct {
	Client Client
	Socket string
	// Poll is the fallback period, and the slow refresh beside events.
	Poll time.Duration

	mu        sync.RWMutex
	agents    map[string]Agent
	listeners map[int]chan struct{}
	next      int
	live      bool // the socket subscription is up
	lastErr   error
}

// NewWatcher makes a Watcher; Run starts it.
func NewWatcher(c Client, socket string) *Watcher {
	return &Watcher{Client: c, Socket: socket, Poll: 3 * time.Second, agents: map[string]Agent{}, listeners: map[int]chan struct{}{}}
}

// Agents returns the last known agents, sorted by pane id.
func (w *Watcher) Agents() []Agent {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]Agent, 0, len(w.agents))
	for _, a := range w.agents {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pane < out[j].Pane })
	return out
}

// Agent returns the last known state of one pane.
func (w *Watcher) Agent(pane string) (Agent, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	a, ok := w.agents[pane]
	return a, ok
}

// Live reports whether status comes from socket events (false: polling).
func (w *Watcher) Live() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.live
}

// Subscribe returns a channel that receives a value after each change, and
// a function that ends the subscription.
func (w *Watcher) Subscribe() (<-chan struct{}, func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	id := w.next
	w.next++
	ch := make(chan struct{}, 1)
	w.listeners[id] = ch
	return ch, func() {
		w.mu.Lock()
		delete(w.listeners, id)
		w.mu.Unlock()
	}
}

// Refresh reloads the agent list now and wakes listeners when it changed.
func (w *Watcher) Refresh(ctx context.Context) error {
	agents, err := w.Client.List(ctx)
	w.mu.Lock()
	defer w.mu.Unlock()
	if err != nil {
		w.lastErr = err
		return err
	}
	w.lastErr = nil
	next := make(map[string]Agent, len(agents))
	changed := len(agents) != len(w.agents)
	for _, a := range agents {
		next[a.Pane] = a
		if old, ok := w.agents[a.Pane]; !ok || old != a {
			changed = true
		}
	}
	w.agents = next
	if changed {
		for _, ch := range w.listeners {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}
	return nil
}

// Err is the last refresh error, or nil.
func (w *Watcher) Err() error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.lastErr
}

// Run keeps the view current until ctx ends.
func (w *Watcher) Run(ctx context.Context) {
	_ = w.Refresh(ctx)
	for ctx.Err() == nil {
		if w.Socket != "" {
			err := w.subscribe(ctx)
			w.mu.Lock()
			w.live = false
			w.mu.Unlock()
			if err == errResubscribe {
				continue
			}
		}
		// Fallback: poll for a while, then try the socket again.
		deadline := time.Now().Add(30 * time.Second)
		for ctx.Err() == nil && (w.Socket == "" || time.Now().Before(deadline)) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(w.Poll):
				_ = w.Refresh(ctx)
			}
		}
	}
}

type resubscribe struct{}

func (resubscribe) Error() string { return "resubscribe" }

var errResubscribe error = resubscribe{}

// subscribe holds one events.subscribe connection. It returns
// errResubscribe when the set of panes changed (pane status subscriptions
// name each pane), and another error when the socket failed.
func (w *Watcher) subscribe(ctx context.Context) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", w.Socket)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()

	known := map[string]bool{}
	subs := []map[string]any{{"type": "pane.created"}, {"type": "pane.closed"}, {"type": "pane.exited"}, {"type": "pane.agent_detected"}}
	for _, a := range w.Agents() {
		known[a.Pane] = true
		subs = append(subs, map[string]any{"type": "pane.agent_status_changed", "pane_id": a.Pane})
	}
	req, _ := json.Marshal(map[string]any{"id": "herdr-console", "method": "events.subscribe", "params": map[string]any{"subscriptions": subs}})
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return err
	}
	scan := bufio.NewScanner(conn)
	scan.Buffer(make([]byte, 64<<10), 4<<20)
	started := false
	slow := time.NewTicker(10 * w.Poll)
	defer slow.Stop()
	lines := make(chan []byte)
	done := make(chan error, 1)
	// quit ends the reader when subscribe returns for any reason (a
	// resubscribe, an error), not only when the Plugin stops; without it a
	// reader blocked on sending a line would wait for ever.
	quit := make(chan struct{})
	defer close(quit)
	go func() {
		for scan.Scan() {
			line := append([]byte(nil), scan.Bytes()...)
			select {
			case lines <- line:
			case <-quit:
				return
			case <-ctx.Done():
				return
			}
		}
		done <- scan.Err()
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-done:
			if err == nil {
				err = net.ErrClosed
			}
			return err
		case <-slow.C:
			_ = w.Refresh(ctx)
			if !samePanes(known, w.Agents()) {
				return errResubscribe
			}
		case line := <-lines:
			var msg struct {
				Result *struct {
					Type string `json:"type"`
				} `json:"result"`
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
				Event string `json:"event"`
			}
			if json.Unmarshal(line, &msg) != nil {
				continue
			}
			if msg.Error != nil {
				return &Error{Code: "subscribe", Message: msg.Error.Message}
			}
			if !started && msg.Result != nil && strings.Contains(msg.Result.Type, "subscription") {
				started = true
				w.mu.Lock()
				w.live = true
				w.mu.Unlock()
				continue
			}
			if msg.Event == "" {
				continue
			}
			_ = w.Refresh(ctx)
			if msg.Event != "pane.agent_status_changed" && !samePanes(known, w.Agents()) {
				return errResubscribe
			}
		}
	}
}

func samePanes(known map[string]bool, agents []Agent) bool {
	if len(known) != len(agents) {
		return false
	}
	for _, a := range agents {
		if !known[a.Pane] {
			return false
		}
	}
	return true
}
