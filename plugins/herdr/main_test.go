package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ndendic/data-start/api/manifest"
	"github.com/ndendic/data-start/sdk/go/publisher"

	"herdr-console/console"
	"herdr-console/herdr"
)

func TestManifestDecodesAndValidates(t *testing.T) {
	raw, err := os.ReadFile("manifest.template.json")
	if err != nil {
		t.Fatal(err)
	}
	var m manifest.Manifest
	if err := manifest.Decode(raw, &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	m.Platform.Targets = []string{runtime.GOOS + "/" + runtime.GOARCH}
	m.Files = map[string]string{"provider": strings.Repeat("ab", 32)}
	if err := m.Validate("1.0.0", runtime.GOOS+"/"+runtime.GOARCH); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !m.DeclaresCaller() {
		t.Fatal("the manifest does not ask for Caller Tickets")
	}
	if len(m.Requirements.HostAccess) != 1 || m.Requirements.HostAccess[0] != "herdr-socket" {
		t.Fatalf("host_access = %v", m.Requirements.HostAccess)
	}
	d := m.Capabilities[0]
	if d.ID != consoleID || d.Version != consoleVersion || len(d.Examples) == 0 {
		t.Fatalf("descriptor = %s %s", d.ID, d.Version)
	}
	for _, ex := range d.Examples {
		if err := d.Request.ValidateJSON(ex.Request); err != nil {
			t.Errorf("example %q request: %v", ex.Title, err)
		}
		if len(ex.Result) > 0 {
			if err := d.Result.ValidateJSON(ex.Result); err != nil {
				t.Errorf("example %q result: %v", ex.Title, err)
			}
		}
		// Every example request is one the Plugin decodes.
		if _, f := decodeInput(ex.Request); f != nil {
			t.Errorf("example %q does not decode: %s", ex.Title, f.Message)
		}
	}
}

func TestEveryResultFitsTheSchema(t *testing.T) {
	raw, _ := os.ReadFile("manifest.template.json")
	var m manifest.Manifest
	if err := manifest.Decode(raw, &m); err != nil {
		t.Fatal(err)
	}
	results := []map[string]any{
		{"ok": true, "view": "picker", "sessions": 2},
		{"ok": true, "view": "agents", "sessions": 9},
		{"ok": true, "view": "confirm", "sent": false},
		{"ok": true, "following": ""},
		{"ok": true, "view": "picker", "sessions": 1, "closed": "w1:p1"},
		{"ok": true, "view": "thread", "pane": "w1:p1", "source": "screen", "taught": true},
		{"ok": false, "message": "x"},
	}
	for _, r := range results {
		b, _ := json.Marshal(r)
		if err := m.Capabilities[0].Result.ValidateJSON(b); err != nil {
			t.Errorf("%s: %v", b, err)
		}
	}
}

func TestStdioFrames(t *testing.T) {
	var got []string
	h := func(_ context.Context, ticket string, req console.Request) (map[string]any, *console.Fault) {
		got = append(got, ticket+"/"+req.Action)
		return map[string]any{"ok": true}, nil
	}
	in := strings.Join([]string{
		`{"id":"1","capability":"artyficial.herdr.console","version":"1.0.0","input":{"action":"open"},"caller":{"ticket":"ct1.x.y"}}`,
		`{"id":"2","capability":"artyficial.herdr.console","version":"1.0.0","input":{"action":"open","actor":"admin"}}`,
		`{"id":"3","capability":"other","version":"1.0.0","input":{}}`,
		`{"id":"4","shutdown":true}`,
	}, "\n") + "\n"
	var out bytes.Buffer
	serve(context.Background(), strings.NewReader(in), &out, h)
	frames := map[string]manifest.WireResponse{}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var first manifest.WireResponse
	_ = json.Unmarshal([]byte(lines[0]), &first)
	if !first.Ready {
		t.Fatalf("first frame = %s", lines[0])
	}
	for _, l := range lines[1:] {
		var r manifest.WireResponse
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			t.Fatal(err)
		}
		frames[r.ID] = r
	}
	if r := frames["1"]; r.Fault != nil || string(r.Result) != `{"ok":true}` {
		t.Fatalf("frame 1 = %+v", r)
	}
	if r := frames["2"]; r.Fault == nil || r.Fault.Code != "invalid_argument" {
		t.Fatalf("an unknown input key was accepted: %+v", r)
	}
	if r := frames["3"]; r.Fault == nil || r.Fault.Code != "not_found" {
		t.Fatalf("frame 3 = %+v", r)
	}
	if _, ok := frames["4"]; !ok {
		t.Fatal("no shutdown answer")
	}
	if len(got) != 1 || got[0] != "ct1.x.y/open" {
		t.Fatalf("handler saw %v", got)
	}
}

