package el

import "gowl/owl"

// Normalization rewrites every axiom into one of four shapes, where each
// letter is a concept name (possibly a fresh one, possibly a pole):
//
//	A ⊑ B          nfSub
//	A₁ ⊓ A₂ ⊑ B    nfConj
//	A ⊑ ∃r.B       nfExists
//	∃r.A ⊑ B       nfExLeft
//
// plus role inclusions r ⊑ s and chains r₁ ∘ r₂ ⊑ s. The completion rules in
// classify.go are written against exactly these shapes, which is what keeps
// them short enough to be checkable by eye.
//
// A complex subexpression is replaced by a fresh name whose defining axiom
// points in the direction its position requires. EL has no negation, so
// polarity never flips: a subexpression on the left of ⊑ is negative all the
// way down, one on the right is positive all the way down. A negative Ĉ gets
// `Ĉ ⊑ X`, a positive D̂ gets `X ⊑ D̂`. Either way, reading X as the expression
// it stands for satisfies the new axioms, so the rewrite adds no entailments
// over the original signature.
type nfKind uint8

const (
	nfSub nfKind = iota
	nfConj
	nfExists
	nfExLeft
)

// nf is one normalized axiom. Which fields are meaningful depends on kind;
// source is the ontology axiom it came from, and is what an explanation cites.
type nf struct {
	kind   nfKind
	a, a2  conceptID
	b      conceptID
	r      roleID
	source owl.Axiom
}

type roleSub struct {
	sub, super roleID
	source     owl.Axiom
}

type roleChain struct {
	first, second, super roleID
	source               owl.Axiom
}

// polarity records which side of a ⊑ a subexpression sits on.
type polarity uint8

const (
	negative polarity = iota
	positive
)

type normalizer struct {
	sym *symbols

	axioms   []nf
	roleSubs []roleSub
	chains   []roleChain

	unsupported []owl.Axiom
	rejected    map[string]bool
}

func normalize(o *owl.Ontology) *normalizer {
	n := &normalizer{sym: newSymbols(), rejected: make(map[string]bool)}
	for ax := range o.All() {
		n.axiom(owl.Unwrap(ax), ax)
	}
	return n
}

// reject records an axiom the classifier cannot use. The same axiom can be
// reached more than once while normalizing, so it is deduplicated here.
func (n *normalizer) reject(src owl.Axiom) {
	if src == nil {
		return
	}
	key := owl.CanonicalKey(src)
	if n.rejected[key] {
		return
	}
	n.rejected[key] = true
	n.unsupported = append(n.unsupported, src)
}

func (n *normalizer) emit(a nf) { n.axioms = append(n.axioms, a) }

// axiom dispatches one ontology axiom. src is the axiom as it appears in the
// file — annotations and all — so explanations quote what the author wrote.
func (n *normalizer) axiom(ax owl.Axiom, src owl.Axiom) {
	switch x := ax.(type) {
	case owl.Declaration:
		// Declarations carry no logic, but registering the entity makes it
		// appear in the taxonomy even when nothing else mentions it.
		switch e := x.Entity.(type) {
		case owl.Class:
			n.sym.concept(e)
		case owl.ObjectProperty:
			n.sym.role(e)
		}

	case owl.SubClassOf:
		n.gci(x.Sub, x.Super, src)

	case owl.EquivalentClasses:
		// A chain of pairwise inclusions is enough: saturation closes the rest.
		for i := 0; i+1 < len(x); i++ {
			n.gci(x[i], x[i+1], src)
			n.gci(x[i+1], x[i], src)
		}

	case owl.DisjointClasses:
		for i := range x {
			for j := i + 1; j < len(x); j++ {
				n.gci(owl.And(x[i], x[j]), owl.Nothing, src)
			}
		}

	case owl.ObjectPropertyDomain:
		// Domain is ∃r.⊤ ⊑ C, which is already an EL axiom. Range is not: it
		// needs a universal restriction, so it lands in Unsupported below.
		n.gci(owl.Some(x.Property, owl.Thing), x.Domain, src)

	case owl.SubObjectPropertyOf:
		n.roleInclusion(x.Sub, x.Super, src)

	case owl.EquivalentObjectProperties:
		for i := 0; i+1 < len(x); i++ {
			n.roleInclusion(x[i], x[i+1], src)
			n.roleInclusion(x[i+1], x[i], src)
		}

	case owl.TransitiveObjectProperty:
		r, ok := n.roleOf(x.Property)
		if !ok {
			n.reject(src)
			return
		}
		n.chains = append(n.chains, roleChain{first: r, second: r, super: r, source: src})

	case owl.SubPropertyChainOf:
		n.chain(x, src)

	case owl.AnnotationAssertion, owl.SubAnnotationPropertyOf,
		owl.AnnotationPropertyDomain, owl.AnnotationPropertyRange:
		// Annotations are not logical axioms; there is nothing to classify and
		// nothing missing when they are skipped.

	default:
		n.reject(src)
	}
}

