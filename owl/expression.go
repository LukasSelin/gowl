package owl

// ClassExpression denotes a set of individuals. A named [Class] is the
// simplest one; the rest are built with the constructors in this file.
type ClassExpression interface {
	Node
	isClassExpression()
}

// ObjectPropertyExpression is an [ObjectProperty] or its [ObjectInverseOf].
type ObjectPropertyExpression interface {
	Node
	isObjectPropertyExpression()
}

// DataPropertyExpression is a [DataProperty]. OWL 2 defines no other form; the
// interface exists so signatures stay symmetric with object properties.
type DataPropertyExpression interface {
	Node
	isDataPropertyExpression()
}

// DataRange denotes a set of literals.
type DataRange interface {
	Node
	isDataRange()
}

// Individual is a [NamedIndividual] or an [AnonymousIndividual].
type Individual interface {
	Node
	isIndividual()
}

// AnnotationValue is the object of an annotation: an [IRI], a [Literal] or an
// [AnonymousIndividual].
type AnnotationValue interface {
	Node
	isAnnotationValue()
}

// Boolean class expressions.
type (
	// ObjectIntersectionOf is the conjunction of its operands.
	ObjectIntersectionOf []ClassExpression
	// ObjectUnionOf is the disjunction of its operands.
	ObjectUnionOf []ClassExpression
	// ObjectComplementOf is the negation of a class expression.
	ObjectComplementOf struct{ Operand ClassExpression }
	// ObjectOneOf is an enumeration of individuals.
	ObjectOneOf []Individual
)

// Object property restrictions.
type (
	// ObjectSomeValuesFrom is an existential restriction.
	ObjectSomeValuesFrom struct {
		Property ObjectPropertyExpression
		Filler   ClassExpression
	}
	// ObjectAllValuesFrom is a universal restriction.
	ObjectAllValuesFrom struct {
		Property ObjectPropertyExpression
		Filler   ClassExpression
	}
	// ObjectHasValue restricts a property to one individual.
	ObjectHasValue struct {
		Property ObjectPropertyExpression
		Value    Individual
	}
	// ObjectHasSelf holds of individuals related to themselves by Property.
	ObjectHasSelf struct{ Property ObjectPropertyExpression }
)

// Object property cardinality restrictions. A nil Filler makes the restriction
// unqualified.
type (
	ObjectMinCardinality struct {
		N        int
		Property ObjectPropertyExpression
		Filler   ClassExpression
	}
	ObjectMaxCardinality struct {
		N        int
		Property ObjectPropertyExpression
		Filler   ClassExpression
	}
	ObjectExactCardinality struct {
		N        int
		Property ObjectPropertyExpression
		Filler   ClassExpression
	}
)

// Data property restrictions. A nil Range makes a cardinality restriction
// unqualified.
type (
	DataSomeValuesFrom struct {
		Property DataPropertyExpression
		Range    DataRange
	}
	DataAllValuesFrom struct {
		Property DataPropertyExpression
		Range    DataRange
	}
	DataHasValue struct {
		Property DataPropertyExpression
		Value    Literal
	}
	DataMinCardinality struct {
		N        int
		Property DataPropertyExpression
		Range    DataRange
	}
	DataMaxCardinality struct {
		N        int
		Property DataPropertyExpression
		Range    DataRange
	}
	DataExactCardinality struct {
		N        int
		Property DataPropertyExpression
		Range    DataRange
	}
)

// ObjectInverseOf denotes the inverse of a named object property.
type ObjectInverseOf struct{ Property ObjectProperty }

// Data ranges.
type (
	DataIntersectionOf []DataRange
	DataUnionOf        []DataRange
	DataComplementOf   struct{ Operand DataRange }
	DataOneOf          []Literal

	// DatatypeRestriction narrows a datatype with facets, e.g. xsd:integer
	// restricted to minInclusive 0.
	DatatypeRestriction struct {
		Datatype Datatype
		Facets   []FacetRestriction
	}
)

// FacetRestriction constrains a datatype by one facet, such as
// [FacetMinInclusive].
type FacetRestriction struct {
	Facet IRI
	Value Literal
}

func (f FacetRestriction) String() string { return Functional(f) }

// Annotation attaches a value to a subject via an annotation property.
type Annotation struct {
	Property AnnotationProperty
	Value    AnnotationValue
}

func (a Annotation) String() string { return Functional(a) }

func (ObjectIntersectionOf) isClassExpression()   {}
func (ObjectUnionOf) isClassExpression()          {}
func (ObjectComplementOf) isClassExpression()     {}
func (ObjectOneOf) isClassExpression()            {}
func (ObjectSomeValuesFrom) isClassExpression()   {}
func (ObjectAllValuesFrom) isClassExpression()    {}
func (ObjectHasValue) isClassExpression()         {}
func (ObjectHasSelf) isClassExpression()          {}
func (ObjectMinCardinality) isClassExpression()   {}
func (ObjectMaxCardinality) isClassExpression()   {}
func (ObjectExactCardinality) isClassExpression() {}
func (DataSomeValuesFrom) isClassExpression()     {}
func (DataAllValuesFrom) isClassExpression()      {}
func (DataHasValue) isClassExpression()           {}
func (DataMinCardinality) isClassExpression()     {}
func (DataMaxCardinality) isClassExpression()     {}
func (DataExactCardinality) isClassExpression()   {}

func (ObjectInverseOf) isObjectPropertyExpression() {}

