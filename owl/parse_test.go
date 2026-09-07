package owl_test

import (
	"strings"
	"testing"

	"gowl/owl"
)

func TestRoundTripDocument(t *testing.T) {
	original, _ := pizza()
	rendered := original.Functional()

	parsed, err := owl.ParseFunctionalString(rendered)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if got := parsed.Functional(); got != rendered {
		t.Errorf("round trip changed the document\n--- want ---\n%s\n--- got ---\n%s", rendered, got)
	}
	if parsed.Len() != original.Len() {
		t.Errorf("axiom count: got %d, want %d", parsed.Len(), original.Len())
	}
	if parsed.IRI != original.IRI {
		t.Errorf("ontology IRI: got %q, want %q", parsed.IRI, original.IRI)
	}
}

func TestRoundTripPreservesQueries(t *testing.T) {
	original, c := pizza()
	parsed, err := owl.ParseFunctionalString(original.Functional())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if got, want := len(parsed.Classes()), len(original.Classes()); got != want {
		t.Errorf("Classes(): got %d, want %d", got, want)
	}
	if got := parsed.Label(c["pizza"]); got != "Pizza" {
		t.Errorf("Label(Pizza) after round trip = %q, want %q", got, "Pizza")
	}
	if got := parsed.AncestorsOf(c["mozzarella"]); len(got) != 2 {
		t.Errorf("AncestorsOf(Mozzarella) after round trip = %v, want 2", got)
	}
}

