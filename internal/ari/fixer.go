package ari

import "slices"

// Fixer picks a catalogue entry for a cause. It never decides whether to act;
// the verifier's gate does.
type Fixer interface {
	Pick(c Cause, cat *Catalogue) (entry, reason string, err error)
}

// labeled is implemented by fixers whose reasons are model words; the feed
// tags those lines "[ ARI : <label> ]".
type labeled interface {
	Label() string
}

// RuleFixer matches the cause's symptom to a catalogue entry. ari uses it
// when no model is set.
type RuleFixer struct{}

// On a retry it prefers an entry that has not failed yet; when the only
// entry for the symptom already failed, it gets its one retry.
func (RuleFixer) Pick(c Cause, cat *Catalogue) (string, string, error) {
	m := cat.BySymptom(c.Symptom)
	if len(m) == 0 {
		return "", "nothing handles " + c.Symptom, nil
	}
	for _, e := range m {
		if !slices.Contains(c.Tried, e.Name) {
			return e.Name, "symptom " + c.Symptom + " matches " + e.Name, nil
		}
	}
	return m[0].Name, "only " + m[0].Name + " handles " + c.Symptom + "; retrying it once", nil
}
