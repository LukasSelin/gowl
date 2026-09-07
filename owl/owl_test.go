package owl_test

import (
	"strings"
	"testing"

	"gowl/owl"
)

// pizza builds a small ontology exercising the DSL end to end.
func pizza() (*owl.Ontology, map[string]owl.Class) {
	o := owl.New("http://example.org/pizza")
	o.Prefix("", "http://example.org/pizza#")

	food := o.Class(":Food")
	pizza := o.Class(":Pizza")
	topping := o.Class(":Topping")
	mozzarella := o.Class(":Mozzarella")
	vegetarian := o.Class(":VegetarianPizza")
	meat := o.Class(":MeatTopping")

	hasTopping := o.ObjectProperty(":hasTopping")
	hasBase := o.ObjectProperty(":hasBase")

	o.DefineObjectProperty(hasTopping).
		Domain(pizza).
		Range(topping).
		Label("has topping")
	o.DefineObjectProperty(hasBase).Functional()

	o.Define(food).Label("Food")
	o.Define(topping).SubClassOf(food)
	o.Define(mozzarella).SubClassOf(topping)
	o.Define(meat).SubClassOf(topping)
	o.Define(pizza).
		SubClassOf(food, owl.Some(hasTopping, topping), owl.Exactly(1, hasBase)).
		Label("Pizza")
	o.Define(vegetarian).
		SubClassOf(pizza).
		EquivalentTo(owl.And(pizza, owl.Only(hasTopping, owl.Not(meat))))

	return o, map[string]owl.Class{
		"food": food, "pizza": pizza, "topping": topping,
		"mozzarella": mozzarella, "vegetarian": vegetarian, "meat": meat,
	}
}

func TestBuilderProducesExpectedAxioms(t *testing.T) {
	o, c := pizza()
	if err := o.Err(); err != nil {
		t.Fatalf("build error: %v", err)
	}

	want := []string{
		"SubClassOf(<http://example.org/pizza#Pizza> <http://example.org/pizza#Food>)",
		"SubClassOf(<http://example.org/pizza#Pizza> ObjectSomeValuesFrom(<http://example.org/pizza#hasTopping> <http://example.org/pizza#Topping>))",
		"SubClassOf(<http://example.org/pizza#Pizza> ObjectExactCardinality(1 <http://example.org/pizza#hasBase>))",
	}
	got := make(map[string]bool)
	for _, ax := range o.Axioms() {
		got[owl.Functional(ax)] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing axiom:\n  %s", w)
		}
	}

	if lbl := o.Label(c["pizza"]); lbl != "Pizza" {
		t.Errorf("Label(Pizza) = %q, want %q", lbl, "Pizza")
	}
}

func TestCardinalityQualification(t *testing.T) {
	p := owl.ObjectProperty("http://example.org/p")
	c := owl.Class("http://example.org/C")

	if got, want := owl.Functional(owl.Min(2, p)), "ObjectMinCardinality(2 <http://example.org/p>)"; got != want {
		t.Errorf("unqualified: got %q, want %q", got, want)
	}
	want := "ObjectMinCardinality(2 <http://example.org/p> <http://example.org/C>)"
	if got := owl.Functional(owl.Min(2, p, c)); got != want {
		t.Errorf("qualified: got %q, want %q", got, want)
	}
}

func TestHierarchyNavigation(t *testing.T) {
	o, c := pizza()

	supers := o.SuperClassesOf(c["mozzarella"])
	if len(supers) != 1 || supers[0] != c["topping"] {
		t.Fatalf("SuperClassesOf(Mozzarella) = %v, want [Topping]", supers)
	}

	ancestors := o.AncestorsOf(c["mozzarella"])
	if len(ancestors) != 2 {
		t.Fatalf("AncestorsOf(Mozzarella) = %v, want Topping and Food", ancestors)
	}

	descendants := o.DescendantsOf(c["food"])
	if len(descendants) != 5 {
		t.Errorf("DescendantsOf(Food) = %v, want 5 classes", descendants)
	}
}

func TestSignatureAndReferences(t *testing.T) {
	o, c := pizza()

	classes := o.Classes()
	if len(classes) != 6 {
		t.Errorf("Classes() = %v, want 6", classes)
	}
	if len(o.ObjectProperties()) != 2 {
		t.Errorf("ObjectProperties() = %v, want 2", o.ObjectProperties())
	}

	// The MeatTopping class is used in the vegetarian equivalence, so it is
	// referenced by more axioms than just its own declaration.
	refs := o.AxiomsReferencing(c["meat"])
	if len(refs) < 3 {
		t.Errorf("AxiomsReferencing(MeatTopping) = %d axioms, want at least 3", len(refs))
	}
}

func TestAssertionsAndQueries(t *testing.T) {
	o := owl.New("http://example.org/pizza")
	o.Prefix("", "http://example.org/pizza#")

	pizza := o.Class(":Pizza")
	hasTopping := o.ObjectProperty(":hasTopping")
	calories := o.DataProperty(":calories")
	margherita := o.Individual(":margherita")
	mozz := o.Individual(":mozzarellaSlice")

	o.DefineIndividual(margherita).
		Type(pizza).
		Fact(hasTopping, mozz).
		Data(calories, owl.Int(800))

	if types := o.TypesOf(margherita); len(types) != 1 || !owl.Equal(types[0], pizza) {
		t.Errorf("TypesOf(margherita) = %v, want [Pizza]", types)
	}
	if got := o.InstancesOf(pizza); len(got) != 1 || got[0] != owl.Individual(margherita) {
		t.Errorf("InstancesOf(Pizza) = %v, want [margherita]", got)
	}
	if got := o.ObjectValues(margherita, hasTopping); len(got) != 1 || got[0] != owl.Individual(mozz) {
		t.Errorf("ObjectValues = %v, want [mozzarellaSlice]", got)
	}
	if got := o.DataValues(margherita, calories); len(got) != 1 || got[0].Value != "800" {
		t.Errorf("DataValues = %v, want [800]", got)
	}
}

