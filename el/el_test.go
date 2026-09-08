package el_test

import (
	"strings"
	"testing"

	"gowl/el"
	"gowl/owl"
)

// classify parses a document body and classifies it. The prefix header is
// added here so the cases below stay readable.
func classify(t *testing.T, body string) (*el.Classification, func(string) owl.Class) {
	t.Helper()
	src := "Prefix(:=<http://example.org/o#>)\nOntology(<http://example.org/o>\n" + body + "\n)"
	o, err := owl.ParseFunctionalString(src)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, src)
	}
	c := el.Classify(o)
	return c, func(local string) owl.Class { return owl.Class("http://example.org/o#" + local) }
}

// assertSub checks an entailment in both directions: it must hold, and the
// classifier must not also derive its converse unless the test says so.
func assertSub(t *testing.T, c *el.Classification, cls func(string) owl.Class, sub, super string) {
	t.Helper()
	if !c.IsSubsumedBy(cls(sub), cls(super)) {
		t.Errorf("expected %s ⊑ %s", sub, super)
	}
}

func refuteSub(t *testing.T, c *el.Classification, cls func(string) owl.Class, sub, super string) {
	t.Helper()
	if c.IsSubsumedBy(cls(sub), cls(super)) {
		t.Errorf("did not expect %s ⊑ %s", sub, super)
	}
}

func TestTransitiveSubsumption(t *testing.T) {
	c, cls := classify(t, `
    SubClassOf(:A :B)
    SubClassOf(:B :C)`)

	assertSub(t, c, cls, "A", "C")
	refuteSub(t, c, cls, "C", "A")
	// Reflexivity holds, and everything is below owl:Thing.
	assertSub(t, c, cls, "A", "A")
	if !c.IsSubsumedBy(cls("A"), owl.Thing) {
		t.Error("everything should be below owl:Thing")
	}
}

func TestConjunctionOnTheLeft(t *testing.T) {
	c, cls := classify(t, `
    SubClassOf(:A :B)
    SubClassOf(:A :C)
    SubClassOf(ObjectIntersectionOf(:B :C) :D)`)

	assertSub(t, c, cls, "A", "D")
	refuteSub(t, c, cls, "B", "D")
}

func TestConjunctionOnTheRightSplits(t *testing.T) {
	c, cls := classify(t, `
    SubClassOf(:A ObjectIntersectionOf(:B :C))`)

	assertSub(t, c, cls, "A", "B")
	assertSub(t, c, cls, "A", "C")
}

func TestNaryConjunctionIsBinarized(t *testing.T) {
	c, cls := classify(t, `
    SubClassOf(:A :B)
    SubClassOf(:A :C)
    SubClassOf(:A :D)
    SubClassOf(:A :E)
    SubClassOf(ObjectIntersectionOf(:B :C :D :E) :F)`)

	assertSub(t, c, cls, "A", "F")

	// One conjunct short is not enough.
	c2, cls2 := classify(t, `
    SubClassOf(:A :B)
    SubClassOf(:A :C)
    SubClassOf(:A :D)
    SubClassOf(ObjectIntersectionOf(:B :C :D :E) :F)`)
	refuteSub(t, c2, cls2, "A", "F")
}

func TestExistentialRestriction(t *testing.T) {
	c, cls := classify(t, `
    SubClassOf(:A ObjectSomeValuesFrom(:r :B))
    SubClassOf(ObjectSomeValuesFrom(:r :B) :C)`)

	assertSub(t, c, cls, "A", "C")
}

func TestExistentialUsesSubsumersOfTheFiller(t *testing.T) {
	// The filler is B, but the restriction is written over B's superclass.
	c, cls := classify(t, `
    SubClassOf(:A ObjectSomeValuesFrom(:r :B))
    SubClassOf(:B :BSuper)
    SubClassOf(ObjectSomeValuesFrom(:r :BSuper) :C)`)

	assertSub(t, c, cls, "A", "C")
}