func TestConfig(t *testing.T) {
	dir := t.TempDir()
	cfg, err := loadConfig(filepath.Join(dir, "missing.json"))
	if err != nil || cfg.ConsoleWorkspace != "Console" || len(cfg.Crew) != 0 || !strings.HasSuffix(cfg.HerdrSocket, "/.config/herdr/herdr.sock") {
		t.Fatalf("defaults = %+v, %v", cfg, err)
	}
	path := filepath.Join(dir, "c.json")
	_ = os.WriteFile(path, []byte(`{"herdr_socket":"~/x.sock","crew":["a"],"console_workspace":"Console-test"}`), 0o600)
	cfg, err = loadConfig(path)
	if err != nil || cfg.ConsoleWorkspace != "Console-test" || len(cfg.Crew) != 1 || !filepath.IsAbs(cfg.HerdrSocket) {
		t.Fatalf("cfg = %+v, %v", cfg, err)
	}
	_ = os.WriteFile(path, []byte(`{"page":"x"}`), 0o600)
	if _, err := loadConfig(path); err == nil {
		t.Fatal("an unknown key was accepted")
	}
	_ = os.WriteFile(path, []byte(`{"console_workspace":""}`), 0o600)
	if _, err := loadConfig(path); err == nil {
		t.Fatal("an empty console_workspace was accepted")
	}
}

// TestPluginEnvReadsNewNameFirst pins the one-release transition: the Plugin
// starts under RT_PLUGIN_* and still under the previous DATA_START_PLUGIN_*.
func TestPluginEnvReadsNewNameFirst(t *testing.T) {
	t.Setenv("RT_PLUGIN_DATA_DIR", "")
	t.Setenv("DATA_START_PLUGIN_DATA_DIR", "/old")
	if got := pluginEnv("DATA_DIR"); got != "/old" {
		t.Fatalf("fallback: got %q, want /old", got)
	}
	t.Setenv("RT_PLUGIN_DATA_DIR", "/new")
	if got := pluginEnv("DATA_DIR"); got != "/new" {
		t.Fatalf("new name first: got %q, want /new", got)
	}
	t.Setenv("RT_PLUGIN_DATA_DIR", "")
	t.Setenv("DATA_START_PLUGIN_DATA_DIR", "")
	if got := pluginEnv("DATA_DIR"); got != "" {
		t.Fatalf("unset: got %q, want empty", got)
	}
}

// nopPatcher takes every patch: the replies below are about the reply, not
// about what reached a page.
type nopPatcher struct{ id string }

func (nopPatcher) PatchElements(context.Context, string, string, ...publisher.PatchOption) error {
	return nil
}
func (nopPatcher) PatchSignals(context.Context, json.RawMessage, bool) error { return nil }
func (nopPatcher) Renew(string)                                              {}
func (p nopPatcher) SessionID() string                                       { return p.id }
func (nopPatcher) Expires() time.Time                                        { return time.Now().Add(time.Hour) }

