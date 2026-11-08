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

// jsonType names the schema type of a Go type; it fails the test for a kind
// the table does not know, so a new kind needs a line here, not a silent pass.
func jsonType(t *testing.T, rt reflect.Type) string {
	t.Helper()
	switch rt.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Map, reflect.Struct:
		return "object"
	case reflect.Pointer:
		return jsonType(t, rt.Elem())
	}
	t.Fatalf("no schema type known for Go kind %s", rt.Kind())
	return ""
}

// requestFields lists the keys encoding/json decodes into rt: the tag's name,
// else the Go field name (decodeInput matches it case-insensitively, core
// matches the schema exactly), through embedded structs without a tag name.
func requestFields(rt reflect.Type, into map[string]reflect.Type) {
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" && f.Type.Kind() == reflect.Struct {
			requestFields(f.Type, into)
			continue
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		into[name] = f.Type
	}
}

// Every field the Plugin decodes from a request is declared by the request
// schema (core refuses an undeclared key before the Plugin sees it), with the
// type, and for an array the element type, the field has; the schema declares
// nothing else.
func TestRequestSchemaDeclaresEveryRequestField(t *testing.T) {
	d := loadConsoleDescriptor(t).Capabilities[0]
	fields := map[string]reflect.Type{}
	requestFields(reflect.TypeOf(console.Request{}), fields)
	for name, ft := range fields {
		prop, ok := d.Request.Properties[name]
		if !ok {
			t.Errorf("request schema lacks %q (a Request field)", name)
			continue
		}
		if want := jsonType(t, ft); prop.Type != want {
			t.Errorf("request property %q is %s, the field is %s", name, prop.Type, want)
		}
		if ft.Kind() == reflect.Slice || ft.Kind() == reflect.Array {
			if prop.Items == nil {
				t.Errorf("request property %q is an array without items", name)
			} else if want := jsonType(t, ft.Elem()); prop.Items.Type != want {
				t.Errorf("request property %q has items of %s, the elements are %s", name, prop.Items.Type, want)
			}
		}
	}
	for name := range d.Request.Properties {
		if _, ok := fields[name]; !ok {
			t.Errorf("request schema declares %q, which Request does not have", name)
		}
	}
}

// requestFields sees an untagged, empty-named and embedded field, which the
// schema test above would otherwise skip.
func TestRequestFieldsSeeEveryDecodedKey(t *testing.T) {
	type inner struct {
		Deep int `json:"deep"`
	}
	type probe struct {
		inner
		Plain  string
		Empty  string `json:",omitempty"`
		Named  bool   `json:"named,omitempty"`
		Hidden string `json:"-"`
		priv   string
	}
	got := map[string]reflect.Type{}
	requestFields(reflect.TypeOf(probe{}), got)
	for _, k := range []string{"deep", "Plain", "Empty", "named"} {
		if got[k] == nil {
			t.Errorf("requestFields misses %q: %v", k, got)
		}
	}
	if len(got) != 4 {
		t.Errorf("requestFields = %v", got)
	}
	_ = probe{}.priv
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
	replyKeys := map[string]bool{}
	do := func(label, ticket string, req console.Request) {
		t.Helper()
		res, f := c.Handle(ctx, ticket, req)
		if res == nil {
			t.Errorf("%s: refused (%v), so no reply was checked", label, f)
			return
		}
		b, err := json.Marshal(res)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.Result.ValidateJSON(b); err != nil {
			t.Errorf("%s: %v\n%.300s", label, err, b)
		}
		for k := range res {
			replyKeys[k] = true
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

	// A session the console opened: keys, teach and close reach their
	// success replies only through one.
	opened := func(ticket string) string {
		t.Helper()
		res, f := c.Handle(ctx, ticket, console.Request{Action: "start", Kind: "claude", Cwd: t.TempDir()})
		if res == nil {
			t.Fatalf("start: %v", f)
		}
		name, _ := res["name"].(string)
		for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
			for _, o := range c.Opened().All() {
				if _, err := h.Get(ctx, o.Pane); err == nil && o.Name == name {
					return o.Pane
				}
			}
		}
		t.Fatal("the started session never appeared")
		return ""
	}
	pane := opened("K:1")
	do("teach", "K:1", console.Request{Action: "teach", Pane: pane})
	do("teach, views", "K:2", console.Request{Action: "teach", Pane: opened("K:2"), Views: true})
	a, _ := h.Get(ctx, pane)
	a.Status = "blocked"
	h.Put(a)
	do("keys", "K:1", console.Request{Action: "keys", Pane: pane, Keys: []string{"1"}})
	do("keys, views", "K:1", console.Request{Action: "keys", Pane: pane, Keys: []string{"1"}, Views: true})
	b := stranger
	b.Status = "blocked"
	h.Put(b)
	do("keys, confirm card", "K:3", console.Request{Action: "keys", Pane: stranger.Pane, Keys: []string{"2"}})
	do("keys, confirmed", "K:3", console.Request{Action: "keys", Pane: stranger.Pane, Keys: []string{"2"}, Confirm: true})
	b.Status = "idle"
	h.Put(b)
	do("send, confirm card", "K:4", console.Request{Action: "send", Pane: stranger.Pane, Text: "hi"})
	do("send, confirmed", "K:4", console.Request{Action: "send", Pane: stranger.Pane, Text: "hi", Confirm: true})
	do("send, confirmed, views", "K:5", console.Request{Action: "send", Pane: stranger.Pane, Text: "hi", Confirm: true, Views: true})
	do("follow the opened pane", "K:6", console.Request{Action: "follow", Pane: pane, Feed: true})
	do("close the followed pane", "K:6", console.Request{Action: "close", Pane: pane, Views: true})
	do("close an unfollowed pane", "K:7", console.Request{Action: "close", Pane: opened("K:7")})

	h.SetStartError(errors.New("herdr: no such workspace"))
	do("failed start", "S:1", console.Request{Action: "start", Kind: "claude", Cwd: t.TempDir()})
	do("failed start, views", "S:2", console.Request{Action: "start", Kind: "claude", Cwd: t.TempDir(), Views: true})
	h.SetStartError(nil)
	do("start", "S:3", console.Request{Action: "start", Kind: "claude", Cwd: t.TempDir(), Views: true})

	if !sawViews || !sawAgent {
		t.Errorf("the replies covered no views (%v) or no agent object (%v)", sawViews, sawAgent)
	}
	if !sawNullAgent {
		t.Error("no real reply carried agent:null")
	}
	// Every property the result schema declares is carried by some real
	// reply (message is the refusal text of an ok:false reply), so a key
	// dropped from the schema or added to a reply without it fails here.
	for k := range d.Result.Properties {
		if !replyKeys[k] {
			t.Errorf("no real reply carried %q, so the schema's entry for it is unchecked", k)
		}
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
