package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ndendic/data-start/api/manifest"

	"herdr-console/console"
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