func loadConsoleDescriptor(t *testing.T) manifest.Manifest {
	t.Helper()
	raw, err := os.ReadFile("manifest.template.json")
	if err != nil {
		t.Fatal(err)
	}
	var m manifest.Manifest
	if err := manifest.Decode(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// Every field the Plugin decodes from a request is declared by the request
// schema (core refuses an undeclared key before the Plugin sees it), and the
// schema declares nothing else.
func TestRequestSchemaDeclaresEveryRequestField(t *testing.T) {
	d := loadConsoleDescriptor(t).Capabilities[0]
	rt := reflect.TypeOf(console.Request{})
	fields := map[string]bool{}
	for i := 0; i < rt.NumField(); i++ {
		name, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		fields[name] = true
		prop, ok := d.Request.Properties[name]
		if !ok {
			t.Errorf("request schema lacks %q (Request.%s)", name, rt.Field(i).Name)
			continue
		}
		want := map[reflect.Kind]string{reflect.String: "string", reflect.Bool: "boolean", reflect.Slice: "array"}[rt.Field(i).Type.Kind()]
		if prop.Type != want {
			t.Errorf("request property %q is %s, the field is %s", name, prop.Type, want)
		}
	}
	for name := range d.Request.Properties {
		if !fields[name] {
			t.Errorf("request schema declares %q, which Request does not have", name)
		}
	}
}

// Real replies of Handle fit the result schema, as core checks them.
func TestRealRepliesFitTheResultSchema(t *testing.T) {
	d := loadConsoleDescriptor(t).Capabilities[0]
	reviewer := herdr.Agent{Pane: "w1:p1", Workspace: "w1", Title: "Reviewer", Kind: "claude", Status: "idle", Cwd: "/tmp"}
	stranger := herdr.Agent{Pane: "w9:p1", Workspace: "w9", Name: "scratch", Kind: "codex", Status: "idle", Cwd: "/tmp"}

	ctx, cancel := context.WithCancel(context.Background())
	h := herdr.NewFake(reviewer, stranger)
	c := console.New(ctx, console.Config{Crew: []string{"reviewer"}, ConsoleWorkspace: "Console", DataDir: t.TempDir(), ClaudeProjects: t.TempDir()}, h, nil,
		func(ticket string) console.Patcher { return nopPatcher{ticket} })
	c.Tick = 20 * time.Millisecond
	t.Cleanup(func() { c.Close(); cancel() })

	var sawViews, sawAgent, sawNullAgent bool
	do := func(label, ticket string, req console.Request) {
		t.Helper()
		res, f := c.Handle(ctx, ticket, req)
		if res == nil {
			t.Logf("%s: a refusal (%v), no reply to check", label, f)
			return
		}
		b, err := json.Marshal(res)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.Result.ValidateJSON(b); err != nil {
			t.Errorf("%s: %v\n%.300s", label, err, b)
		}
		if _, ok := res["views"]; ok {
			sawViews = true
		}
		if a, ok := res["agent"]; ok {
			if a == nil {
				sawNullAgent = true
			} else {
				sawAgent = true
			}
		}
	}

	do("no ticket", "", console.Request{Action: "open"})
	do("plain open", "A:1", console.Request{Action: "open"})
	do("plain select", "A:1", console.Request{Action: "select", Pane: reviewer.Pane})
	do("plain agents", "A:1", console.Request{Action: "agents"})
	do("first send, confirm", "A:1", console.Request{Action: "send", Pane: stranger.Pane, Text: "hi"})
	do("keys", "A:1", console.Request{Action: "keys", Pane: reviewer.Pane, Keys: []string{"1"}})
	do("new form", "A:1", console.Request{Action: "new"})
	do("dismiss", "A:1", console.Request{Action: "dismiss"})
	for _, req := range []console.Request{
		{Action: "open", Views: true},
		{Action: "select", Pane: reviewer.Pane, Views: true},
		{Action: "agents", Views: true},
		{Action: "send", Pane: stranger.Pane, Text: "hi", Views: true},
		{Action: "new", Views: true},
	} {
		do("views "+req.Action, "V:1", req)
	}
	for _, req := range []console.Request{
		{Action: "follow", Pane: reviewer.Pane, Feed: true},
		{Action: "follow", Pane: reviewer.Pane, Feed: true, Resync: true},
		{Action: "agents", Feed: true},
	} {
		do("feed "+req.Action, "F:1", req)
	}
	do("follow nothing", "G:1", console.Request{Action: "follow", Feed: true})
	do("close the followed pane", "F:1", console.Request{Action: "close", Pane: reviewer.Pane, Views: true})

	h.SetStartError(errors.New("herdr: no such workspace"))
	do("failed start", "S:1", console.Request{Action: "start", Kind: "claude", Cwd: t.TempDir()})
	do("failed start, views", "S:2", console.Request{Action: "start", Kind: "claude", Cwd: t.TempDir(), Views: true})
	h.SetStartError(nil)
	do("start", "S:3", console.Request{Action: "start", Kind: "claude", Cwd: t.TempDir(), Views: true})

	if !sawViews || !sawAgent {
		t.Errorf("the replies covered no views (%v) or no agent object (%v)", sawViews, sawAgent)
	}
	if !sawNullAgent {
		t.Log("no reply carried agent:null in this run")
	}

	for _, s := range []string{
		`{"ok":true,"view":"agents","sessions":2,"views":[{"target":"agents","mode":"inner","tree":[{"t":"div","k":["x"]}]}],"agent":null}`,
		`{"ok":true,"following":"w1:p1","agent":{"pane":"w1:p1","name":"Reviewer","kind":"claude","status":"idle","crew":true,"folder":"/tmp"}}`,
	} {
		if err := d.Result.ValidateJSON([]byte(s)); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
	for _, s := range []string{
		`{"ok":true,"views":[{"target":"nowhere","mode":"inner","tree":[]}]}`,
		`{"ok":true,"views":[{"target":"console","mode":"sideways","tree":[]}]}`,
		`{"ok":true,"views":[{"target":"console","mode":"inner","tree":"x"}]}`,
		`{"ok":true,"views":{"target":"console"}}`,
		`{"ok":true,"agent":"w1:p1"}`,
		`{"ok":true,"agent":{"pane":1}}`,
	} {
		if err := d.Result.ValidateJSON([]byte(s)); err == nil {
			t.Errorf("the schema accepts %s", s)
		}
	}
}