func TestParseHeaderAndComments(t *testing.T) {
	src := `
# a leading comment
Prefix(:=<http://example.org/o#>)
Prefix(ex:=<http://other.example/>)

Ontology(<http://example.org/o> <http://example.org/o/1.0>
    Import(<http://example.org/base>)
    Annotation(rdfs:comment "an ontology") # trailing comment
    Declaration(Class(:A))
    SubClassOf(:A ex:B)
)
`
	o, err := owl.ParseFunctionalString(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if o.IRI != "http://example.org/o" {
		t.Errorf("IRI = %q", o.IRI)
	}
	if o.VersionIRI != "http://example.org/o/1.0" {
		t.Errorf("VersionIRI = %q", o.VersionIRI)
	}
	if len(o.Imports) != 1 || o.Imports[0] != "http://example.org/base" {
		t.Errorf("Imports = %v", o.Imports)
	}
	if len(o.Annotations) != 1 || o.Annotations[0].Property != owl.RDFSComment {
		t.Errorf("Annotations = %v", o.Annotations)
	}
	if o.Len() != 2 {
		t.Fatalf("axioms = %d, want 2", o.Len())
	}
	want := "SubClassOf(<http://example.org/o#A> <http://other.example/B>)"
	if got := owl.Functional(o.Axioms()[1]); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestParseAxiomAnnotations(t *testing.T) {
	src := `SubClassOf(Annotation(rdfs:comment "because") ` +
		`<http://example.org/A> <http://example.org/B>)`

	ax, err := owl.ParseAxiom(src, nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	a, ok := ax.(owl.Annotated)
	if !ok {
		t.Fatalf("got %T, want owl.Annotated", ax)
	}
	if len(a.Annotations) != 1 || a.Annotations[0].Property != owl.RDFSComment {
		t.Errorf("annotations = %v", a.Annotations)
	}
	if _, ok := owl.Unwrap(ax).(owl.SubClassOf); !ok {
		t.Errorf("Unwrap gave %T, want owl.SubClassOf", owl.Unwrap(ax))
	}
	// Functional() renders with full IRIs and always makes the datatype
	// explicit, so the annotation comes back expanded rather than byte-identical.
	want := `SubClassOf(Annotation(<http://www.w3.org/2000/01/rdf-schema#comment> ` +
		`"because"^^<http://www.w3.org/2001/XMLSchema#string>) ` +
		`<http://example.org/A> <http://example.org/B>)`
	if got := owl.Functional(ax); got != want {
		t.Errorf("re-render:\n got %s\nwant %s", got, want)
	}
}

func TestAnnotatedAxiomsStayVisibleToQueries(t *testing.T) {
	src := `Prefix(:=<http://example.org/o#>)
Ontology(
    SubClassOf(Annotation(rdfs:comment "why") :Dog :Animal)
    ClassAssertion(Annotation(rdfs:comment "why") :Dog :rex)
)`
	o, err := owl.ParseFunctionalString(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	dog := o.Class(":Dog")
	animal := o.Class(":Animal")
	if got := o.SuperClassesOf(dog); len(got) != 1 || got[0] != animal {
		t.Errorf("SuperClassesOf(Dog) = %v, want [Animal]", got)
	}
	if got := o.InstancesOf(dog); len(got) != 1 {
		t.Errorf("InstancesOf(Dog) = %v, want 1", got)
	}
}

func TestParseExpressionForms(t *testing.T) {
	p := owl.NewPrefixes()
	p.Set("", "http://example.org/o#")

	// Each of these should survive a parse and re-render unchanged.
	for _, src := range []string{
		"ObjectIntersectionOf(:A ObjectSomeValuesFrom(:p :B))",
		"ObjectUnionOf(:A ObjectComplementOf(:B))",
		"ObjectAllValuesFrom(ObjectInverseOf(:p) :B)",
		"ObjectMinCardinality(2 :p)",
		"ObjectExactCardinality(1 :p :B)",
		"ObjectHasValue(:p :bob)",
		"ObjectHasSelf(:p)",
		"ObjectOneOf(:a :b)",
		"DataSomeValuesFrom(:age DatatypeRestriction(xsd:integer xsd:minInclusive \"18\"^^xsd:integer))",
		"DataHasValue(:name \"bob\"^^xsd:string)",
		"DataAllValuesFrom(:tag DataOneOf(\"a\"^^xsd:string \"b\"^^xsd:string))",
		"DataMaxCardinality(3 :tag)",
	} {
		ce, err := owl.ParseClassExpression(src, p)
		if err != nil {
			t.Errorf("parse %s: %v", src, err)
			continue
		}
		got := renderWith(t, p, ce)
		if got != src {
			t.Errorf("re-render:\n got %s\nwant %s", got, src)
		}
	}
}

// renderWith renders a construct using the given prefixes, by routing it
// through a throwaway ontology.
func renderWith(t *testing.T, p *owl.Prefixes, ce owl.ClassExpression) string {
	t.Helper()
	o := &owl.Ontology{Prefixes: p}
	o.Add(owl.SubClassOf{Sub: owl.Thing, Super: ce})
	doc := o.Functional()
	const prefix = "SubClassOf(owl:Thing "
	for _, line := range strings.Split(doc, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSuffix(strings.TrimPrefix(line, prefix), ")")
		}
	}
	t.Fatalf("could not find the rendered axiom in:\n%s", doc)
	return ""
}

func TestParseAxiomForms(t *testing.T) {
	p := owl.NewPrefixes()
	p.Set("", "http://example.org/o#")

	for _, src := range []string{
		"Declaration(NamedIndividual(:bob))",
		"EquivalentClasses(:A :B)",
		"DisjointClasses(:A :B :C)",
		"DisjointUnion(:A :B :C)",
		"SubObjectPropertyOf(ObjectPropertyChain(:p :q) :r)",
		"SubObjectPropertyOf(:p ObjectInverseOf(:q))",
		"InverseObjectProperties(:p :q)",
		"TransitiveObjectProperty(:p)",
		"HasKey(:A (:p) (:d))",
		"SameIndividual(:a :b)",
		"DifferentIndividuals(:a _:x)",
		"ObjectPropertyAssertion(:p :a :b)",
		"NegativeDataPropertyAssertion(:d :a \"1\"^^xsd:integer)",
		"DataPropertyRange(:d DataUnionOf(xsd:string xsd:integer))",
		"DatatypeDefinition(:MyType DataComplementOf(xsd:string))",
		"AnnotationAssertion(rdfs:label :A \"A\"@en)",
		"SubAnnotationPropertyOf(:note rdfs:comment)",
		"AnnotationPropertyRange(:note xsd:string)",
	} {
		ax, err := owl.ParseAxiom(src, p)
		if err != nil {
			t.Errorf("parse %s: %v", src, err)
			continue
		}
		o := &owl.Ontology{Prefixes: p}
		o.Add(ax)
		if !strings.Contains(o.Functional(), src) {
			t.Errorf("re-render lost %s:\n%s", src, o.Functional())
		}
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"missing ontology", "Prefix(:=<http://x/>)", "expected Ontology"},
		{"unknown prefix", "Ontology(SubClassOf(nope:A nope:B))", `unknown prefix "nope"`},
		{"unknown axiom", "Ontology(Frobnicate(<http://x/A>))", "unknown axiom Frobnicate"},
		{"unterminated iri", "Ontology(SubClassOf(<http://x/A <http://x/B>))", "malformed IRI"},
		{"unterminated string", `Ontology(AnnotationAssertion(rdfs:label <http://x/A> "oops))`, "unterminated string"},
		{"truncated", "Ontology(SubClassOf(<http://x/A>", "expected"},
		{"not a class expression", "Ontology(SubClassOf(TransitiveObjectProperty(<http://x/p>) <http://x/B>))", "not a class expression"},
		{"n-ary data restriction", "Ontology(SubClassOf(<http://x/A> DataSomeValuesFrom(<http://x/p> <http://x/q> xsd:string)))", "n-ary"},
		{"anonymous annotation subject", `Ontology(AnnotationAssertion(rdfs:label _:x "hi"))`, "anonymous individuals are not supported"},
		{"trailing junk", "Ontology()Ontology()", "expected end of input"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o, err := owl.ParseFunctionalString(tc.src)
			if err == nil {
				t.Fatalf("want an error, got ontology:\n%s", o.Functional())
			}
			if o != nil {
				t.Errorf("want a nil ontology alongside the error, got %v", o)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q should mention %q", err, tc.want)
			}
			if !strings.HasPrefix(err.Error(), "owl: line ") {
				t.Errorf("error %q should carry a line number", err)
			}
		})
	}
}

func TestParseErrorReportsCorrectLine(t *testing.T) {
	src := "Ontology(\n    Declaration(Class(<http://x/A>))\n    Frobnicate(<http://x/A>)\n)"
	_, err := owl.ParseFunctionalString(src)
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.HasPrefix(err.Error(), "owl: line 3:") {
		t.Errorf("got %q, want the error on line 3", err)
	}
}

func TestParseLiteralForms(t *testing.T) {
	p := owl.NewPrefixes()
	p.Set("", "http://example.org/o#")

	ax, err := owl.ParseAxiom(`AnnotationAssertion(rdfs:comment :A "line\none \"quoted\"")`, p)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	a := ax.(owl.AnnotationAssertion)
	lit, ok := a.Value.(owl.Literal)
	if !ok {
		t.Fatalf("value is %T, want owl.Literal", a.Value)
	}
	if want := "line\none \"quoted\""; lit.Value != want {
		t.Errorf("value = %q, want %q", lit.Value, want)
	}
	// A bare string with no datatype is treated as xsd:string.
	if lit.Datatype != owl.XSDString {
		t.Errorf("datatype = %q, want xsd:string", lit.Datatype)
	}
}

func TestParseEmptyOntology(t *testing.T) {
	o, err := owl.ParseFunctionalString("Ontology()")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if o.IRI != "" || o.Len() != 0 {
		t.Errorf("want an anonymous empty ontology, got %q with %d axioms", o.IRI, o.Len())
	}
}

// FuzzParseFunctional checks that malformed input always produces an error
// rather than a panic: the recover in the parser only converts *parseError, so
// any other panic would escape to the caller.
func FuzzParseFunctional(f *testing.F) {
	f.Add("Ontology()")
	f.Add("Prefix(:=<http://x/>)\nOntology(<http://x/> Declaration(Class(:A)) SubClassOf(:A :B))")
	f.Add(`Ontology(AnnotationAssertion(rdfs:label <http://x/A> "hi"@en))`)
	f.Add("Ontology(SubClassOf(<http://x/A> ObjectMinCardinality(2 <http://x/p>)))")

	f.Fuzz(func(t *testing.T, src string) {
		o, err := owl.ParseFunctionalString(src)
		if err != nil {
			return
		}
		// Anything that parses must render, and the rendering must parse back.
		doc := o.Functional()
		if _, err := owl.ParseFunctionalString(doc); err != nil {
			t.Fatalf("re-parsing our own output failed: %v\n%s", err, doc)
		}
	})
}