func (DataIntersectionOf) isDataRange()  {}
func (DataUnionOf) isDataRange()         {}
func (DataComplementOf) isDataRange()    {}
func (DataOneOf) isDataRange()           {}
func (DatatypeRestriction) isDataRange() {}

func (x ObjectIntersectionOf) String() string   { return Functional(x) }
func (x ObjectUnionOf) String() string          { return Functional(x) }
func (x ObjectComplementOf) String() string     { return Functional(x) }
func (x ObjectOneOf) String() string            { return Functional(x) }
func (x ObjectSomeValuesFrom) String() string   { return Functional(x) }
func (x ObjectAllValuesFrom) String() string    { return Functional(x) }
func (x ObjectHasValue) String() string         { return Functional(x) }
func (x ObjectHasSelf) String() string          { return Functional(x) }
func (x ObjectMinCardinality) String() string   { return Functional(x) }
func (x ObjectMaxCardinality) String() string   { return Functional(x) }
func (x ObjectExactCardinality) String() string { return Functional(x) }
func (x DataSomeValuesFrom) String() string     { return Functional(x) }
func (x DataAllValuesFrom) String() string      { return Functional(x) }
func (x DataHasValue) String() string           { return Functional(x) }
func (x DataMinCardinality) String() string     { return Functional(x) }
func (x DataMaxCardinality) String() string     { return Functional(x) }
func (x DataExactCardinality) String() string   { return Functional(x) }
func (x ObjectInverseOf) String() string        { return Functional(x) }
func (x DataIntersectionOf) String() string     { return Functional(x) }
func (x DataUnionOf) String() string            { return Functional(x) }
func (x DataComplementOf) String() string       { return Functional(x) }
func (x DataOneOf) String() string              { return Functional(x) }
func (x DatatypeRestriction) String() string    { return Functional(x) }

// --- Shorthand constructors -------------------------------------------------
//
// These read closer to description logic than the struct literals do:
//
//	owl.And(Pizza, owl.Some(hasTopping, Mozzarella))

// And builds an intersection.
func And(operands ...ClassExpression) ObjectIntersectionOf { return ObjectIntersectionOf(operands) }

// Or builds a union.
func Or(operands ...ClassExpression) ObjectUnionOf { return ObjectUnionOf(operands) }

// Not builds a complement.
func Not(operand ClassExpression) ObjectComplementOf {
	return ObjectComplementOf{Operand: operand}
}

// OneOf builds an enumeration of individuals.
func OneOf(individuals ...Individual) ObjectOneOf { return ObjectOneOf(individuals) }

// Some builds an existential restriction: ∃ p.filler.
func Some(p ObjectPropertyExpression, filler ClassExpression) ObjectSomeValuesFrom {
	return ObjectSomeValuesFrom{Property: p, Filler: filler}
}

// Only builds a universal restriction: ∀ p.filler.
func Only(p ObjectPropertyExpression, filler ClassExpression) ObjectAllValuesFrom {
	return ObjectAllValuesFrom{Property: p, Filler: filler}
}

// HasValue builds a restriction to a single individual.
func HasValue(p ObjectPropertyExpression, v Individual) ObjectHasValue {
	return ObjectHasValue{Property: p, Value: v}
}

// Self builds a self-restriction.
func Self(p ObjectPropertyExpression) ObjectHasSelf { return ObjectHasSelf{Property: p} }

// Min builds a minimum cardinality restriction, qualified when a filler is
// supplied.
func Min(n int, p ObjectPropertyExpression, filler ...ClassExpression) ObjectMinCardinality {
	return ObjectMinCardinality{N: n, Property: p, Filler: optional(filler)}
}

// Max builds a maximum cardinality restriction, qualified when a filler is
// supplied.
func Max(n int, p ObjectPropertyExpression, filler ...ClassExpression) ObjectMaxCardinality {
	return ObjectMaxCardinality{N: n, Property: p, Filler: optional(filler)}
}

// Exactly builds an exact cardinality restriction, qualified when a filler is
// supplied.
func Exactly(n int, p ObjectPropertyExpression, filler ...ClassExpression) ObjectExactCardinality {
	return ObjectExactCardinality{N: n, Property: p, Filler: optional(filler)}
}

// Inverse builds the inverse of a named object property.
func Inverse(p ObjectProperty) ObjectInverseOf { return ObjectInverseOf{Property: p} }

// DataSome builds an existential restriction over a data property.
func DataSome(p DataPropertyExpression, r DataRange) DataSomeValuesFrom {
	return DataSomeValuesFrom{Property: p, Range: r}
}

// DataOnly builds a universal restriction over a data property.
func DataOnly(p DataPropertyExpression, r DataRange) DataAllValuesFrom {
	return DataAllValuesFrom{Property: p, Range: r}
}

// DataValue builds a restriction of a data property to one literal.
func DataValue(p DataPropertyExpression, v Literal) DataHasValue {
	return DataHasValue{Property: p, Value: v}
}

// Restrict narrows a datatype with facets, e.g.
//
//	owl.Restrict(owl.XSDInteger, owl.Facet(owl.FacetMinInclusive, owl.Int(0)))
func Restrict(dt Datatype, facets ...FacetRestriction) DatatypeRestriction {
	return DatatypeRestriction{Datatype: dt, Facets: facets}
}

// Facet builds a single facet restriction.
func Facet(facet IRI, value Literal) FacetRestriction {
	return FacetRestriction{Facet: facet, Value: value}
}

func optional[T any](vals []T) T {
	var zero T
	if len(vals) == 0 {
		return zero
	}
	return vals[0]
}
