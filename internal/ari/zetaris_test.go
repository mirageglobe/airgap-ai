package ari

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeZetaris logs in once per token and answers run-query with rows.
type fakeZetaris struct {
	logins  int
	expired bool // next query returns 401
	sql     string
	data    [][]string
}

func (f *fakeZetaris) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			f.logins++
			json.NewEncoder(w).Encode(map[string]any{"idToken": "tok", "orgId": 1, "refreshToken": "x"})
		case "/api/proxy/sql-editor/sqls/run-query":
			if r.Header.Get("Authorization") != "Bearer tok" || r.Header.Get("X-Org-ID") != "1" {
				http.Error(w, "bad auth", http.StatusBadRequest)
				return
			}
			if f.expired {
				f.expired = false
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			var body struct{ SQL string }
			json.NewDecoder(r.Body).Decode(&body)
			f.sql = body.SQL
			json.NewEncoder(w).Encode(map[string]any{"headers": []string{"id", "cycle", "kind", "data"}, "data": f.data})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestZetarisEvidenceRows(t *testing.T) {
	f := &fakeZetaris{data: [][]string{
		{"9", "4", "health", `{"status":200,"latency_ms":3}`},
		{"13", "6", "process", `{"alive":false,"pid":42}`},
		{"14", "6", "health", `{"status":0}`},
	}}
	ev := ZetarisEvidence{Z: &Zetaris{BaseURL: f.server(t).URL, User: "u", Password: "p"}, Run: "20261023T120000Z"}
	rows, err := ev.Rows(&Finding{Cycle: 6, Evidence: []int{13, 14}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[1].Kind != "process" || rows[1].Data["alive"] != false {
		t.Fatalf("rows = %+v", rows)
	}
	for _, want := range []string{EvidenceTable, "run = '20261023T120000Z'", "id IN (13,14)", "cycle >= 3 AND cycle < 6"} {
		if !strings.Contains(f.sql, want) {
			t.Errorf("sql %q missing %q", f.sql, want)
		}
	}
}

// A cited row missing from Zetaris fails closed.
func TestZetarisEvidenceMissingRow(t *testing.T) {
	f := &fakeZetaris{data: [][]string{{"13", "6", "process", `{}`}}}
	ev := ZetarisEvidence{Z: &Zetaris{BaseURL: f.server(t).URL}, Run: "r1"}
	if _, err := ev.Rows(&Finding{Cycle: 6, Evidence: []int{13, 14}}); err == nil || !strings.Contains(err.Error(), "row 14") {
		t.Fatalf("err = %v, want row 14 missing", err)
	}
}

func TestZetarisReloginOnExpiry(t *testing.T) {
	f := &fakeZetaris{data: [][]string{{"1", "1", "health", `{}`}}}
	z := &Zetaris{BaseURL: f.server(t).URL}
	ev := ZetarisEvidence{Z: z, Run: "r1"}
	if _, err := ev.Rows(&Finding{Cycle: 1, Evidence: []int{1}}); err != nil {
		t.Fatal(err)
	}
	f.expired = true
	if _, err := ev.Rows(&Finding{Cycle: 1, Evidence: []int{1}}); err != nil {
		t.Fatal(err)
	}
	if f.logins != 2 {
		t.Errorf("logins = %d, want 2", f.logins)
	}
}

func TestZetarisEvidenceRejectsBadRun(t *testing.T) {
	ev := ZetarisEvidence{Z: &Zetaris{}, Run: "x' OR '1'='1"}
	if _, err := ev.Rows(&Finding{Evidence: []int{1}}); err == nil {
		t.Fatal("bad run id accepted")
	}
}
