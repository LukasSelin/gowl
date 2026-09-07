package el

import (
	"sort"

	"gowl/owl"
)

// Unsupported returns the axioms the classifier could not use, in the order
// they appear in the ontology. An empty result means the whole ontology was
// classified; a non-empty one means the classification is sound but possibly
// incomplete, so a subsumption it does not report is not evidence that the
// subsumption fails.
func (c *Classification) Unsupported() []owl.Axiom { return c.norm.unsupported }

// Classes returns every named class the ontology mentions, sorted by IRI.
// owl:Thing and owl:Nothing are excluded: they are always present and are the
// poles the taxonomy hangs from rather than terms of their own.
func (c *Classification) Classes() []owl.Class {
	out := make([]owl.Class, 0, c.sym.countConcepts())
	for id := conceptID(0); int(id) < c.sym.countConcepts(); id++ {
		if id == top || id == bottom || !c.sym.named(id) {
			continue
		}
		out = append(out, c.sym.class(id))
	}
	sortClasses(out)
	return out
}

// IsSubsumedBy reports whether the ontology entails sub ⊑ super.
//
// An unsatisfiable class is below everything, which is why this can be true
// when super does not appear in [Classification.SuperClassesOf].
func (c *Classification) IsSubsumedBy(sub, super owl.Class) bool {
	if super == owl.Thing {
		return true
	}
	a, ok := c.sym.lookup(sub)
	if !ok {
		return false
	}
	if c.subs[a][bottom] {
		return true
	}
	b, ok := c.sym.lookup(super)
	if !ok {
		return false
	}
	return c.subs[a][b]
}

// IsSatisfiable reports whether a class can have instances. An unknown class is
// unconstrained, and so satisfiable.
func (c *Classification) IsSatisfiable(cl owl.Class) bool {
	if cl == owl.Nothing {
		return false
	}
	a, ok := c.sym.lookup(cl)
	if !ok {
		return true
	}
	return !c.subs[a][bottom]
}

// UnsatisfiableClasses returns every named class that cannot have instances,
// sorted by IRI. These are the classes a modelling error usually shows up as:
// each one is equivalent to owl:Nothing, which is rarely what the author meant.
func (c *Classification) UnsatisfiableClasses() []owl.Class {
	var out []owl.Class
	for id := conceptID(0); int(id) < c.sym.countConcepts(); id++ {
		if id == top || id == bottom || !c.sym.named(id) {
			continue
		}
		if c.subs[id][bottom] {
			out = append(out, c.sym.class(id))
		}
	}
	sortClasses(out)
	return out
}

// IsCoherent reports whether every named class is satisfiable.
func (c *Classification) IsCoherent() bool {
	for id := conceptID(0); int(id) < c.sym.countConcepts(); id++ {
		if id != top && id != bottom && c.sym.named(id) && c.subs[id][bottom] {
			return false
		}
	}
	return true
}

// IsConsistent reports whether the ontology has a model at all. Without
// individuals this fails only when the axioms force ⊤ to be empty.
func (c *Classification) IsConsistent() bool { return !c.subs[top][bottom] }

// SuperClassesOf returns every named class the ontology entails above cl,
// excluding cl itself, sorted by IRI. Classes equivalent to cl are included;
// use [Classification.EquivalentClassesOf] to separate them.
//
// For an unsatisfiable class this is the set that was actually derived, not the
// vacuous "everything" that follows from ⊥.
func (c *Classification) SuperClassesOf(cl owl.Class) []owl.Class {
	a, ok := c.sym.lookup(cl)
	if !ok {
		return nil
	}
	var out []owl.Class
	for _, b := range c.subsList[a] {
		if b == a || !c.sym.named(b) || b == top {
			continue
		}
		out = append(out, c.sym.class(b))
	}
	sortClasses(out)
	return out
}

// SubClassesOf returns every named class the ontology entails below cl,
// excluding cl itself, sorted by IRI.
func (c *Classification) SubClassesOf(cl owl.Class) []owl.Class {
	b, ok := c.sym.lookup(cl)
	if !ok {
		return nil
	}
	c.buildInverse()
	var out []owl.Class
	for _, a := range c.subsumedBy[b] {
		if a == b || !c.sym.named(a) || a == bottom {
			continue
		}
		out = append(out, c.sym.class(a))
	}
	sortClasses(out)
	return out
}

// EquivalentClassesOf returns the named classes the ontology entails are equal
// to cl, excluding cl itself, sorted by IRI.
func (c *Classification) EquivalentClassesOf(cl owl.Class) []owl.Class {
	a, ok := c.sym.lookup(cl)
	if !ok {
		return nil
	}
	var out []owl.Class
	for _, b := range c.subsList[a] {
		if b == a || !c.sym.named(b) || b == top || b == bottom {
			continue
		}
		if c.subs[b][a] {
			out = append(out, c.sym.class(b))
		}
	}
	sortClasses(out)
	return out
}

