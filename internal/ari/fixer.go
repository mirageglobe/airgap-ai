package ari

// Fixer picks a catalogue entry for a cause. It never decides whether to act;
// the verifier's gate does.
type Fixer interface {
	Pick(c Cause, cat *Catalogue) (entry, reason string, err error)
}

// RuleFixer matches the cause's symptom to a catalogue entry. It is the
// fallback when the model picks wrong, and the fixer until the model is wired.
type RuleFixer struct{}

func (RuleFixer) Pick(c Cause, cat *Catalogue) (string, string, error) {
	m := cat.BySymptom(c.Symptom)
	if len(m) == 0 {
		return "", "no catalogue entry matches " + c.Symptom, nil
	}
	return m[0].Name, "symptom " + c.Symptom + " matches " + m[0].Name, nil
}
