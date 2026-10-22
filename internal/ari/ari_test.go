package ari

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// injectAt leaves clean cycles before every fault.
const injectAt = 5

var toydBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "toyd")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	toydBin = filepath.Join(dir, "toyd")
	if out, err := exec.Command("go", "build", "-o", toydBin, "../../cmd/toyd").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build toyd: %v\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// Test 1: stopped service, the happy path.
func TestStoppedService(t *testing.T) {
	r := newRig(t)
	before := r.snapshot()
	r.run(RuleFixer{}, 12, func() { r.signal(syscall.SIGKILL) })

	f := r.onlyFinding()
	if f.Symptom != "process_missing" || f.Status != "resolved" || f.Entry != "restart-service" || f.Attempts != 1 {
		t.Fatalf("finding = %+v, want process_missing resolved by restart-service in 1 attempt", f)
	}
	if f.Cycle != injectAt+1 {
		t.Errorf("flagged at cycle %d, want %d", f.Cycle, injectAt+1)
	}
	for _, ev := range r.events() {
		if ev.Event == "finding" && ev.Cycle <= injectAt {
			t.Errorf("clean cycle %d produced a finding", ev.Cycle)
		}
	}
	r.assertInvestigationCites(f.ID, "process", "health")
	r.assertFeedOrder("announce restart-service", "apply restart-service", "result restart-service ok")
	rep := r.report()
	for _, want := range []string{"f1 process_missing via restart-service", "before: row", "after:  row"} {
		if !strings.Contains(rep, want) {
			t.Errorf("report missing %q:\n%s", want, rep)
		}
	}
	r.assertOnlyOwnFiles()
	r.assertTargetWrites(before, "restart-service")
}

// Test 2: bad fix, undo, retry, escalate.
func TestBadFixUndoRetryEscalate(t *testing.T) {
	r := newRig(t)
	before := r.snapshot()
	corrupt := []byte("%% corrupted %%\n")
	r.run(RuleFixer{}, 10, func() {
		writeFile(t, filepath.Join(r.target, "toyd.conf"), corrupt)
		writeFile(t, filepath.Join(r.target, "toyd.conf.good"), corrupt)
	})

	f := r.onlyFinding()
	if f.Symptom != "health_failing" || f.Entry != "restore-config" || f.Status != "unresolved" {
		t.Fatalf("finding = %+v, want health_failing left unresolved after restore-config", f)
	}
	if f.Attempts != maxAttempts {
		t.Errorf("attempts = %d, want exactly %d", f.Attempts, maxAttempts)
	}
	if n := r.execCount("apply"); n != 2 {
		t.Errorf("apply ran %d times, want 2", n)
	}
	if n := r.execCount("undo"); n != 2 {
		t.Errorf("undo ran %d times, want 2", n)
	}
	if got, want := hashFile(t, filepath.Join(r.target, "toyd.conf")), hashBytes(corrupt); got != want {
		t.Errorf("undo did not restore exact bytes: hash %s, want %s", got, want)
	}
	inv := r.eventsFor("investigate", f.ID)
	if len(inv) != 2 {
		t.Fatalf("investigated %d times, want 2", len(inv))
	}
	if !r.citesKind(inv[1].Rows, "done_test") {
		t.Errorf("retry did not get the failed done test as evidence: rows %v", inv[1].Rows)
	}
	if len(f.Evidence) == 0 {
		t.Error("escalation has no evidence rows")
	}
	rep := r.report()
	if !strings.Contains(rep, "left alone:\n  f1 health_failing") {
		t.Errorf("report does not list f1 under left alone:\n%s", rep)
	}
	r.assertOnlyOwnFiles()
	r.assertTargetWrites(before, "restore-config", "toyd.conf.good")
}

