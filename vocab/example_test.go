package vocab_test

import (
	"fmt"

	"gowl/owl"
	"gowl/vocab"
	"gowl/vocab/dcterms"
	"gowl/vocab/foaf"
	"gowl/vocab/prov"
	"gowl/vocab/skos"
)

// Build on the standard vocabularies instead of inventing terms for things
// they already name. The constants are typed, so putting skos:broader where a
// class belongs does not compile.
func Example() {
	o := owl.New("http://example.org/catalogue")
	o.Prefix("", "http://example.org/catalogue#")
	o.Prefix(skos.Prefix, skos.Namespace)
	o.Prefix(dcterms.Prefix, dcterms.Namespace)

	topic := o.Class(":Topic")
	o.Define(topic).SubClassOf(skos.Concept).Label("Topic")
	o.Add(
		owl.SubObjectPropertyOf{Sub: o.ObjectProperty(":broaderTopic"), Super: skos.Broader},
		owl.DataPropertyDomain{Property: dcterms.Title, Domain: topic},
	)

	fmt.Println(o.Functional())
	// Output:
	// Prefix(:=<http://example.org/catalogue#>)
	// Prefix(dcterms:=<http://purl.org/dc/terms/>)
	// Prefix(owl:=<http://www.w3.org/2002/07/owl#>)
	// Prefix(rdf:=<http://www.w3.org/1999/02/22-rdf-syntax-ns#>)
	// Prefix(rdfs:=<http://www.w3.org/2000/01/rdf-schema#>)
	// Prefix(skos:=<http://www.w3.org/2004/02/skos/core#>)
	// Prefix(xsd:=<http://www.w3.org/2001/XMLSchema#>)
	//
	// Ontology(<http://example.org/catalogue>
	//     Declaration(Class(:Topic))
	//     SubClassOf(:Topic skos:Concept)
	//     AnnotationAssertion(rdfs:label :Topic "Topic"^^xsd:string)
	//     SubObjectPropertyOf(:broaderTopic skos:broader)
	//     DataPropertyDomain(dcterms:title :Topic)
	// )
}

// A vocabulary is a closed set, which is what to check an axiom against when
// it arrives from somewhere you do not control.
func ExampleEntry_Vocabulary() {
	v := skos.Vocabulary()

	ax, err := v.ParseAxiom("SubClassOf(skos:OrderedCollection skos:Collection)")
	fmt.Println(ax, err)

	_, err = v.ParseAxiom("SubClassOf(skos:Concept <http://example.org/Invented>)")
	fmt.Println(err)
	// Output:
	// SubClassOf(<http://www.w3.org/2004/02/skos/core#OrderedCollection> <http://www.w3.org/2004/02/skos/core#Collection>) <nil>
	// owl: not in the vocabulary: Class http://example.org/Invented
}

// Given an IRI from somewhere else, find out which standard vocabulary defines
// it and what it means.
func ExampleOwner() {
	iri := owl.IRI("http://www.w3.org/ns/prov#wasDerivedFrom")

	entry, ok := vocab.Owner(iri)
	if !ok {
		fmt.Println("unknown IRI")
		return
	}
	term, _ := entry.Vocabulary().Lookup(string(iri))
	fmt.Printf("%s (%s): %s\n", entry.Title, entry.Prefix, term.Label)
	// Output:
	// PROV-O provenance ontology (prov): wasDerivedFrom
}

// Terms keep their kinds across vocabularies, so an axiom may mix them freely.
func ExamplePrefixes() {
	ax, err := owl.ParseAxiom("SubClassOf(foaf:Person prov:Agent)", vocab.Prefixes())
	if err != nil {
		panic(err)
	}
	fmt.Println(owl.Equal(ax, owl.SubClassOf{Sub: foaf.Person, Super: prov.Agent}))
	// Output:
	// true
}
