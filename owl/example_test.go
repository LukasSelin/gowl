package owl_test

import (
	"fmt"

	"gowl/owl"
)

// Building a small ontology with the fluent DSL and rendering it as OWL 2
// Functional-Style Syntax.
func Example() {
	o := owl.New("http://example.org/wine")
	o.Prefix("", "http://example.org/wine#")

	wine := o.Class(":Wine")
	region := o.Class(":Region")
	redWine := o.Class(":RedWine")
	whiteWine := o.Class(":WhiteWine")

	from := o.ObjectProperty(":from")
	vintage := o.DataProperty(":vintage")

	o.DefineObjectProperty(from).Domain(wine).Range(region).Functional()
	o.DefineDataProperty(vintage).Domain(wine).Range(owl.XSDInteger).Functional()

	o.Define(region)
	o.Define(wine).
		SubClassOf(owl.Exactly(1, from, region)).
		DisjointUnionOf(redWine, whiteWine).
		Label("Wine")
	o.Define(redWine).SubClassOf(wine)
	o.Define(whiteWine).SubClassOf(wine)

	if err := o.Err(); err != nil {
		panic(err)
	}

	fmt.Println(o.SubClassesOf(wine))
	fmt.Println(o.Label(wine))
	fmt.Println(owl.Functional(owl.Some(from, region)))
	// Output:
	// [http://example.org/wine#RedWine http://example.org/wine#WhiteWine]
	// Wine
	// ObjectSomeValuesFrom(<http://example.org/wine#from> <http://example.org/wine#Region>)
}

// A defined class: VegetarianPizza is exactly a pizza whose toppings are all
// non-meat.
func ExampleOntology_Define() {
	o := owl.New("http://example.org/pizza")
	o.Prefix("", "http://example.org/pizza#")

	pizza := o.Class(":Pizza")
	meat := o.Class(":MeatTopping")
	hasTopping := o.ObjectProperty(":hasTopping")

	o.Define(o.Class(":VegetarianPizza")).
		EquivalentTo(owl.And(pizza, owl.Only(hasTopping, owl.Not(meat))))

	for _, ax := range o.Axioms() {
		if _, ok := ax.(owl.EquivalentClasses); ok {
			fmt.Println(ax)
		}
	}
	// Output:
	// EquivalentClasses(<http://example.org/pizza#VegetarianPizza> ObjectIntersectionOf(<http://example.org/pizza#Pizza> ObjectAllValuesFrom(<http://example.org/pizza#hasTopping> ObjectComplementOf(<http://example.org/pizza#MeatTopping>))))
}

// Walking the signature of a construct.
func ExampleSignature() {
	hasTopping := owl.ObjectProperty("http://example.org/hasTopping")
	pizza := owl.Class("http://example.org/Pizza")
	topping := owl.Class("http://example.org/Topping")

	ax := owl.SubClassOf{Sub: pizza, Super: owl.Some(hasTopping, topping)}
	for _, e := range owl.Signature(ax) {
		fmt.Printf("%s %s\n", e.Kind(), e.IRI())
	}
	// Output:
	// Class http://example.org/Pizza
	// Class http://example.org/Topping
	// ObjectProperty http://example.org/hasTopping
}

// Parsing a functional-syntax document and querying it.
func ExampleParseFunctionalString() {
	src := `Prefix(:=<http://example.org/animals#>)
Ontology(<http://example.org/animals>
    Declaration(Class(:Dog))
    SubClassOf(:Dog :Mammal)
    SubClassOf(:Mammal :Animal)
    ClassAssertion(:Dog :rex)
    AnnotationAssertion(rdfs:label :Dog "Dog"@en)
)`

	o, err := owl.ParseFunctionalString(src)
	if err != nil {
		panic(err)
	}

	dog := o.Class(":Dog")
	fmt.Println(o.Label(dog))
	fmt.Println(o.AncestorsOf(dog))
	fmt.Println(o.InstancesOf(dog))
	// Output:
	// Dog
	// [http://example.org/animals#Animal http://example.org/animals#Mammal]
	// [http://example.org/animals#rex]
}
