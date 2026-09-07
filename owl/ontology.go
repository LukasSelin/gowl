package owl

import (
	"iter"
	"sort"
)

// Ontology is a container of axioms plus the metadata that names them: an IRI,
// an optional version IRI, imports, ontology annotations and a prefix table.
//
// The entity constructors ([Ontology.Class] and friends) expand CURIEs through
// the prefix table. Rather than returning an error on every call, they record
// the first failure; check it once with [Ontology.Err] after building.
type Ontology struct {
	IRI         IRI
	VersionIRI  IRI
	Imports     []IRI
	Annotations []Annotation
	Prefixes    *Prefixes

	axioms []Axiom
	err    error

	// idx caches the index behind the query methods. Every mutation clears it;
	// it is rebuilt on the next query.
	idx *Index
}

// New creates an ontology. The iri may be empty for an anonymous ontology, and
// may itself be a CURIE if the prefix is already declared (it is not, on a
// fresh ontology, so pass an absolute IRI here).
func New(iri IRI) *Ontology {
	return &Ontology{IRI: iri, Prefixes: NewPrefixes()}
}

// Err returns the first error recorded while building, typically an
// unresolvable prefix.
func (o *Ontology) Err() error { return o.err }

func (o *Ontology) fail(err error) {
	if o.err == nil {
		o.err = err
	}
}

// Prefix declares a namespace abbreviation. Use the empty prefix for the
// default namespace, so that ":Pizza" resolves.
func (o *Ontology) Prefix(prefix string, ns IRI) *Ontology {
	o.Prefixes.Set(prefix, ns)
	return o
}

// Version sets the ontology's version IRI.
func (o *Ontology) Version(iri IRI) *Ontology {
	o.VersionIRI = iri
	return o
}

// Import adds an owl:imports declaration.
func (o *Ontology) Import(iri IRI) *Ontology {
	o.Imports = append(o.Imports, iri)
	return o
}

// Annotate adds an annotation to the ontology itself.
func (o *Ontology) Annotate(p AnnotationProperty, v AnnotationValue) *Ontology {
	o.Annotations = append(o.Annotations, Annotation{Property: p, Value: v})
	return o
}

func (o *Ontology) expand(name string) IRI {
	iri, err := o.Prefixes.Expand(name)
	if err != nil {
		o.fail(err)
	}
	return iri
}

// Class resolves name to a class. It does not declare the class; use
// [Ontology.Declare] or [Ontology.Define] for that.
func (o *Ontology) Class(name string) Class { return Class(o.expand(name)) }

// ObjectProperty resolves name to an object property.
func (o *Ontology) ObjectProperty(name string) ObjectProperty {
	return ObjectProperty(o.expand(name))
}

// DataProperty resolves name to a data property.
func (o *Ontology) DataProperty(name string) DataProperty { return DataProperty(o.expand(name)) }

// AnnotationProperty resolves name to an annotation property.
func (o *Ontology) AnnotationProperty(name string) AnnotationProperty {
	return AnnotationProperty(o.expand(name))
}

// Individual resolves name to a named individual.
func (o *Ontology) Individual(name string) NamedIndividual {
	return NamedIndividual(o.expand(name))
}

// Datatype resolves name to a datatype.
func (o *Ontology) Datatype(name string) Datatype { return Datatype(o.expand(name)) }

// Add appends axioms in order.
func (o *Ontology) Add(axioms ...Axiom) *Ontology {
	o.axioms = append(o.axioms, axioms...)
	o.idx = nil
	return o
}

// Declare adds a Declaration for each entity.
func (o *Ontology) Declare(entities ...Entity) *Ontology {
	for _, e := range entities {
		o.axioms = append(o.axioms, Declaration{Entity: e})
	}
	o.idx = nil
	return o
}

// Rewrite replaces every axiom with f(axiom), in place. Use this rather than
// mutating the slice from [Ontology.Axioms], which is a copy.
func (o *Ontology) Rewrite(f func(Axiom) Axiom) *Ontology {
	for i, ax := range o.axioms {
		o.axioms[i] = f(ax)
	}
	o.idx = nil
	return o
}

// Sort orders the ontology's axioms by their rendering, for stable output.
func (o *Ontology) Sort() *Ontology {
	SortAxioms(o.axioms)
	o.idx = nil
	return o
}

// Axioms returns a copy of the ontology's axioms in insertion order. It is a
// copy so that mutating it cannot silently invalidate the query index; use
// [Ontology.All] to iterate without allocating, and [Ontology.Rewrite] to
// change axioms in place.
func (o *Ontology) Axioms() []Axiom {
	out := make([]Axiom, len(o.axioms))
	copy(out, o.axioms)
	return out
}

