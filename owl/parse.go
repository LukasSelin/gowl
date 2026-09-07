package owl

import (
	"io"
	"strconv"
)

// ParseFunctional reads an OWL 2 Functional-Style Syntax document.
//
// The parser accepts the full axiom and expression grammar this package
// models, plus `#` line comments. Axiom annotations are preserved by wrapping
// the axiom in [Annotated]. Two things in the grammar are not representable in
// the model and are rejected rather than silently dropped: n-ary
// DataSomeValuesFrom/DataAllValuesFrom, and anonymous individuals as the
// subject of an AnnotationAssertion. Annotations *on* annotations are parsed
// and discarded.
func ParseFunctional(r io.Reader) (*Ontology, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return ParseFunctionalString(string(data))
}

// ParseFunctionalString parses a functional-syntax document held in a string.
func ParseFunctionalString(src string) (o *Ontology, err error) {
	defer func() { o, err = recoverParse(recover(), o, err) }()
	p := newParser(src, nil)
	return p.document(), nil
}

// ParseAxiom parses a single axiom, resolving abbreviated IRIs against
// prefixes. A nil prefixes uses the standard owl, rdf, rdfs and xsd bindings.
func ParseAxiom(src string, prefixes *Prefixes) (ax Axiom, err error) {
	defer func() { ax, err = recoverParse(recover(), ax, err) }()
	p := newParser(src, prefixes)
	ax = p.axiom()
	p.expect(tEOF)
	return ax, nil
}

// ParseClassExpression parses a single class expression.
func ParseClassExpression(src string, prefixes *Prefixes) (ce ClassExpression, err error) {
	defer func() { ce, err = recoverParse(recover(), ce, err) }()
	p := newParser(src, prefixes)
	ce = p.classExpression()
	p.expect(tEOF)
	return ce, nil
}

// recoverParse converts a parser panic into an error, leaving other panics
// alone. It zeroes the result so a failed parse never returns a half-built
// value alongside its error.
func recoverParse[T any](r any, value T, err error) (T, error) {
	if r == nil {
		return value, err
	}
	pe, ok := r.(*parseError)
	if !ok {
		panic(r)
	}
	var zero T
	return zero, pe
}

type parser struct {
	toks     []token
	pos      int
	prefixes *Prefixes
}

func newParser(src string, prefixes *Prefixes) *parser {
	if prefixes == nil {
		prefixes = NewPrefixes()
	}
	return &parser{toks: lex(src), prefixes: prefixes}
}

// --- Token plumbing ---------------------------------------------------------

func (p *parser) peek() token { return p.toks[p.pos] }

func (p *parser) next() token {
	t := p.toks[p.pos]
	if t.kind != tEOF {
		p.pos++
	}
	return t
}

func (p *parser) expect(k tokKind) token {
	t := p.peek()
	if t.kind != k {
		failf(t.line, "expected %s, found %s", k, t.describe())
	}
	return p.next()
}

// atName reports whether the next token is the given keyword.
func (p *parser) atName(name string) bool {
	t := p.peek()
	return t.kind == tName && t.val == name
}

// keyword consumes a keyword followed by '(' and returns the keyword.
func (p *parser) keyword() string {
	t := p.expect(tName)
	p.expect(tOpen)
	return t.val
}

// atIRI reports whether the next token can start an IRI.
func (p *parser) atIRI() bool {
	k := p.peek().kind
	return k == tIRI || k == tPName
}

// iri reads a full or abbreviated IRI.
func (p *parser) iri() IRI {
	t := p.next()
	switch t.kind {
	case tIRI:
		return IRI(t.val)
	case tPName:
		expanded, err := p.prefixes.Expand(t.val)
		if err != nil {
			failf(t.line, "%v", err)
		}
		return expanded
	}
	failf(t.line, "expected an IRI, found %s", t.describe())
	return ""
}

func (p *parser) integer() int {
	t := p.expect(tInt)
	n, err := strconv.Atoi(t.val)
	if err != nil {
		failf(t.line, "malformed integer %q", t.val)
	}
	return n
}

