package el_test

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gowl/el"
	"gowl/owl"
)

// The tests in this file check properties that must hold for every ontology,
// rather than the answer to one hand-worked case. A classifier that passes the
// worked cases can still be wrong; these are the checks that would catch it.

// checkInvariants asserts the properties of a subsumption relation that hold
// no matter what the ontology says.
func checkInvariants(t *testing.T, o *owl.Ontology, c *el.Classification) {
	t.Helper()
	classes := c.Classes()

	for _, a := range classes {
		// Reflexivity.
		if !c.IsSubsumedBy(a, a) {
			t.Errorf("%s is not subsumed by itself", a)
		}
		// Everything is below owl:Thing.
		if !c.IsSubsumedBy(a, owl.Thing) {
			t.Errorf("%s is not below owl:Thing", a)
		}
		// Transitivity.
		for _, b := range c.SuperClassesOf(a) {
			for _, cc := range c.SuperClassesOf(b) {
				if !c.IsSubsumedBy(a, cc) {
					t.Errorf("transitivity broken: %s ⊑ %s ⊑ %s but not %s ⊑ %s", a, b, cc, a, cc)
				}
			}
		}
		// SuperClassesOf and SubClassesOf must agree.
		for _, b := range c.SuperClassesOf(a) {
			if !containsClass(c.SubClassesOf(b), a) {
				t.Errorf("%s ⊑ %s, but %s is not among the subclasses of %s", a, b, a, b)
			}
		}
		// Direct superclasses are a subset of all superclasses, except for
		// owl:Thing which stands in when nothing else is above.
		for _, b := range c.DirectSuperClassesOf(a) {
			if b == owl.Thing {
				continue
			}
			if !c.IsSubsumedBy(a, b) {
				t.Errorf("%s is a direct superclass of %s but not a superclass", b, a)
			}
		}
	}

	// Every asserted subsumption between named classes must be entailed:
	// a classifier that loses an asserted axiom is broken.
	if len(c.Unsupported()) == 0 {
		for ax := range o.All() {
			sub, ok := owl.Unwrap(ax).(owl.SubClassOf)
			if !ok {
				continue
			}
			a, aok := sub.Sub.(owl.Class)
			b, bok := sub.Super.(owl.Class)
			if aok && bok && !c.IsSubsumedBy(a, b) {
				t.Errorf("asserted %s ⊑ %s is not entailed", a, b)
			}
		}
	}

	// Every explanation must actually entail what it explains: reclassifying
	// an ontology of just those axioms must reproduce the subsumption.
	for _, a := range classes {
		supers := c.SuperClassesOf(a)
		if len(supers) == 0 {
			continue
		}
		b := supers[0]
		why := c.Explain(a, b)
		if len(why) == 0 {
			continue
		}
		sub := owl.New(o.IRI)
		sub.Prefixes = o.Prefixes
		sub.Add(why...)
		if !el.Classify(sub).IsSubsumedBy(a, b) {
			t.Errorf("explanation for %s ⊑ %s does not entail it:\n%v", a, b, why)
		}
	}
}

func TestInvariantsOnCorpus(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "owl", "testdata", "*.ofn"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no corpus fixtures found: %v", err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			o, err := owl.ParseFunctionalString(string(data))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			checkInvariants(t, o, el.Classify(o))
		})
	}
}

func TestInvariantsOnRandomOntologies(t *testing.T) {
	// Invariants would hold vacuously over a classifier that derived nothing,
	// so count the subsumptions that were inferred rather than asserted and
	// insist the batch as a whole produces some.
	inferred := 0
	for seed := int64(0); seed < 40; seed++ {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			src := randomOntology(rand.New(rand.NewSource(seed)))
			o, err := owl.ParseFunctionalString(src)
			if err != nil {
				t.Fatalf("parse: %v\n%s", err, src)
			}
			c := el.Classify(o)
			checkInvariants(t, o, c)
			inferred += countInferred(o, c)
		})
	}
	if inferred == 0 {
		t.Error("no subsumption was inferred across the whole batch; the property tests are vacuous")
	} else {
		t.Logf("inferred %d subsumptions beyond those asserted", inferred)
	}
}

// countInferred counts entailed subsumptions between named classes that no
// SubClassOf axiom states outright.
func countInferred(o *owl.Ontology, c *el.Classification) int {
	asserted := make(map[[2]owl.Class]bool)
	for ax := range o.All() {
		if sub, ok := owl.Unwrap(ax).(owl.SubClassOf); ok {
			a, aok := sub.Sub.(owl.Class)
			b, bok := sub.Super.(owl.Class)
			if aok && bok {
				asserted[[2]owl.Class{a, b}] = true
			}
		}
	}
	n := 0
	for _, a := range c.Classes() {
		for _, b := range c.SuperClassesOf(a) {
			if !asserted[[2]owl.Class{a, b}] {
				n++
			}
		}
	}
	return n
}

