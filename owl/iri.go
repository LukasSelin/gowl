// Package owl provides a typed, in-memory representation of OWL 2 ontologies
// together with an idiomatic Go DSL for building and inspecting them.
//
// The model is structural: it mirrors the abstract syntax of the OWL 2
// Structural Specification (entities, class expressions, axioms) rather than
// the RDF triples one particular serialization happens to use. Every OWL 2
// construct is a distinct Go type, so the compiler rejects nonsense such as
// using a data property where an object property belongs.
//
// String rendering follows two conventions: IRIs and entities print as their
// bare IRI, while expressions, axioms and ontologies print as OWL 2
// Functional-Style Syntax. Use [Functional] to force functional syntax for any
// construct, and [Ontology.Functional] to render a whole document with
// abbreviated IRIs.
package owl

import (
	"fmt"
	"sort"
	"strings"
)

// Node is any construct in the OWL 2 structural model: an IRI, an entity, a
// class expression, an axiom, an annotation, or an ontology.
type Node interface {
	String() string
}

// IRI is an Internationalized Resource Identifier. It names every entity in an
// ontology and is also a legal annotation value.
type IRI string

func (i IRI) String() string { return string(i) }

func (i IRI) isAnnotationValue() {}

// IsAbsolute reports whether i carries a scheme and so needs no prefix
// expansion. It is a syntactic check, not full RFC 3987 validation.
func (i IRI) IsAbsolute() bool {
	s := string(i)
	c := strings.IndexByte(s, ':')
	if c <= 0 {
		return false
	}
	if strings.HasPrefix(s[c:], "://") {
		return true
	}
	switch strings.ToLower(s[:c]) {
	case "urn", "mailto", "tag", "doi", "file", "data":
		return true
	}
	return false
}

// Prefixes maps prefix names to namespace IRIs, expanding CURIEs such as
// ":Pizza" or "rdfs:label" into absolute IRIs and abbreviating them back again
// for display. The zero value is not usable; call [NewPrefixes].
type Prefixes struct {
	ns map[string]IRI
}

// NewPrefixes returns a prefix table preloaded with the owl, rdf, rdfs and xsd
// namespaces. Declare a default namespace with Set("", ...) so that bare names
// like ":Pizza" resolve.
func NewPrefixes() *Prefixes {
	p := &Prefixes{ns: make(map[string]IRI, 8)}
	p.Set("owl", NamespaceOWL)
	p.Set("rdf", NamespaceRDF)
	p.Set("rdfs", NamespaceRDFS)
	p.Set("xsd", NamespaceXSD)
	return p
}

// Set declares prefix as an abbreviation for the namespace ns. A trailing
// colon on prefix is ignored, so "rdfs" and "rdfs:" are equivalent.
func (p *Prefixes) Set(prefix string, ns IRI) *Prefixes {
	p.ns[strings.TrimSuffix(prefix, ":")] = ns
	return p
}

// Namespace returns the namespace bound to prefix.
func (p *Prefixes) Namespace(prefix string) (IRI, bool) {
	ns, ok := p.ns[strings.TrimSuffix(prefix, ":")]
	return ns, ok
}

// Names returns the declared prefixes in sorted order.
func (p *Prefixes) Names() []string {
	out := make([]string, 0, len(p.ns))
	for k := range p.ns {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Expand resolves name to an absolute IRI. It accepts an absolute IRI as-is, a
// bracketed <http://...> form, a CURIE such as "rdfs:label", or a bare local
// name resolved against the default prefix.
func (p *Prefixes) Expand(name string) (IRI, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("owl: empty IRI")
	}
	if strings.HasPrefix(name, "<") && strings.HasSuffix(name, ">") {
		inner := name[1 : len(name)-1]
		if inner == "" {
			return "", fmt.Errorf("owl: empty IRI in %q", name)
		}
		return IRI(inner), nil
	}
	if IRI(name).IsAbsolute() {
		return IRI(name), nil
	}
	prefix, local := "", name
	if i := strings.IndexByte(name, ':'); i >= 0 {
		prefix, local = name[:i], name[i+1:]
	}
	ns, ok := p.ns[prefix]
	if !ok {
		if prefix == "" {
			return "", fmt.Errorf("owl: no default prefix declared, cannot expand %q", name)
		}
		return "", fmt.Errorf("owl: unknown prefix %q in %q", prefix, name)
	}
	if local == "" {
		return ns, nil
	}
	return ns + IRI(local), nil
}

// Compact abbreviates i as a CURIE using the longest matching namespace. It
// reports false when no prefix applies or the remainder is not a legal local
// name.
func (p *Prefixes) Compact(i IRI) (string, bool) {
	bestPrefix, bestNS, found := "", IRI(""), false
	for prefix, ns := range p.ns {
		if ns == "" || !strings.HasPrefix(string(i), string(ns)) {
			continue
		}
		if !isLocalName(string(i)[len(ns):]) {
			continue
		}
		// Longest namespace wins; ties break on prefix name for determinism.
		if !found || len(ns) > len(bestNS) || (len(ns) == len(bestNS) && prefix < bestPrefix) {
			bestPrefix, bestNS, found = prefix, ns, true
		}
	}
	if !found {
		return "", false
	}
	return bestPrefix + ":" + string(i)[len(bestNS):], true
}

// isLocalName reports whether s can appear after a prefix without escaping.
func isLocalName(s string) bool {
	if s == "" {
		return false
	}
	return !strings.ContainsAny(s, " \t\r\n/#?&:@<>\"'{}|^`\\()[],;")
}