// --- Document ---------------------------------------------------------------

func (p *parser) document() *Ontology {
	o := &Ontology{Prefixes: p.prefixes}

	for p.atName("Prefix") {
		p.keyword()
		name := p.expect(tPName)
		p.expect(tEq)
		ns := p.expect(tIRI)
		p.expect(tClose)
		p.prefixes.Set(name.val, IRI(ns.val))
	}

	if !p.atName("Ontology") {
		t := p.peek()
		failf(t.line, "expected Ontology(...), found %s", t.describe())
	}
	p.keyword()

	if p.atIRI() {
		o.IRI = p.iri()
		if p.atIRI() {
			o.VersionIRI = p.iri()
		}
	}

	for p.peek().kind != tClose {
		if p.peek().kind == tEOF {
			failf(p.peek().line, "unexpected end of input inside Ontology(...)")
		}
		switch {
		case p.atName("Import"):
			p.keyword()
			o.Imports = append(o.Imports, p.iri())
			p.expect(tClose)
		case p.atName("Annotation"):
			o.Annotations = append(o.Annotations, p.annotation())
		default:
			o.axioms = append(o.axioms, p.axiom())
		}
	}
	p.expect(tClose)
	p.expect(tEOF)
	return o
}

// annotation parses Annotation(... prop value). Annotations nested on an
// annotation are parsed and discarded: the model has nowhere to put them.
func (p *parser) annotation() Annotation {
	p.keyword()
	p.annotations()
	prop := AnnotationProperty(p.iri())
	value := p.annotationValue()
	p.expect(tClose)
	return Annotation{Property: prop, Value: value}
}

// annotations consumes any leading Annotation(...) arguments.
func (p *parser) annotations() []Annotation {
	var out []Annotation
	for p.atName("Annotation") {
		out = append(out, p.annotation())
	}
	return out
}

func (p *parser) annotationValue() AnnotationValue {
	switch t := p.peek(); t.kind {
	case tString:
		return p.literal()
	case tBNode:
		p.next()
		return AnonymousIndividual(t.val)
	case tIRI, tPName:
		return p.iri()
	default:
		failf(t.line, "expected an annotation value, found %s", t.describe())
		return nil
	}
}

func (p *parser) literal() Literal {
	t := p.expect(tString)
	switch p.peek().kind {
	case tLang:
		return LangStr(t.val, p.next().val)
	case tCaret:
		p.next()
		return Literal{Value: t.val, Datatype: Datatype(p.iri())}
	default:
		// The spec requires a datatype or language tag; tolerate writers that
		// omit both and treat the value as xsd:string.
		return Str(t.val)
	}
}

// --- Expressions ------------------------------------------------------------