// buildInverse fills in the subclass direction, which saturation does not
// produce directly.
func (c *Classification) buildInverse() {
	if c.subsumedBy != nil {
		return
	}
	c.subsumedBy = make([][]conceptID, c.sym.countConcepts())
	for a := conceptID(0); int(a) < c.sym.countConcepts(); a++ {
		for _, b := range c.subsList[a] {
			c.subsumedBy[b] = append(c.subsumedBy[b], a)
		}
	}
}

// DirectSuperClassesOf returns the classes immediately above cl in the
// inferred taxonomy: entailed superclasses with nothing strictly between them
// and cl. Equivalent classes are not "above" and are excluded.
//
// An unsatisfiable class has no place in the taxonomy — it is equivalent to
// owl:Nothing — and returns nothing here.
func (c *Classification) DirectSuperClassesOf(cl owl.Class) []owl.Class {
	a, ok := c.sym.lookup(cl)
	if !ok || c.subs[a][bottom] {
		return nil
	}

	// Strict, satisfiable, named superclasses, ⊤ included so that a top-level
	// class still has a parent.
	var strict []conceptID
	for _, b := range c.subsList[a] {
		if b == a || !c.sym.named(b) || c.subs[b][bottom] || c.subs[b][a] {
			continue
		}
		strict = append(strict, b)
	}

	var out []owl.Class
	for _, b := range strict {
		redundant := false
		for _, mid := range strict {
			// mid sits strictly between a and b when a ⊑ mid ⊑ b and mid is
			// equivalent to neither end.
			if mid == b || c.subs[mid][b] && c.subs[b][mid] {
				continue
			}
			if c.subs[mid][b] {
				redundant = true
				break
			}
		}
		if !redundant {
			out = append(out, c.sym.class(b))
		}
	}
	if len(out) == 0 && !c.subs[a][bottom] && a != top {
		// Nothing but ⊤ above it.
		out = append(out, owl.Thing)
	}
	sortClasses(out)
	return out
}

// InferredAxioms renders the classification as ontology axioms: one
// SubClassOf per direct subsumption, an EquivalentClasses for each group of
// mutually subsuming classes, and EquivalentClasses(C, owl:Nothing) for each
// unsatisfiable class. The result is sorted, so two runs over the same
// ontology produce identical output.
func (c *Classification) InferredAxioms() []owl.Axiom {
	var out []owl.Axiom
	seenEquiv := make(map[owl.Class]bool)

	for _, cl := range c.Classes() {
		if !c.IsSatisfiable(cl) {
			out = append(out, owl.EquivalentClasses{cl, owl.Nothing})
			continue
		}
		if !seenEquiv[cl] {
			if eq := c.EquivalentClassesOf(cl); len(eq) > 0 {
				group := append([]owl.Class{cl}, eq...)
				sortClasses(group)
				ces := make([]owl.ClassExpression, 0, len(group))
				for _, g := range group {
					seenEquiv[g] = true
					ces = append(ces, g)
				}
				out = append(out, owl.EquivalentClasses(ces))
			}
		}
		for _, super := range c.DirectSuperClassesOf(cl) {
			out = append(out, owl.SubClassOf{Sub: cl, Super: super})
		}
	}
	owl.SortAxioms(out)
	return out
}

// Explain returns the ontology axioms behind an entailed subsumption, sorted.
//
// This is the support of the derivation the classifier actually found, not a
// minimal justification: it is guaranteed to entail the subsumption, but a
// smaller set may also do so. It returns nil when the subsumption does not
// hold, or when the classification was built with [WithoutExplanations].
func (c *Classification) Explain(sub, super owl.Class) []owl.Axiom {
	if c.why == nil || !c.IsSubsumedBy(sub, super) {
		return nil
	}
	a, ok := c.sym.lookup(sub)
	if !ok {
		return nil
	}

	start := factKey{}
	switch b, known := c.sym.lookup(super); {
	case known && c.subs[a][b]:
		start = subFact(a, b)
	case c.subs[a][bottom]:
		// The subsumption holds only because sub is unsatisfiable, so the
		// interesting question is why it is empty.
		start = subFact(a, bottom)
	default:
		return nil
	}

	seen := make(map[factKey]bool)
	found := make(map[string]owl.Axiom)
	var walk func(factKey)
	walk = func(k factKey) {
		if seen[k] {
			return
		}
		seen[k] = true
		d, ok := c.why[k]
		if !ok {
			return
		}
		if d.axiom != nil {
			found[owl.CanonicalKey(d.axiom)] = d.axiom
		}
		for _, p := range d.from {
			walk(p)
		}
	}
	walk(start)

	out := make([]owl.Axiom, 0, len(found))
	for _, ax := range found {
		out = append(out, ax)
	}
	owl.SortAxioms(out)
	return out
}

func sortClasses(cs []owl.Class) {
	sort.Slice(cs, func(i, j int) bool { return cs[i] < cs[j] })
}
