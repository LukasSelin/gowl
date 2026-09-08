// Package rdf reads RDF graphs from Turtle and RDF/XML.
//
// It exists to feed cmd/vocabgen, which turns published vocabularies into Go
// packages, and covers only what those documents use. It is deliberately not
// part of gowl's public API: the OWL model is structural, and a triple store
// is a different thing that would have to be designed as one.
//
// The reader is a syntactic front end. Nothing here understands OWL; mapping
// triples onto the structural model is the job of gowl/internal/rdfowl.
package rdf

import (
	"fmt"
	"sort"
	"strings"
)

// A Term is one position in a triple: an IRI, a blank node or a literal.
type Term interface {
	// Text is the term's lexical content: the IRI, the blank node label, or
	// the literal's string value.
	Text() string
	isTerm()
}

// IRI is an absolute IRI. Relative references are resolved while parsing, so a
// term that reaches a caller is always absolute.
type IRI string

// Blank is a blank node, identified by a label unique within one parse.
type Blank string

// Literal is a string value with either a datatype or a language tag. A plain
// literal carries the xsd:string datatype, matching RDF 1.1.
type Literal struct {
	Value    string
	Datatype IRI
	Lang     string
}

func (i IRI) Text() string     { return string(i) }
func (b Blank) Text() string   { return string(b) }
func (l Literal) Text() string { return l.Value }

func (IRI) isTerm()     {}
func (Blank) isTerm()   {}
func (Literal) isTerm() {}

func (i IRI) String() string   { return "<" + string(i) + ">" }
func (b Blank) String() string { return "_:" + string(b) }

func (l Literal) String() string {
	s := quote(l.Value)
	switch {
	case l.Lang != "":
		return s + "@" + l.Lang
	case l.Datatype != "" && l.Datatype != XSDString:
		return s + "^^" + l.Datatype.String()
	}
	return s
}

func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString("\\\"")
		case '\\':
			b.WriteString("\\\\")
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case '\t':
			b.WriteString("\\t")
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// Triple is one statement. Subject is an [IRI] or a [Blank]; Object is any
// term.
type Triple struct {
	Subject   Term
	Predicate IRI
	Object    Term
}

func (t Triple) String() string {
	return fmt.Sprintf("%s %s %s .", t.Subject, t.Predicate, t.Object)
}

// Well-known IRIs the readers and the OWL mapping both need.
const (
	NSRDF  = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
	NSRDFS = "http://www.w3.org/2000/01/rdf-schema#"
	NSOWL  = "http://www.w3.org/2002/07/owl#"
	NSXSD  = "http://www.w3.org/2001/XMLSchema#"

	RDFType  IRI = NSRDF + "type"
	RDFFirst IRI = NSRDF + "first"
	RDFRest  IRI = NSRDF + "rest"
	RDFNil   IRI = NSRDF + "nil"

	XSDString  IRI = NSXSD + "string"
	XSDBoolean IRI = NSXSD + "boolean"
	XSDInteger IRI = NSXSD + "integer"
	XSDDecimal IRI = NSXSD + "decimal"
	XSDDouble  IRI = NSXSD + "double"
)

// A Graph is a parsed document: its triples, plus the prefixes the document
// declared. The prefixes are kept because a generated package should carry the
// names the vocabulary's authors chose, not ones invented here.
type Graph struct {
	Triples  []Triple
	Prefixes map[string]IRI

	spo map[Term]map[IRI][]Term
}

// NewGraph indexes triples for lookup.
func NewGraph(triples []Triple, prefixes map[string]IRI) *Graph {
	g := &Graph{Triples: triples, Prefixes: prefixes, spo: make(map[Term]map[IRI][]Term)}
	if g.Prefixes == nil {
		g.Prefixes = make(map[string]IRI)
	}
	for _, t := range triples {
		byPred, ok := g.spo[t.Subject]
		if !ok {
			byPred = make(map[IRI][]Term)
			g.spo[t.Subject] = byPred
		}
		byPred[t.Predicate] = append(byPred[t.Predicate], t.Object)
	}
	return g
}

// Objects returns the objects of (s, p) in document order.
func (g *Graph) Objects(s Term, p IRI) []Term { return g.spo[s][p] }

// Object returns the first object of (s, p).
func (g *Graph) Object(s Term, p IRI) (Term, bool) {
	if o := g.spo[s][p]; len(o) > 0 {
		return o[0], true
	}
	return nil, false
}

// Has reports whether (s, p, o) is in the graph.
func (g *Graph) Has(s Term, p IRI, o Term) bool {
	for _, got := range g.spo[s][p] {
		if got == o {
			return true
		}
	}
	return false
}

// HasType reports whether s is stated to be of type class.
func (g *Graph) HasType(s Term, class IRI) bool { return g.Has(s, RDFType, class) }

// Subjects returns every distinct subject, IRIs sorted first and blank nodes
// after, so that generated output does not depend on map iteration order.
func (g *Graph) Subjects() []Term {
	out := make([]Term, 0, len(g.spo))
	for s := range g.spo {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if ai, bi := isIRI(a), isIRI(b); ai != bi {
			return ai
		}
		return a.Text() < b.Text()
	})
	return out
}

func isIRI(t Term) bool { _, ok := t.(IRI); return ok }

// List reads an RDF collection starting at head, reporting false if the chain
// is not a well-formed list.
func (g *Graph) List(head Term) ([]Term, bool) {
	var out []Term
	seen := make(map[Term]bool)
	for head != Term(RDFNil) {
		if seen[head] {
			return nil, false // cyclic rest chain
		}
		seen[head] = true
		first, ok := g.Object(head, RDFFirst)
		if !ok {
			return nil, false
		}
		rest, ok := g.Object(head, RDFRest)
		if !ok {
			return nil, false
		}
		out = append(out, first)
		head = rest
	}
	return out, true
}