// All iterates the ontology's axioms in insertion order without copying.
func (o *Ontology) All() iter.Seq[Axiom] {
	return func(yield func(Axiom) bool) {
		for _, ax := range o.axioms {
			if !yield(ax) {
				return
			}
		}
	}
}

// Len returns the number of axioms.
func (o *Ontology) Len() int { return len(o.axioms) }

// Index returns the ontology's query index, building it if needed. The result
// is invalidated by the next mutation, so hold it only for a burst of queries.
func (o *Ontology) Index() *Index {
	if o.idx == nil {
		o.idx = NewIndex(o)
	}
	return o.idx
}

// --- Queries ----------------------------------------------------------------

// Signature returns every distinct entity referenced anywhere in the ontology,
// sorted by kind then IRI.
func (o *Ontology) Signature() []Entity { return o.Index().Signature() }

// IsDeclared reports whether the ontology declares e.
func (o *Ontology) IsDeclared(e Entity) bool { return o.Index().IsDeclared(e) }

// Classes returns the named classes in the signature, sorted by IRI.
func (o *Ontology) Classes() []Class { return entitiesOfKind[Class](o) }

// ObjectProperties returns the object properties in the signature.
func (o *Ontology) ObjectProperties() []ObjectProperty {
	return entitiesOfKind[ObjectProperty](o)
}

// DataProperties returns the data properties in the signature.
func (o *Ontology) DataProperties() []DataProperty { return entitiesOfKind[DataProperty](o) }

// AnnotationProperties returns the annotation properties in the signature.
func (o *Ontology) AnnotationProperties() []AnnotationProperty {
	return entitiesOfKind[AnnotationProperty](o)
}

// Individuals returns the named individuals in the signature.
func (o *Ontology) Individuals() []NamedIndividual { return entitiesOfKind[NamedIndividual](o) }

// Datatypes returns the datatypes in the signature.
func (o *Ontology) Datatypes() []Datatype { return entitiesOfKind[Datatype](o) }

func entitiesOfKind[T Entity](o *Ontology) []T {
	var out []T
	for _, e := range o.Index().Signature() {
		if t, ok := e.(T); ok {
			out = append(out, t)
		}
	}
	return out
}

// AxiomsReferencing returns every axiom that mentions e, in insertion order.
func (o *Ontology) AxiomsReferencing(e Entity) []Axiom { return o.Index().AxiomsReferencing(e) }

// SuperClassesOf returns the named classes asserted directly above c by
// SubClassOf or EquivalentClasses axioms. It performs no reasoning: only
// asserted, named superclasses are reported.
func (o *Ontology) SuperClassesOf(c Class) []Class { return o.Index().SuperClassesOf(c) }

// SubClassesOf returns the named classes asserted directly below c. Like
// [Ontology.SuperClassesOf] it reports only asserted relationships.
func (o *Ontology) SubClassesOf(c Class) []Class { return o.Index().SubClassesOf(c) }

// AncestorsOf returns the transitive closure of [Ontology.SuperClassesOf],
// excluding c itself. Cycles are handled; still no reasoning.
func (o *Ontology) AncestorsOf(c Class) []Class { return o.Index().AncestorsOf(c) }

// DescendantsOf returns the transitive closure of [Ontology.SubClassesOf],
// excluding c itself.
func (o *Ontology) DescendantsOf(c Class) []Class { return o.Index().DescendantsOf(c) }

// TypesOf returns the class expressions asserted for an individual.
func (o *Ontology) TypesOf(i Individual) []ClassExpression { return o.Index().TypesOf(i) }

// InstancesOf returns the individuals directly asserted to be instances of c.
func (o *Ontology) InstancesOf(c ClassExpression) []Individual { return o.Index().InstancesOf(c) }

// ObjectValues returns the individuals asserted as objects of p for subject i.
func (o *Ontology) ObjectValues(i Individual, p ObjectPropertyExpression) []Individual {
	return o.Index().ObjectValues(i, p)
}

// DataValues returns the literals asserted as values of p for subject i.
func (o *Ontology) DataValues(i Individual, p DataPropertyExpression) []Literal {
	return o.Index().DataValues(i, p)
}

// Label returns the first rdfs:label asserted for e, or "" if none.
func (o *Ontology) Label(e Entity) string { return o.Index().Label(e) }

func sortedClasses(seen map[Class]bool) []Class {
	out := make([]Class, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
