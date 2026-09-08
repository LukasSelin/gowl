package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// decode parses the captured stdout as a single JSON document. Every -json run
// must produce exactly one, so anything else is a failure worth naming.
func decode[T any](t *testing.T, out string) T {
	t.Helper()
	var v T
	dec := json.NewDecoder(strings.NewReader(out))
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if dec.More() {
		t.Fatalf("expected exactly one JSON document, got more:\n%s", out)
	}
	return v
}

func TestLintJSONReportsFindingsAndSummary(t *testing.T) {
	path := write(t, "cyclic.ofn", cyclicDoc)
	out, code := capture(t, func() int { return run([]string{"lint", "-json", path}) })
	if code != exitFailed {
		t.Errorf("exit = %d, want %d", code, exitFailed)
	}

	doc := decode[lintDoc](t, out)
	if doc.File != path {
		t.Errorf("file = %q, want %q", doc.File, path)
	}
	if len(doc.Findings) == 0 {
		t.Fatal("want findings for a cyclic ontology")
	}
	if doc.Summary.Total != len(doc.Findings) {
		t.Errorf("summary total = %d, findings = %d", doc.Summary.Total, len(doc.Findings))
	}
	if doc.Summary.Worst != "error" || !doc.Summary.Failed {
		t.Errorf("summary = %+v, want worst=error failed=true", doc.Summary)
	}

	// All three severity keys are always present, so consumers need not
	// distinguish zero from absent.
	for _, k := range []string{"info", "warning", "error"} {
		if _, ok := doc.Summary.BySeverity[k]; !ok {
			t.Errorf("by_severity is missing %q: %v", k, doc.Summary.BySeverity)
		}
	}

	var found bool
	for _, f := range doc.Findings {
		if f.Rule == "subclass-cycle" {
			found = true
			if f.Severity != "error" || f.Message == "" {
				t.Errorf("cycle finding = %+v", f)
			}
		}
	}
	if !found {
		t.Errorf("no subclass-cycle finding in %+v", doc.Findings)
	}
}

func TestLintJSONOnCleanOntology(t *testing.T) {
	path := write(t, "clean.ofn", cleanDoc)
	out, code := capture(t, func() int { return run([]string{"lint", "-json", path}) })
	if code != exitOK {
		t.Errorf("exit = %d, want %d", code, exitOK)
	}

	doc := decode[lintDoc](t, out)
	if doc.Summary.Failed {
		t.Errorf("clean ontology reported failed: %+v", doc.Summary)
	}
	if doc.Summary.Worst == "error" {
		t.Errorf("clean ontology has an error-level finding: %+v", doc.Findings)
	}
}

func TestLintJSONEmptyListsAreNotNull(t *testing.T) {
	// cleanDoc still trips orphan-class, so disable it to get a genuinely
	// empty result and check the encoding of an empty list.
	path := write(t, "clean.ofn", cleanDoc)
	out, code := capture(t, func() int {
		return run([]string{"lint", "-json", "-disable", "orphan-class", path})
	})
	if code != exitOK {
		t.Fatalf("exit = %d, want %d", code, exitOK)
	}

	if doc := decode[lintDoc](t, out); len(doc.Findings) != 0 {
		t.Fatalf("want no findings, got %+v", doc.Findings)
	}
	if !strings.Contains(out, `"findings": []`) {
		t.Errorf("empty findings should render as [], never null:\n%s", out)
	}
}

func TestLintJSONListsRules(t *testing.T) {
	out, code := capture(t, func() int { return run([]string{"lint", "-list", "-json"}) })
	if code != exitOK {
		t.Fatalf("exit = %d", code)
	}
	doc := decode[rulesDoc](t, out)
	if len(doc.Rules) == 0 {
		t.Fatal("want at least one rule")
	}
	for _, r := range doc.Rules {
		if r.Name == "" || r.Severity == "" || r.Description == "" {
			t.Errorf("incomplete rule entry: %+v", r)
		}
	}
}

