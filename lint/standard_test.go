package lint

import (
	"strings"
	"testing"

	"gowl/owl"
)

// reference stands in for a published vocabulary. The rule takes any
// *owl.Vocabulary, so a test does not need the real thing.
func reference(t *testing.T) *owl.Vocabulary {
	t.Helper()
	o := owl.New("http://example.org/std")
	o.Prefix("std", "http://example.org/std#")

	person := o.Class("std:Person")
	o.Define(person).Label("Person")
	agent := o.Class("std:Agent")
	o.Define(agent).Label("Agent")

	knows := o.ObjectProperty("std:knows")
	o.DefineObjectProperty(knows)
	o.Add(owl.AnnotationAssertion{
		Property: owl.RDFSLabel, Subject: knows.IRI(), Value: owl.Str("knows"),
	})
	family := o.DataProperty("std:familyName")
	o.Declare(family)
	o.Add(owl.AnnotationAssertion{
		Property: owl.RDFSLabel, Subject: family.IRI(), Value: owl.Str("family name"),
	})

	if err := o.Err(); err != nil {
		t.Fatalf("building the reference: %v", err)
	}
	return owl.NewVocabulary(o)
}

// local builds an ontology of the caller's own terms.
func local(t *testing.T, build func(*owl.Ontology)) *owl.Ontology {
	t.Helper()
	o := owl.New("http://example.org/app")
	o.Prefix("", "http://example.org/app#")
	o.Prefix("std", "http://example.org/std#")
	build(o)
	if err := o.Err(); err != nil {
		t.Fatalf("building the ontology: %v", err)
	}
	return o
}

func messages(findings []Finding) []string {
	out := make([]string, len(findings))
	for i, f := range findings {
		out[i] = f.Message
	}
	return out
}

func TestPreferStandardTermsMatchesNamesAndLabels(t *testing.T) {
	o := local(t, func(o *owl.Ontology) {
		// Same local name as std:Person.
		o.Define(o.Class(":Person"))
		// Different name, but the label gives it away.
		o.Define(o.Class(":Human")).Label("Agent")
		// A property matching on name, with the underscore folded away.
		o.Declare(o.DataProperty(":family_name"))
	})

	got := messages(Run(o, []Rule{PreferStandardTerms(reference(t))}))
	want := []string{
		"class :Human is labelled \"Agent\", which matches std:Agent; consider using it instead",
		"class :Person has the same name as std:Person; consider using it instead",
		"dataproperty :family_name has the same name as std:familyName; consider using it instead",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d findings, want %d:\n%s", len(got), len(want), strings.Join(got, "\n"))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("finding %d:\n got %s\nwant %s", i, got[i], want[i])
		}
	}
}

func TestPreferStandardTermsStaysQuiet(t *testing.T) {
	rule := PreferStandardTerms(reference(t))

	for _, tc := range []struct {
		name  string
		build func(*owl.Ontology)
	}{
		{
			"a standard term used as itself",
			func(o *owl.Ontology) { o.Define(o.Class(":Employee")).SubClassOf(o.Class("std:Person")) },
		},
		{
			"an alignment already stated",
			func(o *owl.Ontology) {
				o.Define(o.Class(":Person")).EquivalentTo(o.Class("std:Person"))
			},
		},
		{
			"a name nothing standard shares",
			func(o *owl.Ontology) { o.Define(o.Class(":Invoice")).Label("Invoice") },
		},
		{
			"a match across entity kinds",
			// :knows is a class here, std:knows an object property.
			func(o *owl.Ontology) { o.Define(o.Class(":knows")) },
		},
		{
			"a name too short to mean anything",
			func(o *owl.Ontology) { o.Declare(o.DataProperty(":id")) },
		},
		{
			"a term the ontology only references",
			// Undeclared entities belong to somebody else; that is
			// undeclared-entity's business, not this rule's.
			func(o *owl.Ontology) {
				o.Add(owl.SubClassOf{Sub: o.Class(":Person"), Super: o.Class(":Party")})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := messages(Run(local(t, tc.build), []Rule{rule})); len(got) > 0 {
				t.Errorf("expected no findings, got:\n%s", strings.Join(got, "\n"))
			}
		})
	}
}

// Reference order is the caller's way of saying which vocabulary it would
// rather reuse, and the result must not depend on map iteration.
func TestPreferStandardTermsIsDeterministic(t *testing.T) {
	other := owl.New("http://example.org/alt")
	other.Prefix("alt", "http://example.org/alt#")
	other.Define(other.Class("alt:Person")).Label("Person")

	o := local(t, func(o *owl.Ontology) { o.Define(o.Class(":Person")) })

	first := PreferStandardTerms(reference(t), owl.NewVocabulary(other))
	second := PreferStandardTerms(owl.NewVocabulary(other), reference(t))

	for i := 0; i < 8; i++ {
		if got := messages(Run(o, []Rule{first})); len(got) != 1 || !strings.Contains(got[0], "std:Person") {
			t.Fatalf("first reference should win, got %v", got)
		}
		if got := messages(Run(o, []Rule{second})); len(got) != 1 || !strings.Contains(got[0], "alt:Person") {
			t.Fatalf("reordering the references should change the suggestion, got %v", got)
		}
	}
}

func TestPreferStandardTermsIsNotDefault(t *testing.T) {
	for _, r := range Default() {
		if r.Name == "prefer-standard-term" {
			t.Error("prefer-standard-term needs reference vocabularies and cannot be a default rule")
		}
	}
}