func (n *normalizer) roleOf(p owl.ObjectPropertyExpression) (roleID, bool) {
	// ObjectInverseOf is outside EL.
	named, ok := p.(owl.ObjectProperty)
	if !ok {
		return 0, false
	}
	return n.sym.role(named), true
}

func (n *normalizer) roleInclusion(sub, super owl.ObjectPropertyExpression, src owl.Axiom) {
	r, ok1 := n.roleOf(sub)
	s, ok2 := n.roleOf(super)
	if !ok1 || !ok2 {
		n.reject(src)
		return
	}
	n.roleSubs = append(n.roleSubs, roleSub{sub: r, super: s, source: src})
}

// chain turns r₁ ∘ … ∘ rₙ ⊑ s into binary chains, since the completion rules
// only ever compose two links at a time.
func (n *normalizer) chain(x owl.SubPropertyChainOf, src owl.Axiom) {
	super, ok := n.roleOf(x.Super)
	if !ok || len(x.Chain) == 0 {
		n.reject(src)
		return
	}
	ids := make([]roleID, 0, len(x.Chain))
	for _, p := range x.Chain {
		r, ok := n.roleOf(p)
		if !ok {
			n.reject(src)
			return
		}
		ids = append(ids, r)
	}
	if len(ids) == 1 {
		n.roleSubs = append(n.roleSubs, roleSub{sub: ids[0], super: super, source: src})
		return
	}
	cur := ids[0]
	for i := 1; i < len(ids)-1; i++ {
		next := n.sym.freshRole()
		n.chains = append(n.chains, roleChain{first: cur, second: ids[i], super: next, source: src})
		cur = next
	}
	n.chains = append(n.chains, roleChain{first: cur, second: ids[len(ids)-1], super: super, source: src})
}

// gci normalizes one general concept inclusion. The special cases for ∃ on
// either side are not required for correctness — concept() would introduce a
// fresh name and reach the same normal form — but they avoid a pointless extra
// name and hop for the two shapes that dominate real ontologies.
func (n *normalizer) gci(lhs, rhs owl.ClassExpression, src owl.Axiom) {
	// Anything is below ⊤.
	if c, ok := rhs.(owl.Class); ok && c == owl.Thing {
		return
	}
	// C ⊑ D₁ ⊓ D₂ splits, which is cheaper than naming the conjunction.
	if and, ok := rhs.(owl.ObjectIntersectionOf); ok {
		for _, d := range and {
			n.gci(lhs, d, src)
		}
		return
	}

	if some, ok := lhs.(owl.ObjectSomeValuesFrom); ok {
		r, ok1 := n.roleOf(some.Property)
		filler, ok2 := n.concept(some.Filler, negative, src)
		b, ok3 := n.concept(rhs, positive, src)
		if !ok1 || !ok2 || !ok3 {
			n.reject(src)
			return
		}
		n.emit(nf{kind: nfExLeft, r: r, a: filler, b: b, source: src})
		return
	}

	var ids []conceptID
	if and, ok := lhs.(owl.ObjectIntersectionOf); ok {
		ids = make([]conceptID, 0, len(and))
		for _, op := range and {
			id, ok := n.concept(op, negative, src)
			if !ok {
				n.reject(src)
				return
			}
			ids = append(ids, id)
		}
	} else {
		id, ok := n.concept(lhs, negative, src)
		if !ok {
			n.reject(src)
			return
		}
		ids = []conceptID{id}
	}

	if some, ok := rhs.(owl.ObjectSomeValuesFrom); ok {
		r, ok1 := n.roleOf(some.Property)
		filler, ok2 := n.concept(some.Filler, positive, src)
		if !ok1 || !ok2 {
			n.reject(src)
			return
		}
		n.emit(nf{kind: nfExists, a: n.single(ids, src), r: r, b: filler, source: src})
		return
	}

	b, ok := n.concept(rhs, positive, src)
	if !ok {
		n.reject(src)
		return
	}
	n.conjunction(ids, b, src)
}