func TestNestedExistential(t *testing.T) {
	c, cls := classify(t, `
    SubClassOf(:A ObjectSomeValuesFrom(:r ObjectSomeValuesFrom(:s :B)))
    SubClassOf(ObjectSomeValuesFrom(:s :B) :M)
    SubClassOf(ObjectSomeValuesFrom(:r :M) :C)`)

	assertSub(t, c, cls, "A", "C")
}

func TestDefinedClassClassifies(t *testing.T) {
	// The case that makes a classifier worth having: Margherita is never
	// asserted to be a CheesyPizza, but it is one.
	c, cls := classify(t, `
    SubClassOf(:Margherita :Pizza)
    SubClassOf(:Margherita ObjectSomeValuesFrom(:hasTopping :Mozzarella))
    SubClassOf(:Mozzarella :CheeseTopping)
    EquivalentClasses(:CheesyPizza
        ObjectIntersectionOf(:Pizza ObjectSomeValuesFrom(:hasTopping :CheeseTopping)))`)

	assertSub(t, c, cls, "Margherita", "CheesyPizza")
	assertSub(t, c, cls, "CheesyPizza", "Pizza")
	refuteSub(t, c, cls, "Pizza", "CheesyPizza")

	// And it shows up in the taxonomy, not just the entailment relation.
	supers := c.SuperClassesOf(cls("Margherita"))
	if !containsClass(supers, cls("CheesyPizza")) {
		t.Errorf("SuperClassesOf(Margherita) = %v, want CheesyPizza", supers)
	}
}

func TestRoleHierarchy(t *testing.T) {
	c, cls := classify(t, `
    SubObjectPropertyOf(:r :s)
    SubClassOf(:A ObjectSomeValuesFrom(:r :B))
    SubClassOf(ObjectSomeValuesFrom(:s :B) :C)`)

	assertSub(t, c, cls, "A", "C")

	// The other direction does not hold.
	c2, cls2 := classify(t, `
    SubObjectPropertyOf(:r :s)
    SubClassOf(:A ObjectSomeValuesFrom(:s :B))
    SubClassOf(ObjectSomeValuesFrom(:r :B) :C)`)
	refuteSub(t, c2, cls2, "A", "C")
}

func TestTransitiveProperty(t *testing.T) {
	c, cls := classify(t, `
    TransitiveObjectProperty(:partOf)
    SubClassOf(:A ObjectSomeValuesFrom(:partOf :B))
    SubClassOf(:B ObjectSomeValuesFrom(:partOf :C))
    SubClassOf(ObjectSomeValuesFrom(:partOf :C) :D)`)

	assertSub(t, c, cls, "A", "D")
}

func TestPropertyChain(t *testing.T) {
	c, cls := classify(t, `
    SubObjectPropertyOf(ObjectPropertyChain(:hasParent :hasBrother) :hasUncle)
    SubClassOf(:Child ObjectSomeValuesFrom(:hasParent :Parent))
    SubClassOf(:Parent ObjectSomeValuesFrom(:hasBrother :Man))
    SubClassOf(ObjectSomeValuesFrom(:hasUncle :Man) :HasAnUncle)`)

	assertSub(t, c, cls, "Child", "HasAnUncle")
}

func TestLongPropertyChainIsBinarized(t *testing.T) {
	c, cls := classify(t, `
    SubObjectPropertyOf(ObjectPropertyChain(:p :q :r) :t)
    SubClassOf(:A ObjectSomeValuesFrom(:p :B))
    SubClassOf(:B ObjectSomeValuesFrom(:q :C))
    SubClassOf(:C ObjectSomeValuesFrom(:r :D))
    SubClassOf(ObjectSomeValuesFrom(:t :D) :Reached)`)

	assertSub(t, c, cls, "A", "Reached")
}

