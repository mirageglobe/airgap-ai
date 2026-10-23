package ari

import (
	"fmt"
	"strconv"
	"strings"
)

// facts reads the evidence the way an operator would: what is wrong in the
// finding's cycle, what was normal in the cycles before it, and which
// repairs already failed. Each fact cites its rows.
func facts(f *Finding, rows []Row, tried []string) []string {
	var out, base []string
	var baseRows, nowRows []Row
	for _, r := range rows {
		switch r.Kind {
		case "process":
			if r.Cycle == f.Cycle && r.Data["alive"] == false {
				out = append(out, fmt.Sprintf("process pid %s is not running (row %d)", num(r.Data["pid"]), r.ID))
			}
		case "health":
			if r.Cycle < f.Cycle {
				baseRows = append(baseRows, r)
			} else if r.Cycle == f.Cycle {
				nowRows = append(nowRows, r)
			}
		case "done_test":
			if r.Data["passed"] == false {
				out = append(out, fmt.Sprintf("%v failed its done test (row %d)", r.Data["entry"], r.ID))
			}
		}
	}
	for _, r := range baseRows {
		base = append(base, strconv.Itoa(r.ID))
	}
	for _, r := range nowRows {
		status, latency := num(r.Data["status"]), val(r.Data["latency_ms"])
		if len(baseRows) == 0 {
			out = append(out, fmt.Sprintf("health status %s, %sms (row %d)", status, num(r.Data["latency_ms"]), r.ID))
			continue
		}
		was := num(baseRows[len(baseRows)-1].Data["status"])
		var sum float64
		for _, b := range baseRows {
			sum += val(b.Data["latency_ms"])
		}
		mean := sum / float64(len(baseRows))
		switch {
		case status != was:
			out = append(out, fmt.Sprintf("health status %s now (row %d), %s in the %d cycles before (rows %s)",
				status, r.ID, was, len(baseRows), strings.Join(base, ",")))
		case latency > 2*mean+50:
			out = append(out, fmt.Sprintf("health takes %sms now (row %d), %sms on average before (rows %s)",
				num(r.Data["latency_ms"]), r.ID, strconv.FormatFloat(mean, 'f', 0, 64), strings.Join(base, ",")))
		}
	}
	if len(tried) > 0 {
		out = append(out, "already tried and failed: "+strings.Join(tried, ", "))
	}
	return out
}

// val reads a number whether it came from memory (int) or back from
// Zetaris as JSON (float64).
func val(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case float64:
		return n
	}
	return 0
}

func num(v any) string {
	if v == nil {
		return "?"
	}
	return strconv.FormatFloat(val(v), 'f', -1, 64)
}
