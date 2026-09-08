package rdf

import (
	"strings"
	"testing"
)

func parseTurtle(t *testing.T, src string) *Graph {
	t.Helper()
	g, err := ParseTurtle(strings.NewReader(src), "http://example.org/base")
	if err != nil {
		t.Fatalf("ParseTurtle: %v", err)
	}
	return g
}

// lines renders a graph one triple per line so that a test can state the
// expected output as text rather than as a pile of structs.
func lines(g *Graph) []string {
	out := make([]string, 0, len(g.Triples))
	for _, tr := range g.Triples {
		out = append(out, tr.String())
	}
	return out
}

func wantTriples(t *testing.T, g *Graph, want ...string) {
	t.Helper()
	got := lines(g)
	if len(got) != len(want) {
		t.Fatalf("got %d triples, want %d:\n%s", len(got), len(want), strings.Join(got, "\n"))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("triple %d:\n got %s\nwant %s", i, got[i], want[i])
		}
	}
}

func TestTurtleBasics(t *testing.T) {
	g := parseTurtle(t, `
		@prefix : <http://example.org/> .
		@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .
		PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>

		:Pizza a :Food ;
			rdfs:label "Pizza"@en , "Pizsa"@et ;
			:calories 250 ;
			:tasty true ;
			:ratio 0.5 ;
			:since "2001"^^xsd:gYear .
	`)
	wantTriples(t, g,
		`<http://example.org/Pizza> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <http://example.org/Food> .`,
		`<http://example.org/Pizza> <http://www.w3.org/2000/01/rdf-schema#label> "Pizza"@en .`,
		`<http://example.org/Pizza> <http://www.w3.org/2000/01/rdf-schema#label> "Pizsa"@et .`,
		`<http://example.org/Pizza> <http://example.org/calories> "250"^^<http://www.w3.org/2001/XMLSchema#integer> .`,
		`<http://example.org/Pizza> <http://example.org/tasty> "true"^^<http://www.w3.org/2001/XMLSchema#boolean> .`,
		`<http://example.org/Pizza> <http://example.org/ratio> "0.5"^^<http://www.w3.org/2001/XMLSchema#decimal> .`,
		`<http://example.org/Pizza> <http://example.org/since> "2001"^^<http://www.w3.org/2001/XMLSchema#gYear> .`,
	)
	if ns := g.Prefixes["rdfs"]; ns != NSRDFS {
		t.Errorf("rdfs prefix = %q, want %q", ns, NSRDFS)
	}
}

func TestTurtleBlankNodesAndCollections(t *testing.T) {
	g := parseTurtle(t, `
		@prefix : <http://example.org/> .
		@prefix owl: <http://www.w3.org/2002/07/owl#> .

		:Vegetarian owl:equivalentClass [
			a owl:Restriction ;
			owl:onProperty :hasTopping ;
			owl:allValuesFrom :Veg
		] .
		:Kinds owl:oneOf ( :a :b ) .
		_:shared :sees _:shared .
	`)

	eq, ok := g.Object(IRI("http://example.org/Vegetarian"), IRI(NSOWL+"equivalentClass"))
	if !ok {
		t.Fatal("no owl:equivalentClass")
	}
	if _, isBlank := eq.(Blank); !isBlank {
		t.Fatalf("equivalentClass object = %s, want a blank node", eq)
	}
	if !g.HasType(eq, IRI(NSOWL+"Restriction")) {
		t.Error("restriction is not typed owl:Restriction")
	}

	head, _ := g.Object(IRI("http://example.org/Kinds"), IRI(NSOWL+"oneOf"))
	items, ok := g.List(head)
	if !ok || len(items) != 2 || items[0] != Term(IRI("http://example.org/a")) {
		t.Errorf("List = %v, ok=%v", items, ok)
	}

	// A repeated label denotes the same node within one document.
	self := g.Triples[len(g.Triples)-1]
	if self.Subject != self.Object {
		t.Errorf("_:shared did not resolve to one node: %s", self)
	}
}

func TestTurtleRelativeIRIs(t *testing.T) {
	g := parseTurtle(t, `
		@base <http://example.org/ns/core> .
		<> <#p> <#o> .
		<other> <#q> "x" .
	`)
	wantTriples(t, g,
		`<http://example.org/ns/core> <http://example.org/ns/core#p> <http://example.org/ns/core#o> .`,
		`<http://example.org/ns/other> <http://example.org/ns/core#q> "x" .`,
	)
}

func TestTurtleStringForms(t *testing.T) {
	g := parseTurtle(t, `
		@prefix : <http://example.org/> .
		:a :b """line one
line "two"\u00e9""" ;
			:c 'single' ;
			:d "tab\there" .
	`)
	got := g.Objects(IRI("http://example.org/a"), IRI("http://example.org/b"))
	if len(got) != 1 || got[0].Text() != "line one\nline \"two\"é" {
		t.Errorf("long string = %q", got[0].Text())
	}
	if v := g.Objects(IRI("http://example.org/a"), IRI("http://example.org/d"))[0]; v.Text() != "tab\there" {
		t.Errorf("escape = %q", v.Text())
	}
}

func TestTurtleLocalNameEdges(t *testing.T) {
	g := parseTurtle(t, `
		@prefix : <http://example.org/> .
		:a.b :p :c\-d .
		:x :p :y.
	`)
	wantTriples(t, g,
		`<http://example.org/a.b> <http://example.org/p> <http://example.org/c-d> .`,
		`<http://example.org/x> <http://example.org/p> <http://example.org/y> .`,
	)
}

func TestTurtleErrors(t *testing.T) {
	for _, src := range []string{
		`:a :b :c .`,                        // undeclared prefix
		`@prefix : <http://e/> . :a :b .`,   // missing object
		`@prefix : <http://e/> . "s" :b 1.`, // literal subject
		`@prefix : <http://e/> . :a :b "x`,  // unterminated string
	} {
		if _, err := ParseTurtle(strings.NewReader(src), ""); err == nil {
			t.Errorf("ParseTurtle(%q) succeeded, want an error", src)
		}
	}
}
