package ari

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Model calls an OpenAI-compatible chat endpoint: Meterless Nightshift in the
// product, any compatible endpoint while building.
type Model struct {
	BaseURL string // e.g. http://127.0.0.1:8088/v1
	Token   string
	Name    string // model or routing mode, e.g. meterless/auto
	Label   string // feed tag for model words, e.g. nightshift
	// Reasoning is sent as reasoning_effort when set; "none" turns thinking
	// off on runtimes that think by default.
	Reasoning string
	Client    *http.Client
}

// Complete sends one system and one user message and returns the reply text.
func (m *Model) Complete(ctx context.Context, system, user string) (string, error) {
	req := map[string]any{
		"model": m.Name,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"temperature": 0,
		"max_tokens":  1024,
	}
	if m.Reasoning != "" {
		req["reasoning_effort"] = m.Reasoning
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(m.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Authorization", "Bearer "+m.Token)
	// Nightshift runtime contract; other endpoints ignore these.
	hreq.Header.Set("meterless-app", "ari")
	hreq.Header.Set("meterless-scope", "user")

	client := m.Client
	if client == nil {
		// Cold model loads can take minutes; short timeouts read as outages.
		client = &http.Client{Timeout: 180 * time.Second}
	}
	resp, err := client.Do(hreq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("model: %s: %s", resp.Status, truncate(string(raw), 200))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("model: bad response: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", errors.New("model: no choices in response")
	}
	return out.Choices[0].Message.Content, nil
}

const fixerPrompt = `You pick one repair for a fault on an offline machine.
You only pick from the catalogue below; you never invent a repair and you do not decide whether to act.
If an entry already failed, prefer another entry for the same finding; pick it again only if it is the only one.
Reply with JSON only, no other text: {"entry": "<catalogue name, or empty if none fits>", "reason": "<one line>"}`

// BadReply is a model reply that is not the JSON asked for. Reply is model
// words, so the engine shows it only on a tagged line.
type BadReply struct {
	Reply string
}

func (*BadReply) Error() string { return "model returned bad json" }

// ModelFixer asks the model to pick a catalogue entry. Bad JSON or no pick
// escalates; a wrong pick is stopped by the verifier's gate.
type ModelFixer struct {
	Model *Model
}

func (f ModelFixer) Pick(c Cause, cat *Catalogue) (string, string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "finding: %s\n", c.Symptom)
	for _, f := range c.Facts {
		fmt.Fprintf(&b, "fact: %s\n", f)
	}
	if len(c.Tried) > 0 {
		fmt.Fprintf(&b, "already tried and failed: %s\n", strings.Join(c.Tried, ", "))
	}
	b.WriteString("\ncatalogue:\n")
	for _, n := range cat.order {
		fmt.Fprintf(&b, "- %s: handles %s\n", n, cat.entries[n].Symptom)
	}
	reply, err := f.Model.Complete(context.Background(), fixerPrompt, b.String())
	if err != nil {
		return "", "", err
	}
	var pick struct {
		Entry  string `json:"entry"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(jsonObject(reply)), &pick); err != nil {
		return "", "", &BadReply{Reply: reply}
	}
	return pick.Entry, oneLine(pick.Reason), nil
}

func (f ModelFixer) Label() string {
	if f.Model.Label == "" {
		return "model"
	}
	return f.Model.Label
}

// jsonObject cuts the outermost {...} out of a reply, dropping code fences
// or prose around it.
func jsonObject(s string) string {
	start, end := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if start < 0 || end < start {
		return s
	}
	return s[start : end+1]
}

func oneLine(s string) string {
	return truncate(strings.Join(strings.Fields(s), " "), 200)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
