package ari

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Scout is ari's implementation of the Meterless Scout Intent engine
// (github.com/meterless/meterless/engines/scout-intent): it senses the
// intent behind a finding, guards the fixer's pick, routes it to one
// catalogue entry and emits a signed execution contract. The verifier acts
// only on a valid contract.
type Scout struct {
	cat    *Catalogue
	secret []byte
	seq    int
	now    func() time.Time
}

// contractTTL is Scout's default: a decision goes stale quickly.
const contractTTL = 5 * time.Minute

const policyPackVersion = "ari-1"

type Contract struct {
	ContractID        string         `json:"contractId"`
	ParentID          string         `json:"parentId,omitempty"`
	TraceID           string         `json:"traceId"`
	Intent            ContractIntent `json:"intent"`
	Risk              ContractRisk   `json:"risk"`
	ToolPlan          []ToolStep     `json:"toolPlan"`
	ModelProfile      string         `json:"modelProfile"`
	Scope             ContractScope  `json:"scope"`
	IssuedAt          time.Time      `json:"issuedAt"`
	ExpiresAt         time.Time      `json:"expiresAt"`
	RegistryVersion   int            `json:"registryVersion"`
	PolicyPackVersion string         `json:"policyPackVersion"`
	Signature         string         `json:"signature"`
	SignedBy          string         `json:"signedBy"`
}

type ContractIntent struct {
	Primary    string            `json:"primary"`
	Parameters map[string]string `json:"parameters"`
	Confidence float64           `json:"confidence"`
	Alternates []IntentCandidate `json:"alternates"`
}

type IntentCandidate struct {
	Intent     string  `json:"intent"`
	Confidence float64 `json:"confidence"`
}

type ContractRisk struct {
	Level string   `json:"level"` // low or high; high is refused, ari has no approvers
	Flags []string `json:"flags"`
}

type ToolStep struct {
	Tool string `json:"tool"`
	Step string `json:"step"`
}

type ContractScope struct {
	User         string   `json:"user"`
	Surface      string   `json:"surface"`
	Capabilities []string `json:"capabilities"` // exact set the verifier may run
}

// Decision is what Scout is asked to decide on.
type Decision struct {
	Run         string
	Cause       Cause
	Pick        string // catalogue entry the fixer chose
	Reason      string
	ByModel     bool
	Model       string // model profile, empty for the rule fixer
	Parent      string // previous contract when re-planning after a failed fix
	BudgetSpent bool
}

func NewScout(cat *Catalogue) *Scout {
	secret := make([]byte, 32)
	rand.Read(secret)
	return &Scout{cat: cat, secret: secret, now: time.Now}
}

func intentOf(symptom string) string { return "toyd." + symptom }

// injectionHints are fragments that have no place in a one line reason for
// picking a catalogue entry; seeing one means the model is being steered.
var injectionHints = []string{
	"ignore previous", "ignore all", "disregard", "system prompt", "new instructions",
	"sudo ", "rm -rf", "curl ", "wget ", "http://", "https://", "$(", "`", "&&", "| sh",
}

func (s *Scout) Decide(d Decision) Contract {
	s.seq++
	now := s.now().UTC()
	c := Contract{
		ContractID: fmt.Sprintf("c-%s-%03d", now.Format("20060102T150405"), s.seq),
		ParentID:   d.Parent,
		TraceID:    d.Run + "/" + d.Cause.Finding,
		Intent: ContractIntent{
			Primary:    intentOf(d.Cause.Symptom),
			Parameters: map[string]string{"finding": d.Cause.Finding, "evidence": joinInts(d.Cause.Evidence)},
		},
		ModelProfile:      "rules",
		Scope:             ContractScope{User: "ari", Surface: "verifier"},
		IssuedAt:          now,
		ExpiresAt:         now.Add(contractTTL),
		RegistryVersion:   len(s.cat.order),
		PolicyPackVersion: policyPackVersion,
		SignedBy:          "scout",
	}
	if d.ByModel {
		c.ModelProfile = d.Model
	}

	var flags []string
	entry, known := s.cat.Get(d.Pick)
	switch {
	case !known:
		flags = append(flags, "route.unknown-entry")
	case entry.Symptom != d.Cause.Symptom:
		flags = append(flags, "scope.mismatch")
		c.Intent.Alternates = append(c.Intent.Alternates, IntentCandidate{Intent: intentOf(entry.Symptom), Confidence: 0.5})
	}
	if d.ByModel {
		low := strings.ToLower(d.Reason)
		for _, h := range injectionHints {
			if strings.Contains(low, h) {
				flags = append(flags, "guard.injection")
				break
			}
		}
	}
	if d.BudgetSpent {
		flags = append(flags, "policy.budget")
	}

	c.Risk = ContractRisk{Level: "low", Flags: flags}
	if len(flags) > 0 {
		c.Risk.Level = "high"
	} else {
		c.Intent.Confidence = 1
		if d.ByModel {
			// The model agrees with the explorer's symptom; rules alone are certain.
			c.Intent.Confidence = 0.9
		}
		c.Scope.Capabilities = []string{entry.Name}
		c.ToolPlan = []ToolStep{
			{Tool: entry.Name, Step: "precondition"},
			{Tool: entry.Name, Step: "apply"},
			{Tool: entry.Name, Step: entry.DoneTest},
			{Tool: entry.Name, Step: "undo on failure"},
		}
	}
	c.Signature = s.sign(c)
	return c
}

// Verify is the verifier's boundary check: a valid signature, not expired,
// low risk, and the entry inside the contract's capabilities.
func (s *Scout) Verify(c Contract, entry string) error {
	if c.SignedBy != "scout" || !hmac.Equal([]byte(c.Signature), []byte(s.sign(c))) {
		return errors.New("bad signature")
	}
	if !s.now().Before(c.ExpiresAt) {
		return errors.New("contract expired")
	}
	if c.Risk.Level != "low" {
		return fmt.Errorf("risk %s: %s", c.Risk.Level, strings.Join(c.Risk.Flags, ", "))
	}
	if !slices.Contains(c.Scope.Capabilities, entry) {
		return fmt.Errorf("%s is outside the contract's capabilities", entry)
	}
	return nil
}

// sign is HMAC_SHA256 over the contract's JSON with the signature empty.
func (s *Scout) sign(c Contract) string {
	c.Signature = ""
	b, _ := json.Marshal(c)
	m := hmac.New(sha256.New, s.secret)
	m.Write(b)
	return hex.EncodeToString(m.Sum(nil))
}
