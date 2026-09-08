package main

import (
	"encoding/json"
	"fmt"
	"os"

	"gowl/el"
	"gowl/lint"
	"gowl/owl"
)

// The documents in this file are the machine-readable contract of the CLI:
// -json makes a command write exactly one JSON object to stdout, including when
// it fails. Field names are snake_case, lists are always present rather than
// null, and axioms are rendered with the ontology's own prefixes so they match
// the source file.
//
// -json changes the format, not the content: flags that select how much detail
// to report (diff -summary, profile -v) mean the same thing in both modes.

// writeJSON emits v as an indented JSON document followed by a newline.
func writeJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	// Ontology text is full of <, > and &; HTML escaping would render every
	// IRI and message unreadable for no benefit here.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(os.Stderr, "gowl:", err)
	}
}

// failWith reports an error as JSON when the caller asked for it, so a harness
// always gets one parseable object on stdout. The exit status is unchanged.
func failWith(jsonOut bool, err error) int {
	if jsonOut {
		writeJSON(errorDoc{Error: err.Error()})
		return exitProblem
	}
	return fail(err)
}

type errorDoc struct {
	Error string `json:"error"`
}

type entityDoc struct {
	Kind string `json:"kind"`
	IRI  string `json:"iri"`
}

func entityDocs(entities []owl.Entity) []entityDoc {
	out := make([]entityDoc, 0, len(entities))
	for _, e := range entities {
		out = append(out, entityDoc{Kind: e.Kind().String(), IRI: string(e.IRI())})
	}
	return out
}

// --- lint -------------------------------------------------------------------

type lintDoc struct {
	File     string       `json:"file"`
	Findings []findingDoc `json:"findings"`
	Summary  lintSummary  `json:"summary"`
}

type findingDoc struct {
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	Subject  string `json:"subject,omitempty"`
	Message  string `json:"message"`
	Axiom    string `json:"axiom,omitempty"`
}

type lintSummary struct {
	Total int `json:"total"`
	// BySeverity always carries all three keys, so a consumer never has to
	// distinguish "no errors" from "field absent".
	BySeverity map[string]int `json:"by_severity"`
	Worst      string         `json:"worst,omitempty"`
	// Failed reports whether the findings met the -fail-on threshold.
	Failed bool `json:"failed"`
}

func lintJSON(file string, o *owl.Ontology, findings []lint.Finding, threshold lint.Severity) lintDoc {
	docs := make([]findingDoc, 0, len(findings))
	bySeverity := map[string]int{
		lint.Info.String():    0,
		lint.Warning.String(): 0,
		lint.Error.String():   0,
	}
	for _, f := range findings {
		d := findingDoc{
			Rule:     f.Rule,
			Severity: f.Severity.String(),
			Subject:  string(f.Subject),
			Message:  f.Message,
		}
		if f.Axiom != nil {
			d.Axiom = o.Render(f.Axiom)
		}
		docs = append(docs, d)
		bySeverity[f.Severity.String()]++
	}

	summary := lintSummary{Total: len(findings), BySeverity: bySeverity}
	if worst, any := lint.MaxSeverity(findings); any {
		summary.Worst = worst.String()
		summary.Failed = worst >= threshold
	}
	return lintDoc{File: file, Findings: docs, Summary: summary}
}

type rulesDoc struct {
	Rules []ruleDoc `json:"rules"`
}

type ruleDoc struct {
	Name        string `json:"name"`
	Severity    string `json:"severity"`
	Description string `json:"description"`
}

func rulesJSON(rules []lint.Rule) rulesDoc {
	out := make([]ruleDoc, 0, len(rules))
	for _, r := range rules {
		out = append(out, ruleDoc{
			Name:        r.Name,
			Severity:    r.Severity.String(),
			Description: r.Description,
		})
	}
	return rulesDoc{Rules: out}
}

// --- diff -------------------------------------------------------------------

type diffDoc struct {
	From string `json:"from"`
	To   string `json:"to"`
	// Changed is false exactly when the two ontologies are equivalent under
	// canonical comparison.
	Changed        bool       `json:"changed"`
	IRIChanged     bool       `json:"iri_changed"`
	VersionChanged bool       `json:"version_changed"`
	Counts         diffCounts `json:"counts"`

	// The detail lists are omitted under -summary.
	AddedAxioms        []string    `json:"added_axioms,omitempty"`
	RemovedAxioms      []string    `json:"removed_axioms,omitempty"`
	AddedImports       []string    `json:"added_imports,omitempty"`
	RemovedImports     []string    `json:"removed_imports,omitempty"`
	AddedAnnotations   []string    `json:"added_annotations,omitempty"`
	RemovedAnnotations []string    `json:"removed_annotations,omitempty"`
	Entities           []entityDoc `json:"entities,omitempty"`

	Summary string `json:"summary"`
}

