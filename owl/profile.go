package owl

import (
	"fmt"
	"strings"
)

// Profile is one of the OWL 2 profiles: sublanguages that trade expressivity
// for tractable reasoning.
type Profile uint8

const (
	// ProfileEL is OWL 2 EL, the profile most large biomedical ontologies
	// target. Classification is polynomial.
	ProfileEL Profile = iota
	// ProfileQL is OWL 2 QL, aimed at query answering over large ABoxes.
	ProfileQL
	// ProfileRL is OWL 2 RL, implementable with a forward-chaining rule engine.
	ProfileRL
	// ProfileDL is OWL 2 DL. See [CheckProfile] for what is and is not checked.
	ProfileDL
)

func (p Profile) String() string {
	switch p {
	case ProfileEL:
		return "EL"
	case ProfileQL:
		return "QL"
	case ProfileRL:
		return "RL"
	case ProfileDL:
		return "DL"
	}
	return "unknown profile"
}

// AllProfiles is every profile [CheckProfile] understands.
var AllProfiles = []Profile{ProfileEL, ProfileQL, ProfileRL, ProfileDL}

// Violation is one reason an ontology falls outside a profile.
type Violation struct {
	Profile Profile
	Axiom   Axiom
	Reason  string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s: %s in %s", v.Profile, v.Reason, Functional(v.Axiom))
}

// CheckProfile reports every reason o falls outside profile p. An empty result
// means the ontology is in the profile.
//
// For EL, QL and RL this checks the syntactic restrictions of the OWL 2
// Profiles specification: which axioms may appear, which class expressions may
// appear, and — for QL and RL — whether an expression is legal in the position
// it occupies. Datatype-map restrictions are not checked.
//
// For DL only the global restriction on simple properties is checked: a
// property that is transitive, or implied by a property chain, may not be used
// in a cardinality restriction, a self restriction, a disjointness axiom, or a
// functional, inverse-functional, irreflexive or asymmetric assertion. This is
// the DL restriction that most often trips reasoners in practice. The other
// global restrictions — typing separation, restricted datatype cycles, and the
// well-formedness of anonymous individuals — are not checked, so an empty
// result for DL is not proof of DL membership.
func CheckProfile(o *Ontology, p Profile) []Violation {
	c := &profileCheck{profile: p, ontology: o}
	switch p {
	case ProfileEL:
		for _, ax := range o.Axioms() {
			c.ax = ax
			c.el(Unwrap(ax))
		}
	case ProfileQL:
		for _, ax := range o.Axioms() {
			c.ax = ax
			c.ql(Unwrap(ax))
		}
	case ProfileRL:
		for _, ax := range o.Axioms() {
			c.ax = ax
			c.rl(Unwrap(ax))
		}
	case ProfileDL:
		c.dl()
	}
	return c.out
}

// Profiles returns every profile o belongs to, in EL, QL, RL, DL order.
func Profiles(o *Ontology) []Profile {
	var out []Profile
	for _, p := range AllProfiles {
		if len(CheckProfile(o, p)) == 0 {
			out = append(out, p)
		}
	}
	return out
}

type profileCheck struct {
	profile  Profile
	ontology *Ontology
	ax       Axiom
	out      []Violation
}

func (c *profileCheck) bad(format string, args ...any) {
	c.out = append(c.out, Violation{
		Profile: c.profile,
		Axiom:   c.ax,
		Reason:  fmt.Sprintf(format, args...),
	})
}

// isThing reports whether ce is owl:Thing. A direct != comparison against the
// Thing constant would panic when the expression's dynamic type is a slice.
func isThing(ce ClassExpression) bool {
	c, ok := ce.(Class)
	return ok && c == Thing
}

// head returns the functional-syntax keyword of a construct, for messages.
func head(n Node) string {
	s := Functional(n)
	if i := strings.IndexByte(s, '('); i > 0 {
		return s[:i]
	}
	return s
}

// --- EL ---------------------------------------------------------------------

