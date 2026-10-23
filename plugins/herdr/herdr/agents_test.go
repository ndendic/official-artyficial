package herdr

import (
	"os"
	"testing"
)

// What herdr 0.9 reported for a Codex and a Pi session it started
// (`herdr agent start --kind codex|pi`): the kind is
// "codex" / "pi"; Pi's session is the path of its session file (herdr's pi
// integration); Codex had no agent_session (its integration reports one
// from Codex's SessionStart hook, which did not report here).
func TestParseListCodexPi(t *testing.T) {
	raw, err := os.ReadFile("testdata/agent-list-codex-pi.json")
	if err != nil {
		t.Fatal(err)
	}
	agents, err := parseList(raw)
	if err != nil || len(agents) != 2 {
		t.Fatalf("agents = %+v, %v", agents, err)
	}
	codex, pi := agents[0], agents[1]
	if codex.Kind != "codex" || codex.Name != "hctcodex" || codex.SessionID != "" || codex.Cwd != "/tmp/hc-codexpi/codex" {
		t.Fatalf("codex = %+v", codex)
	}
	if pi.Kind != "pi" || pi.SessionKind != "path" || pi.SessionID != "/home/u/.pi/agent/sessions/--tmp-hc-codexpi-pi--/2026-10-04T22-06-05-119Z_01a108f3-e93e-70da-be7a-1cd29293959b.jsonl" {
		t.Fatalf("pi = %+v", pi)
	}
}