// TestMonotonicity checks that adding an axiom never removes an entailment.
// Saturation only ever adds facts, so a violation would mean the normalizer
// mishandled an interaction between axioms.
func TestMonotonicity(t *testing.T) {
	for seed := int64(0); seed < 25; seed++ {
		r := rand.New(rand.NewSource(seed + 1000))
		src := randomOntology(r)
		base, err := owl.ParseFunctionalString(src)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		before := el.Classify(base)

		extended, err := owl.ParseFunctionalString(src)
		if err != nil {
			t.Fatal(err)
		}
		extended.Add(owl.SubClassOf{
			Sub:   owl.Class("http://example.org/o#C0"),
			Super: owl.Class("http://example.org/o#C1"),
		})
		after := el.Classify(extended)

		for _, a := range before.Classes() {
			for _, b := range before.SuperClassesOf(a) {
				if !after.IsSubsumedBy(a, b) {
					t.Fatalf("seed %d: adding an axiom lost %s ⊑ %s", seed, a, b)
				}
			}
		}
	}
}

// randomOntology generates a small EL ontology. The shapes are chosen to make
// rule interactions likely: shared fillers, chains, disjointness and defined
// classes all appear.
func randomOntology(r *rand.Rand) string {
	const n = 12
	cls := func(i int) string { return fmt.Sprintf(":C%d", i%n) }
	role := func(i int) string { return fmt.Sprintf(":r%d", i%3) }

	var b strings.Builder
	b.WriteString("Prefix(:=<http://example.org/o#>)\nOntology(<http://example.org/o>\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "  Declaration(Class(%s))\n", cls(i))
	}
	for i := 0; i < 25; i++ {
		switch r.Intn(7) {
		case 0:
			fmt.Fprintf(&b, "  SubClassOf(%s %s)\n", cls(r.Intn(n)), cls(r.Intn(n)))
		case 1:
			fmt.Fprintf(&b, "  SubClassOf(%s ObjectSomeValuesFrom(%s %s))\n",
				cls(r.Intn(n)), role(r.Intn(3)), cls(r.Intn(n)))
		case 2:
			fmt.Fprintf(&b, "  SubClassOf(ObjectSomeValuesFrom(%s %s) %s)\n",
				role(r.Intn(3)), cls(r.Intn(n)), cls(r.Intn(n)))
		case 3:
			fmt.Fprintf(&b, "  SubClassOf(ObjectIntersectionOf(%s %s) %s)\n",
				cls(r.Intn(n)), cls(r.Intn(n)), cls(r.Intn(n)))
		case 4:
			fmt.Fprintf(&b, "  EquivalentClasses(%s ObjectIntersectionOf(%s ObjectSomeValuesFrom(%s %s)))\n",
				cls(r.Intn(n)), cls(r.Intn(n)), role(r.Intn(3)), cls(r.Intn(n)))
		case 5:
			fmt.Fprintf(&b, "  SubObjectPropertyOf(%s %s)\n", role(r.Intn(3)), role(r.Intn(3)))
		case 6:
			if r.Intn(2) == 0 {
				fmt.Fprintf(&b, "  TransitiveObjectProperty(%s)\n", role(r.Intn(3)))
			} else {
				fmt.Fprintf(&b, "  DisjointClasses(%s %s)\n", cls(r.Intn(n)), cls(r.Intn(n)))
			}
		}
	}
	b.WriteString(")\n")
	return b.String()
}

// TestClassifyScales is a smoke test that saturation stays tractable: a deep
// hierarchy with existentials across it must classify in seconds, not minutes.
func TestClassifyScales(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the scale check in short mode")
	}
	const n = 3000

	var b strings.Builder
	b.WriteString("Prefix(:=<http://example.org/o#>)\nOntology(<http://example.org/o>\n")
	for i := 1; i < n; i++ {
		fmt.Fprintf(&b, "  SubClassOf(:C%d :C%d)\n", i, i/2)
		if i%25 == 0 {
			fmt.Fprintf(&b, "  SubClassOf(:C%d ObjectSomeValuesFrom(:part :C%d))\n", i, i/3)
			fmt.Fprintf(&b, "  SubClassOf(ObjectSomeValuesFrom(:part :C%d) :P%d)\n", i/3, i)
		}
	}
	b.WriteString("  TransitiveObjectProperty(:part)\n)\n")

	o, err := owl.ParseFunctionalString(b.String())
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	c := el.Classify(o)
	elapsed := time.Since(start)

	// A deep chain must be closed all the way to the root.
	if !c.IsSubsumedBy(owl.Class("http://example.org/o#C2048"), owl.Class("http://example.org/o#C1")) {
		t.Error("the hierarchy was not closed to the root")
	}
	t.Logf("classified %d classes in %s", len(c.Classes()), elapsed)
	if elapsed > 30*time.Second {
		t.Errorf("classification took %s, which is too slow to be right", elapsed)
	}
}
