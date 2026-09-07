package owl

import "sort"

// Walk calls yield for every entity referenced by n, including duplicates and
// including entities nested inside class expressions and data ranges. It is
// the basis of [Signature] and [Ontology.AxiomsReferencing].
func Walk(n Node, yield func(Entity)) {
	switch x := n.(type) {
	case nil, IRI, AnonymousIndividual:
		// No entity to report. A bare IRI is not typed as an entity.

	case Class:
		yield(x)
	case Datatype:
		yield(x)
	case ObjectProperty:
		yield(x)
	case DataProperty:
		yield(x)
	case AnnotationProperty:
		yield(x)
	case NamedIndividual:
		yield(x)
	case Literal:
		if x.Datatype != "" {
			yield(x.Datatype)
		}
	case Annotation:
		yield(x.Property)
		Walk(x.Value, yield)
	case FacetRestriction:
		Walk(x.Value, yield)

	// Class expressions.
	case ObjectIntersectionOf:
		walkAll(nodes(x), yield)
	case ObjectUnionOf:
		walkAll(nodes(x), yield)
	case ObjectComplementOf:
		Walk(x.Operand, yield)
	case ObjectOneOf:
		walkAll(nodes(x), yield)
	case ObjectSomeValuesFrom:
		Walk(x.Property, yield)
		Walk(x.Filler, yield)
	case ObjectAllValuesFrom:
		Walk(x.Property, yield)
		Walk(x.Filler, yield)
	case ObjectHasValue:
		Walk(x.Property, yield)
		Walk(x.Value, yield)
	case ObjectHasSelf:
		Walk(x.Property, yield)
	case ObjectMinCardinality:
		Walk(x.Property, yield)
		Walk(x.Filler, yield)
	case ObjectMaxCardinality:
		Walk(x.Property, yield)
		Walk(x.Filler, yield)
	case ObjectExactCardinality:
		Walk(x.Property, yield)
		Walk(x.Filler, yield)
	case DataSomeValuesFrom:
		Walk(x.Property, yield)
		Walk(x.Range, yield)
	case DataAllValuesFrom:
		Walk(x.Property, yield)
		Walk(x.Range, yield)
	case DataHasValue:
		Walk(x.Property, yield)
		Walk(x.Value, yield)
	case DataMinCardinality:
		Walk(x.Property, yield)
		Walk(x.Range, yield)
	case DataMaxCardinality:
		Walk(x.Property, yield)
		Walk(x.Range, yield)
	case DataExactCardinality:
		Walk(x.Property, yield)
		Walk(x.Range, yield)

	// Property expressions and data ranges.
	case ObjectInverseOf:
		yield(x.Property)
	case DataIntersectionOf:
		walkAll(nodes(x), yield)
	case DataUnionOf:
		walkAll(nodes(x), yield)
	case DataComplementOf:
		Walk(x.Operand, yield)
	case DataOneOf:
		walkAll(nodes(x), yield)
	case DatatypeRestriction:
		yield(x.Datatype)
		for _, f := range x.Facets {
			Walk(f, yield)
		}

	// Axioms.
	case Annotated:
		for _, a := range x.Annotations {
			Walk(a, yield)
		}
		Walk(x.Axiom, yield)
	case Declaration:
		if x.Entity != nil {
			yield(x.Entity)
		}
	case SubClassOf:
		Walk(x.Sub, yield)
		Walk(x.Super, yield)
	case EquivalentClasses:
		walkAll(nodes(x), yield)
	case DisjointClasses:
		walkAll(nodes(x), yield)
	case DisjointUnion:
		yield(x.Class)
		walkAll(nodes(x.Operands), yield)
	case SubObjectPropertyOf:
		Walk(x.Sub, yield)
		Walk(x.Super, yield)
	case SubPropertyChainOf:
		walkAll(nodes(x.Chain), yield)
		Walk(x.Super, yield)
	case EquivalentObjectProperties:
		walkAll(nodes(x), yield)
	case DisjointObjectProperties:
		walkAll(nodes(x), yield)
	case InverseObjectProperties:
		Walk(x.First, yield)
		Walk(x.Second, yield)
	case ObjectPropertyDomain:
		Walk(x.Property, yield)
		Walk(x.Domain, yield)
	case ObjectPropertyRange:
		Walk(x.Property, yield)
		Walk(x.Range, yield)
	case FunctionalObjectProperty:
		Walk(x.Property, yield)
	case InverseFunctionalObjectProperty:
		Walk(x.Property, yield)
	case ReflexiveObjectProperty:
		Walk(x.Property, yield)
	case IrreflexiveObjectProperty:
		Walk(x.Property, yield)
	case SymmetricObjectProperty:
		Walk(x.Property, yield)
	case AsymmetricObjectProperty:
		Walk(x.Property, yield)
	case TransitiveObjectProperty:
		Walk(x.Property, yield)
	case SubDataPropertyOf:
		Walk(x.Sub, yield)
		Walk(x.Super, yield)
	case EquivalentDataProperties:
		walkAll(nodes(x), yield)
	case DisjointDataProperties:
		walkAll(nodes(x), yield)
	case DataPropertyDomain:
		Walk(x.Property, yield)
		Walk(x.Domain, yield)
	case DataPropertyRange:
		Walk(x.Property, yield)
		Walk(x.Range, yield)
	case FunctionalDataProperty:
		Walk(x.Property, yield)
	case DatatypeDefinition:
		yield(x.Datatype)
		Walk(x.Range, yield)
	case HasKey:
		Walk(x.Class, yield)
		walkAll(nodes(x.ObjectProperties), yield)
		walkAll(nodes(x.DataProperties), yield)
	case SameIndividual:
		walkAll(nodes(x), yield)
	case DifferentIndividuals:
		walkAll(nodes(x), yield)
	case ClassAssertion:
		Walk(x.Class, yield)
		Walk(x.Individual, yield)
	case ObjectPropertyAssertion:
		Walk(x.Property, yield)
		Walk(x.Subject, yield)
		Walk(x.Object, yield)
	case NegativeObjectPropertyAssertion:
		Walk(x.Property, yield)
		Walk(x.Subject, yield)
		Walk(x.Object, yield)
	case DataPropertyAssertion:
		Walk(x.Property, yield)
		Walk(x.Subject, yield)
		Walk(x.Value, yield)
	case NegativeDataPropertyAssertion:
		Walk(x.Property, yield)
		Walk(x.Subject, yield)
		Walk(x.Value, yield)
	case AnnotationAssertion:
		yield(x.Property)
		Walk(x.Value, yield)
	case SubAnnotationPropertyOf:
		yield(x.Sub)
		yield(x.Super)
	case AnnotationPropertyDomain:
		yield(x.Property)
	case AnnotationPropertyRange:
		yield(x.Property)
	}
}

func walkAll(ns []Node, yield func(Entity)) {
	for _, n := range ns {
		Walk(n, yield)
	}
}

// Signature returns the distinct entities referenced by n, sorted by kind then
// IRI.
func Signature(n Node) []Entity {
	seen := make(map[Entity]bool)
	Walk(n, func(e Entity) { seen[e] = true })
	return sortedEntities(seen)
}

func sortedEntities(seen map[Entity]bool) []Entity {
	out := make([]Entity, 0, len(seen))
	for e := range seen {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind() != out[j].Kind() {
			return out[i].Kind() < out[j].Kind()
		}
		return out[i].IRI() < out[j].IRI()
	})
	return out
}

// References reports whether n mentions the entity e anywhere.
func References(n Node, e Entity) bool {
	found := false
	Walk(n, func(got Entity) {
		if got == e {
			found = true
		}
	})
	return found
}
