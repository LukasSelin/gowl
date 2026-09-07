package owl

import "sort"

// Canonicalization puts constructs into a normal form so that two ontologies
// saying the same thing compare equal. It rewrites only where OWL 2 semantics
// make the rewrite safe:
//
//   - operands of commutative constructs (intersections, unions, enumerations,
//     equivalence and disjointness axioms) are sorted and de-duplicated;
//   - nested intersections and unions are flattened;
//   - a one-operand intersection or union collapses to that operand.
//
// Order is preserved where it carries meaning: the two sides of SubClassOf, the
// links of a property chain, the subject and object of an assertion.
//
// The canonical form is for comparison, not for storage. It is deliberately not
// round-trip faithful, so [Ontology.Functional] renders the axioms as asserted
// rather than as canonicalized.

// CanonicalAxiom returns the canonical form of an axiom.
func CanonicalAxiom(ax Axiom) Axiom {
	switch x := ax.(type) {
	case Annotated:
		return Annotated{Annotations: sortDedupe(x.Annotations), Axiom: CanonicalAxiom(x.Axiom)}

	case SubClassOf:
		return SubClassOf{Sub: CanonicalClassExpression(x.Sub), Super: CanonicalClassExpression(x.Super)}
	case EquivalentClasses:
		return EquivalentClasses(sortDedupe(canonAll(x, CanonicalClassExpression)))
	case DisjointClasses:
		return DisjointClasses(sortDedupe(canonAll(x, CanonicalClassExpression)))
	case DisjointUnion:
		return DisjointUnion{Class: x.Class, Operands: sortDedupe(canonAll(x.Operands, CanonicalClassExpression))}

	case SubObjectPropertyOf:
		return SubObjectPropertyOf{Sub: canonObjectProp(x.Sub), Super: canonObjectProp(x.Super)}
	case SubPropertyChainOf:
		// A chain is a composition: its order is meaning, not notation.
		return SubPropertyChainOf{Chain: canonAll(x.Chain, canonObjectProp), Super: canonObjectProp(x.Super)}
	case EquivalentObjectProperties:
		return EquivalentObjectProperties(sortDedupe(canonAll(x, canonObjectProp)))
	case DisjointObjectProperties:
		return DisjointObjectProperties(sortDedupe(canonAll(x, canonObjectProp)))
	case InverseObjectProperties:
		// Inverse is symmetric, so the pair has no inherent order.
		pair := sortDedupe([]ObjectPropertyExpression{canonObjectProp(x.First), canonObjectProp(x.Second)})
		if len(pair) == 1 {
			return InverseObjectProperties{First: pair[0], Second: pair[0]}
		}
		return InverseObjectProperties{First: pair[0], Second: pair[1]}
	case ObjectPropertyDomain:
		return ObjectPropertyDomain{Property: canonObjectProp(x.Property), Domain: CanonicalClassExpression(x.Domain)}
	case ObjectPropertyRange:
		return ObjectPropertyRange{Property: canonObjectProp(x.Property), Range: CanonicalClassExpression(x.Range)}
	case FunctionalObjectProperty:
		return FunctionalObjectProperty{Property: canonObjectProp(x.Property)}
	case InverseFunctionalObjectProperty:
		return InverseFunctionalObjectProperty{Property: canonObjectProp(x.Property)}
	case ReflexiveObjectProperty:
		return ReflexiveObjectProperty{Property: canonObjectProp(x.Property)}
	case IrreflexiveObjectProperty:
		return IrreflexiveObjectProperty{Property: canonObjectProp(x.Property)}
	case SymmetricObjectProperty:
		return SymmetricObjectProperty{Property: canonObjectProp(x.Property)}
	case AsymmetricObjectProperty:
		return AsymmetricObjectProperty{Property: canonObjectProp(x.Property)}
	case TransitiveObjectProperty:
		return TransitiveObjectProperty{Property: canonObjectProp(x.Property)}

	case EquivalentDataProperties:
		return EquivalentDataProperties(sortDedupe(x))
	case DisjointDataProperties:
		return DisjointDataProperties(sortDedupe(x))
	case DataPropertyDomain:
		return DataPropertyDomain{Property: x.Property, Domain: CanonicalClassExpression(x.Domain)}
	case DataPropertyRange:
		return DataPropertyRange{Property: x.Property, Range: canonDataRange(x.Range)}
	case DatatypeDefinition:
		return DatatypeDefinition{Datatype: x.Datatype, Range: canonDataRange(x.Range)}

	case HasKey:
		return HasKey{
			Class:            CanonicalClassExpression(x.Class),
			ObjectProperties: sortDedupe(canonAll(x.ObjectProperties, canonObjectProp)),
			DataProperties:   sortDedupe(x.DataProperties),
		}

	case SameIndividual:
		return SameIndividual(sortDedupe(x))
	case DifferentIndividuals:
		return DifferentIndividuals(sortDedupe(x))
	case ClassAssertion:
		return ClassAssertion{Class: CanonicalClassExpression(x.Class), Individual: x.Individual}
	case ObjectPropertyAssertion:
		return ObjectPropertyAssertion{Property: canonObjectProp(x.Property), Subject: x.Subject, Object: x.Object}
	case NegativeObjectPropertyAssertion:
		return NegativeObjectPropertyAssertion{Property: canonObjectProp(x.Property), Subject: x.Subject, Object: x.Object}
	}
	// Declarations, data property inclusions and annotation axioms have no
	// commutative operands to normalize.
	return ax
}

