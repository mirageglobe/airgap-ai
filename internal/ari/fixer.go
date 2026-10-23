package ari

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

func (RuleFixer) Pick(c Cause, cat *Catalogue) (string, string, error) {
	m := cat.BySymptom(c.Symptom)
	if len(m) == 0 {
		return "", "nothing handles " + c.Symptom, nil
	}
	return m[0].Name, "symptom " + c.Symptom + " matches " + m[0].Name, nil
}
