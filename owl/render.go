package owl

import (
	"io"
	"strconv"
	"strings"
)

// Functional renders any construct as OWL 2 Functional-Style Syntax with full
// IRIs. Use [Ontology.Functional] to render a document with abbreviated IRIs.
func Functional(n Node) string {
	w := &fsw{}
	w.node(n)
	return w.b.String()
}

// Equal reports whether two constructs are structurally identical. It compares
// functional-syntax renderings, so it is safe for slice-shaped constructs that
// Go's == would panic on, and it treats operand order as significant.
func Equal(a, b Node) bool { return Functional(a) == Functional(b) }

// Functional renders the whole ontology as an OWL 2 Functional-Style Syntax
// document, abbreviating IRIs with the ontology's prefixes.
func (o *Ontology) Functional() string {
	var b strings.Builder
	// WriteFunctional only fails when the writer does; a Builder never does.
	_ = o.WriteFunctional(&b)
	return b.String()
}

func (o *Ontology) String() string { return o.Functional() }

// WriteFunctional writes the ontology to w as an OWL 2 Functional-Style Syntax
// document.
func (o *Ontology) WriteFunctional(w io.Writer) error {
	f := &fsw{p: o.Prefixes}

	for _, name := range o.Prefixes.Names() {
		ns, _ := o.Prefixes.Namespace(name)
		f.b.WriteString("Prefix(" + name + ":=<" + string(ns) + ">)\n")
	}
	f.b.WriteString("\nOntology(")
	if o.IRI != "" {
		f.b.WriteString("<" + string(o.IRI) + ">")
		if o.VersionIRI != "" {
			f.b.WriteString(" <" + string(o.VersionIRI) + ">")
		}
	}
	f.b.WriteString("\n")
	for _, imp := range o.Imports {
		f.b.WriteString("    Import(<" + string(imp) + ">)\n")
	}
	for _, a := range o.Annotations {
		f.b.WriteString("    ")
		f.node(a)
		f.b.WriteString("\n")
	}
	for _, ax := range o.axioms {
		f.b.WriteString("    ")
		f.node(ax)
		f.b.WriteString("\n")
	}
	f.b.WriteString(")\n")

	_, err := io.WriteString(w, f.b.String())
	return err
}

// fsw renders constructs into a builder, optionally abbreviating IRIs.
type fsw struct {
	b strings.Builder
	p *Prefixes
}

func (w *fsw) iri(i IRI) {
	if w.p != nil {
		if c, ok := w.p.Compact(i); ok {
			w.b.WriteString(c)
			return
		}
	}
	w.b.WriteString("<" + string(i) + ">")
}

// call writes name(arg1 arg2 ...).
func (w *fsw) call(name string, args ...Node) {
	w.b.WriteString(name)
	w.b.WriteByte('(')
	for i, a := range args {
		if i > 0 {
			w.b.WriteByte(' ')
		}
		w.node(a)
	}
	w.b.WriteByte(')')
}

// group writes (arg1 arg2 ...), used for the parenthesised operand lists in
// HasKey and property chains.
func (w *fsw) group(args []Node) {
	w.b.WriteByte('(')
	for i, a := range args {
		if i > 0 {
			w.b.WriteByte(' ')
		}
		w.node(a)
	}
	w.b.WriteByte(')')
}

// cardinality writes Name(n property [filler]).
func (w *fsw) cardinality(name string, n int, prop Node, filler Node) {
	w.b.WriteString(name + "(" + strconv.Itoa(n) + " ")
	w.node(prop)
	if filler != nil {
		w.b.WriteByte(' ')
		w.node(filler)
	}
	w.b.WriteByte(')')
}

func (w *fsw) literal(l Literal) {
	w.b.WriteString(quote(l.Value))
	switch {
	case l.Lang != "":
		w.b.WriteString("@" + l.Lang)
	case l.Datatype != "":
		w.b.WriteString("^^")
		w.iri(IRI(l.Datatype))
	}
}

