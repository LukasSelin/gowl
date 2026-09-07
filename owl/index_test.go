package owl_test

import (
	"testing"

	"gowl/owl"
)

func TestIndexIsCachedAndInvalidated(t *testing.T) {
	o := GenerateOntology(50, 3)

	first := o.Index()
	if o.Index() != first {
		t.Fatal("Index() rebuilt on a second call")
	}
	// Queries must not invalidate the cache.
	_ = o.Label(o.Class(":C1"))
	_ = o.AncestorsOf(o.Class(":C10"))
	if o.Index() != first {
		t.Fatal("a query invalidated the cached index")
	}

	for _, tc := range []struct {
		name   string
		mutate func()
	}{
		{"Add", func() { o.Add(owl.SubClassOf{Sub: o.Class(":C1"), Super: owl.Thing}) }},
		{"Declare", func() { o.Declare(o.Class(":Fresh")) }},
		{"Rewrite", func() { o.Rewrite(func(a owl.Axiom) owl.Axiom { return a }) }},
		{"Sort", func() { o.Sort() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := o.Index()
			tc.mutate()
			if o.Index() == before {
				t.Errorf("%s did not invalidate the index", tc.name)
			}
		})
	}
}

func TestAddIsVisibleToQueriesImmediately(t *testing.T) {
	o := owl.New("http://example.org/o")
	o.Prefix("", "http://example.org/o#")
	a, b := o.Class(":A"), o.Class(":B")

	if got := o.SuperClassesOf(a); len(got) != 0 {
		t.Fatalf("precondition: got %v", got)
	}
	o.Add(owl.SubClassOf{Sub: a, Super: b})
	if got := o.SuperClassesOf(a); len(got) != 1 || got[0] != b {
		t.Errorf("SuperClassesOf after Add = %v, want [B]", got)
	}
}

func TestAxiomsReturnsACopy(t *testing.T) {
	o := GenerateOntology(10, 2)
	axioms := o.Axioms()
	if len(axioms) == 0 {
		t.Fatal("no axioms")
	}
	// Clobbering the returned slice must not corrupt the ontology, which is why
	// Axioms copies: a stale index would otherwise be unobservable.
	before := o.Len()
	for i := range axioms {
		axioms[i] = owl.Declaration{Entity: owl.Thing}
	}
	if o.Len() != before {
		t.Errorf("length changed: %d, want %d", o.Len(), before)
	}
	for ax := range o.All() {
		if _, isDecl := ax.(owl.Declaration); !isDecl {
			return // found a non-Declaration, so the ontology is intact
		}
	}
	t.Error("mutating the result of Axioms() corrupted the ontology")
}

func TestAllIteratesEveryAxiomAndStopsEarly(t *testing.T) {
	o := GenerateOntology(20, 2)

	n := 0
	for range o.All() {
		n++
	}
	if n != o.Len() {
		t.Errorf("All() yielded %d axioms, want %d", n, o.Len())
	}

	n = 0
	for range o.All() {
		n++
		if n == 3 {
			break
		}
	}
	if n != 3 {
		t.Errorf("break did not stop iteration: %d", n)
	}
}

func TestRewriteAppliesToEveryAxiom(t *testing.T) {
	o := GenerateOntology(20, 2)
	o.Rewrite(owl.CanonicalAxiom)
	for ax := range o.All() {
		if owl.Functional(ax) != owl.CanonicalKey(ax) {
			t.Fatalf("axiom not canonical after Rewrite: %s", owl.Functional(ax))
		}
	}
}

// --- Parity with naive scanning ---------------------------------------------
//
// The index replaced a set of linear scans. These tests re-implement the scans
// and assert the index agrees, so an indexing bug cannot pass unnoticed.

