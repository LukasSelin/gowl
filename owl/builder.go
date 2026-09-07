package owl

// This file holds the fluent builder layer. Everything here is sugar over
// [Ontology.Add]: each method appends ordinary axioms, so a builder chain and a
// hand-written slice of axioms produce identical ontologies.

// Define declares c and returns a builder for axioms about it.
//
//	o.Define(pizza).
//		SubClassOf(owl.Some(hasTopping, topping)).
//		Label("Pizza")
func (o *Ontology) Define(c Class) *ClassBuilder {
	o.Declare(c)
	return &ClassBuilder{o: o, c: c}
}

// ClassBuilder accumulates axioms about one class.
type ClassBuilder struct {
	o *Ontology
	c Class
}

// Class returns the class being built.
func (b *ClassBuilder) Class() Class { return b.c }

// SubClassOf asserts one SubClassOf axiom per operand.
func (b *ClassBuilder) SubClassOf(supers ...ClassExpression) *ClassBuilder {
	for _, s := range supers {
		b.o.Add(SubClassOf{Sub: b.c, Super: s})
	}
	return b
}

// EquivalentTo asserts that the class is equivalent to each operand.
func (b *ClassBuilder) EquivalentTo(others ...ClassExpression) *ClassBuilder {
	for _, other := range others {
		b.o.Add(EquivalentClasses{b.c, other})
	}
	return b
}

// DisjointWith asserts that the class shares no instances with the operands.
func (b *ClassBuilder) DisjointWith(others ...ClassExpression) *ClassBuilder {
	if len(others) > 0 {
		b.o.Add(DisjointClasses(append([]ClassExpression{b.c}, others...)))
	}
	return b
}

// DisjointUnionOf asserts that the class is exactly the union of pairwise
// disjoint operands.
func (b *ClassBuilder) DisjointUnionOf(operands ...ClassExpression) *ClassBuilder {
	b.o.Add(DisjointUnion{Class: b.c, Operands: operands})
	return b
}

// Key asserts that the given object properties uniquely identify instances.
func (b *ClassBuilder) Key(props ...ObjectPropertyExpression) *ClassBuilder {
	b.o.Add(HasKey{Class: b.c, ObjectProperties: props})
	return b
}

// Label attaches an rdfs:label. Pass a language tag to make it language-tagged.
func (b *ClassBuilder) Label(text string, lang ...string) *ClassBuilder {
	b.o.Add(labelAxiom(IRI(b.c), text, lang))
	return b
}

// Comment attaches an rdfs:comment.
func (b *ClassBuilder) Comment(text string, lang ...string) *ClassBuilder {
	b.o.Add(commentAxiom(IRI(b.c), text, lang))
	return b
}

// Annotate attaches an arbitrary annotation.
func (b *ClassBuilder) Annotate(p AnnotationProperty, v AnnotationValue) *ClassBuilder {
	b.o.Add(AnnotationAssertion{Property: p, Subject: IRI(b.c), Value: v})
	return b
}

// DefineObjectProperty declares p and returns a builder for axioms about it.
func (o *Ontology) DefineObjectProperty(p ObjectProperty) *ObjectPropertyBuilder {
	o.Declare(p)
	return &ObjectPropertyBuilder{o: o, p: p}
}

// ObjectPropertyBuilder accumulates axioms about one object property.
type ObjectPropertyBuilder struct {
	o *Ontology
	p ObjectProperty
}

// Property returns the property being built.
func (b *ObjectPropertyBuilder) Property() ObjectProperty { return b.p }

// Domain restricts the subjects of the property.
func (b *ObjectPropertyBuilder) Domain(ces ...ClassExpression) *ObjectPropertyBuilder {
	for _, ce := range ces {
		b.o.Add(ObjectPropertyDomain{Property: b.p, Domain: ce})
	}
	return b
}

// Range restricts the objects of the property.
func (b *ObjectPropertyBuilder) Range(ces ...ClassExpression) *ObjectPropertyBuilder {
	for _, ce := range ces {
		b.o.Add(ObjectPropertyRange{Property: b.p, Range: ce})
	}
	return b
}

