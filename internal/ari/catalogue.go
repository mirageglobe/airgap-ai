package ari

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// Entry is one pre-approved, reversible repair.
type Entry struct {
	Name         string   `json:"name"`
	Symptom      string   `json:"symptom"`
	Precondition string   `json:"precondition"`
	Apply        string   `json:"apply"`
	DoneTest     string   `json:"done_test"`
	Within       string   `json:"within"`
	Undo         string   `json:"undo"`
	Touches      []string `json:"touches"`
}

type Catalogue struct {
	entries map[string]Entry
	order   []string
}

// LoadCatalogue rejects any entry without an undo, a done test or a symptom.
func LoadCatalogue(path string) (*Catalogue, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var es []Entry
	if err := json.Unmarshal(b, &es); err != nil {
		return nil, fmt.Errorf("catalogue: %w", err)
	}
	c := &Catalogue{entries: map[string]Entry{}}
	for _, e := range es {
		if err := validate(e); err != nil {
			return nil, err
		}
		if _, dup := c.entries[e.Name]; dup {
			return nil, fmt.Errorf("catalogue: duplicate entry %s", e.Name)
		}
		c.entries[e.Name] = e
		c.order = append(c.order, e.Name)
	}
	return c, nil
}

func validate(e Entry) error {
	switch {
	case e.Name == "":
		return errors.New("catalogue: entry without a name")
	case e.Undo == "":
		return fmt.Errorf("catalogue: %s has no undo", e.Name)
	case e.Symptom == "":
		return fmt.Errorf("catalogue: %s has no symptom", e.Name)
	case e.Apply == "":
		return fmt.Errorf("catalogue: %s has no apply", e.Name)
	case e.DoneTest != "health_ok":
		return fmt.Errorf("catalogue: %s has unknown done_test %q", e.Name, e.DoneTest)
	}
	if _, err := time.ParseDuration(e.Within); err != nil {
		return fmt.Errorf("catalogue: %s within: %w", e.Name, err)
	}
	return nil
}

func (c *Catalogue) Get(name string) (Entry, bool) {
	e, ok := c.entries[name]
	return e, ok
}

func (c *Catalogue) BySymptom(symptom string) []Entry {
	var out []Entry
	for _, n := range c.order {
		if c.entries[n].Symptom == symptom {
			out = append(out, c.entries[n])
		}
	}
	return out
}
