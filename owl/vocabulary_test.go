package owl_test

import (
	"errors"
	"strings"
	"testing"

	"gowl/owl"
)

// vocabDoc has a labelled class, an unlabelled one, a commented property and a
// term that no prefix can compact.
const vocabDoc = `Prefix(:=<http://example.org/pizza#>)
Ontology(<http://example.org/pizza>
    Declaration(Class(:Pizza))
    Declaration(Class(:Topping))
    Declaration(Class(:Unlabelled))
    Declaration(ObjectProperty(:hasTopping))
    Declaration(DataProperty(:calories))
    Declaration(NamedIndividual(:margherita))
    AnnotationAssertion(rdfs:label :Pizza "Pizza"@en)
    AnnotationAssertion(rdfs:comment :Pizza "A pizza with
    at least one topping.")
    AnnotationAssertion(rdfs:label :Topping "Topping"@en)
    AnnotationAssertion(rdfs:label :hasTopping "has topping"@en)
    AnnotationAssertion(rdfs:label :calories "calories"@en)
    AnnotationAssertion(rdfs:label :margherita "Margherita"@en)
    SubClassOf(:Pizza ObjectSomeValuesFrom(:hasTopping :Topping))
    SubClassOf(:Pizza <http://other.example/Food>)
    ClassAssertion(:Pizza :margherita)
)
`

func vocab(t *testing.T, opts ...owl.VocabularyOption) *owl.Vocabulary {
	t.Helper()
	o, err := owl.ParseFunctionalString(vocabDoc)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return owl.NewVocabulary(o, opts...)
}

func TestVocabularyExcludesBuiltinsByDefault(t *testing.T) {
	v := vocab(t)
	for _, term := range v.Terms() {
		if owl.IsBuiltin(term.Entity) {
			t.Errorf("builtin %s should not be listed by default", term.Entity.IRI())
		}
	}
	// rdfs:label is used by the document but is a builtin.
	if v.Contains(owl.RDFSLabel) {
		t.Error("rdfs:label should not be a member by default")
	}

	withBuiltins := vocab(t, owl.WithBuiltins())
	if !withBuiltins.Contains(owl.RDFSLabel) {
		t.Error("WithBuiltins should include rdfs:label")
	}
	if withBuiltins.Len() <= v.Len() {
		t.Errorf("WithBuiltins should add terms: %d vs %d", withBuiltins.Len(), v.Len())
	}
}

func TestVocabularyCollectsNamesLabelsAndComments(t *testing.T) {
	v := vocab(t)

	term, ok := v.Lookup(":Pizza")
	if !ok {
		t.Fatal("Pizza is missing")
	}
	if term.Name != ":Pizza" {
		t.Errorf("name = %q, want the compact form", term.Name)
	}
	if term.Label != "Pizza" {
		t.Errorf("label = %q", term.Label)
	}
	if !strings.HasPrefix(term.Comment, "A pizza with") {
		t.Errorf("comment = %q", term.Comment)
	}

	// An IRI outside every declared namespace keeps its full form.
	food, ok := v.Lookup("http://other.example/Food")
	if !ok {
		t.Fatal("Food is missing")
	}
	if food.Name != "http://other.example/Food" {
		t.Errorf("name = %q, want the full IRI", food.Name)
	}
}

func TestVocabularyLookupForms(t *testing.T) {
	v := vocab(t)
	for _, name := range []string{
		":Pizza",
		"http://example.org/pizza#Pizza",
		"<http://example.org/pizza#Pizza>",
		"Pizza",   // the rdfs:label
		"  pizza", // labels match without regard to case or surrounding space
	} {
		term, ok := v.Lookup(name)
		if !ok {
			t.Errorf("Lookup(%q) failed", name)
			continue
		}
		if term.Entity.IRI() != "http://example.org/pizza#Pizza" {
			t.Errorf("Lookup(%q) = %s", name, term.Entity.IRI())
		}
	}

	if _, ok := v.Lookup(":Nonexistent"); ok {
		t.Error("an unknown name should not resolve")
	}
	if _, ok := v.Lookup(""); ok {
		t.Error("an empty name should not resolve")
	}
}

func TestVocabularyResolveErrors(t *testing.T) {
	v := vocab(t)

	if _, err := v.Resolve(":Nope"); err == nil {
		t.Error("want an error for an unknown term")
	}

	// A near miss should point at the real term rather than just failing.
	_, err := v.Resolve("has_topping")
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), ":hasTopping") {
		t.Errorf("error %q should suggest :hasTopping", err)
	}
}

func TestVocabularyReportsPunningAsAmbiguous(t *testing.T) {
	// The same IRI used as a class and as an individual.
	o, err := owl.ParseFunctionalString(`Prefix(:=<http://example.org/o#>)
Ontology(
    Declaration(Class(:Thing))
    Declaration(NamedIndividual(:Thing))
)`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	v := owl.NewVocabulary(o)

	if _, ok := v.Lookup(":Thing"); ok {
		t.Error("a punned name should not resolve to one term")
	}
	_, err = v.Resolve(":Thing")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("error = %v, want an ambiguity report", err)
	}
	// The kind disambiguates.
	term, ok := v.LookupKind(":Thing", owl.KindClass)
	if !ok || term.Entity.Kind() != owl.KindClass {
		t.Errorf("LookupKind = %+v, %v", term, ok)
	}
}