func (c *profileCheck) el(ax Axiom) {
	switch x := ax.(type) {
	case Declaration, AnnotationAssertion, SubAnnotationPropertyOf,
		AnnotationPropertyDomain, AnnotationPropertyRange:
		// Always allowed.

	case SubClassOf:
		c.elClass(x.Sub)
		c.elClass(x.Super)
	case EquivalentClasses:
		for _, ce := range x {
			c.elClass(ce)
		}
	case DisjointClasses:
		for _, ce := range x {
			c.elClass(ce)
		}

	case SubObjectPropertyOf:
		c.elObjectProp(x.Sub)
		c.elObjectProp(x.Super)
	case SubPropertyChainOf:
		for _, p := range x.Chain {
			c.elObjectProp(p)
		}
		c.elObjectProp(x.Super)
	case EquivalentObjectProperties:
		for _, p := range x {
			c.elObjectProp(p)
		}
	case ObjectPropertyDomain:
		c.elObjectProp(x.Property)
		c.elClass(x.Domain)
	case ObjectPropertyRange:
		c.elObjectProp(x.Property)
		c.elClass(x.Range)
	case ReflexiveObjectProperty:
		c.elObjectProp(x.Property)
	case TransitiveObjectProperty:
		c.elObjectProp(x.Property)

	case SubDataPropertyOf, EquivalentDataProperties, FunctionalDataProperty:
		// Allowed as-is; no nested expressions to inspect.
	case DataPropertyDomain:
		c.elClass(x.Domain)
	case DataPropertyRange:
		c.elDataRange(x.Range)
	case DatatypeDefinition:
		c.elDataRange(x.Range)

	case HasKey:
		c.elClass(x.Class)
		for _, p := range x.ObjectProperties {
			c.elObjectProp(p)
		}

	case SameIndividual, DifferentIndividuals,
		DataPropertyAssertion, NegativeDataPropertyAssertion:
		// EL permits all assertions, negative ones included.
	case ClassAssertion:
		c.elClass(x.Class)
	case ObjectPropertyAssertion:
		c.elObjectProp(x.Property)
	case NegativeObjectPropertyAssertion:
		c.elObjectProp(x.Property)

	default:
		c.bad("%s is not an EL axiom", head(ax))
	}
}

func (c *profileCheck) elClass(ce ClassExpression) {
	switch x := ce.(type) {
	case Class:
	case ObjectIntersectionOf:
		for _, op := range x {
			c.elClass(op)
		}
	case ObjectSomeValuesFrom:
		c.elObjectProp(x.Property)
		c.elClass(x.Filler)
	case ObjectHasValue:
		c.elObjectProp(x.Property)
	case ObjectHasSelf:
		c.elObjectProp(x.Property)
	case ObjectOneOf:
		if len(x) != 1 {
			c.bad("EL allows ObjectOneOf over exactly one individual, found %d", len(x))
		}
	case DataSomeValuesFrom:
		c.elDataRange(x.Range)
	case DataHasValue:
	default:
		c.bad("%s is not an EL class expression", head(ce))
	}
}

func (c *profileCheck) elObjectProp(p ObjectPropertyExpression) {
	if _, ok := p.(ObjectInverseOf); ok {
		c.bad("EL has no inverse properties")
	}
}

func (c *profileCheck) elDataRange(r DataRange) {
	switch x := r.(type) {
	case Datatype, DatatypeRestriction:
	case DataIntersectionOf:
		for _, op := range x {
			c.elDataRange(op)
		}
	case DataOneOf:
		if len(x) != 1 {
			c.bad("EL allows DataOneOf over exactly one literal, found %d", len(x))
		}
	default:
		c.bad("%s is not an EL data range", head(r))
	}
}

// --- QL ---------------------------------------------------------------------

