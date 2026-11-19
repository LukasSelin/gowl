package el_test

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/LukasSelin/gowl/el"
	"github.com/LukasSelin/gowl/owl"
)

// assertDisjoint checks a disjointness query in both argument orders, since
// A ⊓ B ⊑ ⊥ and B ⊓ A ⊑ ⊥ say the same thing.
func assertDisjoint(t *testing.T, c *el.Classification, a, b owl.Class, want bool) {
	t.Helper()
	if got := c.AreDisjoint(a, b); got != want {
		t.Errorf("AreDisjoint(%s, %s) = %v, want %v", a, b, got, want)
	}
	if got := c.AreDisjoint(b, a); got != want {
		t.Errorf("AreDisjoint(%s, %s) = %v, want %v", b, a, got, want)
	}
}

func TestAreDisjointAsserted(t *testing.T) {
	c, cls := classify(t, `
    DisjointClasses(:Cat :Dog :Fish)
    SubClassOf(:Cat :Animal)
    SubClassOf(:Dog :Animal)`)

	assertDisjoint(t, c, cls("Cat"), cls("Dog"), true)
	assertDisjoint(t, c, cls("Dog"), cls("Fish"), true)
	assertDisjoint(t, c, cls("Cat"), cls("Fish"), true)
	// A class and its superclass overlap, and nothing keeps fish out of Animal.
	assertDisjoint(t, c, cls("Cat"), cls("Animal"), false)
	assertDisjoint(t, c, cls("Fish"), cls("Animal"), false)
}

func TestAreDisjointInheritedThroughSubclasses(t *testing.T) {
	c, cls := classify(t, `
    DisjointClasses(:Plant :Animal)
    SubClassOf(:Cat :Animal)
    SubClassOf(:Kitten :Cat)
    SubClassOf(:Oak :Plant)`)

	assertDisjoint(t, c, cls("Kitten"), cls("Oak"), true)
	assertDisjoint(t, c, cls("Kitten"), cls("Plant"), true)
	assertDisjoint(t, c, cls("Cat"), cls("Oak"), true)
	assertDisjoint(t, c, cls("Kitten"), cls("Animal"), false)
}

func TestAreDisjointUnion(t *testing.T) {
	c, cls := classify(t, `
    DisjointUnion(:Pet :Cat :Dog)
    SubClassOf(:Kitten :Cat)`)

	assertDisjoint(t, c, cls("Cat"), cls("Dog"), true)
	assertDisjoint(t, c, cls("Kitten"), cls("Dog"), true)
	assertDisjoint(t, c, cls("Cat"), cls("Pet"), false)

	// Each operand is a subclass of the union.
	assertSub(t, c, cls, "Cat", "Pet")
	assertSub(t, c, cls, "Kitten", "Pet")

	// Pet ⊑ Cat ⊔ Dog is outside EL and is not used, so the axiom must still
	// be reported: a classification that dropped it is incomplete.
	if got := c.Unsupported(); len(got) != 1 {
		t.Errorf("Unsupported = %v, want the DisjointUnion", got)
	}
}

// TestAreDisjointFollowsFromOtherAxioms covers pairs that no DisjointClasses
// names, directly or through superclasses. Closing the asserted pairs over the
// taxonomy misses every one of these; only the reasoner finds them. Each case
// reaches ⊥ through a different completion rule.
func TestAreDisjointFollowsFromOtherAxioms(t *testing.T) {
	cases := []struct{ name, body string }{
		{"stated as a GCI", `
    SubClassOf(ObjectIntersectionOf(:A :B) owl:Nothing)`},
		{"through a conjunction", `
    SubClassOf(ObjectIntersectionOf(:A :B) :C)
    DisjointClasses(:C :A)`},
		{"through an existential to an empty class", `
    SubClassOf(ObjectIntersectionOf(:A :B) ObjectSomeValuesFrom(:r :Empty))
    SubClassOf(:Empty owl:Nothing)`},
		{"through an existential on the left", `
    SubClassOf(:A ObjectSomeValuesFrom(:r :X))
    SubClassOf(ObjectIntersectionOf(:B ObjectSomeValuesFrom(:r :X)) owl:Nothing)`},
		{"through a role hierarchy", `
    SubClassOf(:A ObjectSomeValuesFrom(:r :X))
    SubObjectPropertyOf(:r :s)
    SubClassOf(ObjectIntersectionOf(:B ObjectSomeValuesFrom(:s :X)) owl:Nothing)`},
		{"through a property chain", `
    SubClassOf(:A ObjectSomeValuesFrom(:r :Y))
    SubClassOf(:Y ObjectSomeValuesFrom(:t :Z))
    SubObjectPropertyOf(ObjectPropertyChain(:r :t) :u)
    SubClassOf(ObjectIntersectionOf(:B ObjectSomeValuesFrom(:u :Z)) owl:Nothing)`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, cls := classify(t, tc.body)
			if !c.IsSatisfiable(cls("A")) || !c.IsSatisfiable(cls("B")) {
				t.Fatal("A and B must each be satisfiable, or the case proves nothing")
			}
			assertDisjoint(t, c, cls("A"), cls("B"), true)
		})
	}
}

