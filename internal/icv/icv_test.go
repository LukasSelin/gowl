package icv

import (
	"slices"
	"strings"
	"testing"

	"gowl/owl"
)

const prefixes = `Prefix(:=<https://example.com/m#>)
Prefix(rdfs:=<http://www.w3.org/2000/01/rdf-schema#>)
Prefix(owl:=<http://www.w3.org/2002/07/owl#>)
`

// A small module: a imports b imports c imports os, and a imports fmt. a and b
// are the core; os does I/O.
const facts = `
	ClassAssertion(:Package :a) ClassAssertion(:Package :b) ClassAssertion(:Package :c)
	ClassAssertion(:External :os) ClassAssertion(:External :fmt)
	ObjectPropertyAssertion(:imports :a :b)
	ObjectPropertyAssertion(:imports :b :c)
	ObjectPropertyAssertion(:imports :c :os)
	ObjectPropertyAssertion(:imports :a :fmt)
	ClassAssertion(:Core :a) ClassAssertion(:Core :b)
	ClassAssertion(:Effectful :os)
`

// dependsOn is imports, followed any distance.
const dependsOn = `
	SubObjectPropertyOf(:imports :dependsOn)
	TransitiveObjectProperty(:dependsOn)
`

func parse(t *testing.T, axioms string) *owl.Ontology {
	t.Helper()
	o, err := owl.ParseFunctionalString(prefixes + "Ontology(<https://example.com/m/x>\n" + axioms + "\n)\n")
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func validate(t *testing.T, constraints string) Report {
	t.Helper()
	return Validate(parse(t, facts), parse(t, constraints))
}

// subjects lists the local names of the violations' subjects.
func subjects(vs []Violation) []string {
	var out []string
	for _, v := range vs {
		_, local, _ := strings.Cut(string(v.Subject), "#")
		out = append(out, local)
	}
	return out
}

// chain renders a path as "a b c os": its first subject, then each object.
func chain(path []owl.ObjectPropertyAssertion) string {
	if len(path) == 0 {
		return ""
	}
	local := func(i owl.Individual) string {
		_, l, _ := strings.Cut(string(i.(owl.NamedIndividual)), "#")
		return l
	}
	out := []string{local(path[0].Subject)}
	for _, f := range path {
		out = append(out, local(f.Object))
	}
	return strings.Join(out, " ")
}

func TestAllValuesFrom(t *testing.T) {
	r := validate(t, "SubClassOf(:Core ObjectAllValuesFrom(:imports ObjectUnionOf(:Core :External)))")
	if got := subjects(r.Violations); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("subjects = %v, want b, which imports c\n%v", got, r.Violations)
	}
	v := r.Violations[0]
	if chain(v.Path) != "b c" {
		t.Errorf("path = %q, want b c", chain(v.Path))
	}
	if !strings.Contains(v.Reason, ":b :imports :c, which is not") {
		t.Errorf("reason = %q", v.Reason)
	}
	if r.Checked != 1 || len(r.Unsupported) != 0 {
		t.Errorf("checked %d, unsupported %v", r.Checked, r.Unsupported)
	}
}

// A derived, transitive property finds what is reached at any distance, and
// says how.
func TestReachedThroughATransitiveProperty(t *testing.T) {
	r := validate(t, dependsOn+"DisjointClasses(:Core ObjectSomeValuesFrom(:dependsOn :Effectful))")
	if got := subjects(r.Violations); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("subjects = %v, want a and b", got)
	}
	if got := chain(r.Violations[0].Path); got != "a b c os" {
		t.Errorf("path for a = %q, want a b c os", got)
	}
	if got := chain(r.Violations[1].Path); got != "b c os" {
		t.Errorf("path for b = %q, want b c os", got)
	}
	// The path is made of what the data asserts, not of the derived property.
	for _, f := range r.Violations[0].Path {
		if !strings.HasSuffix(string(f.Property.(owl.ObjectProperty)), "#imports") {
			t.Errorf("path holds %v, want only asserted :imports facts", f)
		}
	}
	if !strings.Contains(r.Violations[0].Reason, "(through :a →:imports :b →:imports :c →:imports :os)") {
		t.Errorf("reason = %q", r.Violations[0].Reason)
	}
}

func TestNegativeAssertion(t *testing.T) {
	r := validate(t, dependsOn+`
		NegativeObjectPropertyAssertion(:dependsOn :a :os)
		NegativeObjectPropertyAssertion(:imports :a :c)
		NegativeObjectPropertyAssertion(:imports :c :a)
	`)
	if got := subjects(r.Violations); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("subjects = %v, want a alone: a reaches os, and imports c only through b", got)
	}
	if got := chain(r.Violations[0].Path); got != "a b c os" {
		t.Errorf("path = %q", got)
	}
	if r.Checked != 3 {
		t.Errorf("checked = %d, want 3", r.Checked)
	}
}

// An inverse looks at who points at an individual, and the path still runs the
// way the facts do.
func TestInverse(t *testing.T) {
	r := validate(t, `
		SubClassOf(ObjectOneOf(:c) ObjectAllValuesFrom(ObjectInverseOf(:imports) :Core))
		SubClassOf(ObjectOneOf(:b) ObjectAllValuesFrom(ObjectInverseOf(:imports) :External))
	`)
	if got := subjects(r.Violations); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("subjects = %v, want b: a imports it, and a is not external", got)
	}
	if got := chain(r.Violations[0].Path); got != "a b" {
		t.Errorf("path = %q, want a b", got)
	}
}