func (p *parser) classExpression() ClassExpression {
	if p.atIRI() {
		return Class(p.iri())
	}
	t := p.peek()
	name := p.keyword()
	var ce ClassExpression
	switch name {
	case "ObjectIntersectionOf":
		ce = ObjectIntersectionOf(p.classList())
	case "ObjectUnionOf":
		ce = ObjectUnionOf(p.classList())
	case "ObjectComplementOf":
		ce = ObjectComplementOf{Operand: p.classExpression()}
	case "ObjectOneOf":
		ce = ObjectOneOf(p.individualList())
	case "ObjectSomeValuesFrom":
		ce = ObjectSomeValuesFrom{Property: p.objectPropertyExpression(), Filler: p.classExpression()}
	case "ObjectAllValuesFrom":
		ce = ObjectAllValuesFrom{Property: p.objectPropertyExpression(), Filler: p.classExpression()}
	case "ObjectHasValue":
		ce = ObjectHasValue{Property: p.objectPropertyExpression(), Value: p.individual()}
	case "ObjectHasSelf":
		ce = ObjectHasSelf{Property: p.objectPropertyExpression()}
	case "ObjectMinCardinality":
		n, prop, filler := p.objectCardinality()
		ce = ObjectMinCardinality{N: n, Property: prop, Filler: filler}
	case "ObjectMaxCardinality":
		n, prop, filler := p.objectCardinality()
		ce = ObjectMaxCardinality{N: n, Property: prop, Filler: filler}
	case "ObjectExactCardinality":
		n, prop, filler := p.objectCardinality()
		ce = ObjectExactCardinality{N: n, Property: prop, Filler: filler}
	case "DataSomeValuesFrom":
		prop, r := p.dataRestriction(t.line, name)
		ce = DataSomeValuesFrom{Property: prop, Range: r}
	case "DataAllValuesFrom":
		prop, r := p.dataRestriction(t.line, name)
		ce = DataAllValuesFrom{Property: prop, Range: r}
	case "DataHasValue":
		ce = DataHasValue{Property: p.dataPropertyExpression(), Value: p.literal()}
	case "DataMinCardinality":
		n, prop, r := p.dataCardinality()
		ce = DataMinCardinality{N: n, Property: prop, Range: r}
	case "DataMaxCardinality":
		n, prop, r := p.dataCardinality()
		ce = DataMaxCardinality{N: n, Property: prop, Range: r}
	case "DataExactCardinality":
		n, prop, r := p.dataCardinality()
		ce = DataExactCardinality{N: n, Property: prop, Range: r}
	default:
		failf(t.line, "%s is not a class expression", name)
	}
	p.expect(tClose)
	return ce
}

func (p *parser) objectCardinality() (int, ObjectPropertyExpression, ClassExpression) {
	n := p.integer()
	prop := p.objectPropertyExpression()
	var filler ClassExpression
	if p.peek().kind != tClose {
		filler = p.classExpression()
	}
	return n, prop, filler
}

func (p *parser) dataCardinality() (int, DataPropertyExpression, DataRange) {
	n := p.integer()
	prop := p.dataPropertyExpression()
	var r DataRange
	if p.peek().kind != tClose {
		r = p.dataRange()
	}
	return n, prop, r
}

// dataRestriction parses the body of Data{Some,All}ValuesFrom. OWL 2 allows
// several data properties before the range; the model holds only one, so an
// n-ary form is reported rather than silently truncated.
func (p *parser) dataRestriction(line int, name string) (DataPropertyExpression, DataRange) {
	prop := p.dataPropertyExpression()
	r := p.dataRange()
	if p.peek().kind != tClose {
		failf(line, "n-ary %s is not supported: this package models a single data property", name)
	}
	return prop, r
}

func (p *parser) classList() []ClassExpression {
	var out []ClassExpression
	for p.peek().kind != tClose {
		out = append(out, p.classExpression())
	}
	return out
}

func (p *parser) objectPropertyExpression() ObjectPropertyExpression {
	if p.atIRI() {
		return ObjectProperty(p.iri())
	}
	t := p.peek()
	if name := p.keyword(); name != "ObjectInverseOf" {
		failf(t.line, "%s is not an object property expression", name)
	}
	inv := ObjectInverseOf{Property: ObjectProperty(p.iri())}
	p.expect(tClose)
	return inv
}

func (p *parser) objectPropertyList() []ObjectPropertyExpression {
	var out []ObjectPropertyExpression
	for p.peek().kind != tClose {
		out = append(out, p.objectPropertyExpression())
	}
	return out
}

func (p *parser) dataPropertyExpression() DataPropertyExpression {
	return DataProperty(p.iri())
}

func (p *parser) dataPropertyList() []DataPropertyExpression {
	var out []DataPropertyExpression
	for p.peek().kind != tClose {
		out = append(out, p.dataPropertyExpression())
	}
	return out
}