// concept reduces a class expression to a single name, introducing fresh names
// for anything that is not already one.
func (n *normalizer) concept(ce owl.ClassExpression, pol polarity, src owl.Axiom) (conceptID, bool) {
	switch x := ce.(type) {
	case owl.Class:
		switch x {
		case owl.Thing:
			return top, true
		case owl.Nothing:
			return bottom, true
		}
		return n.sym.concept(x), true

	case owl.ObjectIntersectionOf:
		ids := make([]conceptID, 0, len(x))
		for _, op := range x {
			id, ok := n.concept(op, pol, src)
			if !ok {
				return 0, false
			}
			ids = append(ids, id)
		}
		fresh := n.sym.freshConcept()
		if pol == negative {
			n.conjunction(ids, fresh, src)
		} else {
			// X ⊑ C₁ ⊓ … ⊓ Cₙ is X ⊑ Cᵢ for every i.
			for _, id := range ids {
				n.emit(nf{kind: nfSub, a: fresh, b: id, source: src})
			}
		}
		return fresh, true

	case owl.ObjectSomeValuesFrom:
		r, ok := n.roleOf(x.Property)
		if !ok {
			return 0, false
		}
		filler, ok := n.concept(x.Filler, pol, src)
		if !ok {
			return 0, false
		}
		fresh := n.sym.freshConcept()
		if pol == negative {
			n.emit(nf{kind: nfExLeft, r: r, a: filler, b: fresh, source: src})
		} else {
			n.emit(nf{kind: nfExists, a: fresh, r: r, b: filler, source: src})
		}
		return fresh, true
	}
	return 0, false
}

// conjunction emits A₁ ⊓ … ⊓ Aₙ ⊑ target, binarized because the completion
// rules only ever look at two conjuncts.
func (n *normalizer) conjunction(ids []conceptID, target conceptID, src owl.Axiom) {
	kept := make([]conceptID, 0, len(ids))
	for _, id := range ids {
		if id != top { // ⊤ is the identity of ⊓
			kept = append(kept, id)
		}
	}
	switch len(kept) {
	case 0:
		n.emit(nf{kind: nfSub, a: top, b: target, source: src})
	case 1:
		n.emit(nf{kind: nfSub, a: kept[0], b: target, source: src})
	default:
		cur := kept[0]
		for i := 1; i < len(kept)-1; i++ {
			next := n.sym.freshConcept()
			n.emit(nf{kind: nfConj, a: cur, a2: kept[i], b: next, source: src})
			cur = next
		}
		n.emit(nf{kind: nfConj, a: cur, a2: kept[len(kept)-1], b: target, source: src})
	}
}

// single collapses a conjunctive left-hand side to one name, for the axiom
// shapes that admit only a single concept there.
func (n *normalizer) single(ids []conceptID, src owl.Axiom) conceptID {
	if len(ids) == 1 {
		return ids[0]
	}
	fresh := n.sym.freshConcept()
	n.conjunction(ids, fresh, src)
	return fresh
}