type diffCounts struct {
	AddedAxioms        int `json:"added_axioms"`
	RemovedAxioms      int `json:"removed_axioms"`
	AddedImports       int `json:"added_imports"`
	RemovedImports     int `json:"removed_imports"`
	AddedAnnotations   int `json:"added_annotations"`
	RemovedAnnotations int `json:"removed_annotations"`
}

func diffJSON(fromPath, toPath string, d *owl.Diff, summaryOnly bool) diffDoc {
	doc := diffDoc{
		From:           fromPath,
		To:             toPath,
		Changed:        !d.Empty(),
		IRIChanged:     d.IRIChanged,
		VersionChanged: d.VersionChanged,
		Counts: diffCounts{
			AddedAxioms:        len(d.AddedAxioms),
			RemovedAxioms:      len(d.RemovedAxioms),
			AddedImports:       len(d.AddedImports),
			RemovedImports:     len(d.RemovedImports),
			AddedAnnotations:   len(d.AddedAnnotations),
			RemovedAnnotations: len(d.RemovedAnnotations),
		},
		Summary: d.Summary(),
	}
	if summaryOnly {
		return doc
	}

	// Render each side with its own prefixes: a removed axiom belongs to the
	// old document and should read the way it does there.
	doc.AddedAxioms = renderAxioms(d.To, d.AddedAxioms)
	doc.RemovedAxioms = renderAxioms(d.From, d.RemovedAxioms)
	doc.AddedImports = iriStrings(d.AddedImports)
	doc.RemovedImports = iriStrings(d.RemovedImports)
	doc.AddedAnnotations = renderAnnotations(d.To, d.AddedAnnotations)
	doc.RemovedAnnotations = renderAnnotations(d.From, d.RemovedAnnotations)
	doc.Entities = entityDocs(d.Entities())
	return doc
}

func renderAxioms(o *owl.Ontology, axioms []owl.Axiom) []string {
	out := make([]string, 0, len(axioms))
	for _, ax := range axioms {
		out = append(out, o.Render(ax))
	}
	return out
}

func renderAnnotations(o *owl.Ontology, annotations []owl.Annotation) []string {
	out := make([]string, 0, len(annotations))
	for _, a := range annotations {
		out = append(out, o.Render(a))
	}
	return out
}

func iriStrings(iris []owl.IRI) []string {
	out := make([]string, 0, len(iris))
	for _, i := range iris {
		out = append(out, string(i))
	}
	return out
}

// --- classify ---------------------------------------------------------------

type classifyDoc struct {
	File string `json:"file"`
	// Consistent is false only when the axioms force owl:Thing to be empty.
	Consistent bool `json:"consistent"`
	// Coherent is false when some named class cannot have instances.
	Coherent           bool     `json:"coherent"`
	Classes            int      `json:"classes"`
	InferredAxiomCount int      `json:"inferred_axiom_count"`
	Unsatisfiable      []string `json:"unsatisfiable"`
	// Unsupported lists the axioms outside OWL 2 EL that were skipped. When it
	// is non-empty the classification is sound but may be incomplete.
	Unsupported []string `json:"unsupported"`
	Notes       []string `json:"notes"`
	// InferredAxioms is populated only under -axioms.
	InferredAxioms []string `json:"inferred_axioms,omitempty"`
}

func classifyJSON(file string, o *owl.Ontology, c *el.Classification, withAxioms, unsatOnly bool) classifyDoc {
	doc := classifyDoc{
		File:               file,
		Consistent:         c.IsConsistent(),
		Coherent:           c.IsCoherent(),
		Classes:            len(c.Classes()),
		InferredAxiomCount: len(c.InferredAxioms()),
		Unsatisfiable:      renderClasses(o, c.UnsatisfiableClasses()),
		Unsupported:        renderAxioms(o, c.Unsupported()),
		Notes:              []string{},
	}
	if len(doc.Unsupported) > 0 {
		doc.Notes = append(doc.Notes, incompleteNote)
	}
	if withAxioms && !unsatOnly {
		doc.InferredAxioms = renderAxioms(o, c.InferredAxioms())
	}
	return doc
}