func TestDiffJSON(t *testing.T) {
	from := write(t, "from.ofn", cleanDoc)
	to := write(t, "to.ofn", strings.Replace(cleanDoc,
		"    SubClassOf(:Child :Root)\n",
		"    SubClassOf(:Child :Root)\n    Declaration(Class(:Extra))\n", 1))

	out, code := capture(t, func() int { return run([]string{"diff", "-json", "-exit-code", from, to}) })
	if code != exitFailed {
		t.Errorf("exit = %d, want %d for a non-empty diff", code, exitFailed)
	}

	doc := decode[diffDoc](t, out)
	if !doc.Changed {
		t.Error("changed = false, want true")
	}
	if doc.Counts.AddedAxioms != 1 || doc.Counts.RemovedAxioms != 0 {
		t.Errorf("counts = %+v, want one addition", doc.Counts)
	}
	if len(doc.AddedAxioms) != 1 || !strings.Contains(doc.AddedAxioms[0], "Extra") {
		t.Errorf("added_axioms = %v", doc.AddedAxioms)
	}
	// Axioms render with the ontology's prefixes, not as full IRIs.
	if strings.Contains(doc.AddedAxioms[0], "http://") {
		t.Errorf("axiom should use the document's prefixes: %q", doc.AddedAxioms[0])
	}
	if doc.Summary == "" {
		t.Error("summary is empty")
	}
}

func TestDiffJSONSummaryOmitsDetail(t *testing.T) {
	from := write(t, "from.ofn", cleanDoc)
	to := write(t, "to.ofn", cyclicDoc)

	out, _ := capture(t, func() int { return run([]string{"diff", "-json", "-summary", from, to}) })
	doc := decode[diffDoc](t, out)

	if doc.Counts.AddedAxioms == 0 {
		t.Error("counts should survive -summary")
	}
	if len(doc.AddedAxioms) != 0 || len(doc.RemovedAxioms) != 0 || len(doc.Entities) != 0 {
		t.Errorf("-summary should omit the detail lists, got %+v", doc)
	}
}

func TestDiffJSONNoChanges(t *testing.T) {
	path := write(t, "same.ofn", cleanDoc)
	out, code := capture(t, func() int { return run([]string{"diff", "-json", "-exit-code", path, path}) })
	if code != exitOK {
		t.Errorf("exit = %d, want %d", code, exitOK)
	}
	if doc := decode[diffDoc](t, out); doc.Changed {
		t.Error("changed = true for identical files")
	}
}

func TestProfileJSON(t *testing.T) {
	path := write(t, "clean.ofn", cleanDoc)
	out, code := capture(t, func() int { return run([]string{"profile", "-json", path}) })
	if code != exitOK {
		t.Fatalf("exit = %d", code)
	}

	doc := decode[profileDoc](t, out)
	if len(doc.Profiles) != 4 {
		t.Fatalf("want four profiles, got %d", len(doc.Profiles))
	}
	// The EL/QL/RL/DL ordering of the text output is preserved.
	var order []string
	for _, p := range doc.Profiles {
		order = append(order, p.Profile)
	}
	if got, want := strings.Join(order, ","), "EL,QL,RL,DL"; got != want {
		t.Errorf("profile order = %s, want %s", got, want)
	}
	if len(doc.Notes) == 0 {
		t.Error("want the DL caveat recorded in notes")
	}
	// Without -v the violations are counted but not listed.
	for _, p := range doc.Profiles {
		if len(p.Violations) != 0 {
			t.Errorf("%s listed violations without -v", p.Profile)
		}
	}
}

func TestProfileJSONVerboseListsViolations(t *testing.T) {
	// A union is outside EL, so this ontology has something to report.
	doc := `Prefix(:=<http://example.org/o#>)
Ontology(<http://example.org/o>
    Declaration(Class(:A))
    Declaration(Class(:B))
    SubClassOf(:A ObjectUnionOf(:A :B))
)
`
	path := write(t, "union.ofn", doc)
	out, _ := capture(t, func() int { return run([]string{"profile", "-json", "-v", path}) })

	parsed := decode[profileDoc](t, out)
	for _, p := range parsed.Profiles {
		if p.Profile != "EL" {
			continue
		}
		if p.InProfile {
			t.Fatal("a union should put the ontology outside EL")
		}
		if len(p.Violations) != p.ViolationCount || len(p.Violations) == 0 {
			t.Fatalf("violations = %d, count = %d", len(p.Violations), p.ViolationCount)
		}
		if p.Violations[0].Reason == "" || p.Violations[0].Axiom == "" {
			t.Errorf("incomplete violation: %+v", p.Violations[0])
		}
	}
}

