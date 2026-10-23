package herdr

import (
	"bufio"
	"context"
	"errors"
	"net"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestParseList(t *testing.T) {
	out := []byte(`{"id":"cli:agent:list","result":{"agents":[
	 {"agent":"claude","agent_session":{"agent":"claude","kind":"id","source":"herdr:claude","value":"9f4c"},"agent_status":"done","cwd":"/p","focused":false,"name":null,"pane_id":"w1:p1","tab_id":"w1:t1","terminal_title_stripped":"Reviewer","workspace_id":"w1"},
	 {"agent":"codex","agent_status":"idle","cwd":"/q","name":"docs-site","pane_id":"w1:p2","tab_id":"w1:t2","terminal_title_stripped":"docs-site","workspace_id":"w1"}]}}`)
	agents, err := parseList(out)
	if err != nil || len(agents) != 2 {
		t.Fatalf("agents = %+v, %v", agents, err)
	}
	a := agents[0]
	if a.Name != "" || a.Label() != "Reviewer" || a.SessionID != "9f4c" || a.Kind != "claude" || a.Status != "done" || a.Tab != "w1:t1" {
		t.Fatalf("first = %+v", a)
	}
	if agents[1].Label() != "docs-site" || agents[1].SessionID != "" {
		t.Fatalf("second = %+v", agents[1])
	}
}

func TestParseCreated(t *testing.T) {
	o, err := parseCreated([]byte(`{"result":{"workspace":{"workspace_id":"wC"},"tab":{"tab_id":"wC:t1","workspace_id":"wC"},"root_pane":{"pane_id":"wC:p1"}}}`))
	if err != nil || o.Workspace != "wC" || o.Tab != "wC:t1" || o.Pane != "wC:p1" {
		t.Fatalf("o = %+v, %v", o, err)
	}
	if _, err := parseCreated([]byte(`{"result":{}}`)); err == nil {
		t.Fatal("a create without a pane was accepted")
	}
}

func TestCLIError(t *testing.T) {
	err := cliError([]string{"agent", "get"}, []byte(`{"error":{"code":"agent_not_found","message":"agent target w99:p1 not found"},"id":"cli:agent:get"}`), errors.New("exit status 1"))
	if Code(err) != "agent_not_found" {
		t.Fatalf("err = %v", err)
	}
	err = cliError([]string{"agent", "get"}, []byte("boom"), errors.New("exit status 2"))
	if Code(err) != "" || err.Error() == "" {
		t.Fatalf("err = %v", err)
	}
}

func TestWatcherNotifiesOnChange(t *testing.T) {
	f := NewFake(Agent{Pane: "w1:p1", Status: "idle"})
	w := NewWatcher(f, "")
	ch, stop := w.Subscribe()
	defer stop()
	ctx := context.Background()
	if err := w.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("no wake on the first list")
	}
	_ = w.Refresh(ctx)
	select {
	case <-ch:
		t.Fatal("woken without a change")
	default:
	}
	f.Put(Agent{Pane: "w1:p1", Status: "working"})
	_ = w.Refresh(ctx)
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("no wake on a status change")
	}
	if a, ok := w.Agent("w1:p1"); !ok || a.Status != "working" {
		t.Fatalf("agent = %+v", a)
	}
}

func TestKnownKindAndNames(t *testing.T) {
	if !KnownKind("claude") || KnownKind("vim") {
		t.Fatal("KnownKind")
	}
	if !NamePattern.MatchString("claude-docs-site") || NamePattern.MatchString("Core") || NamePattern.MatchString("a b") {
		t.Fatal("NamePattern")
	}
}

// When subscribe returns errResubscribe in the middle of a burst of events,
// its socket reader ends too instead of waiting for ever to hand over the
// next line.
func TestSubscribeReaderEndsOnResubscribe(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "h.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	f := NewFake(Agent{Pane: "w1:p1", Status: "idle"})
	w := NewWatcher(f, sock)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_ = w.Refresh(ctx)
	served := make(chan net.Conn, 1)
	wrote := make(chan struct{})
	before := runtime.NumGoroutine()
	go func() {
		defer close(wrote)
		c, err := ln.Accept()
		if err != nil {
			return
		}
		served <- c
		bufio.NewReader(c).ReadString('\n')
		f.Put(Agent{Pane: "w1:p2", Status: "idle"}) // the pane set changes
		var burst strings.Builder
		burst.WriteString(`{"id":"herdr-console","result":{"type":"subscription_started"}}` + "\n")
		for i := 0; i < 50; i++ {
			burst.WriteString(`{"event":"pane.created","data":{}}` + "\n")
		}
		c.Write([]byte(burst.String()))
	}()
	if err := w.subscribe(ctx); err != errResubscribe {
		t.Fatalf("subscribe = %v, want errResubscribe", err)
	}
	t.Cleanup(func() {
		select {
		case c := <-served:
			c.Close()
		default:
		}
	})
	<-wrote
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before {
		if time.Now().After(deadline) {
			t.Fatalf("the socket reader leaked: %d goroutines, %d before", runtime.NumGoroutine(), before)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