type explainDoc struct {
	Sub   string `json:"sub"`
	Super string `json:"super"`
	// Entailed reports whether the subsumption holds at all.
	Entailed bool `json:"entailed"`
	// Axioms is the support of the derivation found, not a minimal
	// justification: it entails the subsumption, but a smaller set may too.
	Axioms []string `json:"axioms"`
	Notes  []string `json:"notes"`
}

func explainJSON(o *owl.Ontology, sub, super owl.Class, entailed bool, why []owl.Axiom) explainDoc {
	return explainDoc{
		Sub:      string(sub),
		Super:    string(super),
		Entailed: entailed,
		Axioms:   renderAxioms(o, why),
		Notes:    []string{"the axioms are the support of one derivation, not a minimal justification"},
	}
}

func renderClasses(o *owl.Ontology, classes []owl.Class) []string {
	out := make([]string, 0, len(classes))
	for _, cl := range classes {
		out = append(out, string(cl))
	}
	return out
}

// --- profile ----------------------------------------------------------------

type profileDoc struct {
	File     string          `json:"file"`
	Profiles []profileResult `json:"profiles"`
	// InProfiles lists just the profiles the ontology belongs to, for consumers
	// that only need the verdict.
	InProfiles []string `json:"in_profiles"`
	// Notes records the limits of the check itself.
	Notes []string `json:"notes"`
}

type profileResult struct {
	Profile        string `json:"profile"`
	InProfile      bool   `json:"in_profile"`
	ViolationCount int    `json:"violation_count"`
	// Violations is populated only under -v, matching the text output.
	Violations []violationDoc `json:"violations,omitempty"`
}

type violationDoc struct {
	Reason string `json:"reason"`
	Axiom  string `json:"axiom"`
}

func profileJSON(file string, o *owl.Ontology, verbose bool) profileDoc {
	doc := profileDoc{
		File:       file,
		Profiles:   make([]profileResult, 0, len(owl.AllProfiles)),
		InProfiles: []string{},
		Notes:      []string{dlCheckNote},
	}
	for _, p := range owl.AllProfiles {
		violations := owl.CheckProfile(o, p)
		r := profileResult{
			Profile:        p.String(),
			InProfile:      len(violations) == 0,
			ViolationCount: len(violations),
		}
		if r.InProfile {
			doc.InProfiles = append(doc.InProfiles, r.Profile)
		}
		if verbose {
			r.Violations = make([]violationDoc, 0, len(violations))
			for _, v := range violations {
				r.Violations = append(r.Violations, violationDoc{
					Reason: v.Reason,
					Axiom:  o.Render(v.Axiom),
				})
			}
		}
		doc.Profiles = append(doc.Profiles, r)
	}
	return doc
}

// --- stats ------------------------------------------------------------------

type statsDoc struct {
	File       string           `json:"file"`
	Ontology   string           `json:"ontology,omitempty"`
	Version    string           `json:"version,omitempty"`
	Imports    []string         `json:"imports"`
	Counts     statsCounts      `json:"counts"`
	AxiomTypes []axiomTypeCount `json:"axiom_types"`
}

type statsCounts struct {
	Axioms           int `json:"axioms"`
	Classes          int `json:"classes"`
	ObjectProperties int `json:"object_properties"`
	DataProperties   int `json:"data_properties"`
	Individuals      int `json:"individuals"`
	Datatypes        int `json:"datatypes"`
}

// axiomTypeCount is a list entry rather than a map so the ordering the text
// output uses — most frequent first — survives.
type axiomTypeCount struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

func statsJSON(file string, o *owl.Ontology, kinds []string, counts map[string]int) statsDoc {
	types := make([]axiomTypeCount, 0, len(kinds))
	for _, k := range kinds {
		types = append(types, axiomTypeCount{Type: k, Count: counts[k]})
	}
	return statsDoc{
		File:     file,
		Ontology: string(o.IRI),
		Version:  string(o.VersionIRI),
		Imports:  iriStrings(o.Imports),
		Counts: statsCounts{
			Axioms:           o.Len(),
			Classes:          len(o.Classes()),
			ObjectProperties: len(o.ObjectProperties()),
			DataProperties:   len(o.DataProperties()),
			Individuals:      len(o.Individuals()),
			Datatypes:        len(o.Datatypes()),
		},
		AxiomTypes: types,
	}
}
