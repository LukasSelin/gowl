package rdfowl

import (
	"slices"
	"strings"
	"testing"

	"gowl/internal/rdf"
	"gowl/owl"
)

const prologue = `
	@prefix : <http://example.org/> .
	@prefix owl: <http://www.w3.org/2002/07/owl#> .
	@prefix rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#> .
	@prefix rdfs: <http://www.w3.org/2000/01/rdf-schema#> .
	@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .
`

func convert(t *testing.T, body string, opts ...func(*Options)) *Result {
	t.Helper()
	g, err := rdf.ParseTurtle(strings.NewReader(prologue+body), "")
	if err != nil {
		t.Fatalf("ParseTurtle: %v", err)
	}
	o := Options{Namespaces: []string{"http://example.org/"}}
	for _, opt := range opts {
		opt(&o)
	}
	res, err := Convert(g, o)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	return res
}

// hasAxiom reports whether the ontology asserts the given axiom, matched
// against the ontology's own functional-syntax rendering so that expectations
// can be written with prefixed names.
func hasAxiom(o *owl.Ontology, want string) bool {
	for _, line := range strings.Split(o.Functional(), "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

func wantAxioms(t *testing.T, res *Result, want ...string) {
	t.Helper()
	for _, w := range want {
		if !hasAxiom(res.Ontology, w) {
			t.Errorf("missing axiom %s\ngot:\n%s", w, res.Ontology.Functional())
		}
	}
}

func TestConvertEntitiesAndHierarchy(t *testing.T) {
	res := convert(t, `
		<http://example.org/> a owl:Ontology ; owl:versionIRI :v1 ; owl:imports :other .
		:Pizza a owl:Class ; rdfs:label "Pizza"@en ; rdfs:subClassOf :Food .
		:Food a owl:Class .
		:hasTopping a owl:ObjectProperty , owl:TransitiveProperty ;
			rdfs:domain :Pizza ; rdfs:range :Food .
		:calories a owl:DatatypeProperty ; rdfs:domain :Pizza ; rdfs:range xsd:integer .
		:note a owl:AnnotationProperty .
	`)
	wantAxioms(t, res,
		"Declaration(Class(:Pizza))",
		"Declaration(ObjectProperty(:hasTopping))",
		"Declaration(DataProperty(:calories))",
		"Declaration(AnnotationProperty(:note))",
		"SubClassOf(:Pizza :Food)",
		"TransitiveObjectProperty(:hasTopping)",
		"ObjectPropertyDomain(:hasTopping :Pizza)",
		"ObjectPropertyRange(:hasTopping :Food)",
		"DataPropertyDomain(:calories :Pizza)",
		"DataPropertyRange(:calories xsd:integer)",
		`AnnotationAssertion(rdfs:label :Pizza "Pizza"@en)`,
	)
	if res.Ontology.VersionIRI != "http://example.org/v1" {
		t.Errorf("VersionIRI = %q", res.Ontology.VersionIRI)
	}
	if !slices.Contains(res.Ontology.Imports, owl.IRI("http://example.org/other")) {
		t.Errorf("Imports = %v", res.Ontology.Imports)
	}
	if len(res.Skipped) != 0 {
		t.Errorf("Skipped = %v", res.Skipped)
	}
}

func TestConvertClassExpressions(t *testing.T) {
	res := convert(t, `
		:hasTopping a owl:ObjectProperty .
		:age a owl:DatatypeProperty .
		:Veg a owl:Class ; owl:equivalentClass [
			a owl:Class ;
			owl:intersectionOf ( :Pizza [
				a owl:Restriction ;
				owl:onProperty :hasTopping ;
				owl:allValuesFrom [ a owl:Class ; owl:complementOf :Meat ]
			] )
		] .
		:Adult a owl:Class ; rdfs:subClassOf [
			a owl:Restriction ; owl:onProperty :age ; owl:someValuesFrom xsd:integer
		] .
		:Pair a owl:Class ; rdfs:subClassOf [
			a owl:Restriction ; owl:onProperty :hasTopping ;
			owl:minQualifiedCardinality "2"^^xsd:nonNegativeInteger ; owl:onClass :Meat
		] .
		:Choice a owl:Class ; owl:oneOf ( :a :b ) .
	`)
	wantAxioms(t, res,
		"EquivalentClasses(:Veg ObjectIntersectionOf(:Pizza ObjectAllValuesFrom(:hasTopping ObjectComplementOf(:Meat))))",
		"SubClassOf(:Adult DataSomeValuesFrom(:age xsd:integer))",
		"SubClassOf(:Pair ObjectMinCardinality(2 :hasTopping :Meat))",
		"EquivalentClasses(:Choice ObjectOneOf(:a :b))",
	)
}

func TestConvertPropertyAxioms(t *testing.T) {
	res := convert(t, `
		:parent a owl:ObjectProperty .
		:child a owl:ObjectProperty ; owl:inverseOf :parent .
		:ancestor a owl:ObjectProperty ; owl:propertyChainAxiom ( :parent :ancestor ) .
		:name a owl:DatatypeProperty , owl:FunctionalProperty ; rdfs:subPropertyOf :label .
		:label a owl:DatatypeProperty .
		:id a owl:DatatypeProperty .
		:Thing a owl:Class ; owl:hasKey ( :id ) .
	`)
	wantAxioms(t, res,
		"InverseObjectProperties(:child :parent)",
		"SubObjectPropertyOf(ObjectPropertyChain(:parent :ancestor) :ancestor)",
		"FunctionalDataProperty(:name)",
		"SubDataPropertyOf(:name :label)",
		"HasKey(:Thing () (:id))",
	)
}

// A bare rdf:Property is read from its range, and a source may nominate extra
// predicates that count as one — the case schema.org needs.
func TestConvertBarePropertyKind(t *testing.T) {
	res := convert(t, `
		:Text a rdfs:Datatype .
		:literal a rdf:Property ; rdfs:range xsd:string .
		:linked  a rdf:Property ; rdfs:range :Pizza .
		:hinted  a rdf:Property ; :rangeIncludes :Text .
		:unknown a rdf:Property .
		:Pizza a owl:Class .
	`, func(o *Options) { o.RangeHints = []rdf.IRI{"http://example.org/rangeIncludes"} })

	wantAxioms(t, res,
		"Declaration(DataProperty(:literal))",
		"Declaration(ObjectProperty(:linked))",
		"Declaration(DataProperty(:hinted))",
		"Declaration(ObjectProperty(:unknown))",
	)
}

// schema.org puts its datatypes under a root class rather than declaring them
// rdfs:Datatype, and nests them: Integer is under Number, Number under
// DataType.
func TestConvertDatatypeRoots(t *testing.T) {
	res := convert(t, `
		:DataType a rdfs:Class .
		:Number a rdfs:Class ; a :DataType .
		:Integer a rdfs:Class ; rdfs:subClassOf :Number .
		:count a rdf:Property ; rdfs:range :Integer .
	`, func(o *Options) { o.DatatypeRoots = []rdf.IRI{"http://example.org/DataType"} })

	wantAxioms(t, res,
		"Declaration(Datatype(:Integer))",
		"Declaration(DataProperty(:count))",
	)
	// Datatype subsumption has no OWL axiom and must be reported, not dropped.
	var reasons []string
	for _, s := range res.Skipped {
		reasons = append(reasons, s.Reason)
	}
	if !slices.Contains(reasons, "datatype subsumption") {
		t.Errorf("Skipped reasons = %v, want a datatype subsumption entry", reasons)
	}
}

// An unrecognised predicate is documentation, not an error: schema.org's
// domainIncludes must not become rdfs:domain.
func TestConvertUnknownPredicateBecomesAnnotation(t *testing.T) {
	res := convert(t, `
		:Pizza a owl:Class ; :domainIncludes :Food ; :status "stable" .
	`)
	wantAxioms(t, res,
		"AnnotationAssertion(:domainIncludes :Pizza :Food)",
		`AnnotationAssertion(:status :Pizza "stable"^^xsd:string)`,
	)
}

func TestConvertReportsAnonymousIndividuals(t *testing.T) {
	res := convert(t, `
		<http://example.org/> a owl:Ontology ; :editor [ :name "Ada" ] .
	`)
	if len(res.Skipped) == 0 {
		t.Fatal("an anonymous editor was dropped without a report")
	}
	for _, s := range res.Skipped {
		if !strings.Contains(s.Reason, "anonymous") {
			t.Errorf("Skipped = %v, want only anonymous-node reports", s)
		}
	}
}

// An RDF graph is a set of triples, but two of them can say one thing: an
// inverse or a disjointness written from both sides is one axiom.
func TestConvertDeduplicates(t *testing.T) {
	res := convert(t, `
		:parent a owl:ObjectProperty ; owl:inverseOf :child .
		:child a owl:ObjectProperty ; owl:inverseOf :parent .
		:Cat a owl:Class ; owl:disjointWith :Dog .
		:Dog a owl:Class ; owl:disjointWith :Cat .
	`)
	counts := map[string]int{}
	for ax := range res.Ontology.All() {
		counts[owl.CanonicalKey(ax)]++
	}
	for key, n := range counts {
		if n > 1 {
			t.Errorf("axiom asserted %d times: %s", n, key)
		}
	}
}

// A property characteristic other than functionality belongs to object
// properties. FOAF marks foaf:mbox_sha1sum both a datatype property and
// inverse functional, which is OWL Full; writing an axiom anyway would pun the
// property across two kinds.
func TestConvertRejectsCharacteristicsOnDataProperties(t *testing.T) {
	res := convert(t, `
		:hash a owl:DatatypeProperty , owl:InverseFunctionalProperty , owl:FunctionalProperty .
	`)
	wantAxioms(t, res,
		"Declaration(DataProperty(:hash))",
		"FunctionalDataProperty(:hash)", // functionality does apply to both
	)
	for ax := range res.Ontology.All() {
		owl.Walk(ax, func(e owl.Entity) {
			if e.IRI() == "http://example.org/hash" && e.Kind() != owl.KindDataProperty {
				t.Errorf("%s leaked in as %s", e.IRI(), e.Kind())
			}
		})
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Reason != "property characteristic on a data property" {
		t.Errorf("Skipped = %v", res.Skipped)
	}
}

// An axiom relating two properties has to read both ends. schema.org states
// that an object property elsewhere is equivalent to schema:description, whose
// ranges make it a data property; taking the subject's kind alone would write
// the other end down as an object property and pun the IRI across two kinds.
func TestConvertReadsBothEndsOfAPropertyAxiom(t *testing.T) {
	res := convert(t, `
		:text a owl:DatatypeProperty .
		:label a rdf:Property ; rdfs:range xsd:string .
		:untyped owl:equivalentProperty :text .
		:alias rdfs:subPropertyOf :label .
		:link a owl:ObjectProperty .
		:clash a owl:DatatypeProperty ; owl:equivalentProperty :link .
	`)
	// An untyped property takes its kind from the end that has one.
	wantAxioms(t, res,
		"EquivalentDataProperties(:untyped :text)",
		"SubDataPropertyOf(:alias :label)",
	)
	// A genuine disagreement is reported rather than resolved by force.
	var reasons []string
	for _, s := range res.Skipped {
		reasons = append(reasons, s.Reason)
	}
	if !slices.Contains(reasons, "equivalence between a data and an object property") {
		t.Errorf("Skipped reasons = %v", reasons)
	}
	// Whatever happened, no IRI may come out as two kinds of property.
	kinds := map[owl.IRI]map[owl.Kind]bool{}
	for _, e := range res.Ontology.Signature() {
		if kinds[e.IRI()] == nil {
			kinds[e.IRI()] = map[owl.Kind]bool{}
		}
		kinds[e.IRI()][e.Kind()] = true
	}
	for iri, ks := range kinds {
		if ks[owl.KindDataProperty] && ks[owl.KindObjectProperty] {
			t.Errorf("%s is both a data and an object property", iri)
		}
	}
}

// The ontology's own IRI may be stated by the caller rather than typed by the
// document, and its metadata belongs in the header either way.
func TestConvertHeaderFromStatedIRI(t *testing.T) {
	res := convert(t, `
		<http://example.org/> :title "Example terms"@en .
		:Pizza a owl:Class .
	`, func(o *Options) { o.IRI = "http://example.org/" })

	if res.Ontology.IRI != "http://example.org/" {
		t.Errorf("IRI = %q", res.Ontology.IRI)
	}
	if len(res.Ontology.Annotations) != 1 {
		t.Fatalf("Annotations = %v, want the title", res.Ontology.Annotations)
	}
	for ax := range res.Ontology.All() {
		if a, ok := owl.Unwrap(ax).(owl.AnnotationAssertion); ok && a.Subject == "http://example.org/" {
			t.Errorf("the document's own title was read as a term annotation: %s", ax)
		}
	}
}

// DCAT says "dcat:inCatalog owl:inverseOf dcat:resource" and gives
// dcat:inCatalog no rdf:type at all. The axiom settles the kind — only an
// object property has an inverse — and OWL 2 wants every entity declared.
func TestConvertDeclaresUntypedOwnTerms(t *testing.T) {
	res := convert(t, `
		:resource a owl:ObjectProperty .
		:inCatalog owl:inverseOf :resource .
		<http://elsewhere.org/thing> owl:inverseOf :resource .
	`)
	wantAxioms(t, res, "Declaration(ObjectProperty(:inCatalog))")

	for _, e := range res.Ontology.Signature() {
		if e.IRI() == "http://elsewhere.org/thing" && res.Ontology.IsDeclared(e) {
			t.Error("a term from another namespace was declared")
		}
	}
}

func TestConvertDeclaresOnlyOwnedTerms(t *testing.T) {
	res := convert(t, `
		:Pizza a owl:Class ; rdfs:subClassOf <http://elsewhere.org/Food> .
	`)
	for _, e := range res.Ontology.Signature() {
		if e.IRI() == "http://elsewhere.org/Food" && res.Ontology.IsDeclared(e) {
			t.Error("a term from another namespace was declared")
		}
	}
	if !hasAxiom(res.Ontology, "SubClassOf(:Pizza <http://elsewhere.org/Food>)") {
		t.Error("the foreign superclass should still be referenced")
	}
}