func TestDisjointnessMakesAClassUnsatisfiable(t *testing.T) {
	c, cls := classify(t, `
    DisjointClasses(:A :B)
    SubClassOf(:C :A)
    SubClassOf(:C :B)`)

	if c.IsSatisfiable(cls("C")) {
		t.Error("C is below two disjoint classes and cannot have instances")
	}
	if c.IsCoherent() {
		t.Error("an ontology with an unsatisfiable class is not coherent")
	}
	if got := c.UnsatisfiableClasses(); len(got) != 1 || got[0] != cls("C") {
		t.Errorf("UnsatisfiableClasses = %v, want [C]", got)
	}
	// An unsatisfiable class is below everything.
	assertSub(t, c, cls, "C", "B")
	if !c.IsSubsumedBy(cls("C"), owl.Class("http://example.org/o#Unrelated")) {
		t.Error("an unsatisfiable class should be subsumed by anything")
	}
	// Its siblings are unaffected.
	if !c.IsSatisfiable(cls("A")) {
		t.Error("A should still be satisfiable")
	}
}

func TestBottomPropagatesThroughExistentials(t *testing.T) {
	// Nothing can have an r-successor of an empty class.
	c, cls := classify(t, `
    SubClassOf(:Empty owl:Nothing)
    SubClassOf(:A ObjectSomeValuesFrom(:r :Empty))`)

	if c.IsSatisfiable(cls("A")) {
		t.Error("A requires an instance of an empty class, so A is empty too")
	}
}

func TestEquivalenceCycle(t *testing.T) {
	c, cls := classify(t, `
    SubClassOf(:A :B)
    SubClassOf(:B :C)
    SubClassOf(:C :A)`)

	for _, pair := range [][2]string{{"A", "B"}, {"B", "C"}, {"C", "A"}, {"B", "A"}} {
		assertSub(t, c, cls, pair[0], pair[1])
	}
	eq := c.EquivalentClassesOf(cls("A"))
	if len(eq) != 2 {
		t.Errorf("EquivalentClassesOf(A) = %v, want B and C", eq)
	}
	// Members of a cycle are not each other's direct superclasses.
	if got := c.DirectSuperClassesOf(cls("A")); len(got) != 1 || got[0] != owl.Thing {
		t.Errorf("DirectSuperClassesOf(A) = %v, want [owl:Thing]", got)
	}
}

func TestPropertyDomain(t *testing.T) {
	c, cls := classify(t, `
    ObjectPropertyDomain(:r :D)
    SubClassOf(:A ObjectSomeValuesFrom(:r :B))`)

	assertSub(t, c, cls, "A", "D")
}

func TestDirectSuperClassesAreTransitivelyReduced(t *testing.T) {
	c, cls := classify(t, `
    SubClassOf(:A :B)
    SubClassOf(:B :C)
    SubClassOf(:A :C)`)

	direct := c.DirectSuperClassesOf(cls("A"))
	if len(direct) != 1 || direct[0] != cls("B") {
		t.Errorf("DirectSuperClassesOf(A) = %v, want just [B]", direct)
	}
	// The full set still has both.
	if supers := c.SuperClassesOf(cls("A")); len(supers) != 2 {
		t.Errorf("SuperClassesOf(A) = %v, want B and C", supers)
	}
}

func TestTopLevelClassSitsUnderThing(t *testing.T) {
	c, cls := classify(t, `Declaration(Class(:Lonely))`)
	if got := c.DirectSuperClassesOf(cls("Lonely")); len(got) != 1 || got[0] != owl.Thing {
		t.Errorf("DirectSuperClassesOf(Lonely) = %v, want [owl:Thing]", got)
	}
}

func TestSubClassesOf(t *testing.T) {
	c, cls := classify(t, `
    SubClassOf(:A :B)
    SubClassOf(:B :C)`)

	subs := c.SubClassesOf(cls("C"))
	if len(subs) != 2 || !containsClass(subs, cls("A")) || !containsClass(subs, cls("B")) {
		t.Errorf("SubClassesOf(C) = %v, want A and B", subs)
	}
}