func (c *profileCheck) ql(ax Axiom) {
	switch x := ax.(type) {
	case Declaration, AnnotationAssertion, SubAnnotationPropertyOf,
		AnnotationPropertyDomain, AnnotationPropertyRange:

	case SubClassOf:
		c.qlSubClass(x.Sub)
		c.qlSuperClass(x.Super)
	case EquivalentClasses:
		for _, ce := range x {
			c.qlSubClass(ce)
		}
	case DisjointClasses:
		for _, ce := range x {
			c.qlSubClass(ce)
		}

	case SubObjectPropertyOf, EquivalentObjectProperties, DisjointObjectProperties,
		InverseObjectProperties, SymmetricObjectProperty, AsymmetricObjectProperty,
		ReflexiveObjectProperty, IrreflexiveObjectProperty,
		SubDataPropertyOf, EquivalentDataProperties, DisjointDataProperties:

	case ObjectPropertyDomain:
		c.qlSuperClass(x.Domain)
	case ObjectPropertyRange:
		c.qlSuperClass(x.Range)
	case DataPropertyDomain:
		c.qlSuperClass(x.Domain)
	case DataPropertyRange:

	case ClassAssertion:
		// QL restricts assertions to atomic classes.
		if _, ok := x.Class.(Class); !ok {
			c.bad("QL allows only a named class in ClassAssertion, found %s", head(x.Class))
		}
	case ObjectPropertyAssertion, DataPropertyAssertion, DifferentIndividuals:

	default:
		c.bad("%s is not a QL axiom", head(ax))
	}
}

// qlSubClass checks a class expression appearing on the left of an inclusion.
func (c *profileCheck) qlSubClass(ce ClassExpression) {
	switch x := ce.(type) {
	case Class:
	case ObjectSomeValuesFrom:
		// In subclass position QL allows only an unqualified existential.
		if !isThing(x.Filler) {
			c.bad("in QL subclass position ObjectSomeValuesFrom must have owl:Thing as its filler, found %s", head(x.Filler))
		}
	case DataSomeValuesFrom:
	default:
		c.bad("%s is not a QL subclass expression", head(ce))
	}
}

// qlSuperClass checks a class expression appearing on the right of an
// inclusion, where QL is more permissive.
func (c *profileCheck) qlSuperClass(ce ClassExpression) {
	switch x := ce.(type) {
	case Class:
	case ObjectIntersectionOf:
		for _, op := range x {
			c.qlSuperClass(op)
		}
	case ObjectComplementOf:
		c.qlSubClass(x.Operand)
	case ObjectSomeValuesFrom:
		if _, ok := x.Filler.(Class); !ok {
			c.bad("in QL superclass position ObjectSomeValuesFrom needs a named filler, found %s", head(x.Filler))
		}
	case DataSomeValuesFrom:
	default:
		c.bad("%s is not a QL superclass expression", head(ce))
	}
}

// --- RL ---------------------------------------------------------------------

func (c *profileCheck) rl(ax Axiom) {
	switch x := ax.(type) {
	case Declaration, AnnotationAssertion, SubAnnotationPropertyOf,
		AnnotationPropertyDomain, AnnotationPropertyRange:

	case SubClassOf:
		c.rlSubClass(x.Sub)
		c.rlSuperClass(x.Super)
	case EquivalentClasses:
		for _, ce := range x {
			c.rlEquivClass(ce)
		}
	case DisjointClasses:
		for _, ce := range x {
			c.rlSubClass(ce)
		}

	case SubObjectPropertyOf, SubPropertyChainOf, EquivalentObjectProperties,
		DisjointObjectProperties, InverseObjectProperties, FunctionalObjectProperty,
		InverseFunctionalObjectProperty, IrreflexiveObjectProperty,
		SymmetricObjectProperty, AsymmetricObjectProperty, TransitiveObjectProperty,
		SubDataPropertyOf, EquivalentDataProperties, DisjointDataProperties,
		FunctionalDataProperty, HasKey:

	case ObjectPropertyDomain:
		c.rlSuperClass(x.Domain)
	case ObjectPropertyRange:
		c.rlSuperClass(x.Range)
	case DataPropertyDomain:
		c.rlSuperClass(x.Domain)
	case DataPropertyRange:

	case ClassAssertion:
		c.rlSuperClass(x.Class)
	case ObjectPropertyAssertion, NegativeObjectPropertyAssertion,
		DataPropertyAssertion, NegativeDataPropertyAssertion,
		SameIndividual, DifferentIndividuals:

	default:
		c.bad("%s is not an RL axiom", head(ax))
	}
}

