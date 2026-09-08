// Package rdfowl maps an RDF graph onto gowl's structural OWL model.
//
// This is the reverse of the OWL 2 Mapping to RDF Graphs: published
// vocabularies ship as triples, and gowl wants entities, class expressions and
// axioms. The mapping covers the constructs those vocabularies use and
// reports, rather than silently drops, anything it does not recognise — see
// [Result.Skipped]. Nothing here guesses: a triple either has a structural
// reading or it becomes an annotation.
//
// Two judgement calls are worth naming, because they are not in any spec:
//
//   - A bare rdf:Property is not an OWL entity kind. It is read as a data
//     property when everything its range says is a datatype, and as an object
//     property otherwise. [Options.RangeHints] lets a source nominate the
//     predicates that count as a range, which is how schema.org's
//     rangeIncludes is honoured.
//   - A predicate with no structural reading becomes an AnnotationAssertion
//     rather than an error. That keeps schema.org's domainIncludes and
//     rangeIncludes as the documentation they are, instead of promoting them
//     to rdfs:domain and rdfs:range, which they explicitly are not.
package rdfowl

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gowl/internal/rdf"
	"gowl/owl"
)

// Options configures one conversion.
type Options struct {
	// Namespaces are the IRI prefixes whose terms this ontology owns. An
	// entity outside them may still be referenced, but is not declared.
	Namespaces []string

	// IRI is the ontology's own IRI. When empty, the subject typed
	// owl:Ontology is used.
	IRI string

	// RangeHints are predicates consulted, in addition to rdfs:range, when
	// deciding whether a bare rdf:Property is a data or an object property.
	RangeHints []rdf.IRI

	// DatatypeRoots are classes whose instances and subclasses are datatypes
	// rather than classes — schema:DataType being the case this exists for.
	DatatypeRoots []rdf.IRI
}

// Skip records a triple the mapping had no structural reading for and did not
// turn into an annotation either.
type Skip struct {
	Triple rdf.Triple
	Reason string
}

func (s Skip) String() string { return s.Reason + ": " + s.Triple.String() }

// Result is a converted graph.
type Result struct {
	Ontology *owl.Ontology
	Skipped  []Skip
}

// Convert maps a graph onto an ontology.
func Convert(g *rdf.Graph, opts Options) (*Result, error) {
	c := &conv{g: g, opts: opts, kinds: map[owl.IRI]owl.Kind{}}
	c.classify()

	o := owl.New(owl.IRI(c.ontologyIRI()))
	for name, ns := range g.Prefixes {
		o.Prefix(name, owl.IRI(ns))
	}
	c.o = o

	for _, s := range g.Subjects() {
		if c.isStructural(s) {
			continue // consumed as part of an expression or an axiom shell
		}
		c.subject(s)
	}
	if err := o.Err(); err != nil {
		return nil, err
	}
	o.Sort()
	return &Result{Ontology: o, Skipped: c.skipped}, nil
}

type conv struct {
	g       *rdf.Graph
	opts    Options
	o       *owl.Ontology
	kinds   map[owl.IRI]owl.Kind
	skipped []Skip
}

func (c *conv) skip(t rdf.Triple, reason string) {
	c.skipped = append(c.skipped, Skip{Triple: t, Reason: reason})
}

func (c *conv) ontologyIRI() string {
	if c.opts.IRI != "" {
		return c.opts.IRI
	}
	for _, s := range c.g.Subjects() {
		if c.g.HasType(s, owlIRI("Ontology")) {
			return s.Text()
		}
	}
	return ""
}

// owns reports whether an IRI is one of the terms this ontology defines.
func (c *conv) owns(iri string) bool {
	for _, ns := range c.opts.Namespaces {
		if strings.HasPrefix(iri, ns) && len(iri) > len(ns) {
			return true
		}
	}
	return false
}

// --- entity classification --------------------------------------------------

func owlIRI(local string) rdf.IRI  { return rdf.IRI(rdf.NSOWL + local) }
func rdfsIRI(local string) rdf.IRI { return rdf.IRI(rdf.NSRDFS + local) }

// typeKinds maps an rdf:type to the entity kind it declares, with a rank for
// how strong a signal it is. An explicit owl:ObjectProperty outranks the
// owl:InverseFunctionalProperty that appears beside it, which only says that
// the subject is some property; both outrank the bare rdfs:Class an OWL
// vocabulary asserts for RDFS compatibility.
type typeKind struct {
	kind owl.Kind
	rank int
}

