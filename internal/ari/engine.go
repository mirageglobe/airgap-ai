// Package ari runs the explorer, investigator, fixer, verifier and reporter
// against one target.
package ari

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"text/template"
	"time"
)

// maxAttempts is the first try plus one retry through the investigator.
const maxAttempts = 2

type Config struct {
	DataDir        string
	TargetDir      string
	Toyd           string
	HealthURL      string
	Cycles         int // 0 runs until stopped
	Interval       time.Duration
	BaselineCycles int
	FixBudget      int // fixes per hour
	Feed           io.Writer
	AfterCycle     func(cycle int)
}

type Row struct {
	ID    int            `json:"id"`
	Cycle int            `json:"cycle"`
	Kind  string         `json:"kind"`
	Data  map[string]any `json:"data"`
}

type Finding struct {
	ID       string `json:"id"`
	Cycle    int    `json:"cycle"`
	Symptom  string `json:"symptom"`
	Status   string `json:"status"`
	Entry    string `json:"entry,omitempty"`
	Attempts int    `json:"attempts"`
	Reason   string `json:"reason,omitempty"`
	Before   []int  `json:"before"`
	After    []int  `json:"after,omitempty"`
	Evidence []int  `json:"evidence"`
}

type Cause struct {
	Finding  string
	Symptom  string
	Summary  string
	Evidence []int
}

type Event struct {
	Time    time.Time `json:"ts"`
	Cycle   int       `json:"cycle"`
	Event   string    `json:"event"`
	Finding string    `json:"finding,omitempty"`
	Entry   string    `json:"entry,omitempty"`
	Step    string    `json:"step,omitempty"`
	Detail  string    `json:"detail,omitempty"`
	Rows    []int     `json:"rows,omitempty"`
	Row     *Row      `json:"row,omitempty"`
	Exit    *int      `json:"exit,omitempty"`
}

type observation struct {
	alive   bool
	status  int
	latency time.Duration
	timeout bool
	rows    []int
}

type Engine struct {
	cfg       Config
	cat       *Catalogue
	fixer     Fixer
	client    *http.Client
	log       *os.File
	feed      *os.File
	findings  *os.File
	rows      []Row
	nFindings int
	escalated map[string]bool
	fixes     []time.Time
	baseline  []time.Duration
	cycle     int
	err       error
}