func (c *profileCheck) rlSubClass(ce ClassExpression) {
	switch x := ce.(type) {
	case Class:
		c.rlNotThing(x)
	case ObjectIntersectionOf:
		for _, op := range x {
			c.rlSubClass(op)
		}
	case ObjectUnionOf:
		for _, op := range x {
			c.rlSubClass(op)
		}
	case ObjectOneOf:
	case ObjectSomeValuesFrom:
		if !isThing(x.Filler) {
			c.rlSubClass(x.Filler)
		}
	case ObjectHasValue:
	case DataSomeValuesFrom, DataHasValue:
	default:
		c.bad("%s is not an RL subclass expression", head(ce))
	}
}

func (c *profileCheck) rlSuperClass(ce ClassExpression) {
	switch x := ce.(type) {
	case Class:
		c.rlNotThing(x)
	case ObjectIntersectionOf:
		for _, op := range x {
			c.rlSuperClass(op)
		}
	case ObjectComplementOf:
		c.rlSubClass(x.Operand)
	case ObjectAllValuesFrom:
		c.rlSuperClass(x.Filler)
	case ObjectHasValue:
	case ObjectMaxCardinality:
		if x.N > 1 {
			c.bad("RL allows ObjectMaxCardinality of 0 or 1, found %d", x.N)
		}
		if x.Filler != nil {
			c.rlSuperClass(x.Filler)
		}
	case DataAllValuesFrom, DataHasValue:
	case DataMaxCardinality:
		if x.N > 1 {
			c.bad("RL allows DataMaxCardinality of 0 or 1, found %d", x.N)
		}
	default:
		c.bad("%s is not an RL superclass expression", head(ce))
	}
}

func (c *profileCheck) rlEquivClass(ce ClassExpression) {
	switch x := ce.(type) {
	case Class:
		c.rlNotThing(x)
	case ObjectIntersectionOf:
		for _, op := range x {
			c.rlEquivClass(op)
		}
	case ObjectHasValue, DataHasValue:
	default:
		c.bad("%s may not appear in an RL EquivalentClasses axiom", head(ce))
	}
}

func (c *profileCheck) rlNotThing(x Class) {
	if x == Thing {
		c.bad("RL does not allow owl:Thing in class expressions")
	}
}

// --- DL: simple properties --------------------------------------------------

// NonSimpleProperties returns the object properties that are not simple: those
// that are transitive, are implied by a property chain of length two or more,
// or have a non-simple subproperty. OWL 2 DL forbids using such a property in a
// cardinality restriction, a self restriction, a disjointness axiom, or a
// functional, inverse-functional, irreflexive or asymmetric assertion.
func NonSimpleProperties(o *Ontology) map[ObjectProperty]bool {
	nonSimple := make(map[ObjectProperty]bool)
	// subOf[p] is the set of properties p is directly below.
	subOf := make(map[ObjectProperty][]ObjectProperty)

	for _, ax := range o.Axioms() {
		switch x := Unwrap(ax).(type) {
		case TransitiveObjectProperty:
			if p, ok := named(x.Property); ok {
				nonSimple[p] = true
			}
		case SubPropertyChainOf:
			if len(x.Chain) >= 2 {
				if p, ok := named(x.Super); ok {
					nonSimple[p] = true
				}
			} else if len(x.Chain) == 1 {
				addSub(subOf, x.Chain[0], x.Super)
			}
		case SubObjectPropertyOf:
			addSub(subOf, x.Sub, x.Super)
		case EquivalentObjectProperties:
			// Equivalence propagates non-simplicity both ways.
			for _, a := range x {
				for _, b := range x {
					addSub(subOf, a, b)
				}
			}
		case InverseObjectProperties:
			addSub(subOf, x.First, x.Second)
			addSub(subOf, x.Second, x.First)
		}
	}

	// Non-simplicity flows upward through the property hierarchy.
	for changed := true; changed; {
		changed = false
		for sub, supers := range subOf {
			if !nonSimple[sub] {
				continue
			}
			for _, super := range supers {
				if !nonSimple[super] {
					nonSimple[super] = true
					changed = true
				}
			}
		}
	}
	return nonSimple
}