func TestAreDisjointNotDisjoint(t *testing.T) {
	c, cls := classify(t, `
    DisjointClasses(:A :B)
    SubClassOf(:X :P)
    SubClassOf(:Y :P)
    SubClassOf(ObjectIntersectionOf(:A :X) :E)`)

	// Siblings are not disjoint unless something says so.
	assertDisjoint(t, c, cls("X"), cls("Y"), false)
	assertDisjoint(t, c, cls("A"), cls("X"), false)
	assertDisjoint(t, c, cls("B"), cls("P"), false)
	assertDisjoint(t, c, cls("A"), cls("E"), false)
	// A satisfiable class overlaps itself and owl:Thing.
	assertDisjoint(t, c, cls("A"), cls("A"), false)
	assertDisjoint(t, c, cls("A"), owl.Thing, false)
	// A class the ontology never mentions is unconstrained.
	unknown := owl.Class("http://elsewhere.example/U")
	assertDisjoint(t, c, unknown, cls("A"), false)
	assertDisjoint(t, c, unknown, unknown, false)
}

func TestAreDisjointUnsatisfiableIsDisjointFromEverything(t *testing.T) {
	c, cls := classify(t, `
    Declaration(Class(:Unrelated))
    DisjointClasses(:A :B)
    SubClassOf(:Empty :A)
    SubClassOf(:Empty :B)`)

	unknown := owl.Class("http://elsewhere.example/U")
	for _, other := range []owl.Class{cls("A"), cls("B"), cls("Unrelated"), cls("Empty"), owl.Thing, unknown} {
		assertDisjoint(t, c, cls("Empty"), other, true)
		assertDisjoint(t, c, owl.Nothing, other, true)
	}
}

func TestAreDisjointInconsistentOntology(t *testing.T) {
	c, cls := classify(t, `
    Declaration(Class(:A))
    Declaration(Class(:B))
    SubClassOf(owl:Thing owl:Nothing)`)

	assertDisjoint(t, c, cls("A"), cls("B"), true)
	assertDisjoint(t, c, owl.Thing, owl.Thing, true)
}

// TestAreDisjointMatchesReclassification checks AreDisjoint against the
// long way round: add a fresh class Q ⊑ A ⊓ B, classify again, and ask whether
// Q is satisfiable. The two must agree for every pair, on ontologies random
// enough to hit rule interactions no hand-worked case thought of.
func TestAreDisjointMatchesReclassification(t *testing.T) {
	const n = 12 // the classes randomOntology declares
	cls := func(i int) owl.Class { return owl.Class(fmt.Sprintf("http://example.org/o#C%d", i)) }
	query := func(i, j int) owl.Class { return owl.Class(fmt.Sprintf("http://example.org/o#Q%d_%d", i, j)) }

	// Pairs that are disjoint while both sides are satisfiable are the ones
	// that need the reasoner; insist the batch produces some.
	nontrivial := 0
	for seed := int64(0); seed < 40; seed++ {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			src := randomOntology(rand.New(rand.NewSource(seed)))
			base, err := owl.ParseFunctionalString(src)
			if err != nil {
				t.Fatalf("parse: %v\n%s", err, src)
			}
			extended, err := owl.ParseFunctionalString(src)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < n; i++ {
				for j := i; j < n; j++ {
					extended.Add(owl.SubClassOf{Sub: query(i, j), Super: owl.And(cls(i), cls(j))})
				}
			}

			c := el.Classify(base)
			before := fmt.Sprint(c.InferredAxioms())
			oracle := el.Classify(extended)
			for i := 0; i < n; i++ {
				for j := i; j < n; j++ {
					want := !oracle.IsSatisfiable(query(i, j))
					if got := c.AreDisjoint(cls(i), cls(j)); got != want {
						t.Errorf("AreDisjoint(C%d, C%d) = %v, but reclassifying says %v\n%s", i, j, got, want, src)
					}
					if want && c.IsSatisfiable(cls(i)) && c.IsSatisfiable(cls(j)) {
						nontrivial++
					}
				}
			}
			// Asking must not change the classification.
			if after := fmt.Sprint(c.InferredAxioms()); after != before {
				t.Error("AreDisjoint changed the classification")
			}
		})
	}
	if nontrivial == 0 {
		t.Error("no pair of satisfiable classes was disjoint across the whole batch; the check is vacuous")
	} else {
		t.Logf("checked %d disjoint pairs of satisfiable classes", nontrivial)
	}
}
