package ari

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
)

// fakeModel answers every chat completion with reply and records the request.
type fakeModel struct {
	srv    *httptest.Server
	header http.Header
	body   map[string]any
}

func newFakeModel(t *testing.T, reply string) *fakeModel {
	t.Helper()
	f := &fakeModel{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.header = r.Header.Clone()
		json.NewDecoder(r.Body).Decode(&f.body)
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": reply}}},
		})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeModel) fixer() ModelFixer {
	return ModelFixer{Model: &Model{BaseURL: f.srv.URL + "/v1", Token: "test-token", Name: "meterless/auto", Label: "nightshift"}}
}

func TestModelFixerPickIsTagged(t *testing.T) {
	m := newFakeModel(t, "```json\n{\"entry\": \"restart-service\", \"reason\": \"toyd is gone MARKER\"}\n```")
	r := newRig(t)
	r.run(m.fixer(), 12, func() { r.signal(syscall.SIGKILL) })

	f := r.onlyFinding()
	if f.Status != "resolved" || f.Entry != "restart-service" {
		t.Fatalf("finding = %+v, want resolved by restart-service", f)
	}
	tagged := 0
	r.lines("feed.log", func(b []byte) {
		line := string(b)
		if strings.Contains(line, "MARKER") {
			if !strings.Contains(line, "[ ARI : nightshift ]") {
				t.Errorf("model words on an untagged line: %s", line)
			}
			tagged++
		}
	})
	if tagged == 0 {
		t.Error("no tagged model line in the feed")
	}
	if got := m.header.Get("Authorization"); got != "Bearer test-token" {
		t.Errorf("Authorization = %q", got)
	}
	if m.header.Get("meterless-app") == "" || m.header.Get("meterless-scope") != "user" {
		t.Errorf("missing Nightshift headers: %v", m.header)
	}
	if m.body["model"] != "meterless/auto" {
		t.Errorf("model = %v", m.body["model"])
	}
}

func TestModelBadJSONEscalates(t *testing.T) {
	m := newFakeModel(t, "sure, just restart it")
	r := newRig(t)
	r.run(m.fixer(), 12, func() { r.signal(syscall.SIGKILL) })

	f := r.onlyFinding()
	if f.Status != "unresolved" || f.Attempts != 0 {
		t.Fatalf("finding = %+v, want escalated with no attempt", f)
	}
	if n := r.execCount(""); n != 0 {
		t.Errorf("%d commands ran after bad json, want none", n)
	}
}

func TestModelUnreachableEscalates(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	url := "http://" + l.Addr().String() + "/v1"
	l.Close()
	r := newRig(t)
	r.run(ModelFixer{Model: &Model{BaseURL: url, Name: "x", Label: "nightshift"}}, 12, func() { r.signal(syscall.SIGKILL) })

	f := r.onlyFinding()
	if f.Status != "unresolved" || !strings.Contains(f.Reason, "fixer error") {
		t.Fatalf("finding = %+v, want escalated with a fixer error", f)
	}
	if n := r.execCount(""); n != 0 {
		t.Errorf("%d commands ran with the model down, want none", n)
	}
}

func TestJSONObject(t *testing.T) {
	for in, want := range map[string]string{
		`{"entry":"a"}`:                      `{"entry":"a"}`,
		"```json\n{\"entry\":\"a\"}\n```":    `{"entry":"a"}`,
		`ok: {"entry":"a","reason":"b"} bye`: `{"entry":"a","reason":"b"}`,
		"no json here":                       "no json here",
	} {
		if got := jsonObject(in); got != want {
			t.Errorf("jsonObject(%q) = %q, want %q", in, got, want)
		}
	}
}
