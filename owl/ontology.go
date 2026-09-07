package owl

import "sort"

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
	return o
}

// Declare adds a Declaration for each entity.
func (o *Ontology) Declare(entities ...Entity) *Ontology {
	for _, e := range entities {
		o.axioms = append(o.axioms, Declaration{Entity: e})
	}
	return o
}

// Axioms returns the ontology's axioms in insertion order. The slice aliases
// internal storage; copy it before modifying.
func (o *Ontology) Axioms() []Axiom { return o.axioms }

// Len returns the number of axioms.
func (o *Ontology) Len() int { return len(o.axioms) }

// --- Queries ----------------------------------------------------------------

// Signature returns every distinct entity referenced anywhere in the ontology,
// sorted by kind then IRI.
func (o *Ontology) Signature() []Entity {
	seen := make(map[Entity]bool)
	for _, ax := range o.axioms {
		Walk(ax, func(e Entity) { seen[e] = true })
	}
	for _, a := range o.Annotations {
		Walk(a, func(e Entity) { seen[e] = true })
	}
	return sortedEntities(seen)
}

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
	for _, e := range o.Signature() {
		if t, ok := e.(T); ok {
			out = append(out, t)
		}
	}
	return out
}

// AxiomsReferencing returns every axiom that mentions e, in insertion order.
func (o *Ontology) AxiomsReferencing(e Entity) []Axiom {
	var out []Axiom
	for _, ax := range o.axioms {
		if References(ax, e) {
			out = append(out, ax)
		}
	}
	return out
}

// SuperClassesOf returns the named classes asserted directly above c by
// SubClassOf or EquivalentClasses axioms. It performs no reasoning: only
// asserted, named superclasses are reported.
func (o *Ontology) SuperClassesOf(c Class) []Class {
	seen := make(map[Class]bool)
	for _, ax := range o.axioms {
		switch x := ax.(type) {
		case SubClassOf:
			if sub, ok := x.Sub.(Class); ok && sub == c {
				if super, ok := x.Super.(Class); ok {
					seen[super] = true
				}
			}
		case EquivalentClasses:
			if containsClass(x, c) {
				for _, ce := range x {
					if other, ok := ce.(Class); ok && other != c {
						seen[other] = true
					}
				}
			}
		}
	}
	return sortedClasses(seen)
}

// SubClassesOf returns the named classes asserted directly below c. Like
// [Ontology.SuperClassesOf] it reports only asserted relationships.
func (o *Ontology) SubClassesOf(c Class) []Class {
	seen := make(map[Class]bool)
	for _, ax := range o.axioms {
		switch x := ax.(type) {
		case SubClassOf:
			if super, ok := x.Super.(Class); ok && super == c {
				if sub, ok := x.Sub.(Class); ok {
					seen[sub] = true
				}
			}
		case EquivalentClasses:
			if containsClass(x, c) {
				for _, ce := range x {
					if other, ok := ce.(Class); ok && other != c {
						seen[other] = true
					}
				}
			}
		}
	}
	return sortedClasses(seen)
}

// AncestorsOf returns the transitive closure of [Ontology.SuperClassesOf],
// excluding c itself. Cycles are handled; still no reasoning.
func (o *Ontology) AncestorsOf(c Class) []Class {
	seen := make(map[Class]bool)
	var visit func(Class)
	visit = func(cur Class) {
		for _, super := range o.SuperClassesOf(cur) {
			if super == c || seen[super] {
				continue
			}
			seen[super] = true
			visit(super)
		}
	}
	visit(c)
	return sortedClasses(seen)
}

// DescendantsOf returns the transitive closure of [Ontology.SubClassesOf],
// excluding c itself.
func (o *Ontology) DescendantsOf(c Class) []Class {
	seen := make(map[Class]bool)
	var visit func(Class)
	visit = func(cur Class) {
		for _, sub := range o.SubClassesOf(cur) {
			if sub == c || seen[sub] {
				continue
			}
			seen[sub] = true
			visit(sub)
		}
	}
	visit(c)
	return sortedClasses(seen)
}

// TypesOf returns the class expressions asserted for an individual.
func (o *Ontology) TypesOf(i Individual) []ClassExpression {
	var out []ClassExpression
	for _, ax := range o.axioms {
		if a, ok := ax.(ClassAssertion); ok && a.Individual == i {
			out = append(out, a.Class)
		}
	}
	return out
}

// InstancesOf returns the individuals directly asserted to be instances of c.
func (o *Ontology) InstancesOf(c ClassExpression) []Individual {
	var out []Individual
	for _, ax := range o.axioms {
		if a, ok := ax.(ClassAssertion); ok && Equal(a.Class, c) {
			out = append(out, a.Individual)
		}
	}
	return out
}

// ObjectValues returns the individuals asserted as objects of p for subject i.
func (o *Ontology) ObjectValues(i Individual, p ObjectPropertyExpression) []Individual {
	var out []Individual
	for _, ax := range o.axioms {
		if a, ok := ax.(ObjectPropertyAssertion); ok && a.Subject == i && Equal(a.Property, p) {
			out = append(out, a.Object)
		}
	}
	return out
}

// DataValues returns the literals asserted as values of p for subject i.
func (o *Ontology) DataValues(i Individual, p DataPropertyExpression) []Literal {
	var out []Literal
	for _, ax := range o.axioms {
		if a, ok := ax.(DataPropertyAssertion); ok && a.Subject == i && Equal(a.Property, p) {
			out = append(out, a.Value)
		}
	}
	return out
}

// Label returns the first rdfs:label asserted for e, or "" if none.
func (o *Ontology) Label(e Entity) string {
	for _, ax := range o.axioms {
		a, ok := ax.(AnnotationAssertion)
		if !ok || a.Property != RDFSLabel || a.Subject != e.IRI() {
			continue
		}
		if l, ok := a.Value.(Literal); ok {
			return l.Value
		}
	}
	return ""
}

func containsClass(ces []ClassExpression, c Class) bool {
	for _, ce := range ces {
		if other, ok := ce.(Class); ok && other == c {
			return true
		}
	}
	return false
}

func sortedClasses(seen map[Class]bool) []Class {
	out := make([]Class, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