var typeKinds = map[rdf.IRI]typeKind{
	owlIRI("Class"):              {owl.KindClass, 3},
	rdfsIRI("Datatype"):          {owl.KindDatatype, 3},
	owlIRI("ObjectProperty"):     {owl.KindObjectProperty, 3},
	owlIRI("DatatypeProperty"):   {owl.KindDataProperty, 3},
	owlIRI("AnnotationProperty"): {owl.KindAnnotationProperty, 3},
	owlIRI("NamedIndividual"):    {owl.KindNamedIndividual, 3},

	owlIRI("OntologyProperty"): {owl.KindAnnotationProperty, 2},

	owlIRI("TransitiveProperty"):        {owl.KindObjectProperty, 1},
	owlIRI("SymmetricProperty"):         {owl.KindObjectProperty, 1},
	owlIRI("AsymmetricProperty"):        {owl.KindObjectProperty, 1},
	owlIRI("ReflexiveProperty"):         {owl.KindObjectProperty, 1},
	owlIRI("IrreflexiveProperty"):       {owl.KindObjectProperty, 1},
	owlIRI("InverseFunctionalProperty"): {owl.KindObjectProperty, 1},

	rdfsIRI("Class"): {owl.KindClass, 0},
}

// classify decides each named term's entity kind. It runs in passes because a
// bare rdf:Property is classified from its range, and a range may itself be a
// datatype only because of a DatatypeRoots subclass edge.
func (c *conv) classify() {
	datatypes := c.datatypeClasses()
	for iri := range datatypes {
		c.kinds[owl.IRI(iri)] = owl.KindDatatype
	}
	// The OWL, RDF and RDFS terms OWL 2 reserves keep the kind the spec gives
	// them however their own document types them. rdfs:label is declared an
	// rdf:Property with a literal range, but in OWL it is an annotation
	// property, and the rdfs package should say so.
	fixed := func(iri rdf.IRI) bool {
		if _, ok := datatypes[iri]; ok {
			return true
		}
		_, ok := builtinKinds[string(iri)]
		return ok
	}

	var bareProperties []rdf.Term
	for _, s := range c.g.Subjects() {
		iri, ok := s.(rdf.IRI)
		if !ok {
			continue
		}
		if fixed(iri) {
			continue
		}
		best, found := typeKind{}, false
		for _, t := range c.g.Objects(s, rdf.RDFType) {
			k, ok := typeKinds[rdf.IRI(t.Text())]
			if !ok {
				continue
			}
			// Ties break on kind order rather than on the order the document
			// happens to list its types, so the result does not move.
			if !found || k.rank > best.rank || (k.rank == best.rank && k.kind < best.kind) {
				best, found = k, true
			}
		}
		switch {
		case found:
			c.kinds[owl.IRI(iri)] = best.kind
		case c.g.HasType(s, rdf.IRI(rdf.NSRDF+"Property")):
			bareProperties = append(bareProperties, s)
		}
	}

	for _, s := range bareProperties {
		c.kinds[owl.IRI(s.Text())] = c.propertyKind(s, datatypes)
	}
}

