package rdf

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ParseXML reads an RDF/XML document. SKOS and FOAF are only published in this
// syntax, which is why it is here.
//
// It implements the striped syntax those documents use: node elements with
// rdf:about, rdf:ID and rdf:nodeID, property elements with rdf:resource,
// rdf:datatype and xml:lang, property attributes, rdf:parseType "Resource" and
// "Collection", and rdf:li containers. rdf:parseType="Literal" is rejected
// rather than mangled; no vocabulary in the source list uses it.
func ParseXML(r io.Reader, base string) (*Graph, error) {
	root, prefixes, err := readXML(r)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, fmt.Errorf("rdfxml: empty document")
	}
	p := &rdfxml{prefixes: prefixes}

	env := scope{base: base}
	// A document may drop the rdf:RDF wrapper when it holds a single node
	// element; handling both keeps the reader honest about what it accepts.
	if name(root.Name) == NSRDF+"RDF" {
		env = env.with(root)
		for _, kid := range root.Kids {
			if _, err := p.nodeElement(kid, env); err != nil {
				return nil, err
			}
		}
	} else if _, err := p.nodeElement(root, env); err != nil {
		return nil, err
	}
	return NewGraph(p.triples, p.prefixes), nil
}

type rdfxml struct {
	triples  []Triple
	prefixes map[string]IRI
	fresh    int
}

// scope carries the xml:base and xml:lang in force, which both inherit down
// the element tree.
type scope struct {
	base string
	lang string
}

func (s scope) with(n *xnode) scope {
	for _, a := range n.Attrs {
		if !isXMLNamespace(a.Name.Space) {
			continue
		}
		switch a.Name.Local {
		case "base":
			s.base = a.Value
		case "lang":
			s.lang = a.Value
		}
	}
	return s
}

// isXMLNamespace reports whether a namespace is the reserved xml: one.
// encoding/xml reports the predefined prefix unexpanded, but a document that
// declares it explicitly yields the full IRI.
func isXMLNamespace(space string) bool {
	return space == "xml" || space == "http://www.w3.org/XML/1998/namespace"
}

func (p *rdfxml) emit(s Term, pred IRI, o Term) {
	p.triples = append(p.triples, Triple{Subject: s, Predicate: pred, Object: o})
}

func (p *rdfxml) blank() Blank {
	p.fresh++
	return Blank(fmt.Sprintf("x%d", p.fresh))
}

// nodeElement reads one node element and returns the subject it describes.
func (p *rdfxml) nodeElement(n *xnode, env scope) (Term, error) {
	env = env.with(n)

	var subj Term
	switch {
	case attr(n, NSRDF, "about") != "":
		subj = IRI(resolveXML(env.base, attr(n, NSRDF, "about")))
	case attr(n, NSRDF, "ID") != "":
		subj = IRI(resolveXML(env.base, "#"+attr(n, NSRDF, "ID")))
	case attr(n, NSRDF, "nodeID") != "":
		subj = Blank("n" + attr(n, NSRDF, "nodeID"))
	default:
		subj = p.blank()
	}

	if t := name(n.Name); t != NSRDF+"Description" {
		p.emit(subj, RDFType, IRI(t))
	}
	if err := p.propertyAttrs(n, subj, env); err != nil {
		return nil, err
	}
	li := 0
	for _, kid := range n.Kids {
		if err := p.propertyElement(kid, subj, env, &li); err != nil {
			return nil, err
		}
	}
	return subj, nil
}

// propertyAttrs turns the non-reserved attributes of a node element into
// literal-valued triples, the abbreviation FOAF uses for its ontology header.
func (p *rdfxml) propertyAttrs(n *xnode, subj Term, env scope) error {
	for _, a := range n.Attrs {
		if a.Name.Space == "xmlns" || a.Name.Local == "xmlns" || isXMLNamespace(a.Name.Space) {
			continue
		}
		if a.Name.Space == NSRDF {
			switch a.Name.Local {
			case "about", "ID", "nodeID", "resource", "datatype", "parseType":
				continue
			case "type":
				p.emit(subj, RDFType, IRI(resolveXML(env.base, a.Value)))
				continue
			}
		}
		if a.Name.Space == "" {
			return fmt.Errorf("rdfxml: attribute %q has no namespace", a.Name.Local)
		}
		p.emit(subj, IRI(a.Name.Space+a.Name.Local), literal(a.Value, "", env.lang))
	}
	return nil
}