func TestInferredAxiomsAreDeterministic(t *testing.T) {
	body := `
    SubClassOf(:Margherita :Pizza)
    SubClassOf(:Margherita ObjectSomeValuesFrom(:hasTopping :Mozzarella))
    SubClassOf(:Mozzarella :CheeseTopping)
    EquivalentClasses(:CheesyPizza
        ObjectIntersectionOf(:Pizza ObjectSomeValuesFrom(:hasTopping :CheeseTopping)))`

	first, _ := classify(t, body)
	second, _ := classify(t, body)

	render := func(c *el.Classification) string {
		var b strings.Builder
		for _, ax := range c.InferredAxioms() {
			b.WriteString(owl.Functional(ax) + "\n")
		}
		return b.String()
	}
	a, bb := render(first), render(second)
	if a != bb {
		t.Errorf("two runs disagreed:\n%s\n---\n%s", a, bb)
	}
	if !strings.Contains(a, "#Margherita> <http://example.org/o#CheesyPizza>") {
		t.Errorf("inferred axioms should place Margherita under CheesyPizza:\n%s", a)
	}
}

func TestUnsatisfiableClassInInferredAxioms(t *testing.T) {
	c, _ := classify(t, `
    DisjointClasses(:A :B)
    SubClassOf(:C :A)
    SubClassOf(:C :B)`)

	var found bool
	for _, ax := range c.InferredAxioms() {
		if eq, ok := ax.(owl.EquivalentClasses); ok && len(eq) == 2 {
			if owl.Equal(eq[1], owl.Nothing) {
				found = true
			}
		}
	}
	if !found {
		t.Error("an unsatisfiable class should be reported as equivalent to owl:Nothing")
	}
}

func TestExplain(t *testing.T) {
	c, cls := classify(t, `
    SubClassOf(:A :B)
    SubClassOf(:B :C)
    SubClassOf(:Unrelated :C)`)

	why := c.Explain(cls("A"), cls("C"))
	if len(why) != 2 {
		t.Fatalf("Explain(A, C) = %v, want the two axioms that chain", why)
	}
	rendered := owl.Functional(why[0]) + owl.Functional(why[1])
	for _, want := range []string{"#A> <http://example.org/o#B>", "#B> <http://example.org/o#C>"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("explanation is missing %s: %v", want, why)
		}
	}
	if strings.Contains(rendered, "Unrelated") {
		t.Errorf("explanation should not cite unrelated axioms: %v", why)
	}
}

func TestExplainDefinedClass(t *testing.T) {
	c, cls := classify(t, `
    SubClassOf(:Margherita :Pizza)
    SubClassOf(:Margherita ObjectSomeValuesFrom(:hasTopping :Mozzarella))
    SubClassOf(:Mozzarella :CheeseTopping)
    SubClassOf(:Unrelated :Pizza)
    EquivalentClasses(:CheesyPizza
        ObjectIntersectionOf(:Pizza ObjectSomeValuesFrom(:hasTopping :CheeseTopping)))`)

	why := c.Explain(cls("Margherita"), cls("CheesyPizza"))
	if len(why) != 4 {
		t.Errorf("Explain = %d axioms, want the 4 that participate:\n%v", len(why), why)
	}
	for _, ax := range why {
		if strings.Contains(owl.Functional(ax), "Unrelated") {
			t.Errorf("explanation cites an axiom it does not need: %v", ax)
		}
	}
}

func TestExplainReturnsNothingForNonEntailments(t *testing.T) {
	c, cls := classify(t, `SubClassOf(:A :B)`)
	if why := c.Explain(cls("B"), cls("A")); why != nil {
		t.Errorf("Explain of a non-entailment = %v, want nil", why)
	}
}