func (p *parser) dataRange() DataRange {
	if p.atIRI() {
		return Datatype(p.iri())
	}
	t := p.peek()
	name := p.keyword()
	var r DataRange
	switch name {
	case "DataIntersectionOf":
		r = DataIntersectionOf(p.dataRangeList())
	case "DataUnionOf":
		r = DataUnionOf(p.dataRangeList())
	case "DataComplementOf":
		r = DataComplementOf{Operand: p.dataRange()}
	case "DataOneOf":
		var lits []Literal
		for p.peek().kind != tClose {
			lits = append(lits, p.literal())
		}
		r = DataOneOf(lits)
	case "DatatypeRestriction":
		dr := DatatypeRestriction{Datatype: Datatype(p.iri())}
		for p.peek().kind != tClose {
			dr.Facets = append(dr.Facets, FacetRestriction{Facet: p.iri(), Value: p.literal()})
		}
		r = dr
	default:
		failf(t.line, "%s is not a data range", name)
	}
	p.expect(tClose)
	return r
}

func (p *parser) dataRangeList() []DataRange {
	var out []DataRange
	for p.peek().kind != tClose {
		out = append(out, p.dataRange())
	}
	return out
}

func (p *parser) individual() Individual {
	if t := p.peek(); t.kind == tBNode {
		p.next()
		return AnonymousIndividual(t.val)
	}
	return NamedIndividual(p.iri())
}

func (p *parser) individualList() []Individual {
	var out []Individual
	for p.peek().kind != tClose {
		out = append(out, p.individual())
	}
	return out
}

// group parses a parenthesised operand list, as used by HasKey and
// ObjectPropertyChain.
func (p *parser) group(parse func()) {
	p.expect(tOpen)
	for p.peek().kind != tClose {
		parse()
	}
	p.expect(tClose)
}

// --- Axioms -----------------------------------------------------------------

func (p *parser) axiom() Axiom {
	t := p.peek()
	name := p.keyword()
	annots := p.annotations()
	ax := p.axiomBody(t.line, name)
	p.expect(tClose)
	if len(annots) > 0 {
		return Annotated{Annotations: annots, Axiom: ax}
	}
	return ax
}

