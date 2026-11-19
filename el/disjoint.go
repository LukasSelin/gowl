package el

import "github.com/LukasSelin/gowl/owl"

// AreDisjoint reports whether the ontology entails that a and b share no
// instances, that is a ⊓ b ⊑ ⊥.
//
// That covers classes asserted disjoint, their subclasses, and pairs whose
// disjointness follows only from other axioms. An unsatisfiable class is
// disjoint from every class, itself included, and in an inconsistent ontology
// every pair is. A class the ontology never mentions is unconstrained, so it is
// disjoint only from unsatisfiable classes.
//
// As with [Classification.IsSubsumedBy], false is not proof that the classes
// can overlap when [Classification.Unsupported] is non-empty.
func (c *Classification) AreDisjoint(a, b owl.Class) bool {
	q := conjunctionQuery{c: c, subs: make(map[conceptID]bool)}
	q.add(c.idOrTop(a))
	q.add(c.idOrTop(b))
	return q.saturate()
}

// idOrTop returns the concept for cl, standing in ⊤ for a class the ontology
// never mentions: with no axioms about it, it constrains nothing.
func (c *Classification) idOrTop(cl owl.Class) conceptID {
	if id, ok := c.sym.lookup(cl); ok {
		return id
	}
	return top
}

// conjunctionQuery works out S(X) for one extra concept X, defined by X ⊑ A
// and X ⊑ B, against a finished classification. A ⊓ B ⊑ ⊥ holds exactly when
// ⊥ ∈ S(X).
//
// Running all seven completion rules for X is unnecessary. X is fresh, so no
// axiom has it on the right of ⊑ and no existential has it as a filler:
// nothing links to X, and adding it changes no other concept's facts. Every
// link leaving X is made by CR3 from some b ∈ S(X), then perhaps extended by
// CR6 and CR7, and b has that same link. So whatever CR4 and CR5 derive for X
// along it is already in S(b), and two steps reach all of S(X):
//
//	b ∈ S(X)                        ⟹  S(b) ⊆ S(X)
//	B₁, B₂ ∈ S(X), B₁ ⊓ B₂ ⊑ C      ⟹  C ∈ S(X)        (CR2)
//
// The first lets the saturated classification do nearly all the work. The
// second is what it could not do ahead of time: combine a subsumer of A with
// a subsumer of B. The query only reads c, so asking never changes the
// classification.
type conjunctionQuery struct {
	c     *Classification
	subs  map[conceptID]bool
	queue []conceptID
}

// add puts b and everything above it into S(X). ⊤ is in every S(b), so it
// needs no seeding of its own.
func (q *conjunctionQuery) add(b conceptID) {
	if q.subs[b] {
		// b arrived as part of some S(b′), and S(b) ⊆ S(b′) is in already.
		return
	}
	for _, d := range q.c.subsList[b] {
		if !q.subs[d] {
			q.subs[d] = true
			q.queue = append(q.queue, d)
		}
	}
}

// saturate applies CR2 until nothing new appears or ⊥ is derived.
func (q *conjunctionQuery) saturate() bool {
	for len(q.queue) > 0 && !q.subs[bottom] {
		b := q.queue[len(q.queue)-1]
		q.queue = q.queue[:len(q.queue)-1]
		for _, e := range q.c.conjOf[b] {
			if q.subs[e.partner] {
				q.add(e.axiom.b)
			}
		}
	}
	return q.subs[bottom]
}
