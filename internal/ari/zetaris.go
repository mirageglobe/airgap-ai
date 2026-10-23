package ari

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// EvidenceTable is where the explorer's rows live, as Zetaris sees them.
const EvidenceTable = "ari_evidence.evidence_rows"

// Zetaris runs Lightning SQL through the platform's UI proxy, which takes any
// statement and logs in with a user and password.
type Zetaris struct {
	BaseURL  string // UI origin, e.g. http://127.0.0.1:3000
	User     string
	Password string
	Client   *http.Client

	token string
	org   string
}

var errUnauthorized = errors.New("zetaris: unauthorized")

// Query runs one statement and returns its rows keyed by column. Zetaris
// returns every value as a string.
func (z *Zetaris) Query(ctx context.Context, sql string, limit int) ([]map[string]string, error) {
	if z.token == "" {
		if err := z.login(ctx); err != nil {
			return nil, err
		}
	}
	rows, err := z.query(ctx, sql, limit)
	if errors.Is(err, errUnauthorized) {
		if err := z.login(ctx); err != nil {
			return nil, err
		}
		rows, err = z.query(ctx, sql, limit)
	}
	return rows, err
}

func (z *Zetaris) login(ctx context.Context) error {
	var out struct {
		IDToken string          `json:"idToken"`
		OrgID   json.RawMessage `json:"orgId"`
	}
	body := map[string]string{"username": z.User, "password": z.Password}
	if err := z.post(ctx, "/api/auth/login", body, false, &out); err != nil {
		return fmt.Errorf("zetaris login: %w", err)
	}
	if out.IDToken == "" || len(out.OrgID) == 0 {
		return errors.New("zetaris login: no token or organisation in response")
	}
	z.token, z.org = out.IDToken, strings.Trim(string(out.OrgID), `"`)
	return nil
}

func (z *Zetaris) query(ctx context.Context, sql string, limit int) ([]map[string]string, error) {
	var out struct {
		Headers []string   `json:"headers"`
		Data    [][]string `json:"data"`
	}
	body := map[string]any{"sql": sql, "limit": limit}
	if err := z.post(ctx, "/api/proxy/sql-editor/sqls/run-query", body, true, &out); err != nil {
		return nil, err
	}
	if out.Headers == nil {
		return nil, errors.New("zetaris: response has no headers")
	}
	rows := make([]map[string]string, 0, len(out.Data))
	for _, d := range out.Data {
		m := make(map[string]string, len(out.Headers))
		for i, h := range out.Headers {
			if i < len(d) {
				m[h] = d[i]
			}
		}
		rows = append(rows, m)
	}
	return rows, nil
}

func (z *Zetaris) post(ctx context.Context, path string, body any, auth bool, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(z.BaseURL, "/")+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if auth {
		req.Header.Set("Authorization", "Bearer "+z.token)
		req.Header.Set("X-Org-ID", z.org)
		req.Header.Set("X-Request-ID", requestID())
	}
	client := z.Client
	if client == nil {
		// Spark answers in under a second once warm; the first query is slower.
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return errUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("zetaris: %s: %s", resp.Status, truncate(string(raw), 200))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("zetaris: bad response: %w", err)
	}
	return nil
}

func requestID() string {
	b := make([]byte, 16)
	rand.Read(b)
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

var runPattern = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// ZetarisEvidence reads a finding's rows back through Zetaris, plus the
// health rows of the cycles before it as the baseline to compare against.
type ZetarisEvidence struct {
	Z   *Zetaris
	Run string
}

// baselineCycles is how many earlier cycles of health rows come with a finding.
const baselineCycles = 3

func (z ZetarisEvidence) Rows(f *Finding) ([]Row, error) {
	if !runPattern.MatchString(z.Run) {
		return nil, fmt.Errorf("bad run id %q", z.Run)
	}
	sql := fmt.Sprintf("SELECT id, cycle, kind, data FROM %s WHERE run = '%s' AND (id IN (%s) OR (kind = 'health' AND cycle >= %d AND cycle < %d)) ORDER BY id",
		EvidenceTable, z.Run, joinInts(f.Evidence), f.Cycle-baselineCycles, f.Cycle)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	res, err := z.Z.Query(ctx, sql, 100)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(res))
	seen := map[int]bool{}
	for _, m := range res {
		r, err := parseRow(m)
		if err != nil {
			return nil, err
		}
		seen[r.ID] = true
		rows = append(rows, r)
	}
	// Fail closed: a finding's own rows must all be there.
	for _, id := range f.Evidence {
		if !seen[id] {
			return nil, fmt.Errorf("row %d not in zetaris", id)
		}
	}
	return rows, nil
}

func parseRow(m map[string]string) (Row, error) {
	id, err := strconv.Atoi(m["id"])
	if err != nil {
		return Row{}, fmt.Errorf("zetaris row id %q: %w", m["id"], err)
	}
	cycle, err := strconv.Atoi(m["cycle"])
	if err != nil {
		return Row{}, fmt.Errorf("zetaris row %d cycle %q: %w", id, m["cycle"], err)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(m["data"]), &data); err != nil {
		return Row{}, fmt.Errorf("zetaris row %d data: %w", id, err)
	}
	return Row{ID: id, Cycle: cycle, Kind: m["kind"], Data: data}, nil
}
