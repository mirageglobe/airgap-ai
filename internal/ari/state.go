package ari

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// StopFile in the data folder halts ari at once and keeps it stopped until a
// human removes it.
const StopFile = "STOP"

const stateFile = "state.json"

// State is what `ari status` shows: written by the running engine every cycle.
type State struct {
	PID       int       `json:"pid"`
	Run       string    `json:"run"`
	State     string    `json:"state"` // running, stopped, finished
	Cycle     int       `json:"cycle"`
	Updated   time.Time `json:"updated"`
	LastFix   *FixNote  `json:"last_fix,omitempty"`
	LeftAlone int       `json:"left_alone"`
}

type FixNote struct {
	Finding string    `json:"finding"`
	Entry   string    `json:"entry"`
	Status  string    `json:"status"`
	At      time.Time `json:"at"`
}

// RequestStop writes the stop flag; a running ari halts before its next
// action (a fix already applying finishes its done test, and undo if needed).
func RequestStop(dataDir, by string) error {
	note := fmt.Sprintf("stop requested by %s at %s\n", by, time.Now().UTC().Format(time.RFC3339))
	return os.WriteFile(filepath.Join(dataDir, StopFile), []byte(note), 0o644)
}

func ReadState(dataDir string) (State, error) {
	var s State
	b, err := os.ReadFile(filepath.Join(dataDir, stateFile))
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(b, &s)
}

// Status renders State for a human, checking the recorded pid is still alive.
func Status(dataDir string) (string, error) {
	s, err := ReadState(dataDir)
	if errors.Is(err, os.ErrNotExist) {
		return "ari has not run with this data folder\n", nil
	}
	if err != nil {
		return "", err
	}
	state := s.State
	if state == "running" && !alive(s.PID) {
		state = "not running (it stopped without saying so)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "ari: %s (pid %d, run %s)\n", state, s.PID, s.Run)
	if _, err := os.Stat(filepath.Join(dataDir, StopFile)); err == nil {
		fmt.Fprintf(&b, "stop flag: set; remove %s to let it start again\n", filepath.Join(dataDir, StopFile))
	}
	fmt.Fprintf(&b, "last cycle: %d at %s (%s ago)\n", s.Cycle, s.Updated.Format(time.RFC3339), time.Since(s.Updated).Round(time.Second))
	if s.LastFix != nil {
		fmt.Fprintf(&b, "last fix: %s %s %s at %s\n", s.LastFix.Finding, s.LastFix.Entry, s.LastFix.Status, s.LastFix.At.Format(time.RFC3339))
	} else {
		b.WriteString("last fix: none\n")
	}
	fmt.Fprintf(&b, "left alone: %d finding(s); see ari report\n", s.LeftAlone)
	return b.String(), nil
}

func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func (e *Engine) stopRequested() bool {
	_, err := os.Stat(filepath.Join(e.cfg.DataDir, StopFile))
	return err == nil
}

// sleep waits out the interval but wakes early for the stop flag.
func (e *Engine) sleep(d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) && !e.stopRequested() {
		time.Sleep(min(200*time.Millisecond, time.Until(deadline)))
	}
}

// writeState replaces state.json whole, so a reader never sees half of it.
func (e *Engine) writeState(state string) {
	s := State{
		PID:       os.Getpid(),
		Run:       e.cfg.RunID,
		State:     state,
		Cycle:     e.cycle,
		Updated:   time.Now().UTC(),
		LastFix:   e.lastFix,
		LeftAlone: e.leftAlone,
	}
	b, err := json.Marshal(s)
	if err == nil {
		tmp := filepath.Join(e.cfg.DataDir, stateFile+".tmp")
		if err = os.WriteFile(tmp, b, 0o644); err == nil {
			err = os.Rename(tmp, filepath.Join(e.cfg.DataDir, stateFile))
		}
	}
	if err != nil && e.err == nil {
		e.err = err
	}
}
