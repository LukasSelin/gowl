// Package lint checks ontologies against structural policy: conventions and
// hygiene rules that hold regardless of what the ontology means.
//
// Nothing here reasons. Every rule is a walk over asserted axioms, so lint runs
// in CI on ontologies no reasoner could handle. A rule is an ordinary value, so
// projects can drop the built-ins they disagree with and add their own.
package lint

import (
	"fmt"
	"sort"
	"strings"

	"gowl/owl"
)

// Severity ranks a finding. Only Error is meant to fail a build.
type Severity int

const (
	// Info reports something worth a look that is often deliberate.
	Info Severity = iota
	// Warning reports a likely mistake.
	Warning
	// Error reports something that is almost certainly wrong.
	Error
)

func (s Severity) String() string {
	switch s {
	case Info:
		return "info"
	case Warning:
		return "warning"
	case Error:
		return "error"
	}
	return "unknown"
}

// ParseSeverity converts a name such as "warning" to a [Severity].
func ParseSeverity(s string) (Severity, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "info":
		return Info, nil
	case "warning", "warn":
		return Warning, nil
	case "error":
		return Error, nil
	}
	return 0, fmt.Errorf("lint: unknown severity %q", s)
}

// Finding is one problem found in an ontology.
type Finding struct {
	Rule     string
	Severity Severity
	// Subject is the entity or IRI the finding is about, when there is one.
	Subject owl.IRI
	Message string
	// Axiom is the axiom that triggered the finding, when one did.
	Axiom owl.Axiom
}

func (f Finding) String() string {
	var b strings.Builder
	b.WriteString(f.Severity.String())
	b.WriteString(": ")
	b.WriteString(f.Rule)
	b.WriteString(": ")
	b.WriteString(f.Message)
	return b.String()
}

// Rule is a named check. Check reports findings without setting Rule or
// Severity; [Run] fills those in from the rule.
type Rule struct {
	Name        string
	Description string
	Severity    Severity
	Check       func(*owl.Ontology) []Finding
}

// Run applies rules to o and returns the findings, ordered by severity
// (most severe first), then rule name, then subject, so output is stable.
func Run(o *owl.Ontology, rules []Rule) []Finding {
	var out []Finding
	for _, r := range rules {
		for _, f := range r.Check(o) {
			f.Rule = r.Name
			f.Severity = r.Severity
			out = append(out, f)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return out[i].Severity > out[j].Severity
		}
		if out[i].Rule != out[j].Rule {
			return out[i].Rule < out[j].Rule
		}
		return out[i].Subject < out[j].Subject
	})
	return out
}

// Select returns the rules whose names are not in disabled. Unknown names are
// returned so callers can report a typo instead of silently disabling nothing.
func Select(rules []Rule, disabled []string) (kept []Rule, unknown []string) {
	drop := make(map[string]bool, len(disabled))
	for _, name := range disabled {
		if name = strings.TrimSpace(name); name != "" {
			drop[name] = true
		}
	}
	known := make(map[string]bool, len(rules))
	for _, r := range rules {
		known[r.Name] = true
		if !drop[r.Name] {
			kept = append(kept, r)
		}
	}
	for name := range drop {
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	return kept, unknown
}

// MaxSeverity returns the highest severity among findings, and whether there
// were any.
func MaxSeverity(findings []Finding) (Severity, bool) {
	if len(findings) == 0 {
		return Info, false
	}
	max := findings[0].Severity
	for _, f := range findings[1:] {
		if f.Severity > max {
			max = f.Severity
		}
	}
	return max, true
}
