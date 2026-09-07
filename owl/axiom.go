package owl

// Axiom is a statement asserted by an ontology. Axioms are values, so they can
// be compared, copied and stored freely; slice-shaped axioms such as
// [EquivalentClasses] are compared by rendering rather than with ==.
//
// Axiom annotations (Annotation(...) inside an axiom) are not yet modelled;
// use [AnnotationAssertion] on the entity instead.
type Axiom interface {
	Node
	isAxiom()
}

// Declaration introduces an entity into the ontology's signature.
type Declaration struct{ Entity Entity }

// Class axioms.
type (
	// SubClassOf asserts that every instance of Sub is an instance of Super.
	SubClassOf struct{ Sub, Super ClassExpression }
	// EquivalentClasses asserts that all operands denote the same set.
	EquivalentClasses []ClassExpression
	// DisjointClasses asserts that the operands share no instances.
	DisjointClasses []ClassExpression
	// DisjointUnion asserts Class is exactly the union of pairwise disjoint
	// operands.
	DisjointUnion struct {
		Class    Class
		Operands []ClassExpression
	}
)

// Object property axioms.
type (
	SubObjectPropertyOf struct{ Sub, Super ObjectPropertyExpression }
	// SubPropertyChainOf asserts that composing Chain implies Super.
	SubPropertyChainOf struct {
		Chain []ObjectPropertyExpression
		Super ObjectPropertyExpression
	}
	EquivalentObjectProperties []ObjectPropertyExpression
	DisjointObjectProperties   []ObjectPropertyExpression
	InverseObjectProperties    struct{ First, Second ObjectPropertyExpression }
	ObjectPropertyDomain       struct {
		Property ObjectPropertyExpression
		Domain   ClassExpression
	}
	ObjectPropertyRange struct {
		Property ObjectPropertyExpression
		Range    ClassExpression
	}
	FunctionalObjectProperty        struct{ Property ObjectPropertyExpression }
	InverseFunctionalObjectProperty struct{ Property ObjectPropertyExpression }
	ReflexiveObjectProperty         struct{ Property ObjectPropertyExpression }
	IrreflexiveObjectProperty       struct{ Property ObjectPropertyExpression }
	SymmetricObjectProperty         struct{ Property ObjectPropertyExpression }
	AsymmetricObjectProperty        struct{ Property ObjectPropertyExpression }
	TransitiveObjectProperty        struct{ Property ObjectPropertyExpression }
)

// Data property axioms.
type (
	SubDataPropertyOf        struct{ Sub, Super DataPropertyExpression }
	EquivalentDataProperties []DataPropertyExpression
	DisjointDataProperties   []DataPropertyExpression
	DataPropertyDomain       struct {
		Property DataPropertyExpression
		Domain   ClassExpression
	}
	DataPropertyRange struct {
		Property DataPropertyExpression
		Range    DataRange
	}
	FunctionalDataProperty struct{ Property DataPropertyExpression }
	// DatatypeDefinition names a data range.
	DatatypeDefinition struct {
		Datatype Datatype
		Range    DataRange
	}
)

// HasKey asserts that the listed properties uniquely identify instances of
// Class.
type HasKey struct {
	Class            ClassExpression
	ObjectProperties []ObjectPropertyExpression
	DataProperties   []DataPropertyExpression
}

// Assertions about individuals.
type (
	SameIndividual       []Individual
	DifferentIndividuals []Individual
	ClassAssertion       struct {
		Class      ClassExpression
		Individual Individual
	}
	ObjectPropertyAssertion struct {
		Property        ObjectPropertyExpression
		Subject, Object Individual
	}
	NegativeObjectPropertyAssertion struct {
		Property        ObjectPropertyExpression
		Subject, Object Individual
	}
	DataPropertyAssertion struct {
		Property DataPropertyExpression
		Subject  Individual
		Value    Literal
	}
	NegativeDataPropertyAssertion struct {
		Property DataPropertyExpression
		Subject  Individual
		Value    Literal
	}
)

// Annotation axioms.
type (
	// AnnotationAssertion attaches an annotation to any IRI.
	AnnotationAssertion struct {
		Property AnnotationProperty
		Subject  IRI
		Value    AnnotationValue
	}
	SubAnnotationPropertyOf  struct{ Sub, Super AnnotationProperty }
	AnnotationPropertyDomain struct {
		Property AnnotationProperty
		Domain   IRI
	}
	AnnotationPropertyRange struct {
		Property AnnotationProperty
		Range    IRI
	}
)