func naiveSuperClassesOf(o *owl.Ontology, c owl.Class) map[owl.Class]bool {
	seen := map[owl.Class]bool{}
	for ax := range o.All() {
		switch x := owl.Unwrap(ax).(type) {
		case owl.SubClassOf:
			if sub, ok := x.Sub.(owl.Class); ok && sub == c {
				if super, ok := x.Super.(owl.Class); ok {
					seen[super] = true
				}
			}
		case owl.EquivalentClasses:
			var has bool
			for _, ce := range x {
				if other, ok := ce.(owl.Class); ok && other == c {
					has = true
				}
			}
			if has {
				for _, ce := range x {
					if other, ok := ce.(owl.Class); ok && other != c {
						seen[other] = true
					}
				}
			}
		}
	}
	return seen
}

func naiveAxiomsReferencing(o *owl.Ontology, e owl.Entity) int {
	n := 0
	for ax := range o.All() {
		if owl.References(ax, e) {
			n++
		}
	}
	return n
}

func TestIndexMatchesNaiveScan(t *testing.T) {
	ontologies := map[string]*owl.Ontology{
		"generated": GenerateOntology(200, 5),
	}
	if p, _ := pizza(); p != nil {
		ontologies["pizza"] = p
	}

	for name, o := range ontologies {
		t.Run(name, func(t *testing.T) {
			for _, c := range o.Classes() {
				want := naiveSuperClassesOf(o, c)
				got := o.SuperClassesOf(c)
				if len(got) != len(want) {
					t.Fatalf("SuperClassesOf(%s): got %v, want %d entries", c, got, len(want))
				}
				for _, g := range got {
					if !want[g] {
						t.Errorf("SuperClassesOf(%s) returned unexpected %s", c, g)
					}
				}
				if n := naiveAxiomsReferencing(o, c); n != len(o.AxiomsReferencing(c)) {
					t.Errorf("AxiomsReferencing(%s): index says %d, scan says %d",
						c, len(o.AxiomsReferencing(c)), n)
				}
			}
		})
	}
}

func TestIndexEquivalenceIsSymmetric(t *testing.T) {
	o := mustParse(t, doc("    EquivalentClasses(:A :B)"))
	a, b := o.Class(":A"), o.Class(":B")

	// The scanning implementation reported equivalent classes in both
	// directions; the index must not quietly change that.
	if got := o.SuperClassesOf(a); len(got) != 1 || got[0] != b {
		t.Errorf("SuperClassesOf(A) = %v, want [B]", got)
	}
	if got := o.SubClassesOf(a); len(got) != 1 || got[0] != b {
		t.Errorf("SubClassesOf(A) = %v, want [B]", got)
	}
}

func TestIndexHandlesHierarchyCycles(t *testing.T) {
	o := mustParse(t, doc("    SubClassOf(:A :B)\n    SubClassOf(:B :C)\n    SubClassOf(:C :A)"))
	// A cycle must terminate and exclude the starting class.
	got := o.AncestorsOf(o.Class(":A"))
	if len(got) != 2 {
		t.Errorf("AncestorsOf(A) = %v, want B and C", got)
	}
	for _, c := range got {
		if c == o.Class(":A") {
			t.Error("AncestorsOf should exclude the class itself")
		}
	}
}

func TestIndexDirectHierarchyIsSortedAndDeduped(t *testing.T) {
	o := mustParse(t, doc("    SubClassOf(:A :C)\n    SubClassOf(:A :B)\n    SubClassOf(:A :B)"))
	got := o.SuperClassesOf(o.Class(":A"))
	if len(got) != 2 {
		t.Fatalf("got %v, want two distinct superclasses", got)
	}
	if got[0] >= got[1] {
		t.Errorf("not sorted: %v", got)
	}
}

func TestIndexIsDeclared(t *testing.T) {
	o := mustParse(t, doc("    Declaration(Class(:A))\n    SubClassOf(:A :B)"))
	if !o.IsDeclared(o.Class(":A")) {
		t.Error("A should be declared")
	}
	if o.IsDeclared(o.Class(":B")) {
		t.Error("B should not be declared")
	}
}