func named(p ObjectPropertyExpression) (ObjectProperty, bool) {
	switch x := p.(type) {
	case ObjectProperty:
		return x, true
	case ObjectInverseOf:
		// An inverse is simple exactly when the property it wraps is.
		return x.Property, true
	}
	return "", false
}

func addSub(m map[ObjectProperty][]ObjectProperty, sub, super ObjectPropertyExpression) {
	s, ok := named(sub)
	if !ok {
		return
	}
	p, ok := named(super)
	if !ok || s == p {
		return
	}
	m[s] = append(m[s], p)
}

func (c *profileCheck) dl() {
	nonSimple := NonSimpleProperties(c.ontology)

	requireSimple := func(p ObjectPropertyExpression, where string) {
		if q, ok := named(p); ok && nonSimple[q] {
			c.bad("%s is not simple and may not be used in %s", c.ontology.Render(q), where)
		}
	}

	for _, ax := range c.ontology.Axioms() {
		c.ax = ax
		switch x := Unwrap(ax).(type) {
		case FunctionalObjectProperty:
			requireSimple(x.Property, "FunctionalObjectProperty")
		case InverseFunctionalObjectProperty:
			requireSimple(x.Property, "InverseFunctionalObjectProperty")
		case IrreflexiveObjectProperty:
			requireSimple(x.Property, "IrreflexiveObjectProperty")
		case AsymmetricObjectProperty:
			requireSimple(x.Property, "AsymmetricObjectProperty")
		case DisjointObjectProperties:
			for _, p := range x {
				requireSimple(p, "DisjointObjectProperties")
			}
		}
		// Cardinality and self restrictions can be nested anywhere a class
		// expression is allowed, so scan the whole axiom.
		c.dlClassExpressions(ax, requireSimple)
	}
}

// dlClassExpressions walks every class expression inside an axiom, reporting
// non-simple properties in the positions DL forbids them.
func (c *profileCheck) dlClassExpressions(ax Axiom, requireSimple func(ObjectPropertyExpression, string)) {
	var visit func(ClassExpression)
	visit = func(ce ClassExpression) {
		switch x := ce.(type) {
		case ObjectIntersectionOf:
			for _, op := range x {
				visit(op)
			}
		case ObjectUnionOf:
			for _, op := range x {
				visit(op)
			}
		case ObjectComplementOf:
			visit(x.Operand)
		case ObjectSomeValuesFrom:
			visit(x.Filler)
		case ObjectAllValuesFrom:
			visit(x.Filler)
		case ObjectHasSelf:
			requireSimple(x.Property, "ObjectHasSelf")
		case ObjectMinCardinality:
			requireSimple(x.Property, "ObjectMinCardinality")
			visit(x.Filler)
		case ObjectMaxCardinality:
			requireSimple(x.Property, "ObjectMaxCardinality")
			visit(x.Filler)
		case ObjectExactCardinality:
			requireSimple(x.Property, "ObjectExactCardinality")
			visit(x.Filler)
		}
	}
	for _, ce := range classExpressionsOf(ax) {
		visit(ce)
	}
}

// classExpressionsOf returns the class expressions an axiom holds directly.
func classExpressionsOf(ax Axiom) []ClassExpression {
	switch x := Unwrap(ax).(type) {
	case SubClassOf:
		return []ClassExpression{x.Sub, x.Super}
	case EquivalentClasses:
		return x
	case DisjointClasses:
		return x
	case DisjointUnion:
		return append([]ClassExpression{x.Class}, x.Operands...)
	case ObjectPropertyDomain:
		return []ClassExpression{x.Domain}
	case ObjectPropertyRange:
		return []ClassExpression{x.Range}
	case DataPropertyDomain:
		return []ClassExpression{x.Domain}
	case ClassAssertion:
		return []ClassExpression{x.Class}
	case HasKey:
		return []ClassExpression{x.Class}
	}
	return nil
}