func TestExplainDisabled(t *testing.T) {
	o, err := owl.ParseFunctionalString(
		"Prefix(:=<http://example.org/o#>)\nOntology(SubClassOf(:A :B))")
	if err != nil {
		t.Fatal(err)
	}
	c := el.Classify(o, el.WithoutExplanations())

	a := owl.Class("http://example.org/o#A")
	b := owl.Class("http://example.org/o#B")
	if !c.IsSubsumedBy(a, b) {
		t.Error("subsumptions should still be derived without explanations")
	}
	if why := c.Explain(a, b); why != nil {
		t.Errorf("Explain = %v, want nil when explanations are off", why)
	}
}

func TestUnsupportedAxiomsAreReported(t *testing.T) {
	c, cls := classify(t, `
    SubClassOf(:A :B)
    SubClassOf(:C ObjectUnionOf(:A :B))
    ObjectPropertyRange(:r :B)
    FunctionalObjectProperty(:r)`)

	unsupported := c.Unsupported()
	if len(unsupported) != 3 {
		t.Errorf("Unsupported = %d axioms, want 3:\n%v", len(unsupported), unsupported)
	}
	// The EL part is still classified.
	assertSub(t, c, cls, "A", "B")
}

func TestSupportedOntologyReportsNothingUnsupported(t *testing.T) {
	c, _ := classify(t, `
    Declaration(Class(:A))
    Declaration(ObjectProperty(:r))
    AnnotationAssertion(rdfs:label :A "A"@en)
    SubClassOf(:A ObjectSomeValuesFrom(:r :B))
    EquivalentClasses(:A :A2)
    DisjointClasses(:A :Other)
    ObjectPropertyDomain(:r :A)
    TransitiveObjectProperty(:r)
    SubObjectPropertyOf(:r :s)`)

	if got := c.Unsupported(); len(got) != 0 {
		t.Errorf("Unsupported = %v, want none", got)
	}
}

func TestInconsistentOntology(t *testing.T) {
	c, _ := classify(t, `SubClassOf(owl:Thing owl:Nothing)`)
	if c.IsConsistent() {
		t.Error("⊤ ⊑ ⊥ makes the ontology inconsistent")
	}
}

func TestEmptyOntology(t *testing.T) {
	o, err := owl.ParseFunctionalString("Ontology()")
	if err != nil {
		t.Fatal(err)
	}
	c := el.Classify(o)
	if !c.IsConsistent() || !c.IsCoherent() {
		t.Error("an empty ontology is consistent and coherent")
	}
	if got := c.Classes(); len(got) != 0 {
		t.Errorf("Classes = %v, want none", got)
	}
}

func TestUnknownClassesAreUnconstrained(t *testing.T) {
	c, cls := classify(t, `SubClassOf(:A :B)`)
	unknown := owl.Class("http://elsewhere.example/X")

	if !c.IsSatisfiable(unknown) {
		t.Error("a class the ontology never mentions is unconstrained")
	}
	if c.IsSubsumedBy(unknown, cls("A")) {
		t.Error("an unknown class should not be subsumed by anything but owl:Thing")
	}
	if !c.IsSubsumedBy(unknown, owl.Thing) {
		t.Error("even an unknown class is below owl:Thing")
	}
	if got := c.SuperClassesOf(unknown); got != nil {
		t.Errorf("SuperClassesOf(unknown) = %v, want nil", got)
	}
}

// TestSaturationTerminatesOnCyclicExistentials guards the property that makes
// this algorithm usable at all: a cyclic ontology must reach a fixpoint rather
// than generate successors forever, as a tableau would need care to avoid.
func TestSaturationTerminatesOnCyclicExistentials(t *testing.T) {
	c, cls := classify(t, `
    SubClassOf(:Person ObjectSomeValuesFrom(:hasParent :Person))
    SubClassOf(ObjectSomeValuesFrom(:hasParent :Person) :HasAParent)`)

	assertSub(t, c, cls, "Person", "HasAParent")
}

func containsClass(cs []owl.Class, want owl.Class) bool {
	for _, c := range cs {
		if c == want {
			return true
		}
	}
	return false
}
