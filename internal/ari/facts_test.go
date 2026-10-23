package ari

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The investigator states what changed against the baseline and what
// already failed, citing rows; numbers read the same from memory or Zetaris.
func TestFacts(t *testing.T) {
	f := &Finding{Cycle: 6}
	rows := []Row{
		{ID: 7, Cycle: 4, Kind: "health", Data: map[string]any{"status": float64(200), "latency_ms": float64(2)}},
		{ID: 9, Cycle: 5, Kind: "health", Data: map[string]any{"status": 200, "latency_ms": 3}},
		{ID: 11, Cycle: 6, Kind: "process", Data: map[string]any{"alive": false, "pid": float64(42)}},
		{ID: 12, Cycle: 6, Kind: "health", Data: map[string]any{"status": 500, "latency_ms": 1}},
		{ID: 13, Cycle: 6, Kind: "done_test", Data: map[string]any{"entry": "restore-config", "passed": false}},
	}
	got := strings.Join(facts(f, rows, []string{"restore-config"}), " | ")
	for _, want := range []string{
		"process pid 42 is not running (row 11)",
		"health status 500 now (row 12), 200 in the 2 cycles before (rows 7,9)",
		"restore-config failed its done test (row 13)",
		"already tried and failed: restore-config",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("facts missing %q:\n%s", want, got)
		}
	}
	slow := []Row{
		{ID: 1, Cycle: 1, Kind: "health", Data: map[string]any{"status": 200, "latency_ms": 2}},
		{ID: 2, Cycle: 2, Kind: "health", Data: map[string]any{"status": 200, "latency_ms": 520}},
	}
	if got := facts(&Finding{Cycle: 2}, slow, nil); len(got) != 1 || !strings.Contains(got[0], "520ms now (row 2), 2ms on average before") {
		t.Errorf("slow facts = %v", got)
	}
}

// With a second entry for the symptom, the retry tries it instead of
// repeating the repair that just failed.
func TestRetryPrefersUntriedEntry(t *testing.T) {
	r := newRig(t)
	b, err := os.ReadFile("../../catalogue/toyd.json")
	if err != nil {
		t.Fatal(err)
	}
	extra := `,
  {
    "name": "reload-config",
    "symptom": "health_failing",
    "precondition": "true",
    "apply": "true",
    "done_test": "health_ok",
    "within": "1s",
    "undo": "true",
    "touches": []
  }
]`
	s := strings.TrimSpace(string(b))
	path := filepath.Join(t.TempDir(), "cat.json")
	writeFile(t, path, []byte(strings.TrimSuffix(s, "]")+extra))
	if r.cat, err = LoadCatalogue(path); err != nil {
		t.Fatal(err)
	}
	corrupt := []byte("%% corrupted %%\n")
	r.run(RuleFixer{}, 10, func() {
		writeFile(t, filepath.Join(r.target, "toyd.conf"), corrupt)
		writeFile(t, filepath.Join(r.target, "toyd.conf.good"), corrupt)
	})
	var applied []string
	for _, ev := range r.events() {
		if ev.Event == "exec" && ev.Step == "apply" {
			applied = append(applied, ev.Entry)
		}
	}
	if !slices.Equal(applied, []string{"restore-config", "reload-config"}) {
		t.Fatalf("applied %v, want restore-config then reload-config", applied)
	}
	inv := r.eventsFor("investigate", "f1")
	if len(inv) != 2 || !strings.Contains(inv[1].Detail, "already tried and failed: restore-config") {
		t.Errorf("retry cause does not carry the failure: %+v", inv)
	}
}