// datatypeClasses collects the terms that stand for datatypes: those declared
// rdfs:Datatype, and, for a source that names DatatypeRoots, that root and
// everything under it. The subclass walk is transitive because schema.org
// puts schema:Integer under schema:Number, and only Number under DataType.
func (c *conv) datatypeClasses() map[rdf.IRI]bool {
	out := map[rdf.IRI]bool{}
	for _, root := range c.opts.DatatypeRoots {
		out[root] = true
	}
	subjects := c.g.Subjects()
	for _, s := range subjects {
		iri, ok := s.(rdf.IRI)
		if !ok {
			continue
		}
		if c.g.HasType(s, rdfsIRI("Datatype")) {
			out[iri] = true
		}
		for _, root := range c.opts.DatatypeRoots {
			if c.g.Has(s, rdf.RDFType, root) {
				out[iri] = true
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, s := range subjects {
			iri, ok := s.(rdf.IRI)
			if !ok || out[iri] {
				continue
			}
			for _, super := range c.g.Objects(s, rdfsIRI("subClassOf")) {
				if superIRI, ok := super.(rdf.IRI); ok && out[superIRI] {
					out[iri], changed = true, true
					break
				}
			}
		}
	}
	return out
}

// propertyKind reads a bare rdf:Property's range to decide between a data and
// an object property.
func (c *conv) propertyKind(s rdf.Term, datatypes map[rdf.IRI]bool) owl.Kind {
	preds := append([]rdf.IRI{rdfsIRI("range")}, c.opts.RangeHints...)
	ranges := 0
	dataRanges := 0
	for _, p := range preds {
		for _, o := range c.g.Objects(s, p) {
			iri, ok := o.(rdf.IRI)
			if !ok {
				continue
			}
			ranges++
			if datatypes[iri] || strings.HasPrefix(string(iri), rdf.NSXSD) || iri == rdfsIRI("Literal") {
				dataRanges++
			}
		}
	}
	if ranges > 0 && ranges == dataRanges {
		return owl.KindDataProperty
	}
	return owl.KindObjectProperty
}

// kindOf returns an IRI's entity kind, falling back to what the OWL, RDF, RDFS
// and XSD vocabularies fix for their own terms.
func (c *conv) kindOf(iri string) (owl.Kind, bool) {
	if k, ok := c.kinds[owl.IRI(iri)]; ok {
		return k, true
	}
	if k, ok := builtinKinds[iri]; ok {
		return k, true
	}
	if strings.HasPrefix(iri, rdf.NSXSD) {
		return owl.KindDatatype, true
	}
	return 0, false
}

var builtinKinds = map[string]owl.Kind{
	rdf.NSOWL + "Thing":                  owl.KindClass,
	rdf.NSOWL + "Nothing":                owl.KindClass,
	rdf.NSOWL + "topObjectProperty":      owl.KindObjectProperty,
	rdf.NSOWL + "bottomObjectProperty":   owl.KindObjectProperty,
	rdf.NSOWL + "topDataProperty":        owl.KindDataProperty,
	rdf.NSOWL + "bottomDataProperty":     owl.KindDataProperty,
	rdf.NSOWL + "versionInfo":            owl.KindAnnotationProperty,
	rdf.NSOWL + "deprecated":             owl.KindAnnotationProperty,
	rdf.NSOWL + "priorVersion":           owl.KindAnnotationProperty,
	rdf.NSOWL + "backwardCompatibleWith": owl.KindAnnotationProperty,
	rdf.NSOWL + "incompatibleWith":       owl.KindAnnotationProperty,
	rdf.NSRDFS + "label":                 owl.KindAnnotationProperty,
	rdf.NSRDFS + "comment":               owl.KindAnnotationProperty,
	rdf.NSRDFS + "seeAlso":               owl.KindAnnotationProperty,
	rdf.NSRDFS + "isDefinedBy":           owl.KindAnnotationProperty,
	rdf.NSRDFS + "Literal":               owl.KindDatatype,
	rdf.NSRDFS + "Resource":              owl.KindClass,
	rdf.NSRDF + "langString":             owl.KindDatatype,
	rdf.NSRDF + "PlainLiteral":           owl.KindDatatype,
	rdf.NSRDF + "XMLLiteral":             owl.KindDatatype,
}

func (c *conv) entity(iri string, k owl.Kind) owl.Entity {
	switch k {
	case owl.KindClass:
		return owl.Class(iri)
	case owl.KindDatatype:
		return owl.Datatype(iri)
	case owl.KindObjectProperty:
		return owl.ObjectProperty(iri)
	case owl.KindDataProperty:
		return owl.DataProperty(iri)
	case owl.KindAnnotationProperty:
		return owl.AnnotationProperty(iri)
	default:
		return owl.NamedIndividual(iri)
	}
}

// --- statement conversion ---------------------------------------------------

// isStructural reports whether a subject is scaffolding — a class expression,
// a list cell, or an axiom shell — that a containing statement will read, so
// that the top-level walk does not also emit it as annotations.
func (c *conv) isStructural(s rdf.Term) bool {
	if _, ok := s.(rdf.Blank); !ok {
		return false
	}
	if _, ok := c.g.Object(s, rdf.RDFFirst); ok {
		return true
	}
	for _, t := range c.g.Objects(s, rdf.RDFType) {
		switch rdf.IRI(t.Text()) {
		case owlIRI("Restriction"), owlIRI("Class"), rdfsIRI("Datatype"),
			owlIRI("AllDisjointClasses"), owlIRI("AllDisjointProperties"), owlIRI("AllDifferent"),
			owlIRI("Axiom"):
			return true
		}
	}
	return false
}

func (c *conv) subject(s rdf.Term) {
	iri, isIRI := s.(rdf.IRI)
	if !isIRI {
		// A blank node that is not scaffolding is an anonymous individual.
		// OWL 2 can assert about one, but only once its properties are known
		// to be object or data properties, and a vocabulary document does not
		// say: these nodes are the editors named in an ontology header, whose
		// properties are defined elsewhere. Guessing would put a wrong entity
		// kind into the ontology, so they are reported instead.
		for _, t := range c.g.Triples {
			if t.Subject == s {
				c.skip(t, "anonymous individual")
			}
		}
		return
	}

	if c.g.HasType(s, owlIRI("Ontology")) {
		c.header(s)
		return
	}

	kind, known := c.kindOf(string(iri))
	if known && c.owns(string(iri)) {
		c.o.Declare(c.entity(string(iri), kind))
	}

	for _, p := range c.predicates(s) {
		for _, o := range c.g.Objects(s, p) {
			c.statement(rdf.Triple{Subject: s, Predicate: p, Object: o})
		}
	}
}

// predicates returns a subject's predicates in a fixed order.
func (c *conv) predicates(s rdf.Term) []rdf.IRI {
	seen := map[rdf.IRI]bool{}
	var out []rdf.IRI
	for _, t := range c.g.Triples {
		if t.Subject == s && !seen[t.Predicate] {
			seen[t.Predicate] = true
			out = append(out, t.Predicate)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (c *conv) header(s rdf.Term) {
	c.o.IRI = owl.IRI(s.Text())
	for _, p := range c.predicates(s) {
		for _, o := range c.g.Objects(s, p) {
			switch p {
			case owlIRI("versionIRI"):
				c.o.Version(owl.IRI(o.Text()))
			case owlIRI("imports"):
				c.o.Import(owl.IRI(o.Text()))
			case rdf.RDFType:
			default:
				if v, ok := c.annotationValue(o); ok {
					c.o.Annotate(owl.AnnotationProperty(p), v)
				} else {
					c.skip(rdf.Triple{Subject: s, Predicate: p, Object: o}, "anonymous annotation value")
				}
			}
		}
	}
}

// statement converts one triple.
func (c *conv) statement(t rdf.Triple) {
	subj := string(t.Subject.Text())
	kind, _ := c.kindOf(subj)

	switch t.Predicate {
	case rdf.RDFType:
		c.typeStatement(t)
		return

	case rdfsIRI("subClassOf"):
		if kind == owl.KindDatatype {
			// One datatype declared beneath another — rdf:HTML under
			// rdfs:Literal, schema:Integer under schema:Number. OWL 2 has no
			// axiom for datatype subsumption, so there is nothing to record.
			c.skip(t, "datatype subsumption")
			return
		}
		if super, ok := c.classExpression(t.Object); ok {
			c.o.Add(owl.SubClassOf{Sub: owl.Class(subj), Super: super})
		} else {
			c.skip(t, "unreadable superclass")
		}
		return

	case owlIRI("equivalentClass"):
		if ce, ok := c.classExpression(t.Object); ok {
			c.o.Add(owl.EquivalentClasses{owl.Class(subj), ce})
		} else if dr, ok := c.dataRange(t.Object); ok {
			c.o.Add(owl.DatatypeDefinition{Datatype: owl.Datatype(subj), Range: dr})
		} else {
			c.skip(t, "unreadable equivalent class")
		}
		return

	case owlIRI("disjointWith"):
		if ce, ok := c.classExpression(t.Object); ok {
			c.o.Add(owl.DisjointClasses{owl.Class(subj), ce})
		} else {
			c.skip(t, "unreadable disjoint class")
		}
		return

	case owlIRI("disjointUnionOf"):
		if ops, ok := c.classList(t.Object); ok {
			c.o.Add(owl.DisjointUnion{Class: owl.Class(subj), Operands: ops})
		} else {
			c.skip(t, "unreadable disjoint union")
		}
		return

	case owlIRI("intersectionOf"), owlIRI("unionOf"), owlIRI("oneOf"), owlIRI("complementOf"):
		// A named class defined by an expression written on the class itself.
		if ce, ok := c.definition(t.Subject); ok {
			c.o.Add(owl.EquivalentClasses{owl.Class(subj), ce})
		} else {
			c.skip(t, "unreadable class definition")
		}
		return

	case rdfsIRI("subPropertyOf"):
		c.subProperty(t, kind)
		return

	case owlIRI("propertyChainAxiom"):
		if chain, ok := c.propertyList(t.Object); ok {
			c.o.Add(owl.SubPropertyChainOf{Chain: chain, Super: owl.ObjectProperty(subj)})
		} else {
			c.skip(t, "unreadable property chain")
		}
		return

	case owlIRI("equivalentProperty"):
		switch kind {
		case owl.KindDataProperty:
			c.o.Add(owl.EquivalentDataProperties{owl.DataProperty(subj), owl.DataProperty(t.Object.Text())})
		default:
			c.o.Add(owl.EquivalentObjectProperties{owl.ObjectProperty(subj), owl.ObjectProperty(t.Object.Text())})
		}
		return

	case owlIRI("propertyDisjointWith"):
		switch kind {
		case owl.KindDataProperty:
			c.o.Add(owl.DisjointDataProperties{owl.DataProperty(subj), owl.DataProperty(t.Object.Text())})
		default:
			c.o.Add(owl.DisjointObjectProperties{owl.ObjectProperty(subj), owl.ObjectProperty(t.Object.Text())})
		}
		return

	case owlIRI("inverseOf"):
		if p, ok := c.objectPropertyExpression(t.Object); ok {
			c.o.Add(owl.InverseObjectProperties{First: owl.ObjectProperty(subj), Second: p})
		} else {
			c.skip(t, "unreadable inverse property")
		}
		return

	case rdfsIRI("domain"):
		c.domain(t, kind)
		return

	case rdfsIRI("range"):
		c.rangeOf(t, kind)
		return

	case owlIRI("hasKey"):
		c.hasKey(t)
		return

	case owlIRI("sameAs"):
		c.o.Add(owl.SameIndividual{owl.NamedIndividual(subj), owl.NamedIndividual(t.Object.Text())})
		return

	case owlIRI("differentFrom"):
		c.o.Add(owl.DifferentIndividuals{owl.NamedIndividual(subj), owl.NamedIndividual(t.Object.Text())})
		return

	case owlIRI("onDatatype"), owlIRI("withRestrictions"), owlIRI("datatypeComplementOf"):
		// Read as part of the datatype definition the subject carries.
		if dr, ok := c.dataDefinition(t.Subject); ok {
			c.o.Add(owl.DatatypeDefinition{Datatype: owl.Datatype(subj), Range: dr})
		} else {
			c.skip(t, "unreadable datatype definition")
		}
		return
	}

	c.annotation(t)
}

func (c *conv) typeStatement(t rdf.Triple) {
	subj := t.Object.Text()
	self := t.Subject.Text()
	switch rdf.IRI(subj) {
	case owlIRI("FunctionalProperty"):
		if k, _ := c.kindOf(self); k == owl.KindDataProperty {
			c.o.Add(owl.FunctionalDataProperty{Property: owl.DataProperty(self)})
		} else {
			c.o.Add(owl.FunctionalObjectProperty{Property: owl.ObjectProperty(self)})
		}
		return
	case owlIRI("InverseFunctionalProperty"):
		c.o.Add(owl.InverseFunctionalObjectProperty{Property: owl.ObjectProperty(self)})
		return
	case owlIRI("TransitiveProperty"):
		c.o.Add(owl.TransitiveObjectProperty{Property: owl.ObjectProperty(self)})
		return
	case owlIRI("SymmetricProperty"):
		c.o.Add(owl.SymmetricObjectProperty{Property: owl.ObjectProperty(self)})
		return
	case owlIRI("AsymmetricProperty"):
		c.o.Add(owl.AsymmetricObjectProperty{Property: owl.ObjectProperty(self)})
		return
	case owlIRI("ReflexiveProperty"):
		c.o.Add(owl.ReflexiveObjectProperty{Property: owl.ObjectProperty(self)})
		return
	case owlIRI("IrreflexiveProperty"):
		c.o.Add(owl.IrreflexiveObjectProperty{Property: owl.ObjectProperty(self)})
		return
	}
	if _, isEntityType := typeKinds[rdf.IRI(subj)]; isEntityType {
		return // already a declaration
	}
	if rdf.IRI(subj) == rdf.IRI(rdf.NSRDF+"Property") || rdf.IRI(subj) == owlIRI("Ontology") {
		return
	}

	if k, _ := c.kindOf(self); k == owl.KindDatatype {
		c.skip(t, "datatype declared as an instance")
		return
	}
	if k, known := c.kindOf(subj); known && k == owl.KindDatatype {
		c.skip(t, "individual typed by a datatype")
		return
	}

	// Any other type is a class assertion, which also makes the subject an
	// individual worth declaring.
	if ce, ok := c.classExpression(t.Object); ok {
		if c.owns(self) {
			c.o.Declare(owl.NamedIndividual(self))
		}
		c.o.Add(owl.ClassAssertion{Class: ce, Individual: owl.NamedIndividual(self)})
		return
	}
	c.skip(t, "unreadable type")
}

func (c *conv) subProperty(t rdf.Triple, kind owl.Kind) {
	sub, super := t.Subject.Text(), t.Object.Text()
	switch kind {
	case owl.KindDataProperty:
		c.o.Add(owl.SubDataPropertyOf{Sub: owl.DataProperty(sub), Super: owl.DataProperty(super)})
	case owl.KindAnnotationProperty:
		c.o.Add(owl.SubAnnotationPropertyOf{
			Sub:   owl.AnnotationProperty(sub),
			Super: owl.AnnotationProperty(super),
		})
	default:
		p, ok := c.objectPropertyExpression(t.Object)
		if !ok {
			c.skip(t, "unreadable superproperty")
			return
		}
		c.o.Add(owl.SubObjectPropertyOf{Sub: owl.ObjectProperty(sub), Super: p})
	}
}

func (c *conv) domain(t rdf.Triple, kind owl.Kind) {
	subj := t.Subject.Text()
	if kind == owl.KindAnnotationProperty {
		c.o.Add(owl.AnnotationPropertyDomain{
			Property: owl.AnnotationProperty(subj),
			Domain:   owl.IRI(t.Object.Text()),
		})
		return
	}
	ce, ok := c.classExpression(t.Object)
	if !ok {
		c.skip(t, "unreadable domain")
		return
	}
	if kind == owl.KindDataProperty {
		c.o.Add(owl.DataPropertyDomain{Property: owl.DataProperty(subj), Domain: ce})
		return
	}
	c.o.Add(owl.ObjectPropertyDomain{Property: owl.ObjectProperty(subj), Domain: ce})
}

func (c *conv) rangeOf(t rdf.Triple, kind owl.Kind) {
	subj := t.Subject.Text()
	switch kind {
	case owl.KindAnnotationProperty:
		c.o.Add(owl.AnnotationPropertyRange{
			Property: owl.AnnotationProperty(subj),
			Range:    owl.IRI(t.Object.Text()),
		})
	case owl.KindDataProperty:
		dr, ok := c.dataRange(t.Object)
		if !ok {
			c.skip(t, "unreadable data range")
			return
		}
		c.o.Add(owl.DataPropertyRange{Property: owl.DataProperty(subj), Range: dr})
	default:
		ce, ok := c.classExpression(t.Object)
		if !ok {
			c.skip(t, "unreadable range")
			return
		}
		c.o.Add(owl.ObjectPropertyRange{Property: owl.ObjectProperty(subj), Range: ce})
	}
}

func (c *conv) hasKey(t rdf.Triple) {
	members, ok := c.g.List(t.Object)
	if !ok {
		c.skip(t, "unreadable key list")
		return
	}
	key := owl.HasKey{Class: owl.Class(t.Subject.Text())}
	for _, m := range members {
		if k, _ := c.kindOf(m.Text()); k == owl.KindDataProperty {
			key.DataProperties = append(key.DataProperties, owl.DataProperty(m.Text()))
		} else {
			key.ObjectProperties = append(key.ObjectProperties, owl.ObjectProperty(m.Text()))
		}
	}
	c.o.Add(key)
}

func (c *conv) annotation(t rdf.Triple) {
	if _, ok := t.Subject.(rdf.IRI); !ok {
		c.skip(t, "annotation on a blank node")
		return
	}
	v, ok := c.annotationValue(t.Object)
	if !ok {
		c.skip(t, "unreadable annotation value")
		return
	}
	c.o.Add(owl.AnnotationAssertion{
		Property: owl.AnnotationProperty(t.Predicate),
		Subject:  owl.IRI(t.Subject.Text()),
		Value:    v,
	})
}

func (c *conv) annotationValue(o rdf.Term) (owl.AnnotationValue, bool) {
	switch v := o.(type) {
	case rdf.IRI:
		return owl.IRI(v), true
	case rdf.Literal:
		return literal(v), true
	}
	return nil, false
}

func literal(l rdf.Literal) owl.Literal {
	if l.Lang != "" {
		return owl.LangStr(l.Value, l.Lang)
	}
	dt := l.Datatype
	if dt == "" {
		dt = rdf.XSDString
	}
	return owl.Literal{Value: l.Value, Datatype: owl.Datatype(dt)}
}

// --- expressions ------------------------------------------------------------

// classExpression reads a term in class position. A named class denotes
// itself; only an anonymous one is read from the predicates hanging off it.
func (c *conv) classExpression(t rdf.Term) (owl.ClassExpression, bool) {
	if iri, ok := t.(rdf.IRI); ok {
		if k, known := c.kindOf(string(iri)); known && k != owl.KindClass {
			return nil, false
		}
		return owl.Class(iri), true
	}
	if _, ok := t.(rdf.Blank); !ok {
		return nil, false
	}
	return c.definition(t)
}

// definition reads the class expression a term's own predicates spell out.
// It works on a named subject too, which is how a class defined by
// owl:unionOf or owl:oneOf on itself is read.
func (c *conv) definition(b rdf.Term) (owl.ClassExpression, bool) {
	if _, isRestriction := c.g.Object(b, owlIRI("onProperty")); isRestriction {
		return c.restriction(b)
	}
	if o, ok := c.g.Object(b, owlIRI("intersectionOf")); ok {
		ops, ok := c.classList(o)
		return owl.ObjectIntersectionOf(ops), ok
	}
	if o, ok := c.g.Object(b, owlIRI("unionOf")); ok {
		ops, ok := c.classList(o)
		return owl.ObjectUnionOf(ops), ok
	}
	if o, ok := c.g.Object(b, owlIRI("complementOf")); ok {
		op, ok := c.classExpression(o)
		return owl.ObjectComplementOf{Operand: op}, ok
	}
	if o, ok := c.g.Object(b, owlIRI("oneOf")); ok {
		members, ok := c.g.List(o)
		if !ok {
			return nil, false
		}
		var out owl.ObjectOneOf
		for _, m := range members {
			if _, isIRI := m.(rdf.IRI); !isIRI {
				return nil, false
			}
			out = append(out, owl.NamedIndividual(m.Text()))
		}
		return out, true
	}
	return nil, false
}

// classExpression for a named class defined by intersectionOf and friends: the
// same reader applies, because those predicates hang off the subject either
// way. This wrapper only exists to name the case.
func (c *conv) classList(t rdf.Term) ([]owl.ClassExpression, bool) {
	members, ok := c.g.List(t)
	if !ok {
		return nil, false
	}
	out := make([]owl.ClassExpression, 0, len(members))
	for _, m := range members {
		ce, ok := c.classExpression(m)
		if !ok {
			return nil, false
		}
		out = append(out, ce)
	}
	return out, true
}

func (c *conv) propertyList(t rdf.Term) ([]owl.ObjectPropertyExpression, bool) {
	members, ok := c.g.List(t)
	if !ok {
		return nil, false
	}
	out := make([]owl.ObjectPropertyExpression, 0, len(members))
	for _, m := range members {
		p, ok := c.objectPropertyExpression(m)
		if !ok {
			return nil, false
		}
		out = append(out, p)
	}
	return out, true
}

func (c *conv) objectPropertyExpression(t rdf.Term) (owl.ObjectPropertyExpression, bool) {
	switch v := t.(type) {
	case rdf.IRI:
		return owl.ObjectProperty(v), true
	case rdf.Blank:
		if inner, ok := c.g.Object(v, owlIRI("inverseOf")); ok {
			if iri, ok := inner.(rdf.IRI); ok {
				return owl.ObjectInverseOf{Property: owl.ObjectProperty(iri)}, true
			}
		}
	}
	return nil, false
}

// restriction reads an owl:Restriction. Which of the object and data forms
// applies follows from the restricted property's kind, exactly as the OWL 2
// mapping says it should.
func (c *conv) restriction(b rdf.Term) (owl.ClassExpression, bool) {
	prop, _ := c.g.Object(b, owlIRI("onProperty"))
	kind, _ := c.kindOf(prop.Text())
	data := kind == owl.KindDataProperty

	if data {
		return c.dataRestriction(b, owl.DataProperty(prop.Text()))
	}
	p, ok := c.objectPropertyExpression(prop)
	if !ok {
		return nil, false
	}

	if o, ok := c.g.Object(b, owlIRI("someValuesFrom")); ok {
		filler, ok := c.classExpression(o)
		return owl.ObjectSomeValuesFrom{Property: p, Filler: filler}, ok
	}
	if o, ok := c.g.Object(b, owlIRI("allValuesFrom")); ok {
		filler, ok := c.classExpression(o)
		return owl.ObjectAllValuesFrom{Property: p, Filler: filler}, ok
	}
	if o, ok := c.g.Object(b, owlIRI("hasValue")); ok {
		if _, isIRI := o.(rdf.IRI); !isIRI {
			return nil, false
		}
		return owl.ObjectHasValue{Property: p, Value: owl.NamedIndividual(o.Text())}, true
	}
	if o, ok := c.g.Object(b, owlIRI("hasSelf")); ok && o.Text() == "true" {
		return owl.ObjectHasSelf{Property: p}, true
	}

	n, filler, form, ok := c.cardinality(b)
	if !ok {
		return nil, false
	}
	var fillers []owl.ClassExpression
	if filler != nil {
		ce, ok := c.classExpression(filler)
		if !ok {
			return nil, false
		}
		fillers = append(fillers, ce)
	}
	switch form {
	case "min":
		return owl.Min(n, p, fillers...), true
	case "max":
		return owl.Max(n, p, fillers...), true
	default:
		return owl.Exactly(n, p, fillers...), true
	}
}

func (c *conv) dataRestriction(b rdf.Term, p owl.DataProperty) (owl.ClassExpression, bool) {
	if o, ok := c.g.Object(b, owlIRI("someValuesFrom")); ok {
		dr, ok := c.dataRange(o)
		return owl.DataSomeValuesFrom{Property: p, Range: dr}, ok
	}
	if o, ok := c.g.Object(b, owlIRI("allValuesFrom")); ok {
		dr, ok := c.dataRange(o)
		return owl.DataAllValuesFrom{Property: p, Range: dr}, ok
	}
	if o, ok := c.g.Object(b, owlIRI("hasValue")); ok {
		l, ok := o.(rdf.Literal)
		if !ok {
			return nil, false
		}
		return owl.DataHasValue{Property: p, Value: literal(l)}, true
	}

	n, filler, form, ok := c.cardinality(b)
	if !ok {
		return nil, false
	}
	var dr owl.DataRange
	if filler != nil {
		if dr, ok = c.dataRange(filler); !ok {
			return nil, false
		}
	}
	switch form {
	case "min":
		return owl.DataMinCardinality{N: n, Property: p, Range: dr}, true
	case "max":
		return owl.DataMaxCardinality{N: n, Property: p, Range: dr}, true
	default:
		return owl.DataExactCardinality{N: n, Property: p, Range: dr}, true
	}
}

// cardinality reads the cardinality of a restriction, returning the bound, the
// qualifying filler if there is one, and which of min, max or exact it is.
func (c *conv) cardinality(b rdf.Term) (n int, filler rdf.Term, form string, ok bool) {
	for _, cand := range []struct {
		pred rdf.IRI
		form string
	}{
		{owlIRI("minCardinality"), "min"},
		{owlIRI("minQualifiedCardinality"), "min"},
		{owlIRI("maxCardinality"), "max"},
		{owlIRI("maxQualifiedCardinality"), "max"},
		{owlIRI("cardinality"), "exact"},
		{owlIRI("qualifiedCardinality"), "exact"},
	} {
		o, has := c.g.Object(b, cand.pred)
		if !has {
			continue
		}
		v, err := strconv.Atoi(strings.TrimSpace(o.Text()))
		if err != nil {
			return 0, nil, "", false
		}
		if f, has := c.g.Object(b, owlIRI("onClass")); has {
			filler = f
		} else if f, has := c.g.Object(b, owlIRI("onDataRange")); has {
			filler = f
		}
		return v, filler, cand.form, true
	}
	return 0, nil, "", false
}

// dataRange reads a term in data range position.
func (c *conv) dataRange(t rdf.Term) (owl.DataRange, bool) {
	if iri, ok := t.(rdf.IRI); ok {
		if k, known := c.kindOf(string(iri)); known && k != owl.KindDatatype {
			return nil, false
		}
		return owl.Datatype(iri), true
	}
	if _, ok := t.(rdf.Blank); !ok {
		return nil, false
	}
	return c.dataDefinition(t)
}

// dataDefinition reads the data range a term's own predicates spell out, on a
// named datatype as well as an anonymous one.
func (c *conv) dataDefinition(b rdf.Term) (owl.DataRange, bool) {
	if o, ok := c.g.Object(b, owlIRI("onDatatype")); ok {
		base, ok := o.(rdf.IRI)
		if !ok {
			return nil, false
		}
		restrictions := c.g.Objects(b, owlIRI("withRestrictions"))
		if len(restrictions) == 0 {
			return owl.Datatype(base), true
		}
		facets, ok := c.facets(restrictions[0])
		if !ok {
			return nil, false
		}
		return owl.DatatypeRestriction{Datatype: owl.Datatype(base), Facets: facets}, true
	}
	if o, ok := c.g.Object(b, owlIRI("datatypeComplementOf")); ok {
		inner, ok := c.dataRange(o)
		return owl.DataComplementOf{Operand: inner}, ok
	}
	if o, ok := c.g.Object(b, owlIRI("oneOf")); ok {
		members, ok := c.g.List(o)
		if !ok {
			return nil, false
		}
		var out owl.DataOneOf
		for _, m := range members {
			l, ok := m.(rdf.Literal)
			if !ok {
				return nil, false
			}
			out = append(out, literal(l))
		}
		return out, true
	}
	return nil, false
}

func (c *conv) facets(head rdf.Term) ([]owl.FacetRestriction, bool) {
	cells, ok := c.g.List(head)
	if !ok {
		return nil, false
	}
	var out []owl.FacetRestriction
	for _, cell := range cells {
		b, ok := cell.(rdf.Blank)
		if !ok {
			return nil, false
		}
		found := false
		for _, t := range c.g.Triples {
			if t.Subject != rdf.Term(b) {
				continue
			}
			l, ok := t.Object.(rdf.Literal)
			if !ok {
				continue
			}
			out = append(out, owl.FacetRestriction{Facet: owl.IRI(t.Predicate), Value: literal(l)})
			found = true
		}
		if !found {
			return nil, false
		}
	}
	return out, true
}

// Summary describes what a conversion produced, for the generator's log.
func (r *Result) Summary() string {
	return fmt.Sprintf("%d axioms, %d entities, %d skipped",
		r.Ontology.Len(), len(r.Ontology.Signature()), len(r.Skipped))
}
