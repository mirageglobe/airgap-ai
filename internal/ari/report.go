package ari

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Report renders the one-screen summary from the data folder. Row and
// finding ids restart every run, so everything is keyed by run.
func Report(dataDir string) (string, error) {
	rows := map[string]map[int]Row{}
	contracts := map[string][]string{} // run/finding -> contract notes
	err := eachLine(filepath.Join(dataDir, "log.jsonl"), func(b []byte) error {
		var ev Event
		if err := json.Unmarshal(b, &ev); err != nil {
			return err
		}
		if ev.Row != nil {
			if rows[ev.Run] == nil {
				rows[ev.Run] = map[int]Row{}
			}
			rows[ev.Run][ev.Row.ID] = *ev.Row
		}
		if c := ev.Contract; c != nil && ev.Event == "contract.issued" {
			note := fmt.Sprintf("%s risk %s", c.ContractID, c.Risk.Level)
			if len(c.Risk.Flags) > 0 {
				note += " (" + strings.Join(c.Risk.Flags, ", ") + ")"
			} else {
				note += " for " + strings.Join(c.Scope.Capabilities, ", ")
			}
			k := ev.Run + "/" + ev.Finding
			contracts[k] = append(contracts[k], note)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	var fixed, left []Finding
	err = eachLine(filepath.Join(dataDir, "findings.jsonl"), func(b []byte) error {
		var f Finding
		if err := json.Unmarshal(b, &f); err != nil {
			return err
		}
		if f.Status == "resolved" {
			fixed = append(fixed, f)
		} else {
			left = append(left, f)
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("ari report\n\nfixed:\n")
	if len(fixed) == 0 {
		b.WriteString("  none\n")
	}
	for _, f := range fixed {
		fmt.Fprintf(&b, "  %s %s via %s (%d attempt(s), cycle %d%s)\n", f.ID, f.Symptom, f.Entry, f.Attempts, f.Cycle, runNote(f.Run))
		fmt.Fprintf(&b, "    before: %s\n", describeIDs(rows[f.Run], f.Before))
		fmt.Fprintf(&b, "    after:  %s\n", describeIDs(rows[f.Run], f.After))
		writeContracts(&b, contracts[f.Run+"/"+f.ID])
	}
	b.WriteString("\nleft alone:\n")
	if len(left) == 0 {
		b.WriteString("  none\n")
	}
	for _, f := range left {
		fmt.Fprintf(&b, "  %s %s: %s (cycle %d%s)\n", f.ID, f.Symptom, f.Reason, f.Cycle, runNote(f.Run))
		fmt.Fprintf(&b, "    evidence: %s\n", describeIDs(rows[f.Run], f.Evidence))
		writeContracts(&b, contracts[f.Run+"/"+f.ID])
	}
	return b.String(), nil
}

func runNote(run string) string {
	if run == "" {
		return ""
	}
	return ", run " + run
}

func writeContracts(b *strings.Builder, notes []string) {
	for _, n := range notes {
		fmt.Fprintf(b, "    scout:  %s\n", n)
	}
}

func describeIDs(rows map[int]Row, ids []int) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		if r, ok := rows[id]; ok {
			parts = append(parts, describeRow(r))
		}
	}
	return strings.Join(parts, "; ")
}

func eachLine(path string, fn func([]byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 1<<20), 1<<20)
	for s.Scan() {
		if len(s.Bytes()) == 0 {
			continue
		}
		if err := fn(s.Bytes()); err != nil {
			return err
		}
	}
	return s.Err()
}
