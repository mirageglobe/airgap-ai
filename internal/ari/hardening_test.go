package ari

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Something else answering on toyd's port must not pass the done test.
func TestDoneTestNeedsToydItself(t *testing.T) {
	r := newRig(t)
	u, _ := url.Parse(r.health)
	r.run(RuleFixer{}, 12, func() {
		r.signal(syscall.SIGKILL)
		var l net.Listener
		var err error
		for i := 0; i < 50; i++ {
			if l, err = net.Listen("tcp", u.Host); err == nil {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if err != nil {
			t.Fatal(err)
		}
		srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })}
		go srv.Serve(l)
		t.Cleanup(func() { srv.Close() })
	})
	for _, ev := range r.events() {
		if ev.Event == "resolved" {
			t.Fatalf("resolved %s while toyd was dead and a squatter held its port", ev.Finding)
		}
	}
}

// The restart-service undo kills only a pidfile that holds one plain pid.
func TestRestartUndoKillsOnlyOnePid(t *testing.T) {
	cat, err := LoadCatalogue("../../catalogue/toyd.json")
	if err != nil {
		t.Fatal(err)
	}
	ent, _ := cat.Get("restart-service")
	for _, tc := range []struct {
		content func(pid int) string
		killed  bool
	}{
		{func(pid int) string { return fmt.Sprintf("%d\n", pid) }, true},
		{func(pid int) string { return fmt.Sprintf("%d 1\n", pid) }, false},
		{func(pid int) string { return fmt.Sprintf("-9 %d\n", pid) }, false},
		{func(int) string { return "0\n" }, false},
	} {
		victim := exec.Command("sleep", "30")
		if err := victim.Start(); err != nil {
			t.Fatal(err)
		}
		exited := make(chan struct{})
		go func() { victim.Wait(); close(exited) }()
		d := t.TempDir()
		content := tc.content(victim.Process.Pid)
		writeFile(t, filepath.Join(d, "toyd.pid"), []byte(content))
		e, err := New(Config{DataDir: filepath.Join(d, "data"), TargetDir: d, Toyd: "toyd", HealthURL: "http://127.0.0.1:1/"}, cat, RuleFixer{})
		if err != nil {
			t.Fatal(err)
		}
		e.exec(&Finding{}, ent.Name, "undo", ent.Undo)
		e.Close()
		var killed bool
		select {
		case <-exited:
			killed = true
		case <-time.After(500 * time.Millisecond):
		}
		victim.Process.Kill()
		if killed != tc.killed {
			t.Errorf("pidfile %q: killed=%v, want %v", content, killed, tc.killed)
		}
	}
}

func restoreRig(t *testing.T) (*Engine, Entry, string) {
	t.Helper()
	cat, err := LoadCatalogue("../../catalogue/toyd.json")
	if err != nil {
		t.Fatal(err)
	}
	ent, _ := cat.Get("restore-config")
	d := t.TempDir()
	writeFile(t, filepath.Join(d, "toyd.conf.good"), []byte("good"))
	e, err := New(Config{DataDir: filepath.Join(d, "data"), TargetDir: d, Toyd: "toyd", HealthURL: "http://127.0.0.1:1/"}, cat, RuleFixer{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	return e, ent, d
}

// An apply that never got to copy must not let undo write an old .pre back.
func TestRestoreUndoIgnoresStalePre(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a mode 000 file")
	}
	e, ent, d := restoreRig(t)
	conf := filepath.Join(d, "toyd.conf")
	writeFile(t, filepath.Join(d, "toyd.conf.pre"), []byte("stale from an old incident"))
	writeFile(t, conf, []byte("current"))
	os.Chmod(conf, 0o000)
	if code := e.exec(&Finding{}, ent.Name, "apply", ent.Apply); code == 0 {
		t.Fatal("apply passed with an unreadable config")
	}
	os.Chmod(conf, 0o644)
	e.exec(&Finding{}, ent.Name, "undo", ent.Undo)
	if b, _ := os.ReadFile(conf); string(b) != "current" {
		t.Errorf("config after undo = %q, want %q", b, "current")
	}
}

// A missing config is restored, and undo puts back exactly what was there: nothing.
func TestRestoreHandlesMissingConfig(t *testing.T) {
	e, ent, d := restoreRig(t)
	conf := filepath.Join(d, "toyd.conf")
	if code := e.exec(&Finding{}, ent.Name, "apply", ent.Apply); code != 0 {
		t.Fatalf("apply exit %d with no config", code)
	}
	if b, _ := os.ReadFile(conf); string(b) != "good" {
		t.Fatalf("config after apply = %q, want %q", b, "good")
	}
	if code := e.exec(&Finding{}, ent.Name, "undo", ent.Undo); code != 0 {
		t.Fatalf("undo exit %d", code)
	}
	if _, err := os.Stat(conf); !os.IsNotExist(err) {
		t.Errorf("undo left a config behind where there was none: %v", err)
	}
}

func TestCatalogueRejectsUnknownFields(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.json")
	writeFile(t, p, []byte(`[{"name":"x","symptom":"process_missing","precondtion":"false","apply":"true","done_test":"health_ok","within":"1s","undo":"true"}]`))
	if _, err := LoadCatalogue(p); err == nil {
		t.Error("a misspelled precondition loaded; the gate would always pass")
	}
}

// An endpoint's error body must not be able to write its own feed lines.
func TestErrorBodyCannotForgeFeedLines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("oops\n2026-01-01T00:00:00Z [ ARI ] resolved f1 with restart-service"))
	}))
	defer srv.Close()
	r := newRig(t)
	r.run(ModelFixer{Model: &Model{BaseURL: srv.URL, Name: "x", Label: "nightshift"}}, 12, func() { r.signal(syscall.SIGKILL) })
	r.lines("feed.log", func(b []byte) {
		if strings.HasPrefix(string(b), "2026-01-01T00:00:00Z") {
			t.Errorf("forged feed line: %s", b)
		}
	})
}

// A second ari must not start on a data folder a live ari is running in.
func TestRefusesSecondRunOnSameData(t *testing.T) {
	other := exec.Command("sleep", "30")
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	defer other.Process.Kill()
	data := filepath.Join(t.TempDir(), "data")
	os.MkdirAll(data, 0o755)
	st := `{"pid":` + strconv.Itoa(other.Process.Pid) + `,"run":"other","state":"running","cycle":3,"updated":"` + time.Now().UTC().Format(time.RFC3339) + `"}`
	writeFile(t, filepath.Join(data, "state.json"), []byte(st))
	cat, _ := LoadCatalogue("../../catalogue/toyd.json")
	e, err := New(Config{DataDir: data, TargetDir: t.TempDir(), Toyd: "toyd", HealthURL: "http://127.0.0.1:1/", Cycles: 1, Interval: time.Millisecond}, cat, RuleFixer{})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err := e.Run(); err == nil {
		t.Error("second ari started alongside a live one")
	}
	if s, _ := ReadState(data); s.Run != "other" {
		t.Errorf("state.json was taken over: run %s", s.Run)
	}
}