func TestHasValueAndComplement(t *testing.T) {
	r := validate(t, "SubClassOf(:Core ObjectComplementOf(ObjectHasValue(:imports :fmt)))")
	if got := subjects(r.Violations); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("subjects = %v, want a, which imports fmt", got)
	}
	if got := chain(r.Violations[0].Path); got != "a fmt" {
		t.Errorf("path = %q", got)
	}
}

// The comment on a rule travels with its violations: it is the why.
func TestViolationKeepsTheAxiomAsWritten(t *testing.T) {
	r := validate(t, `SubClassOf(Annotation(rdfs:comment "the core stays small") :Core ObjectAllValuesFrom(:imports :Core))`)
	if len(r.Violations) == 0 {
		t.Fatal("no violations")
	}
	if _, ok := r.Violations[0].Axiom.(owl.Annotated); !ok {
		t.Errorf("axiom = %v, want it with its annotation", r.Violations[0].Axiom)
	}
}

// What is outside the fragment is listed, with a reason, and not checked.
func TestRefusals(t *testing.T) {
	for _, c := range []struct{ axiom, reason string }{
		{"TransitiveObjectProperty(:imports)", "which the data states"},
		{"SubObjectPropertyOf(:dependsOn :imports)", "which the data states"},
		{"EquivalentClasses(:Core ObjectOneOf(:a :b))", "reads like a definition"},
		{"ClassAssertion(:Core :c)", "facts belong in the data"},
		{"SubClassOf(owl:Thing :Package)", "not anchored"},
		{"SubClassOf(ObjectComplementOf(:Core) :External)", "not anchored"},
		{"SubClassOf(ObjectAllValuesFrom(:imports :Core) :Core)", "not anchored"},
		{"DisjointClasses(:Core ObjectComplementOf(:Package))", "not anchored"},
		{"SubClassOf(:Core ObjectMinCardinality(2 :imports))", "outside the fragment"},
		{"ObjectPropertyDomain(:imports :Package)", "outside the fragment"},
		{"SubObjectPropertyOf(ObjectPropertyChain(:imports :imports) :dependsOn)", "outside the fragment"},
	} {
		r := validate(t, c.axiom)
		if len(r.Unsupported) != 1 || r.Checked != 0 || len(r.Violations) != 0 {
			t.Errorf("%s: unsupported %v, checked %d, violations %v", c.axiom, r.Unsupported, r.Checked, r.Violations)
			continue
		}
		u := r.Unsupported[0]
		if u.Subject != "" || u.Path != nil || !strings.Contains(u.Reason, c.reason) {
			t.Errorf("%s: refusal %+v, want a reason saying %q", c.axiom, u, c.reason)
		}
	}
}

// Declarations and annotations are not checks, and are not refused either.
func TestNonLogicalAxiomsAreQuiet(t *testing.T) {
	r := validate(t, `Declaration(Class(:Core)) AnnotationAssertion(rdfs:comment :Core "the core")`)
	if r.Checked != 0 || len(r.Unsupported) != 0 || len(r.Violations) != 0 {
		t.Errorf("report = %+v", r)
	}
}

// A misspelled group or property makes a rule hold for nothing; Empty says so.
func TestEmptyTerms(t *testing.T) {
	r := validate(t, "SubClassOf(:Cor ObjectAllValuesFrom(:imprts :Core))")
	var got []string
	for _, e := range r.Empty {
		got = append(got, string(e.IRI()))
	}
	want := []string{"https://example.com/m#Cor", "https://example.com/m#imprts"}
	if !slices.Equal(got, want) {
		t.Errorf("empty = %v, want %v", got, want)
	}
	if r := validate(t, "SubClassOf(:Core ObjectAllValuesFrom(:imports owl:Thing))"); len(r.Empty) != 0 {
		t.Errorf("empty = %v, want none", r.Empty)
	}
	// A property a rule defines is no misspelling, even when it relates
	// nothing: here nothing is :uses, so nothing :reaches anything.
	if r := validate(t, "SubObjectPropertyOf(:uses :reaches) DisjointClasses(:Core ObjectSomeValuesFrom(:reaches :Core))"); len(r.Empty) != 1 || r.Empty[0].IRI() != "https://example.com/m#uses" {
		t.Errorf("empty = %v, want :uses alone", r.Empty)
	}
}

func TestDuplicateConstraintsCountOnce(t *testing.T) {
	r := validate(t, `
		SubClassOf(:Core ObjectAllValuesFrom(:imports :Core))
		SubClassOf(:Core ObjectAllValuesFrom(:imports :Core))
	`)
	if r.Checked != 1 {
		t.Errorf("checked = %d, want 1", r.Checked)
	}
}

// A term the data declares is defined, even with nothing in it.
func TestDeclaredTermsAreNotEmpty(t *testing.T) {
	data := parse(t, facts+"Declaration(Class(:Vendor)) Declaration(ObjectProperty(:embeds))")
	r := Validate(data, parse(t, "SubObjectPropertyOf(:embeds :reaches) DisjointClasses(:Vendor ObjectSomeValuesFrom(:reaches :Core))"))
	if len(r.Empty) != 0 {
		t.Errorf("empty = %v, want none: the data declares both", r.Empty)
	}
}