func TestPrefixExpansionAndCompaction(t *testing.T) {
	p := owl.NewPrefixes()
	p.Set("", "http://example.org/pizza#")

	cases := []struct{ in, want string }{
		{":Pizza", "http://example.org/pizza#Pizza"},
		{"Pizza", "http://example.org/pizza#Pizza"},
		{"rdfs:label", "http://www.w3.org/2000/01/rdf-schema#label"},
		{"http://other.example/X", "http://other.example/X"},
		{"<http://other.example/Y>", "http://other.example/Y"},
	}
	for _, tc := range cases {
		got, err := p.Expand(tc.in)
		if err != nil {
			t.Errorf("Expand(%q): %v", tc.in, err)
			continue
		}
		if string(got) != tc.want {
			t.Errorf("Expand(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	if _, err := p.Expand("nope:Thing"); err == nil {
		t.Error("Expand with unknown prefix: want error, got nil")
	}

	if got, ok := p.Compact("http://example.org/pizza#Pizza"); !ok || got != ":Pizza" {
		t.Errorf("Compact = %q (%v), want \":Pizza\"", got, ok)
	}
	if _, ok := p.Compact("http://unknown.example/Thing"); ok {
		t.Error("Compact of unprefixed IRI: want false")
	}
}

func TestUnknownPrefixIsRecordedNotPanicked(t *testing.T) {
	o := owl.New("http://example.org/x")
	_ = o.Class("missing:Thing")
	if o.Err() == nil {
		t.Fatal("want an error recorded for an unknown prefix")
	}
	if !strings.Contains(o.Err().Error(), "missing") {
		t.Errorf("error %v should name the offending prefix", o.Err())
	}
}

func TestFunctionalDocument(t *testing.T) {
	o, _ := pizza()
	doc := o.Functional()

	for _, want := range []string{
		"Prefix(:=<http://example.org/pizza#>)",
		"Ontology(<http://example.org/pizza>",
		"Declaration(Class(:Pizza))",
		"SubClassOf(:Pizza ObjectSomeValuesFrom(:hasTopping :Topping))",
		"EquivalentClasses(:VegetarianPizza ObjectIntersectionOf(:Pizza ObjectAllValuesFrom(:hasTopping ObjectComplementOf(:MeatTopping))))",
		`AnnotationAssertion(rdfs:label :Pizza "Pizza"^^xsd:string)`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("document missing:\n  %s\n\ngot:\n%s", want, doc)
		}
	}
}

func TestPropertyChainAndCharacteristics(t *testing.T) {
	o := owl.New("http://example.org/family")
	o.Prefix("", "http://example.org/family#")

	hasParent := o.ObjectProperty(":hasParent")
	hasBrother := o.ObjectProperty(":hasBrother")
	hasUncle := o.ObjectProperty(":hasUncle")
	hasAncestor := o.ObjectProperty(":hasAncestor")

	o.DefineObjectProperty(hasUncle).Chain(hasParent, hasBrother)
	o.DefineObjectProperty(hasAncestor).Transitive().InverseOf(o.ObjectProperty(":hasDescendant"))

	doc := o.Functional()
	for _, want := range []string{
		"SubObjectPropertyOf(ObjectPropertyChain(:hasParent :hasBrother) :hasUncle)",
		"TransitiveObjectProperty(:hasAncestor)",
		"InverseObjectProperties(:hasAncestor :hasDescendant)",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("document missing:\n  %s\n\ngot:\n%s", want, doc)
		}
	}
}

func TestDatatypeRestriction(t *testing.T) {
	adult := owl.Restrict(owl.XSDInteger, owl.Facet(owl.FacetMinInclusive, owl.Int(18)))
	want := "DatatypeRestriction(<http://www.w3.org/2001/XMLSchema#integer> " +
		"<http://www.w3.org/2001/XMLSchema#minInclusive> " +
		`"18"^^<http://www.w3.org/2001/XMLSchema#integer>)`
	if got := owl.Functional(adult); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestEqualHandlesSliceShapedConstructs(t *testing.T) {
	a := owl.And(owl.Class("http://example.org/A"), owl.Class("http://example.org/B"))
	b := owl.And(owl.Class("http://example.org/A"), owl.Class("http://example.org/B"))
	c := owl.And(owl.Class("http://example.org/B"), owl.Class("http://example.org/A"))

	if !owl.Equal(a, b) {
		t.Error("identical intersections should be equal")
	}
	if owl.Equal(a, c) {
		t.Error("operand order is significant; these should differ")
	}
}

func TestLiteralEscaping(t *testing.T) {
	l := owl.Str(`say "hi"` + "\n")
	if got, want := owl.Functional(l), `"say \"hi\"\n"^^<http://www.w3.org/2001/XMLSchema#string>`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	if got, want := owl.Functional(owl.LangStr("Pizza", "en")), `"Pizza"@en`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