// Test 3: fault not in the catalogue.
func TestFaultNotInCatalogue(t *testing.T) {
	r := newRig(t)
	r.run(RuleFixer{}, 15, func() { r.signal(syscall.SIGUSR1) })

	f := r.onlyFinding()
	if f.Symptom != "health_slow" || f.Status != "unresolved" || f.Attempts != 0 {
		t.Fatalf("finding = %+v, want health_slow escalated with no attempt", f)
	}
	if !strings.Contains(f.Reason, "no catalogue entry") {
		t.Errorf("reason = %q", f.Reason)
	}
	if n := r.execCount(""); n != 0 {
		t.Errorf("%d commands ran, want none", n)
	}
	esc := r.eventsFor("escalate", f.ID)
	if len(esc) != 1 || len(esc[0].Rows) == 0 {
		t.Errorf("escalation note does not cite rows: %+v", esc)
	}
	rep := r.report()
	if !strings.Contains(rep, "left alone:\n  f1 health_slow") {
		t.Errorf("report does not list f1 under left alone:\n%s", rep)
	}
	r.assertOnlyOwnFiles()
}

// The model never gets a veto: a wrong pick is stopped at the gate.
func TestGateRejectsWrongPick(t *testing.T) {
	for name, pick := range map[string]string{
		"catalogue entry for another symptom": "restart-service",
		"entry not in the catalogue":          "reboot-host",
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.run(pickFixer(pick), 15, func() { r.signal(syscall.SIGUSR1) })
			f := r.onlyFinding()
			if f.Status != "unresolved" || f.Attempts != 0 {
				t.Fatalf("finding = %+v, want escalated with no attempt", f)
			}
			if n := r.execCount(""); n != 0 {
				t.Errorf("%d commands ran after a wrong pick, want none", n)
			}
		})
	}
}

func TestCatalogueRejectsEntryWithoutUndo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cat.json")
	writeFile(t, path, []byte(`[{"name":"x","symptom":"s","apply":"true","done_test":"health_ok","within":"1s"}]`))
	if _, err := LoadCatalogue(path); err == nil || !strings.Contains(err.Error(), "no undo") {
		t.Fatalf("err = %v, want a no undo rejection", err)
	}
}

type pickFixer string

func (p pickFixer) Pick(Cause, *Catalogue) (string, string, error) {
	return string(p), "model says act", nil
}

