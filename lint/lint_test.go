package lint_test

import (
	"strings"
	"testing"

	"gowl/lint"
	"gowl/owl"
)

// parse builds an ontology from axioms with the default prefix bound.
func parse(t *testing.T, axioms ...string) *owl.Ontology {
	t.Helper()
	src := "Prefix(:=<http://example.org/o#>)\nOntology(<http://example.org/o>\n    " +
		strings.Join(axioms, "\n    ") + "\n)"
	o, err := owl.ParseFunctionalString(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return o
}

// only runs a single named rule.
func only(t *testing.T, o *owl.Ontology, name string) []lint.Finding {
	t.Helper()
	for _, r := range lint.Default() {
		if r.Name == name {
			return lint.Run(o, []lint.Rule{r})
		}
	}
	t.Fatalf("no rule named %q", name)
	return nil
}

func TestUndeclaredEntity(t *testing.T) {
	o := parse(t,
		"Declaration(Class(:A))",
		"SubClassOf(:A :B)",
	)
	f := only(t, o, "undeclared-entity")
	if len(f) != 1 {
		t.Fatalf("got %d findings, want 1: %v", len(f), f)
	}
	if f[0].Subject != "http://example.org/o#B" {
		t.Errorf("subject = %q, want B", f[0].Subject)
	}
}

func TestUndeclaredEntityIgnoresBuiltins(t *testing.T) {
	// rdfs:label and xsd:string are never declared by an ontology.
	o := parse(t,
		"Declaration(Class(:A))",
		`AnnotationAssertion(rdfs:label :A "A"^^xsd:string)`,
	)
	if f := only(t, o, "undeclared-entity"); len(f) != 0 {
		t.Errorf("built-in vocabulary should not be flagged: %v", f)
	}
}

func TestPunnedEntity(t *testing.T) {
	o := parse(t,
		"Declaration(Class(:Thing))",
		"Declaration(ObjectProperty(:Thing))",
	)
	f := only(t, o, "punned-entity")
	if len(f) != 1 {
		t.Fatalf("got %d findings, want 1: %v", len(f), f)
	}
	if !strings.Contains(f[0].Message, "Class") || !strings.Contains(f[0].Message, "ObjectProperty") {
		t.Errorf("message should name both kinds: %q", f[0].Message)
	}
}

func TestMissingLabel(t *testing.T) {
	o := parse(t,
		"Declaration(Class(:A))",
		"Declaration(Class(:B))",
		`AnnotationAssertion(rdfs:label :A "A"^^xsd:string)`,
	)
	f := only(t, o, "missing-label")
	if len(f) != 1 || f[0].Subject != "http://example.org/o#B" {
		t.Errorf("got %v, want only B flagged", f)
	}
}

func TestDeprecatedReference(t *testing.T) {
	o := parse(t,
		"Declaration(Class(:Old))",
		"Declaration(Class(:New))",
		`AnnotationAssertion(owl:deprecated :Old "true"^^xsd:boolean)`,
		`AnnotationAssertion(rdfs:comment :Old "use New instead"^^xsd:string)`,
		"SubClassOf(:New :Old)",
	)
	f := only(t, o, "deprecated-reference")
	if len(f) != 1 {
		t.Fatalf("got %d findings, want 1 (the SubClassOf): %v", len(f), f)
	}
	if !strings.Contains(f[0].Message, "SubClassOf") {
		t.Errorf("message = %q, want the SubClassOf axiom", f[0].Message)
	}
}

func TestDeprecatedReferenceAllowsSelfDocumentation(t *testing.T) {
	// Declaring and annotating a deprecated term is how it stays documented.
	o := parse(t,
		"Declaration(Class(:Old))",
		`AnnotationAssertion(owl:deprecated :Old "true"^^xsd:boolean)`,
		`AnnotationAssertion(rdfs:label :Old "Old"^^xsd:string)`,
	)
	if f := only(t, o, "deprecated-reference"); len(f) != 0 {
		t.Errorf("self-documentation should not be flagged: %v", f)
	}
}

func TestDuplicateAxiomIgnoresOperandOrder(t *testing.T) {
	o := parse(t,
		"EquivalentClasses(:A :B)",
		"EquivalentClasses(:B :A)",
	)
	f := only(t, o, "duplicate-axiom")
	if len(f) != 1 {
		t.Errorf("reordered operands are the same axiom: got %d findings", len(f))
	}
}

func TestTrivialAxiom(t *testing.T) {
	o := parse(t,
		"SubClassOf(:A :A)",
		"EquivalentClasses(:B :B)",
		"SubClassOf(:A :B)",
	)
	f := only(t, o, "trivial-axiom")
	if len(f) != 2 {
		t.Errorf("got %d findings, want 2: %v", len(f), f)
	}
}

func TestSubclassCycle(t *testing.T) {
	o := parse(t,
		"SubClassOf(:A :B)",
		"SubClassOf(:B :C)",
		"SubClassOf(:C :A)",
	)
	f := only(t, o, "subclass-cycle")
	if len(f) == 0 {
		t.Fatal("expected a cycle finding")
	}
	if f[0].Severity != lint.Error {
		t.Errorf("severity = %v, want error", f[0].Severity)
	}
	if !strings.Contains(f[0].Message, "->") {
		t.Errorf("message should show the cycle: %q", f[0].Message)
	}
}

func TestNoCycleOnDiamond(t *testing.T) {
	// A class reachable by two paths is not a cycle.
	o := parse(t,
		"SubClassOf(:A :B)",
		"SubClassOf(:A :C)",
		"SubClassOf(:B :D)",
		"SubClassOf(:C :D)",
	)
	if f := only(t, o, "subclass-cycle"); len(f) != 0 {
		t.Errorf("a diamond is not a cycle: %v", f)
	}
}

func TestOrphanClass(t *testing.T) {
	o := parse(t,
		"Declaration(Class(:Root))",
		"SubClassOf(:Child :Root)",
	)
	f := only(t, o, "orphan-class")
	if len(f) != 1 || f[0].Subject != "http://example.org/o#Root" {
		t.Errorf("got %v, want only Root flagged", f)
	}
}

func TestRunOrdersBySeverity(t *testing.T) {
	o := parse(t,
		"SubClassOf(:A :B)",
		"SubClassOf(:B :A)",
	)
	findings := lint.Run(o, lint.Default())
	if len(findings) < 2 {
		t.Fatalf("expected several findings, got %v", findings)
	}
	for i := 1; i < len(findings); i++ {
		if findings[i-1].Severity < findings[i].Severity {
			t.Errorf("findings are not ordered most-severe first: %v", findings)
			break
		}
	}
}

func TestSelectReportsUnknownRules(t *testing.T) {
	kept, unknown := lint.Select(lint.Default(), []string{"missing-label", "nope"})
	if len(unknown) != 1 || unknown[0] != "nope" {
		t.Errorf("unknown = %v, want [nope]", unknown)
	}
	for _, r := range kept {
		if r.Name == "missing-label" {
			t.Error("missing-label should have been dropped")
		}
	}
	if len(kept) != len(lint.Default())-1 {
		t.Errorf("kept %d rules, want %d", len(kept), len(lint.Default())-1)
	}
}

func TestSelectWithEmptyDisableKeepsEverything(t *testing.T) {
	// The CLI passes strings.Split("", ",") which is [""], not an empty slice.
	kept, unknown := lint.Select(lint.Default(), []string{""})
	if len(unknown) != 0 {
		t.Errorf("unknown = %v, want none", unknown)
	}
	if len(kept) != len(lint.Default()) {
		t.Errorf("kept %d rules, want all %d", len(kept), len(lint.Default()))
	}
}

func TestMaxSeverity(t *testing.T) {
	if _, any := lint.MaxSeverity(nil); any {
		t.Error("no findings should report none")
	}
	got, any := lint.MaxSeverity([]lint.Finding{
		{Severity: lint.Info},
		{Severity: lint.Error},
		{Severity: lint.Warning},
	})
	if !any || got != lint.Error {
		t.Errorf("got %v (%v), want error", got, any)
	}
}

func TestCleanOntologyHasNoErrors(t *testing.T) {
	o := parse(t,
		"Declaration(Class(:Root))",
		"Declaration(Class(:Child))",
		"Declaration(ObjectProperty(:p))",
		`AnnotationAssertion(rdfs:label :Root "Root"^^xsd:string)`,
		`AnnotationAssertion(rdfs:label :Child "Child"^^xsd:string)`,
		`AnnotationAssertion(rdfs:label :p "p"^^xsd:string)`,
		"SubClassOf(:Child :Root)",
		"ObjectPropertyDomain(:p :Root)",
		"ObjectPropertyRange(:p :Child)",
	)
	for _, f := range lint.Run(o, lint.Default()) {
		if f.Severity >= lint.Warning {
			t.Errorf("unexpected %v: %s", f.Severity, f.Message)
		}
	}
}