// SubPropertyOf asserts the property is below each operand.
func (b *ObjectPropertyBuilder) SubPropertyOf(supers ...ObjectPropertyExpression) *ObjectPropertyBuilder {
	for _, s := range supers {
		b.o.Add(SubObjectPropertyOf{Sub: b.p, Super: s})
	}
	return b
}

// EquivalentTo asserts the property is equivalent to the operands.
func (b *ObjectPropertyBuilder) EquivalentTo(others ...ObjectPropertyExpression) *ObjectPropertyBuilder {
	if len(others) > 0 {
		b.o.Add(EquivalentObjectProperties(append([]ObjectPropertyExpression{b.p}, others...)))
	}
	return b
}

// InverseOf asserts an inverse pairing.
func (b *ObjectPropertyBuilder) InverseOf(other ObjectPropertyExpression) *ObjectPropertyBuilder {
	b.o.Add(InverseObjectProperties{First: b.p, Second: other})
	return b
}

// Chain asserts that composing the given properties implies this one, e.g.
// hasParent ∘ hasBrother ⊑ hasUncle.
func (b *ObjectPropertyBuilder) Chain(chain ...ObjectPropertyExpression) *ObjectPropertyBuilder {
	b.o.Add(SubPropertyChainOf{Chain: chain, Super: b.p})
	return b
}

// Property characteristics.
func (b *ObjectPropertyBuilder) Functional() *ObjectPropertyBuilder {
	b.o.Add(FunctionalObjectProperty{Property: b.p})
	return b
}

func (b *ObjectPropertyBuilder) InverseFunctional() *ObjectPropertyBuilder {
	b.o.Add(InverseFunctionalObjectProperty{Property: b.p})
	return b
}

func (b *ObjectPropertyBuilder) Transitive() *ObjectPropertyBuilder {
	b.o.Add(TransitiveObjectProperty{Property: b.p})
	return b
}

func (b *ObjectPropertyBuilder) Symmetric() *ObjectPropertyBuilder {
	b.o.Add(SymmetricObjectProperty{Property: b.p})
	return b
}

func (b *ObjectPropertyBuilder) Asymmetric() *ObjectPropertyBuilder {
	b.o.Add(AsymmetricObjectProperty{Property: b.p})
	return b
}

func (b *ObjectPropertyBuilder) Reflexive() *ObjectPropertyBuilder {
	b.o.Add(ReflexiveObjectProperty{Property: b.p})
	return b
}

func (b *ObjectPropertyBuilder) Irreflexive() *ObjectPropertyBuilder {
	b.o.Add(IrreflexiveObjectProperty{Property: b.p})
	return b
}

// Label attaches an rdfs:label.
func (b *ObjectPropertyBuilder) Label(text string, lang ...string) *ObjectPropertyBuilder {
	b.o.Add(labelAxiom(IRI(b.p), text, lang))
	return b
}

// Comment attaches an rdfs:comment.
func (b *ObjectPropertyBuilder) Comment(text string, lang ...string) *ObjectPropertyBuilder {
	b.o.Add(commentAxiom(IRI(b.p), text, lang))
	return b
}

// DefineDataProperty declares p and returns a builder for axioms about it.
func (o *Ontology) DefineDataProperty(p DataProperty) *DataPropertyBuilder {
	o.Declare(p)
	return &DataPropertyBuilder{o: o, p: p}
}

// DataPropertyBuilder accumulates axioms about one data property.
type DataPropertyBuilder struct {
	o *Ontology
	p DataProperty
}

// Property returns the property being built.
func (b *DataPropertyBuilder) Property() DataProperty { return b.p }

// Domain restricts the subjects of the property.
func (b *DataPropertyBuilder) Domain(ces ...ClassExpression) *DataPropertyBuilder {
	for _, ce := range ces {
		b.o.Add(DataPropertyDomain{Property: b.p, Domain: ce})
	}
	return b
}

// Range restricts the values of the property.
func (b *DataPropertyBuilder) Range(rs ...DataRange) *DataPropertyBuilder {
	for _, r := range rs {
		b.o.Add(DataPropertyRange{Property: b.p, Range: r})
	}
	return b
}