func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// nodes widens a slice of constructs to []Node.
func nodes[T Node](in []T) []Node {
	out := make([]Node, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

func (w *fsw) node(n Node) {
	switch x := n.(type) {
	case nil:
		w.b.WriteString("<nil>")

	// Entities and simple values.
	case IRI:
		w.iri(x)
	case Class:
		w.iri(IRI(x))
	case Datatype:
		w.iri(IRI(x))
	case ObjectProperty:
		w.iri(IRI(x))
	case DataProperty:
		w.iri(IRI(x))
	case AnnotationProperty:
		w.iri(IRI(x))
	case NamedIndividual:
		w.iri(IRI(x))
	case AnonymousIndividual:
		w.b.WriteString("_:" + string(x))
	case Literal:
		w.literal(x)
	case Annotation:
		w.call("Annotation", x.Property, x.Value)
	case FacetRestriction:
		w.iri(x.Facet)
		w.b.WriteByte(' ')
		w.literal(x.Value)

	// Class expressions.
	case ObjectIntersectionOf:
		w.call("ObjectIntersectionOf", nodes(x)...)
	case ObjectUnionOf:
		w.call("ObjectUnionOf", nodes(x)...)
	case ObjectComplementOf:
		w.call("ObjectComplementOf", x.Operand)
	case ObjectOneOf:
		w.call("ObjectOneOf", nodes(x)...)
	case ObjectSomeValuesFrom:
		w.call("ObjectSomeValuesFrom", x.Property, x.Filler)
	case ObjectAllValuesFrom:
		w.call("ObjectAllValuesFrom", x.Property, x.Filler)
	case ObjectHasValue:
		w.call("ObjectHasValue", x.Property, x.Value)
	case ObjectHasSelf:
		w.call("ObjectHasSelf", x.Property)
	case ObjectMinCardinality:
		w.cardinality("ObjectMinCardinality", x.N, x.Property, x.Filler)
	case ObjectMaxCardinality:
		w.cardinality("ObjectMaxCardinality", x.N, x.Property, x.Filler)
	case ObjectExactCardinality:
		w.cardinality("ObjectExactCardinality", x.N, x.Property, x.Filler)
	case DataSomeValuesFrom:
		w.call("DataSomeValuesFrom", x.Property, x.Range)
	case DataAllValuesFrom:
		w.call("DataAllValuesFrom", x.Property, x.Range)
	case DataHasValue:
		w.call("DataHasValue", x.Property, x.Value)
	case DataMinCardinality:
		w.cardinality("DataMinCardinality", x.N, x.Property, x.Range)
	case DataMaxCardinality:
		w.cardinality("DataMaxCardinality", x.N, x.Property, x.Range)
	case DataExactCardinality:
		w.cardinality("DataExactCardinality", x.N, x.Property, x.Range)

	// Property expressions and data ranges.
	case ObjectInverseOf:
		w.call("ObjectInverseOf", x.Property)
	case DataIntersectionOf:
		w.call("DataIntersectionOf", nodes(x)...)
	case DataUnionOf:
		w.call("DataUnionOf", nodes(x)...)
	case DataComplementOf:
		w.call("DataComplementOf", x.Operand)
	case DataOneOf:
		w.call("DataOneOf", nodes(x)...)
	case DatatypeRestriction:
		w.b.WriteString("DatatypeRestriction(")
		w.node(x.Datatype)
		for _, f := range x.Facets {
			w.b.WriteByte(' ')
			w.node(f)
		}
		w.b.WriteByte(')')

	// Axioms.
	case Declaration:
		w.b.WriteString("Declaration(")
		if x.Entity == nil {
			w.b.WriteString("<nil>")
		} else {
			w.b.WriteString(x.Entity.Kind().String() + "(")
			w.iri(x.Entity.IRI())
			w.b.WriteByte(')')
		}
		w.b.WriteByte(')')
	case SubClassOf:
		w.call("SubClassOf", x.Sub, x.Super)
	case EquivalentClasses:
		w.call("EquivalentClasses", nodes(x)...)
	case DisjointClasses:
		w.call("DisjointClasses", nodes(x)...)
	case DisjointUnion:
		w.call("DisjointUnion", append([]Node{x.Class}, nodes(x.Operands)...)...)
	case SubObjectPropertyOf:
		w.call("SubObjectPropertyOf", x.Sub, x.Super)
	case SubPropertyChainOf:
		w.b.WriteString("SubObjectPropertyOf(ObjectPropertyChain")
		w.group(nodes(x.Chain))
		w.b.WriteByte(' ')
		w.node(x.Super)
		w.b.WriteByte(')')
	case EquivalentObjectProperties:
		w.call("EquivalentObjectProperties", nodes(x)...)
	case DisjointObjectProperties:
		w.call("DisjointObjectProperties", nodes(x)...)
	case InverseObjectProperties:
		w.call("InverseObjectProperties", x.First, x.Second)
	case ObjectPropertyDomain:
		w.call("ObjectPropertyDomain", x.Property, x.Domain)
	case ObjectPropertyRange:
		w.call("ObjectPropertyRange", x.Property, x.Range)
	case FunctionalObjectProperty:
		w.call("FunctionalObjectProperty", x.Property)
	case InverseFunctionalObjectProperty:
		w.call("InverseFunctionalObjectProperty", x.Property)
	case ReflexiveObjectProperty:
		w.call("ReflexiveObjectProperty", x.Property)
	case IrreflexiveObjectProperty:
		w.call("IrreflexiveObjectProperty", x.Property)
	case SymmetricObjectProperty:
		w.call("SymmetricObjectProperty", x.Property)
	case AsymmetricObjectProperty:
		w.call("AsymmetricObjectProperty", x.Property)
	case TransitiveObjectProperty:
		w.call("TransitiveObjectProperty", x.Property)
	case SubDataPropertyOf:
		w.call("SubDataPropertyOf", x.Sub, x.Super)
	case EquivalentDataProperties:
		w.call("EquivalentDataProperties", nodes(x)...)
	case DisjointDataProperties:
		w.call("DisjointDataProperties", nodes(x)...)
	case DataPropertyDomain:
		w.call("DataPropertyDomain", x.Property, x.Domain)
	case DataPropertyRange:
		w.call("DataPropertyRange", x.Property, x.Range)
	case FunctionalDataProperty:
		w.call("FunctionalDataProperty", x.Property)
	case DatatypeDefinition:
		w.call("DatatypeDefinition", x.Datatype, x.Range)
	case HasKey:
		w.b.WriteString("HasKey(")
		w.node(x.Class)
		w.b.WriteByte(' ')
		w.group(nodes(x.ObjectProperties))
		w.b.WriteByte(' ')
		w.group(nodes(x.DataProperties))
		w.b.WriteByte(')')
	case SameIndividual:
		w.call("SameIndividual", nodes(x)...)
	case DifferentIndividuals:
		w.call("DifferentIndividuals", nodes(x)...)
	case ClassAssertion:
		w.call("ClassAssertion", x.Class, x.Individual)
	case ObjectPropertyAssertion:
		w.call("ObjectPropertyAssertion", x.Property, x.Subject, x.Object)
	case NegativeObjectPropertyAssertion:
		w.call("NegativeObjectPropertyAssertion", x.Property, x.Subject, x.Object)
	case DataPropertyAssertion:
		w.call("DataPropertyAssertion", x.Property, x.Subject, x.Value)
	case NegativeDataPropertyAssertion:
		w.call("NegativeDataPropertyAssertion", x.Property, x.Subject, x.Value)
	case AnnotationAssertion:
		w.call("AnnotationAssertion", x.Property, x.Subject, x.Value)
	case SubAnnotationPropertyOf:
		w.call("SubAnnotationPropertyOf", x.Sub, x.Super)
	case AnnotationPropertyDomain:
		w.call("AnnotationPropertyDomain", x.Property, x.Domain)
	case AnnotationPropertyRange:
		w.call("AnnotationPropertyRange", x.Property, x.Range)

	default:
		w.b.WriteString(x.String())
	}
}
