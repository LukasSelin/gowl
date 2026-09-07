package owl_test

import (
	"testing"

	"gowl/owl"
)

func cls(name string) owl.Class { return owl.Class("http://example.org/" + name) }

func TestCanonicalSortsAndDedupes(t *testing.T) {
	a, b, c := cls("A"), cls("B"), cls("C")

	cases := []struct {
		name     string
		in, want owl.ClassExpression
	}{
		{"intersection is sorted", owl.And(b, a), owl.And(a, b)},
		{"union is sorted", owl.Or(c, a, b), owl.Or(a, b, c)},
		{"duplicates collapse", owl.And(a, b, a), owl.And(a, b)},
		{"one operand collapses", owl.And(a, a), a},
		{"nesting flattens", owl.And(owl.And(b, a), c), owl.And(a, b, c)},
		{"union nesting flattens", owl.Or(a, owl.Or(c, b)), owl.Or(a, b, c)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := owl.CanonicalClassExpression(tc.in)
			if !owl.Equal(got, tc.want) {
				t.Errorf("got  %s\nwant %s", owl.Functional(got), owl.Functional(tc.want))
			}
		})
	}
}

func TestCanonicalPreservesMeaningfulOrder(t *testing.T) {
	a, b := cls("A"), cls("B")
	p := owl.ObjectProperty("http://example.org/p")
	q := owl.ObjectProperty("http://example.org/q")

	// SubClassOf is directional: canonicalization must not sort its sides.
	sub := owl.CanonicalAxiom(owl.SubClassOf{Sub: b, Super: a}).(owl.SubClassOf)
	if !owl.Equal(sub.Sub, b) || !owl.Equal(sub.Super, a) {
		t.Errorf("SubClassOf sides were reordered: %s", owl.Functional(sub))
	}

	// A property chain is a composition, so its order is meaning.
	chain := owl.CanonicalAxiom(owl.SubPropertyChainOf{Chain: []owl.ObjectPropertyExpression{q, p}, Super: p})
	want := "SubObjectPropertyOf(ObjectPropertyChain(<http://example.org/q> <http://example.org/p>) <http://example.org/p>)"
	if got := owl.Functional(chain); got != want {
		t.Errorf("chain reordered:\n got %s\nwant %s", got, want)
	}
}

func TestCanonicalRecursesIntoNestedExpressions(t *testing.T) {
	a, b := cls("A"), cls("B")
	p := owl.ObjectProperty("http://example.org/p")

	in := owl.Some(p, owl.And(b, a))
	want := owl.Some(p, owl.And(a, b))
	if got := owl.CanonicalClassExpression(in); !owl.Equal(got, want) {
		t.Errorf("got %s, want %s", owl.Functional(got), owl.Functional(want))
	}
}

func TestCanonicalKeyIgnoresOperandOrder(t *testing.T) {
	a, b := cls("A"), cls("B")
	x := owl.EquivalentClasses{a, b}
	y := owl.EquivalentClasses{b, a}

	if owl.Equal(x, y) {
		t.Fatal("precondition: Equal should be order-sensitive")
	}
	if owl.CanonicalKey(x) != owl.CanonicalKey(y) {
		t.Errorf("canonical keys differ:\n%s\n%s", owl.CanonicalKey(x), owl.CanonicalKey(y))
	}
}

func TestCanonicalHandlesNilCardinalityFiller(t *testing.T) {
	p := owl.ObjectProperty("http://example.org/p")
	// An unqualified restriction has a nil filler; canonicalizing must not
	// dereference it.
	got := owl.CanonicalClassExpression(owl.Min(2, p))
	if want := "ObjectMinCardinality(2 <http://example.org/p>)"; owl.Functional(got) != want {
		t.Errorf("got %s, want %s", owl.Functional(got), want)
	}
}