func (p *parser) axiomBody(line int, name string) Axiom {
	switch name {
	case "Declaration":
		return Declaration{Entity: p.entity()}

	// Class axioms.
	case "SubClassOf":
		return SubClassOf{Sub: p.classExpression(), Super: p.classExpression()}
	case "EquivalentClasses":
		return EquivalentClasses(p.classList())
	case "DisjointClasses":
		return DisjointClasses(p.classList())
	case "DisjointUnion":
		return DisjointUnion{Class: Class(p.iri()), Operands: p.classList()}

	// Object property axioms.
	case "SubObjectPropertyOf":
		if p.atName("ObjectPropertyChain") {
			p.keyword()
			chain := p.objectPropertyList()
			p.expect(tClose)
			return SubPropertyChainOf{Chain: chain, Super: p.objectPropertyExpression()}
		}
		return SubObjectPropertyOf{Sub: p.objectPropertyExpression(), Super: p.objectPropertyExpression()}
	case "EquivalentObjectProperties":
		return EquivalentObjectProperties(p.objectPropertyList())
	case "DisjointObjectProperties":
		return DisjointObjectProperties(p.objectPropertyList())
	case "InverseObjectProperties":
		return InverseObjectProperties{First: p.objectPropertyExpression(), Second: p.objectPropertyExpression()}
	case "ObjectPropertyDomain":
		return ObjectPropertyDomain{Property: p.objectPropertyExpression(), Domain: p.classExpression()}
	case "ObjectPropertyRange":
		return ObjectPropertyRange{Property: p.objectPropertyExpression(), Range: p.classExpression()}
	case "FunctionalObjectProperty":
		return FunctionalObjectProperty{Property: p.objectPropertyExpression()}
	case "InverseFunctionalObjectProperty":
		return InverseFunctionalObjectProperty{Property: p.objectPropertyExpression()}
	case "ReflexiveObjectProperty":
		return ReflexiveObjectProperty{Property: p.objectPropertyExpression()}
	case "IrreflexiveObjectProperty":
		return IrreflexiveObjectProperty{Property: p.objectPropertyExpression()}
	case "SymmetricObjectProperty":
		return SymmetricObjectProperty{Property: p.objectPropertyExpression()}
	case "AsymmetricObjectProperty":
		return AsymmetricObjectProperty{Property: p.objectPropertyExpression()}
	case "TransitiveObjectProperty":
		return TransitiveObjectProperty{Property: p.objectPropertyExpression()}

	// Data property axioms.
	case "SubDataPropertyOf":
		return SubDataPropertyOf{Sub: p.dataPropertyExpression(), Super: p.dataPropertyExpression()}
	case "EquivalentDataProperties":
		return EquivalentDataProperties(p.dataPropertyList())
	case "DisjointDataProperties":
		return DisjointDataProperties(p.dataPropertyList())
	case "DataPropertyDomain":
		return DataPropertyDomain{Property: p.dataPropertyExpression(), Domain: p.classExpression()}
	case "DataPropertyRange":
		return DataPropertyRange{Property: p.dataPropertyExpression(), Range: p.dataRange()}
	case "FunctionalDataProperty":
		return FunctionalDataProperty{Property: p.dataPropertyExpression()}
	case "DatatypeDefinition":
		return DatatypeDefinition{Datatype: Datatype(p.iri()), Range: p.dataRange()}

	case "HasKey":
		k := HasKey{Class: p.classExpression()}
		p.group(func() { k.ObjectProperties = append(k.ObjectProperties, p.objectPropertyExpression()) })
		if p.peek().kind != tClose {
			p.group(func() { k.DataProperties = append(k.DataProperties, p.dataPropertyExpression()) })
		}
		return k

	// Assertions.
	case "SameIndividual":
		return SameIndividual(p.individualList())
	case "DifferentIndividuals":
		return DifferentIndividuals(p.individualList())
	case "ClassAssertion":
		return ClassAssertion{Class: p.classExpression(), Individual: p.individual()}
	case "ObjectPropertyAssertion":
		return ObjectPropertyAssertion{Property: p.objectPropertyExpression(), Subject: p.individual(), Object: p.individual()}
	case "NegativeObjectPropertyAssertion":
		return NegativeObjectPropertyAssertion{Property: p.objectPropertyExpression(), Subject: p.individual(), Object: p.individual()}
	case "DataPropertyAssertion":
		return DataPropertyAssertion{Property: p.dataPropertyExpression(), Subject: p.individual(), Value: p.literal()}
	case "NegativeDataPropertyAssertion":
		return NegativeDataPropertyAssertion{Property: p.dataPropertyExpression(), Subject: p.individual(), Value: p.literal()}

	// Annotation axioms.
	case "AnnotationAssertion":
		prop := AnnotationProperty(p.iri())
		if p.peek().kind == tBNode {
			failf(line, "anonymous individuals are not supported as AnnotationAssertion subjects")
		}
		return AnnotationAssertion{Property: prop, Subject: p.iri(), Value: p.annotationValue()}
	case "SubAnnotationPropertyOf":
		return SubAnnotationPropertyOf{Sub: AnnotationProperty(p.iri()), Super: AnnotationProperty(p.iri())}
	case "AnnotationPropertyDomain":
		return AnnotationPropertyDomain{Property: AnnotationProperty(p.iri()), Domain: p.iri()}
	case "AnnotationPropertyRange":
		return AnnotationPropertyRange{Property: AnnotationProperty(p.iri()), Range: p.iri()}
	}

	failf(line, "unknown axiom %s", name)
	return nil
}

// entity parses the Class(...) / ObjectProperty(...) wrapper inside a
// Declaration.
func (p *parser) entity() Entity {
	t := p.peek()
	name := p.keyword()
	iri := p.iri()
	p.expect(tClose)
	switch name {
	case "Class":
		return Class(iri)
	case "Datatype":
		return Datatype(iri)
	case "ObjectProperty":
		return ObjectProperty(iri)
	case "DataProperty":
		return DataProperty(iri)
	case "AnnotationProperty":
		return AnnotationProperty(iri)
	case "NamedIndividual":
		return NamedIndividual(iri)
	}
	failf(t.line, "%s is not an entity kind", name)
	return nil
}
