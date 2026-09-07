package owl

import "sort"

// Index is a precomputed view of an ontology's axioms. Building it costs one
// pass; every query it answers is then a map lookup rather than a scan, which
// turns the hierarchy closures from quadratic into linear in the part of the
// graph they actually reach.
//
// An Index is a snapshot. It reflects the ontology as of the moment it was
// built and never updates itself, so it is safe to share across goroutines for
// reading but must be rebuilt after the ontology changes. [Ontology] keeps a
// cached index for its own queries and discards it whenever axioms are added,
// so callers rarely need to manage one directly.
type Index struct {
	declared  map[Entity]bool
	signature []Entity
	byEntity  map[Entity][]Axiom

	supers map[Class][]Class
	subs   map[Class][]Class

	types      map[Individual][]ClassExpression
	instances  map[string][]Individual
	objectVals map[propKey][]Individual
	dataVals   map[propKey][]Literal
	labels     map[IRI]string
}

// propKey identifies a (subject, property) pair. The property is held as its
// rendering rather than as the expression, so the key is always hashable.
type propKey struct {
	subject Individual
	prop    string
}

// NewIndex builds an index over o's axioms.
func NewIndex(o *Ontology) *Index {
	x := &Index{
		declared:   make(map[Entity]bool),
		byEntity:   make(map[Entity][]Axiom),
		supers:     make(map[Class][]Class),
		subs:       make(map[Class][]Class),
		types:      make(map[Individual][]ClassExpression),
		instances:  make(map[string][]Individual),
		objectVals: make(map[propKey][]Individual),
		dataVals:   make(map[propKey][]Literal),
		labels:     make(map[IRI]string),
	}

	sig := make(map[Entity]bool)
	seen := make(map[Entity]bool)
	for _, ax := range o.axioms {
		// An axiom that mentions an entity twice should still be listed once.
		clear(seen)
		Walk(ax, func(e Entity) {
			sig[e] = true
			if !seen[e] {
				seen[e] = true
				x.byEntity[e] = append(x.byEntity[e], ax)
			}
		})
		x.record(ax)
	}
	for _, a := range o.Annotations {
		Walk(a, func(e Entity) { sig[e] = true })
	}

	x.signature = sortedEntities(sig)
	for c, list := range x.supers {
		x.supers[c] = dedupeClasses(list)
	}
	for c, list := range x.subs {
		x.subs[c] = dedupeClasses(list)
	}
	return x
}

func (x *Index) record(ax Axiom) {
	switch a := Unwrap(ax).(type) {
	case Declaration:
		if a.Entity != nil {
			x.declared[a.Entity] = true
		}

	case SubClassOf:
		sub, subOK := a.Sub.(Class)
		super, superOK := a.Super.(Class)
		if subOK && superOK {
			x.supers[sub] = append(x.supers[sub], super)
			x.subs[super] = append(x.subs[super], sub)
		}

	case EquivalentClasses:
		// Equivalent classes are each other's super- and subclasses, which is
		// what the scanning implementation reported too.
		var named []Class
		for _, ce := range a {
			if c, ok := ce.(Class); ok {
				named = append(named, c)
			}
		}
		for _, c := range named {
			for _, other := range named {
				if c != other {
					x.supers[c] = append(x.supers[c], other)
					x.subs[c] = append(x.subs[c], other)
				}
			}
		}

	case ClassAssertion:
		x.types[a.Individual] = append(x.types[a.Individual], a.Class)
		key := Functional(a.Class)
		x.instances[key] = append(x.instances[key], a.Individual)

	case ObjectPropertyAssertion:
		k := propKey{subject: a.Subject, prop: Functional(a.Property)}
		x.objectVals[k] = append(x.objectVals[k], a.Object)

	case DataPropertyAssertion:
		k := propKey{subject: a.Subject, prop: Functional(a.Property)}
		x.dataVals[k] = append(x.dataVals[k], a.Value)

	case AnnotationAssertion:
		if a.Property != RDFSLabel {
			return
		}
		if lit, ok := a.Value.(Literal); ok {
			// Label reports the first label asserted, so later ones lose.
			if _, exists := x.labels[a.Subject]; !exists {
				x.labels[a.Subject] = lit.Value
			}
		}
	}
}

// Signature returns every entity referenced by the ontology, sorted by kind
// then IRI. The slice is shared; do not modify it.
func (x *Index) Signature() []Entity { return x.signature }

// IsDeclared reports whether the ontology has a Declaration for e.
func (x *Index) IsDeclared(e Entity) bool { return x.declared[e] }

// AxiomsReferencing returns the axioms mentioning e, in the order they appear.
func (x *Index) AxiomsReferencing(e Entity) []Axiom { return x.byEntity[e] }

// SuperClassesOf returns the named classes asserted directly above c.
func (x *Index) SuperClassesOf(c Class) []Class { return x.supers[c] }

// SubClassesOf returns the named classes asserted directly below c.
func (x *Index) SubClassesOf(c Class) []Class { return x.subs[c] }

// AncestorsOf returns the transitive closure of [Index.SuperClassesOf],
// excluding c.
func (x *Index) AncestorsOf(c Class) []Class { return x.closure(c, x.supers) }

// DescendantsOf returns the transitive closure of [Index.SubClassesOf],
// excluding c.
func (x *Index) DescendantsOf(c Class) []Class { return x.closure(c, x.subs) }

// closure walks edges breadth-first from start, excluding start itself and
// tolerating cycles.
func (x *Index) closure(start Class, edges map[Class][]Class) []Class {
	seen := make(map[Class]bool)
	queue := append([]Class(nil), edges[start]...)
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur == start || seen[cur] {
			continue
		}
		seen[cur] = true
		queue = append(queue, edges[cur]...)
	}
	return sortedClasses(seen)
}

// TypesOf returns the class expressions asserted for an individual.
func (x *Index) TypesOf(i Individual) []ClassExpression { return x.types[i] }

// InstancesOf returns the individuals directly asserted to be instances of ce.
func (x *Index) InstancesOf(ce ClassExpression) []Individual {
	return x.instances[Functional(ce)]
}

// ObjectValues returns the individuals asserted as objects of p for subject i.
func (x *Index) ObjectValues(i Individual, p ObjectPropertyExpression) []Individual {
	return x.objectVals[propKey{subject: i, prop: Functional(p)}]
}

// DataValues returns the literals asserted as values of p for subject i.
func (x *Index) DataValues(i Individual, p DataPropertyExpression) []Literal {
	return x.dataVals[propKey{subject: i, prop: Functional(p)}]
}

// Label returns the first rdfs:label asserted for e, or "".
func (x *Index) Label(e Entity) string { return x.labels[e.IRI()] }

// dedupeClasses sorts by IRI and drops duplicates, so the direct-hierarchy
// accessors return the same ordering the scanning implementation did.
func dedupeClasses(in []Class) []Class {
	if len(in) < 2 {
		return in
	}
	sort.Slice(in, func(i, j int) bool { return in[i] < in[j] })
	out := in[:1]
	for _, c := range in[1:] {
		if c != out[len(out)-1] {
			out = append(out, c)
		}
	}
	return out
}