func TestStatsJSON(t *testing.T) {
	path := write(t, "clean.ofn", cleanDoc)
	out, code := capture(t, func() int { return run([]string{"stats", "-json", path}) })
	if code != exitOK {
		t.Fatalf("exit = %d", code)
	}

	doc := decode[statsDoc](t, out)
	if doc.Ontology != "http://example.org/o" {
		t.Errorf("ontology = %q", doc.Ontology)
	}
	if doc.Counts.Classes != 2 {
		t.Errorf("classes = %d, want 2", doc.Counts.Classes)
	}
	if doc.Counts.Axioms == 0 || len(doc.AxiomTypes) == 0 {
		t.Errorf("want axiom counts, got %+v", doc)
	}
	// axiom_types is ordered most frequent first.
	for i := 1; i < len(doc.AxiomTypes); i++ {
		if doc.AxiomTypes[i-1].Count < doc.AxiomTypes[i].Count {
			t.Errorf("axiom_types is not ordered by count: %+v", doc.AxiomTypes)
			break
		}
	}
	if !strings.Contains(out, `"imports": []`) {
		t.Errorf("empty imports should render as [], got:\n%s", out)
	}
}

func TestStatsJSONMatchesTextCounts(t *testing.T) {
	path := write(t, "clean.ofn", cleanDoc)
	jsonOut, _ := capture(t, func() int { return run([]string{"stats", "-json", path}) })
	textOut, _ := capture(t, func() int { return run([]string{"stats", path}) })

	doc := decode[statsDoc](t, jsonOut)
	// The two modes must agree; -json changes the format, not the content.
	for _, want := range []string{
		"classes             2",
		"axioms              5",
	} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("text output changed, fixture needs updating:\n%s", textOut)
		}
	}
	if doc.Counts.Classes != 2 || doc.Counts.Axioms != 5 {
		t.Errorf("json counts disagree with text: %+v", doc.Counts)
	}
}

func TestJSONErrorsAreDocumentsOnStdout(t *testing.T) {
	path := write(t, "bad.ofn", "Ontology(Frobnicate(<http://x/A>))")

	for _, args := range [][]string{
		{"lint", "-json", path},
		{"profile", "-json", path},
		{"stats", "-json", path},
		{"diff", "-json", path, path},
	} {
		t.Run(args[0], func(t *testing.T) {
			out, code := capture(t, func() int { return run(args) })
			if code != exitProblem {
				t.Errorf("exit = %d, want %d", code, exitProblem)
			}
			doc := decode[errorDoc](t, out)
			if !strings.Contains(doc.Error, "Frobnicate") {
				t.Errorf("error = %q, should name the offending input", doc.Error)
			}
		})
	}
}

func TestJSONBadFlagValueIsAlsoADocument(t *testing.T) {
	path := write(t, "clean.ofn", cleanDoc)
	out, code := capture(t, func() int {
		return run([]string{"lint", "-json", "-fail-on", "nonsense", path})
	})
	if code != exitProblem {
		t.Errorf("exit = %d, want %d", code, exitProblem)
	}
	if doc := decode[errorDoc](t, out); doc.Error == "" {
		t.Error("want a non-empty error message")
	}
}

func TestJSONDoesNotEscapeAngleBrackets(t *testing.T) {
	// Ontology text is full of <, > and &. Go's encoder escapes those by
	// default, which would make every full IRI unreadable.
	path := write(t, "bad.ofn", "Ontology(Frobnicate(<http://x/A>))")
	out, _ := capture(t, func() int { return run([]string{"lint", "-json", path}) })
	if strings.Contains(out, `<`) || strings.Contains(out, `>`) {
		t.Errorf("output contains HTML-escaped angle brackets:\n%s", out)
	}
}