// CanonicalClassExpression returns the canonical form of a class expression.
func CanonicalClassExpression(ce ClassExpression) ClassExpression {
	switch x := ce.(type) {
	case ObjectIntersectionOf:
		ops := sortDedupe(flattenIntersection(x))
		if len(ops) == 1 {
			return ops[0]
		}
		return ObjectIntersectionOf(ops)
	case ObjectUnionOf:
		ops := sortDedupe(flattenUnion(x))
		if len(ops) == 1 {
			return ops[0]
		}
		return ObjectUnionOf(ops)
	case ObjectComplementOf:
		return ObjectComplementOf{Operand: CanonicalClassExpression(x.Operand)}
	case ObjectOneOf:
		return ObjectOneOf(sortDedupe(x))
	case ObjectSomeValuesFrom:
		return ObjectSomeValuesFrom{Property: canonObjectProp(x.Property), Filler: CanonicalClassExpression(x.Filler)}
	case ObjectAllValuesFrom:
		return ObjectAllValuesFrom{Property: canonObjectProp(x.Property), Filler: CanonicalClassExpression(x.Filler)}
	case ObjectHasValue:
		return ObjectHasValue{Property: canonObjectProp(x.Property), Value: x.Value}
	case ObjectHasSelf:
		return ObjectHasSelf{Property: canonObjectProp(x.Property)}
	case ObjectMinCardinality:
		return ObjectMinCardinality{N: x.N, Property: canonObjectProp(x.Property), Filler: canonFiller(x.Filler)}
	case ObjectMaxCardinality:
		return ObjectMaxCardinality{N: x.N, Property: canonObjectProp(x.Property), Filler: canonFiller(x.Filler)}
	case ObjectExactCardinality:
		return ObjectExactCardinality{N: x.N, Property: canonObjectProp(x.Property), Filler: canonFiller(x.Filler)}
	case DataSomeValuesFrom:
		return DataSomeValuesFrom{Property: x.Property, Range: canonDataRange(x.Range)}
	case DataAllValuesFrom:
		return DataAllValuesFrom{Property: x.Property, Range: canonDataRange(x.Range)}
	case DataMinCardinality:
		return DataMinCardinality{N: x.N, Property: x.Property, Range: canonDataRangeOrNil(x.Range)}
	case DataMaxCardinality:
		return DataMaxCardinality{N: x.N, Property: x.Property, Range: canonDataRangeOrNil(x.Range)}
	case DataExactCardinality:
		return DataExactCardinality{N: x.N, Property: x.Property, Range: canonDataRangeOrNil(x.Range)}
	}
	return ce
}

func canonFiller(ce ClassExpression) ClassExpression {
	if ce == nil {
		return nil
	}
	return CanonicalClassExpression(ce)
}

func canonDataRangeOrNil(r DataRange) DataRange {
	if r == nil {
		return nil
	}
	return canonDataRange(r)
}

func canonDataRange(r DataRange) DataRange {
	switch x := r.(type) {
	case DataIntersectionOf:
		ops := sortDedupe(canonAll(x, canonDataRange))
		if len(ops) == 1 {
			return ops[0]
		}
		return DataIntersectionOf(ops)
	case DataUnionOf:
		ops := sortDedupe(canonAll(x, canonDataRange))
		if len(ops) == 1 {
			return ops[0]
		}
		return DataUnionOf(ops)
	case DataComplementOf:
		return DataComplementOf{Operand: canonDataRange(x.Operand)}
	case DataOneOf:
		return DataOneOf(sortDedupe(x))
	case DatatypeRestriction:
		return DatatypeRestriction{Datatype: x.Datatype, Facets: sortDedupe(x.Facets)}
	}
	return r
}

// canonObjectProp normalizes a property expression. ObjectInverseOf wraps a
// named property, so there is nothing below it to rewrite.
func canonObjectProp(p ObjectPropertyExpression) ObjectPropertyExpression { return p }

func flattenIntersection(in ObjectIntersectionOf) []ClassExpression {
	var out []ClassExpression
	for _, op := range in {
		switch c := CanonicalClassExpression(op).(type) {
		case ObjectIntersectionOf:
			out = append(out, c...)
		default:
			out = append(out, c)
		}
	}
	return out
}

func flattenUnion(in ObjectUnionOf) []ClassExpression {
	var out []ClassExpression
	for _, op := range in {
		switch c := CanonicalClassExpression(op).(type) {
		case ObjectUnionOf:
			out = append(out, c...)
		default:
			out = append(out, c)
		}
	}
	return out
}

func canonAll[T Node](in []T, f func(T) T) []T {
	out := make([]T, len(in))
	for i, v := range in {
		out[i] = f(v)
	}
	return out
}

// sortDedupe orders constructs by their rendering and drops duplicates. Keys
// are rendered once up front rather than inside the comparison, which would
// re-render on every call.
func sortDedupe[T Node](in []T) []T {
	if len(in) < 2 {
		return in
	}
	type keyed struct {
		key string
		val T
	}
	ks := make([]keyed, len(in))
	for i, v := range in {
		ks[i] = keyed{key: Functional(v), val: v}
	}
	sort.Slice(ks, func(i, j int) bool { return ks[i].key < ks[j].key })

	out := make([]T, 0, len(ks))
	for i, e := range ks {
		if i == 0 || e.key != ks[i-1].key {
			out = append(out, e.val)
		}
	}
	return out
}

// CanonicalKey returns a string that is equal for two constructs exactly when
// their canonical forms are equal. It is the key used to compare axioms in
// [DiffOntologies].
func CanonicalKey(ax Axiom) string { return Functional(CanonicalAxiom(ax)) }