// SubPropertyOf asserts the property is below each operand.
func (b *DataPropertyBuilder) SubPropertyOf(supers ...DataPropertyExpression) *DataPropertyBuilder {
	for _, s := range supers {
		b.o.Add(SubDataPropertyOf{Sub: b.p, Super: s})
	}
	return b
}

// Functional asserts the property has at most one value per individual.
func (b *DataPropertyBuilder) Functional() *DataPropertyBuilder {
	b.o.Add(FunctionalDataProperty{Property: b.p})
	return b
}

// Label attaches an rdfs:label.
func (b *DataPropertyBuilder) Label(text string, lang ...string) *DataPropertyBuilder {
	b.o.Add(labelAxiom(IRI(b.p), text, lang))
	return b
}

// Comment attaches an rdfs:comment.
func (b *DataPropertyBuilder) Comment(text string, lang ...string) *DataPropertyBuilder {
	b.o.Add(commentAxiom(IRI(b.p), text, lang))
	return b
}

// DefineIndividual declares i and returns a builder for assertions about it.
func (o *Ontology) DefineIndividual(i NamedIndividual) *IndividualBuilder {
	o.Declare(i)
	return &IndividualBuilder{o: o, i: i}
}

// IndividualBuilder accumulates assertions about one individual.
type IndividualBuilder struct {
	o *Ontology
	i NamedIndividual
}

// Individual returns the individual being built.
func (b *IndividualBuilder) Individual() NamedIndividual { return b.i }

// Type asserts class membership.
func (b *IndividualBuilder) Type(ces ...ClassExpression) *IndividualBuilder {
	for _, ce := range ces {
		b.o.Add(ClassAssertion{Class: ce, Individual: b.i})
	}
	return b
}

// Fact asserts an object property value.
func (b *IndividualBuilder) Fact(p ObjectPropertyExpression, object Individual) *IndividualBuilder {
	b.o.Add(ObjectPropertyAssertion{Property: p, Subject: b.i, Object: object})
	return b
}

// NotFact asserts that an object property value does not hold.
func (b *IndividualBuilder) NotFact(p ObjectPropertyExpression, object Individual) *IndividualBuilder {
	b.o.Add(NegativeObjectPropertyAssertion{Property: p, Subject: b.i, Object: object})
	return b
}

// Data asserts a data property value.
func (b *IndividualBuilder) Data(p DataPropertyExpression, v Literal) *IndividualBuilder {
	b.o.Add(DataPropertyAssertion{Property: p, Subject: b.i, Value: v})
	return b
}

// SameAs asserts identity with other individuals.
func (b *IndividualBuilder) SameAs(others ...Individual) *IndividualBuilder {
	if len(others) > 0 {
		b.o.Add(SameIndividual(append([]Individual{b.i}, others...)))
	}
	return b
}

// DifferentFrom asserts non-identity with other individuals.
func (b *IndividualBuilder) DifferentFrom(others ...Individual) *IndividualBuilder {
	if len(others) > 0 {
		b.o.Add(DifferentIndividuals(append([]Individual{b.i}, others...)))
	}
	return b
}

// Label attaches an rdfs:label.
func (b *IndividualBuilder) Label(text string, lang ...string) *IndividualBuilder {
	b.o.Add(labelAxiom(IRI(b.i), text, lang))
	return b
}

// Comment attaches an rdfs:comment.
func (b *IndividualBuilder) Comment(text string, lang ...string) *IndividualBuilder {
	b.o.Add(commentAxiom(IRI(b.i), text, lang))
	return b
}

func labelAxiom(subject IRI, text string, lang []string) AnnotationAssertion {
	return AnnotationAssertion{Property: RDFSLabel, Subject: subject, Value: text2lit(text, lang)}
}

func commentAxiom(subject IRI, text string, lang []string) AnnotationAssertion {
	return AnnotationAssertion{Property: RDFSComment, Subject: subject, Value: text2lit(text, lang)}
}

func text2lit(text string, lang []string) Literal {
	if len(lang) > 0 && lang[0] != "" {
		return LangStr(text, lang[0])
	}
	return Str(text)
}
