package owl

// Kind identifies one of the six OWL 2 entity types.
type Kind int

// The six OWL 2 entity kinds.
const (
	KindClass Kind = iota
	KindDatatype
	KindObjectProperty
	KindDataProperty
	KindAnnotationProperty
	KindNamedIndividual
)

func (k Kind) String() string {
	switch k {
	case KindClass:
		return "Class"
	case KindDatatype:
		return "Datatype"
	case KindObjectProperty:
		return "ObjectProperty"
	case KindDataProperty:
		return "DataProperty"
	case KindAnnotationProperty:
		return "AnnotationProperty"
	case KindNamedIndividual:
		return "NamedIndividual"
	}
	return "UnknownKind"
}

// Entity is a named OWL 2 term: a class, datatype, object property, data
// property, annotation property or named individual. Entities are comparable
// string values, so they work directly as map keys.
type Entity interface {
	Node
	IRI() IRI
	Kind() Kind
}

// Class is a named OWL class. It is also the simplest class expression.
type Class IRI

// Datatype is a named OWL datatype. It is also the simplest data range.
type Datatype IRI

// ObjectProperty relates individuals to individuals.
type ObjectProperty IRI

// DataProperty relates individuals to literals.
type DataProperty IRI

// AnnotationProperty carries non-logical metadata such as labels and comments.
type AnnotationProperty IRI

// NamedIndividual is an individual identified by an IRI.
type NamedIndividual IRI

// AnonymousIndividual is an individual identified only by a local node ID. It
// is not an entity: it has no IRI and never appears in a signature.
type AnonymousIndividual string

func (c Class) IRI() IRI              { return IRI(c) }
func (d Datatype) IRI() IRI           { return IRI(d) }
func (p ObjectProperty) IRI() IRI     { return IRI(p) }
func (p DataProperty) IRI() IRI       { return IRI(p) }
func (p AnnotationProperty) IRI() IRI { return IRI(p) }
func (i NamedIndividual) IRI() IRI    { return IRI(i) }

func (Class) Kind() Kind              { return KindClass }
func (Datatype) Kind() Kind           { return KindDatatype }
func (ObjectProperty) Kind() Kind     { return KindObjectProperty }
func (DataProperty) Kind() Kind       { return KindDataProperty }
func (AnnotationProperty) Kind() Kind { return KindAnnotationProperty }
func (NamedIndividual) Kind() Kind    { return KindNamedIndividual }

func (c Class) String() string               { return string(c) }
func (d Datatype) String() string            { return string(d) }
func (p ObjectProperty) String() string      { return string(p) }
func (p DataProperty) String() string        { return string(p) }
func (p AnnotationProperty) String() string  { return string(p) }
func (i NamedIndividual) String() string     { return string(i) }
func (i AnonymousIndividual) String() string { return "_:" + string(i) }

func (Class) isClassExpression()                   {}
func (Datatype) isDataRange()                      {}
func (ObjectProperty) isObjectPropertyExpression() {}
func (DataProperty) isDataPropertyExpression()     {}
func (NamedIndividual) isIndividual()              {}
func (AnonymousIndividual) isIndividual()          {}
func (AnonymousIndividual) isAnnotationValue()     {}

// Standard namespaces.
const (
	NamespaceOWL  IRI = "http://www.w3.org/2002/07/owl#"
	NamespaceRDF  IRI = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
	NamespaceRDFS IRI = "http://www.w3.org/2000/01/rdf-schema#"
	NamespaceXSD  IRI = "http://www.w3.org/2001/XMLSchema#"
)

// Built-in classes and properties from the OWL vocabulary.
const (
	Thing   Class = Class(NamespaceOWL + "Thing")
	Nothing Class = Class(NamespaceOWL + "Nothing")

	TopObjectProperty    ObjectProperty = ObjectProperty(NamespaceOWL + "topObjectProperty")
	BottomObjectProperty ObjectProperty = ObjectProperty(NamespaceOWL + "bottomObjectProperty")
	TopDataProperty      DataProperty   = DataProperty(NamespaceOWL + "topDataProperty")
	BottomDataProperty   DataProperty   = DataProperty(NamespaceOWL + "bottomDataProperty")
)

// Built-in annotation properties.
const (
	RDFSLabel       AnnotationProperty = AnnotationProperty(NamespaceRDFS + "label")
	RDFSComment     AnnotationProperty = AnnotationProperty(NamespaceRDFS + "comment")
	RDFSSeeAlso     AnnotationProperty = AnnotationProperty(NamespaceRDFS + "seeAlso")
	RDFSIsDefinedBy AnnotationProperty = AnnotationProperty(NamespaceRDFS + "isDefinedBy")
	OWLVersionInfo  AnnotationProperty = AnnotationProperty(NamespaceOWL + "versionInfo")
	OWLDeprecated   AnnotationProperty = AnnotationProperty(NamespaceOWL + "deprecated")
)

// Built-in datatypes. RDFSLiteral is the top data range.
const (
	RDFSLiteral     Datatype = Datatype(NamespaceRDFS + "Literal")
	RDFPlainLiteral Datatype = Datatype(NamespaceRDF + "PlainLiteral")
	RDFLangString   Datatype = Datatype(NamespaceRDF + "langString")

	XSDString   Datatype = Datatype(NamespaceXSD + "string")
	XSDBoolean  Datatype = Datatype(NamespaceXSD + "boolean")
	XSDInteger  Datatype = Datatype(NamespaceXSD + "integer")
	XSDDecimal  Datatype = Datatype(NamespaceXSD + "decimal")
	XSDFloat    Datatype = Datatype(NamespaceXSD + "float")
	XSDDouble   Datatype = Datatype(NamespaceXSD + "double")
	XSDDateTime Datatype = Datatype(NamespaceXSD + "dateTime")
	XSDAnyURI   Datatype = Datatype(NamespaceXSD + "anyURI")
)

// Facets usable in a [DatatypeRestriction].
const (
	FacetMinInclusive IRI = NamespaceXSD + "minInclusive"
	FacetMaxInclusive IRI = NamespaceXSD + "maxInclusive"
	FacetMinExclusive IRI = NamespaceXSD + "minExclusive"
	FacetMaxExclusive IRI = NamespaceXSD + "maxExclusive"
	FacetLength       IRI = NamespaceXSD + "length"
	FacetMinLength    IRI = NamespaceXSD + "minLength"
	FacetMaxLength    IRI = NamespaceXSD + "maxLength"
	FacetPattern      IRI = NamespaceXSD + "pattern"
)