func TestVocabularyValidate(t *testing.T) {
	v := vocab(t)
	pizza := owl.Class("http://example.org/pizza#Pizza")
	topping := owl.Class("http://example.org/pizza#Topping")
	alien := owl.Class("http://elsewhere.example/Alien")

	if err := v.Validate(owl.SubClassOf{Sub: pizza, Super: topping}); err != nil {
		t.Errorf("in-vocabulary axiom rejected: %v", err)
	}

	// Builtins are always allowed, listed or not.
	if err := v.Validate(owl.SubClassOf{Sub: pizza, Super: owl.Thing}); err != nil {
		t.Errorf("owl:Thing should always be allowed: %v", err)
	}

	err := v.Validate(owl.SubClassOf{Sub: pizza, Super: alien})
	if err == nil {
		t.Fatal("want an error for an outside term")
	}
	var unknown *owl.UnknownTermsError
	if !errors.As(err, &unknown) {
		t.Fatalf("error is %T, want *owl.UnknownTermsError", err)
	}
	if len(unknown.Terms) != 1 || unknown.Terms[0].IRI() != alien.IRI() {
		t.Errorf("unknown terms = %v", unknown.Terms)
	}
}

func TestVocabularyParseAxiomGatesUnknownTerms(t *testing.T) {
	v := vocab(t)

	ax, err := v.ParseAxiom("SubClassOf(:Topping :Pizza)")
	if err != nil {
		t.Fatalf("valid axiom rejected: %v", err)
	}
	if _, ok := ax.(owl.SubClassOf); !ok {
		t.Errorf("got %T", ax)
	}

	if _, err := v.ParseAxiom("SubClassOf(:Topping :Invented)"); err == nil {
		t.Error("an axiom naming an undefined term should be rejected")
	}
	// A syntax error still surfaces as a parse error, with its line number.
	_, err = v.ParseAxiom("SubClassOf(:Topping")
	if err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Errorf("error = %v, want a parse error", err)
	}
}

func TestVocabularyParseClassExpression(t *testing.T) {
	v := vocab(t)

	if _, err := v.ParseClassExpression("ObjectSomeValuesFrom(:hasTopping :Topping)"); err != nil {
		t.Errorf("valid expression rejected: %v", err)
	}
	if _, err := v.ParseClassExpression("ObjectSomeValuesFrom(:hasTopping :Nope)"); err == nil {
		t.Error("want rejection for an undefined filler")
	}
}

func TestVocabularyOptions(t *testing.T) {
	// Food is referenced but never declared.
	all := vocab(t)
	if _, ok := all.Lookup("http://other.example/Food"); !ok {
		t.Fatal("Food should be present by default")
	}
	declared := vocab(t, owl.DeclaredOnly())
	if _, ok := declared.Lookup("http://other.example/Food"); ok {
		t.Error("DeclaredOnly should drop an undeclared entity")
	}

	classesOnly := vocab(t, owl.OnlyKinds(owl.KindClass))
	if len(classesOnly.TermsOfKind(owl.KindObjectProperty)) != 0 {
		t.Error("OnlyKinds(KindClass) should drop object properties")
	}
	if len(classesOnly.TermsOfKind(owl.KindClass)) == 0 {
		t.Error("OnlyKinds(KindClass) should keep classes")
	}
}

func TestVocabularyPrompt(t *testing.T) {
	got := vocab(t).Prompt()

	for _, want := range []string{
		"Prefixes\n  : http://example.org/pizza#",
		"Classes (4)",
		"  :Pizza — Pizza — A pizza with at least one topping.",
		"  :Unlabelled\n",
		"Object properties (1)\n  :hasTopping — has topping",
		"Individuals (1)\n  :margherita — Margherita",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt is missing:\n  %s\n\ngot:\n%s", want, got)
		}
	}

	// A multi-line comment must not break the one-term-per-line shape.
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "    ") {
			t.Errorf("a wrapped comment leaked onto its own line: %q", line)
		}
	}
	// Unused namespaces are not declared: nothing here is written with xsd:.
	if strings.Contains(got, "xsd:") {
		t.Errorf("prompt declares a prefix it never uses:\n%s", got)
	}
}

func TestVocabularyPromptEmpty(t *testing.T) {
	o, err := owl.ParseFunctionalString("Ontology()")
	if err != nil {
		t.Fatal(err)
	}
	if got := owl.NewVocabulary(o).Prompt(); got != "" {
		t.Errorf("an empty vocabulary should render as empty, got %q", got)
	}
}

func TestIsBuiltin(t *testing.T) {
	for _, e := range []owl.Entity{owl.Thing, owl.RDFSLabel, owl.XSDString, owl.OWLDeprecated} {
		if !owl.IsBuiltin(e) {
			t.Errorf("%s should be builtin", e.IRI())
		}
	}
	for _, e := range []owl.Entity{
		owl.Class("http://example.org/o#A"),
		// The namespace IRI itself is not an entity in it.
		owl.Class(owl.NamespaceOWL),
	} {
		if owl.IsBuiltin(e) {
			t.Errorf("%s should not be builtin", e.IRI())
		}
	}
}