func New(cfg Config, cat *Catalogue, fixer Fixer) (*Engine, error) {
	if cfg.BaselineCycles == 0 {
		cfg.BaselineCycles = 3
	}
	if cfg.FixBudget == 0 {
		cfg.FixBudget = 6
	}
	if cfg.Interval == 0 {
		cfg.Interval = 5 * time.Second
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, err
	}
	e := &Engine{
		cfg:       cfg,
		cat:       cat,
		fixer:     fixer,
		client:    &http.Client{Timeout: 2 * time.Second},
		escalated: map[string]bool{},
	}
	var err error
	for _, f := range []struct {
		dst  **os.File
		name string
	}{{&e.log, "log.jsonl"}, {&e.feed, "feed.log"}, {&e.findings, "findings.jsonl"}} {
		if *f.dst, err = os.OpenFile(filepath.Join(cfg.DataDir, f.name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err != nil {
			e.Close()
			return nil, err
		}
	}
	return e, nil
}

func (e *Engine) Close() error {
	var errs []error
	for _, f := range []*os.File{e.log, e.feed, e.findings} {
		if f != nil {
			errs = append(errs, f.Close())
		}
	}
	return errors.Join(errs...)
}

// Run explores once per interval and handles each new finding to the end
// before the next cycle.
func (e *Engine) Run() error {
	for e.cycle = 1; e.cfg.Cycles <= 0 || e.cycle <= e.cfg.Cycles; e.cycle++ {
		o := e.explore()
		if s := e.classify(o); s != "" && !e.escalated[s] {
			e.handle(s, o.rows)
		}
		if e.cfg.AfterCycle != nil {
			e.cfg.AfterCycle(e.cycle)
		}
		if e.cfg.Cycles <= 0 || e.cycle < e.cfg.Cycles {
			time.Sleep(e.cfg.Interval)
		}
	}
	return e.err
}

func (e *Engine) explore() observation {
	pid, alive := e.process()
	p := e.addRow("process", map[string]any{"alive": alive, "pid": pid})
	o, h := e.health()
	o.alive = alive
	o.rows = []int{p.ID, h.ID}
	return o
}

func (e *Engine) process() (int, bool) {
	b, err := os.ReadFile(filepath.Join(e.cfg.TargetDir, "toyd.pid"))
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, syscall.Kill(pid, 0) == nil
}

func (e *Engine) health() (observation, Row) {
	var o observation
	start := time.Now()
	resp, err := e.client.Get(e.cfg.HealthURL)
	o.latency = time.Since(start)
	data := map[string]any{"latency_ms": o.latency.Milliseconds(), "status": 0}
	if err != nil {
		var ne net.Error
		o.timeout = errors.As(err, &ne) && ne.Timeout()
		data["error"] = err.Error()
	} else {
		resp.Body.Close()
		o.status = resp.StatusCode
		data["status"] = resp.StatusCode
	}
	return o, e.addRow("health", data)
}

func (e *Engine) classify(o observation) string {
	switch {
	case !o.alive:
		return "process_missing"
	case o.timeout:
		return "health_slow"
	case o.status != http.StatusOK:
		return "health_failing"
	case e.slow(o.latency):
		return "health_slow"
	}
	if len(e.baseline) < e.cfg.BaselineCycles {
		e.baseline = append(e.baseline, o.latency)
	}
	return ""
}

// slow means over 5x the baseline mean and over 100ms.
func (e *Engine) slow(d time.Duration) bool {
	if len(e.baseline) < e.cfg.BaselineCycles {
		return false
	}
	var sum time.Duration
	for _, b := range e.baseline {
		sum += b
	}
	limit := max(100*time.Millisecond, 5*sum/time.Duration(len(e.baseline)))
	return d > limit
}

func (e *Engine) handle(symptom string, rows []int) {
	e.nFindings++
	f := &Finding{
		ID:       fmt.Sprintf("f%d", e.nFindings),
		Cycle:    e.cycle,
		Symptom:  symptom,
		Status:   "open",
		Before:   rows,
		Evidence: append([]int(nil), rows...),
	}
	e.event(Event{Event: "finding", Finding: f.ID, Detail: symptom, Rows: rows})
	e.say("finding %s: %s (rows %s)", f.ID, symptom, joinInts(rows))

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		cause := e.investigate(f)
		name, reason, err := e.fixer.Pick(cause, e.cat)
		if err != nil {
			e.escalate(f, "fixer error: "+err.Error())
			return
		}
		if name == "" {
			e.escalate(f, reason)
			return
		}
		entry, ok := e.cat.Get(name)
		if why := e.gate(f, cause, entry, ok, name); why != "" {
			e.escalate(f, why)
			return
		}
		f.Entry, f.Attempts = name, attempt
		if e.fix(f, entry, reason) {
			f.Status = "resolved"
			_, after := e.health()
			f.After = []int{after.ID}
			e.event(Event{Event: "resolved", Finding: f.ID, Entry: name, Rows: f.After})
			e.say("resolved %s with %s", f.ID, name)
			e.record(f)
			return
		}
	}
	e.escalate(f, fmt.Sprintf("%s failed its done test %d times", f.Entry, maxAttempts))
}

func (e *Engine) investigate(f *Finding) Cause {
	parts := make([]string, 0, len(f.Evidence))
	for _, id := range f.Evidence {
		parts = append(parts, describeRow(e.rows[id-1]))
	}
	c := Cause{
		Finding:  f.ID,
		Symptom:  f.Symptom,
		Summary:  f.Symptom + ": " + strings.Join(parts, "; "),
		Evidence: append([]int(nil), f.Evidence...),
	}
	e.event(Event{Event: "investigate", Finding: f.ID, Detail: c.Summary, Rows: c.Evidence})
	return c
}

// gate decides whether a picked repair may run. The fixer has no say here.
func (e *Engine) gate(f *Finding, c Cause, entry Entry, ok bool, name string) string {
	switch {
	case !ok:
		return fmt.Sprintf("%q is not in the catalogue", name)
	case entry.Symptom != c.Symptom:
		return fmt.Sprintf("%s handles %s, not %s", name, entry.Symptom, c.Symptom)
	case e.budgetSpent():
		return "hourly fix budget spent"
	case e.exec(f, name, "precondition", entry.Precondition) != 0:
		return name + " precondition failed"
	}
	return ""
}

func (e *Engine) budgetSpent() bool {
	n := 0
	for _, t := range e.fixes {
		if time.Since(t) < time.Hour {
			n++
		}
	}
	return n >= e.cfg.FixBudget
}

func (e *Engine) fix(f *Finding, entry Entry, reason string) bool {
	e.say("announce %s for %s: %s", entry.Name, f.ID, reason)
	e.fixes = append(e.fixes, time.Now())
	e.say("apply %s", entry.Name)
	passed := e.exec(f, entry.Name, "apply", entry.Apply) == 0 && e.doneTest(entry)
	row := e.addRow("done_test", map[string]any{"entry": entry.Name, "finding": f.ID, "passed": passed})
	f.Evidence = append(f.Evidence, row.ID)
	if passed {
		e.say("result %s ok", entry.Name)
		return true
	}
	e.say("result %s failed, undoing", entry.Name)
	e.exec(f, entry.Name, "undo", entry.Undo)
	e.say("undo %s done", entry.Name)
	return false
}

func (e *Engine) doneTest(entry Entry) bool {
	within, _ := time.ParseDuration(entry.Within)
	deadline := time.Now().Add(within)
	for {
		if e.healthOK() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (e *Engine) healthOK() bool {
	resp, err := e.client.Get(e.cfg.HealthURL)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (e *Engine) escalate(f *Finding, why string) {
	f.Status, f.Reason = "unresolved", why
	e.escalated[f.Symptom] = true
	e.event(Event{Event: "escalate", Finding: f.ID, Detail: why, Rows: f.Evidence})
	e.say("escalate %s: %s (rows %s)", f.ID, why, joinInts(f.Evidence))
	e.record(f)
}

// exec runs a catalogue command in the target dir and logs it. Only catalogue
// entries reach here.
func (e *Engine) exec(f *Finding, entry, step, tmpl string) int {
	if tmpl == "" {
		return 0
	}
	code := -1
	text, err := render(tmpl, map[string]string{"Dir": e.cfg.TargetDir, "Toyd": e.cfg.Toyd})
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cmd := exec.CommandContext(ctx, "sh", "-c", text)
		cmd.Dir = e.cfg.TargetDir
		code = exitCode(cmd.Run())
		cancel()
	}
	e.event(Event{Event: "exec", Finding: f.ID, Entry: entry, Step: step, Detail: text, Exit: &code})
	return code
}

func render(tmpl string, vars map[string]string) (string, error) {
	t, err := template.New("cmd").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := t.Execute(&b, vars); err != nil {
		return "", err
	}
	return b.String(), nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

func (e *Engine) addRow(kind string, data map[string]any) Row {
	r := Row{ID: len(e.rows) + 1, Cycle: e.cycle, Kind: kind, Data: data}
	e.rows = append(e.rows, r)
	e.event(Event{Event: "row", Row: &r})
	return r
}

func (e *Engine) event(ev Event) {
	ev.Time, ev.Cycle = time.Now().UTC(), e.cycle
	e.writeJSON(e.log, ev)
}

func (e *Engine) record(f *Finding) {
	e.writeJSON(e.findings, f)
}

func (e *Engine) writeJSON(w io.Writer, v any) {
	b, err := json.Marshal(v)
	if err == nil {
		_, err = w.Write(append(b, '\n'))
	}
	if err != nil && e.err == nil {
		e.err = err
	}
}

func (e *Engine) say(format string, args ...any) {
	line := fmt.Sprintf("%s [ ARI ] %s\n", time.Now().UTC().Format(time.RFC3339), fmt.Sprintf(format, args...))
	if _, err := io.WriteString(e.feed, line); err != nil && e.err == nil {
		e.err = err
	}
	if e.cfg.Feed != nil {
		io.WriteString(e.cfg.Feed, line)
	}
}

func describeRow(r Row) string {
	keys := make([]string, 0, len(r.Data))
	for k := range r.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	kv := make([]string, 0, len(keys))
	for _, k := range keys {
		kv = append(kv, fmt.Sprintf("%s=%v", k, r.Data[k]))
	}
	return fmt.Sprintf("row %d %s %s", r.ID, r.Kind, strings.Join(kv, " "))
}

func joinInts(ids []int) string {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = strconv.Itoa(id)
	}
	return strings.Join(s, ",")
}
