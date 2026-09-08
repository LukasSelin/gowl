package rdf

import (
	"strings"
	"testing"
)

func TestXMLStripedSyntax(t *testing.T) {
	g, err := ParseXML(strings.NewReader(`<?xml version="1.0"?>
	<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"
	         xmlns:rdfs="http://www.w3.org/2000/01/rdf-schema#"
	         xmlns:owl="http://www.w3.org/2002/07/owl#"
	         xml:base="http://example.org/core">
	  <rdf:Description rdf:about="#Concept">
	    <rdfs:label xml:lang="en">Concept</rdfs:label>
	    <rdf:type rdf:resource="http://www.w3.org/2002/07/owl#Class"/>
	    <rdfs:subClassOf rdf:resource="#Thing"/>
	  </rdf:Description>
	  <owl:ObjectProperty rdf:ID="broader" rdfs:label="has broader"/>
	</rdf:RDF>`), "")
	if err != nil {
		t.Fatalf("ParseXML: %v", err)
	}
	wantTriples(t, g,
		`<http://example.org/core#Concept> <http://www.w3.org/2000/01/rdf-schema#label> "Concept"@en .`,
		`<http://example.org/core#Concept> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <http://www.w3.org/2002/07/owl#Class> .`,
		`<http://example.org/core#Concept> <http://www.w3.org/2000/01/rdf-schema#subClassOf> <http://example.org/core#Thing> .`,
		`<http://example.org/core#broader> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <http://www.w3.org/2002/07/owl#ObjectProperty> .`,
		`<http://example.org/core#broader> <http://www.w3.org/2000/01/rdf-schema#label> "has broader" .`,
	)
}

func TestXMLNestedAndCollections(t *testing.T) {
	g, err := ParseXML(strings.NewReader(`<?xml version="1.0"?>
	<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"
	         xmlns:owl="http://www.w3.org/2002/07/owl#"
	         xmlns:ex="http://example.org/">
	  <owl:Class rdf:about="http://example.org/Vegetarian">
	    <owl:equivalentClass>
	      <owl:Restriction>
	        <owl:onProperty rdf:resource="http://example.org/hasTopping"/>
	      </owl:Restriction>
	    </owl:equivalentClass>
	    <owl:unionOf rdf:parseType="Collection">
	      <owl:Class rdf:about="http://example.org/A"/>
	      <owl:Class rdf:about="http://example.org/B"/>
	    </owl:unionOf>
	    <ex:meta rdf:parseType="Resource">
	      <ex:note>inline</ex:note>
	    </ex:meta>
	  </owl:Class>
	</rdf:RDF>`), "")
	if err != nil {
		t.Fatalf("ParseXML: %v", err)
	}

	veg := IRI("http://example.org/Vegetarian")
	restriction, ok := g.Object(veg, IRI(NSOWL+"equivalentClass"))
	if !ok || !g.HasType(restriction, IRI(NSOWL+"Restriction")) {
		t.Errorf("equivalentClass = %v, want an owl:Restriction", restriction)
	}

	head, _ := g.Object(veg, IRI(NSOWL+"unionOf"))
	items, ok := g.List(head)
	if !ok || len(items) != 2 {
		t.Fatalf("unionOf list = %v, ok=%v", items, ok)
	}
	if items[1] != Term(IRI("http://example.org/B")) {
		t.Errorf("second member = %v", items[1])
	}

	meta, _ := g.Object(veg, IRI("http://example.org/meta"))
	if note, ok := g.Object(meta, IRI("http://example.org/note")); !ok || note.Text() != "inline" {
		t.Errorf("parseType=Resource note = %v", note)
	}
}

func TestXMLRejectsLiteralParseType(t *testing.T) {
	_, err := ParseXML(strings.NewReader(`<?xml version="1.0"?>
	<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:ex="http://example.org/">
	  <rdf:Description rdf:about="http://example.org/a">
	    <ex:p rdf:parseType="Literal"><b>x</b></ex:p>
	  </rdf:Description>
	</rdf:RDF>`), "")
	if err == nil || !strings.Contains(err.Error(), "parseType") {
		t.Errorf("err = %v, want a parseType complaint", err)
	}
}