type rig struct {
	t      *testing.T
	target string
	data   string
	health string
	cat    *Catalogue
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, target: t.TempDir(), data: filepath.Join(t.TempDir(), "data")}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	conf := []byte(fmt.Sprintf("port=%d\nstatus=ok\n", port))
	writeFile(t, filepath.Join(r.target, "toyd.conf"), conf)
	writeFile(t, filepath.Join(r.target, "toyd.conf.good"), conf)
	r.health = fmt.Sprintf("http://127.0.0.1:%d/health", port)

	if r.cat, err = LoadCatalogue("../../catalogue/toyd.json"); err != nil {
		t.Fatal(err)
	}
	start, _ := r.cat.Get("restart-service")
	cmd, err := render(start.Apply, map[string]string{"Dir": r.target, "Toyd": toydBin})
	if err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("sh", "-c", cmd).Run(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.signal(syscall.SIGKILL) })
	deadline := time.Now().Add(10 * time.Second)
	for {
		if resp, err := http.Get(r.health); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return r
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("toyd did not become healthy")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (r *rig) run(fixer Fixer, cycles int, inject func()) {
	r.t.Helper()
	e, err := New(Config{
		DataDir:   r.data,
		TargetDir: r.target,
		Toyd:      toydBin,
		HealthURL: r.health,
		Cycles:    cycles,
		Interval:  100 * time.Millisecond,
		AfterCycle: func(c int) {
			if c == injectAt {
				inject()
			}
		},
	}, r.cat, fixer)
	if err != nil {
		r.t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- e.Run() }()
	select {
	case err := <-done:
		if err != nil {
			r.t.Fatal(err)
		}
	case <-time.After(3 * time.Minute):
		r.t.Fatal("--cycles did not stop the run")
	}
	if err := e.Close(); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) signal(sig syscall.Signal) {
	b, err := os.ReadFile(filepath.Join(r.target, "toyd.pid"))
	if err != nil {
		return
	}
	if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 0 {
		syscall.Kill(pid, sig)
	}
}

func (r *rig) events() []Event {
	r.t.Helper()
	var out []Event
	r.lines("log.jsonl", func(b []byte) {
		var ev Event
		if err := json.Unmarshal(b, &ev); err != nil {
			r.t.Fatal(err)
		}
		out = append(out, ev)
	})
	return out
}

func (r *rig) eventsFor(kind, finding string) []Event {
	var out []Event
	for _, ev := range r.events() {
		if ev.Event == kind && ev.Finding == finding {
			out = append(out, ev)
		}
	}
	return out
}

func (r *rig) onlyFinding() Finding {
	r.t.Helper()
	var fs []Finding
	r.lines("findings.jsonl", func(b []byte) {
		var f Finding
		if err := json.Unmarshal(b, &f); err != nil {
			r.t.Fatal(err)
		}
		fs = append(fs, f)
	})
	if len(fs) != 1 {
		r.t.Fatalf("got %d findings, want 1: %+v", len(fs), fs)
	}
	return fs[0]
}

// execCount counts catalogue commands run; step "" counts every step.
func (r *rig) execCount(step string) int {
	n := 0
	for _, ev := range r.events() {
		if ev.Event == "exec" && (step == "" || ev.Step == step) {
			n++
		}
	}
	return n
}

func (r *rig) citesKind(ids []int, kind string) bool {
	kinds := map[int]string{}
	for _, ev := range r.events() {
		if ev.Row != nil {
			kinds[ev.Row.ID] = ev.Row.Kind
		}
	}
	for _, id := range ids {
		if kinds[id] == kind {
			return true
		}
	}
	return false
}

func (r *rig) assertInvestigationCites(finding string, kinds ...string) {
	r.t.Helper()
	inv := r.eventsFor("investigate", finding)
	if len(inv) == 0 {
		r.t.Fatalf("no investigation for %s", finding)
	}
	for _, k := range kinds {
		if !r.citesKind(inv[0].Rows, k) {
			r.t.Errorf("investigation does not cite a %s row: %v", k, inv[0].Rows)
		}
	}
}

func (r *rig) assertFeedOrder(want ...string) {
	r.t.Helper()
	i := 0
	r.lines("feed.log", func(b []byte) {
		if i < len(want) && strings.Contains(string(b), want[i]) {
			i++
		}
	})
	if i != len(want) {
		r.t.Errorf("feed did not show %q in order (matched %d)", want, i)
	}
}

func (r *rig) report() string {
	r.t.Helper()
	rep, err := Report(r.data)
	if err != nil {
		r.t.Fatal(err)
	}
	return rep
}

// assertOnlyOwnFiles: ari writes nothing in its data folder but its own files.
func (r *rig) assertOnlyOwnFiles() {
	r.t.Helper()
	entries, err := os.ReadDir(r.data)
	if err != nil {
		r.t.Fatal(err)
	}
	own := map[string]bool{"log.jsonl": true, "feed.log": true, "findings.jsonl": true}
	for _, e := range entries {
		if !own[e.Name()] {
			r.t.Errorf("unexpected file in data folder: %s", e.Name())
		}
	}
}

// assertTargetWrites: the target changes only where a catalogue entry
// declares it may, or where the test injected the fault.
func (r *rig) assertTargetWrites(before map[string]string, allowed ...string) {
	r.t.Helper()
	ok := map[string]bool{}
	for _, a := range allowed {
		if e, isEntry := r.cat.Get(a); isEntry {
			for _, p := range e.Touches {
				ok[p] = true
			}
		} else {
			ok[a] = true
		}
	}
	for name, sum := range r.snapshot() {
		if before[name] != sum && !ok[name] {
			r.t.Errorf("target file %s changed but no entry declares it", name)
		}
	}
}

func (r *rig) snapshot() map[string]string {
	r.t.Helper()
	entries, err := os.ReadDir(r.target)
	if err != nil {
		r.t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range entries {
		out[e.Name()] = hashFile(r.t, filepath.Join(r.target, e.Name()))
	}
	return out
}

func (r *rig) lines(name string, fn func([]byte)) {
	r.t.Helper()
	f, err := os.Open(filepath.Join(r.data, name))
	if err != nil {
		r.t.Fatal(err)
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 1<<20), 1<<20)
	for s.Scan() {
		fn(s.Bytes())
	}
}

func writeFile(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func hashFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return hashBytes(b)
}

func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