// propertyElement reads one property of subj. li counts rdf:li children so
// that a container's members get their rdf:_1, rdf:_2 … predicates.
func (p *rdfxml) propertyElement(n *xnode, subj Term, env scope, li *int) error {
	env = env.with(n)

	pred := IRI(name(n.Name))
	if pred == IRI(NSRDF+"li") {
		*li++
		pred = IRI(NSRDF + "_" + strconv.Itoa(*li))
	}

	switch attr(n, NSRDF, "parseType") {
	case "Resource":
		b := p.blank()
		p.emit(subj, pred, b)
		inner := 0
		for _, kid := range n.Kids {
			if err := p.propertyElement(kid, b, env, &inner); err != nil {
				return err
			}
		}
		return nil
	case "Collection":
		items := make([]Term, 0, len(n.Kids))
		for _, kid := range n.Kids {
			item, err := p.nodeElement(kid, env)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		p.emit(subj, pred, p.list(items))
		return nil
	case "Literal":
		return fmt.Errorf("rdfxml: rdf:parseType=%q is not supported", "Literal")
	case "":
	default:
		return fmt.Errorf("rdfxml: unknown rdf:parseType %q", attr(n, NSRDF, "parseType"))
	}

	if res := attr(n, NSRDF, "resource"); res != "" {
		obj := Term(IRI(resolveXML(env.base, res)))
		p.emit(subj, pred, obj)
		// An abbreviated node element: the remaining attributes describe the
		// object, not this property.
		return p.propertyAttrs(n, obj, env)
	}
	if id := attr(n, NSRDF, "nodeID"); id != "" {
		p.emit(subj, pred, Blank("n"+id))
		return nil
	}

	if len(n.Kids) > 0 {
		for _, kid := range n.Kids {
			obj, err := p.nodeElement(kid, env)
			if err != nil {
				return err
			}
			p.emit(subj, pred, obj)
		}
		return nil
	}

	// An empty property element with property attributes describes a fresh
	// blank node; otherwise the element's text is the value.
	if hasPropertyAttrs(n) {
		b := p.blank()
		p.emit(subj, pred, b)
		return p.propertyAttrs(n, b, env)
	}
	p.emit(subj, pred, literal(n.Text, attr(n, NSRDF, "datatype"), env.lang))
	return nil
}

func (p *rdfxml) list(items []Term) Term {
	if len(items) == 0 {
		return RDFNil
	}
	head := p.blank()
	node := head
	for i, item := range items {
		p.emit(node, RDFFirst, item)
		if i == len(items)-1 {
			p.emit(node, RDFRest, RDFNil)
			break
		}
		next := p.blank()
		p.emit(node, RDFRest, next)
		node = next
	}
	return head
}

func hasPropertyAttrs(n *xnode) bool {
	for _, a := range n.Attrs {
		if a.Name.Space == "xmlns" || a.Name.Local == "xmlns" || isXMLNamespace(a.Name.Space) {
			continue
		}
		if a.Name.Space == NSRDF && (a.Name.Local == "datatype" || a.Name.Local == "ID") {
			continue
		}
		return true
	}
	return false
}

func literal(value, datatype, lang string) Literal {
	switch {
	case datatype != "":
		return Literal{Value: value, Datatype: IRI(datatype)}
	case lang != "":
		return Literal{Value: value, Datatype: IRI(NSRDF + "langString"), Lang: lang}
	}
	return Literal{Value: value, Datatype: XSDString}
}

func name(n xml.Name) string { return n.Space + n.Local }

func attr(n *xnode, space, local string) string {
	for _, a := range n.Attrs {
		if a.Name.Space == space && a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// resolveXML applies an xml:base to a reference. Like the Turtle resolver it
// covers the reference forms documents actually use.
func resolveXML(base, ref string) string {
	if base == "" || strings.Contains(ref, "://") || strings.HasPrefix(ref, "urn:") {
		return ref
	}
	p := &ttl{base: IRI(base)}
	return string(p.resolve(ref))
}

// --- element tree -----------------------------------------------------------

// xnode is one element: encoding/xml's streaming API makes the striped syntax
// awkward to read directly, and these documents are small enough to hold.
type xnode struct {
	Name  xml.Name
	Attrs []xml.Attr
	Kids  []*xnode
	Text  string
}

func readXML(r io.Reader) (*xnode, map[string]IRI, error) {
	dec := xml.NewDecoder(r)
	dec.Strict = true

	prefixes := make(map[string]IRI)
	var root *xnode
	var stack []*xnode
	var text strings.Builder

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("rdfxml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &xnode{Name: t.Name, Attrs: t.Attr}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" {
					prefixes[a.Name.Local] = IRI(a.Value)
				}
			}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.Kids = append(parent.Kids, n)
			} else if root == nil {
				root = n
			}
			stack = append(stack, n)
			text.Reset()
		case xml.CharData:
			text.Write(t)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, nil, fmt.Errorf("rdfxml: unbalanced %s", t.Name.Local)
			}
			n := stack[len(stack)-1]
			if len(n.Kids) == 0 {
				n.Text = text.String()
			}
			stack = stack[:len(stack)-1]
			text.Reset()
		}
	}
	return root, prefixes, nil
}