func (Declaration) isAxiom()                     {}
func (SubClassOf) isAxiom()                      {}
func (EquivalentClasses) isAxiom()               {}
func (DisjointClasses) isAxiom()                 {}
func (DisjointUnion) isAxiom()                   {}
func (SubObjectPropertyOf) isAxiom()             {}
func (SubPropertyChainOf) isAxiom()              {}
func (EquivalentObjectProperties) isAxiom()      {}
func (DisjointObjectProperties) isAxiom()        {}
func (InverseObjectProperties) isAxiom()         {}
func (ObjectPropertyDomain) isAxiom()            {}
func (ObjectPropertyRange) isAxiom()             {}
func (FunctionalObjectProperty) isAxiom()        {}
func (InverseFunctionalObjectProperty) isAxiom() {}
func (ReflexiveObjectProperty) isAxiom()         {}
func (IrreflexiveObjectProperty) isAxiom()       {}
func (SymmetricObjectProperty) isAxiom()         {}
func (AsymmetricObjectProperty) isAxiom()        {}
func (TransitiveObjectProperty) isAxiom()        {}
func (SubDataPropertyOf) isAxiom()               {}
func (EquivalentDataProperties) isAxiom()        {}
func (DisjointDataProperties) isAxiom()          {}
func (DataPropertyDomain) isAxiom()              {}
func (DataPropertyRange) isAxiom()               {}
func (FunctionalDataProperty) isAxiom()          {}
func (DatatypeDefinition) isAxiom()              {}
func (HasKey) isAxiom()                          {}
func (SameIndividual) isAxiom()                  {}
func (DifferentIndividuals) isAxiom()            {}
func (ClassAssertion) isAxiom()                  {}
func (ObjectPropertyAssertion) isAxiom()         {}
func (NegativeObjectPropertyAssertion) isAxiom() {}
func (DataPropertyAssertion) isAxiom()           {}
func (NegativeDataPropertyAssertion) isAxiom()   {}
func (AnnotationAssertion) isAxiom()             {}
func (SubAnnotationPropertyOf) isAxiom()         {}
func (AnnotationPropertyDomain) isAxiom()        {}
func (AnnotationPropertyRange) isAxiom()         {}

func (x Declaration) String() string                     { return Functional(x) }
func (x SubClassOf) String() string                      { return Functional(x) }
func (x EquivalentClasses) String() string               { return Functional(x) }
func (x DisjointClasses) String() string                 { return Functional(x) }
func (x DisjointUnion) String() string                   { return Functional(x) }
func (x SubObjectPropertyOf) String() string             { return Functional(x) }
func (x SubPropertyChainOf) String() string              { return Functional(x) }
func (x EquivalentObjectProperties) String() string      { return Functional(x) }
func (x DisjointObjectProperties) String() string        { return Functional(x) }
func (x InverseObjectProperties) String() string         { return Functional(x) }
func (x ObjectPropertyDomain) String() string            { return Functional(x) }
func (x ObjectPropertyRange) String() string             { return Functional(x) }
func (x FunctionalObjectProperty) String() string        { return Functional(x) }
func (x InverseFunctionalObjectProperty) String() string { return Functional(x) }
func (x ReflexiveObjectProperty) String() string         { return Functional(x) }
func (x IrreflexiveObjectProperty) String() string       { return Functional(x) }
func (x SymmetricObjectProperty) String() string         { return Functional(x) }
func (x AsymmetricObjectProperty) String() string        { return Functional(x) }
func (x TransitiveObjectProperty) String() string        { return Functional(x) }
func (x SubDataPropertyOf) String() string               { return Functional(x) }
func (x EquivalentDataProperties) String() string        { return Functional(x) }
func (x DisjointDataProperties) String() string          { return Functional(x) }
func (x DataPropertyDomain) String() string              { return Functional(x) }
func (x DataPropertyRange) String() string               { return Functional(x) }
func (x FunctionalDataProperty) String() string          { return Functional(x) }
func (x DatatypeDefinition) String() string              { return Functional(x) }
func (x HasKey) String() string                          { return Functional(x) }
func (x SameIndividual) String() string                  { return Functional(x) }
func (x DifferentIndividuals) String() string            { return Functional(x) }
func (x ClassAssertion) String() string                  { return Functional(x) }
func (x ObjectPropertyAssertion) String() string         { return Functional(x) }
func (x NegativeObjectPropertyAssertion) String() string { return Functional(x) }
func (x DataPropertyAssertion) String() string           { return Functional(x) }
func (x NegativeDataPropertyAssertion) String() string   { return Functional(x) }
func (x AnnotationAssertion) String() string             { return Functional(x) }
func (x SubAnnotationPropertyOf) String() string         { return Functional(x) }
func (x AnnotationPropertyDomain) String() string        { return Functional(x) }
func (x AnnotationPropertyRange) String() string         { return Functional(x) }
