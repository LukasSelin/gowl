package main

import "gowl/internal/rdf"

// A source is one published vocabulary and how to read it.
//
// The list below is the point of this program, and every entry is a judgement:
// these are the vocabularies a Go program describing things on the web
// actually reaches for. They are all either W3C Recommendations, ISO or DCMI
// standards, or de-facto standards with a decade of deployment behind them.
// Nothing here is generated from a registry crawl — a vocabulary earns a place
// by being one other people's data already uses.
type source struct {
	// pkg is the Go package name, and the directory under vocab/.
	pkg string
	// title and summary go into the package doc comment.
	title   string
	summary string

	url string
	// format is "turtle" or "rdfxml". Several of these documents are only
	// published in one of the two.
	format string
	// base resolves relative IRI references. Documents that use them and do
	// not declare their own base need it set here.
	base string

	prefix    string
	namespace string
	// namespaces defaults to the single namespace above. It is a list because
	// one document sometimes defines terms in more than one namespace.
	namespaces []string
	// iri overrides the ontology IRI when a document declares none.
	iri string

	rangeHints    []rdf.IRI
	datatypeRoots []rdf.IRI
}

func (s source) allNamespaces() []string {
	if len(s.namespaces) > 0 {
		return s.namespaces
	}
	return []string{s.namespace}
}

const (
	nsSchema  = "https://schema.org/"
	nsDCTerms = "http://purl.org/dc/terms/"
)

var sources = []source{
	{
		pkg:     "rdf",
		title:   "RDF vocabulary",
		summary: "The terms RDF itself defines: rdf:type, the container membership properties, the reification vocabulary and the datatypes RDF 1.1 adds to XML Schema.",
		url:     "http://www.w3.org/1999/02/22-rdf-syntax-ns",
		format:  "turtle",

		prefix:    "rdf",
		namespace: rdf.NSRDF,
	},
	{
		pkg:     "rdfs",
		title:   "RDF Schema vocabulary",
		summary: "RDF Schema: classes, properties, the subclass and subproperty relations, and the annotation properties every other vocabulary on this list is documented with.",
		url:     "http://www.w3.org/2000/01/rdf-schema",
		format:  "turtle",

		prefix:    "rdfs",
		namespace: rdf.NSRDFS,
	},
	{
		pkg:     "skos",
		title:   "SKOS Core vocabulary",
		summary: "Simple Knowledge Organization System: concepts, concept schemes and the broader/narrower/related relations that thesauri, taxonomies and subject heading lists are built from. A W3C Recommendation since 2009.",
		url:     "https://www.w3.org/2009/08/skos-reference/skos.rdf",
		format:  "rdfxml",
		base:    "http://www.w3.org/2004/02/skos/core",

		prefix:    "skos",
		namespace: "http://www.w3.org/2004/02/skos/core#",
		iri:       "http://www.w3.org/2004/02/skos/core",
	},
	{
		pkg:     "skosxl",
		title:   "SKOS eXtension for Labels",
		summary: "SKOS-XL, which turns a label into a resource of its own so that labels can carry their own provenance and relationships.",
		url:     "https://www.w3.org/2008/05/skos-xl.rdf",
		format:  "rdfxml",
		base:    "http://www.w3.org/2008/05/skos-xl",

		prefix:    "skosxl",
		namespace: "http://www.w3.org/2008/05/skos-xl#",
		iri:       "http://www.w3.org/2008/05/skos-xl",
	},
	{
		pkg:     "dcterms",
		title:   "DCMI Metadata Terms",
		summary: "Dublin Core's full term set: title, creator, subject, license, provenance and the rest of the descriptive metadata vocabulary that libraries, repositories and data portals have standardised on.",
		url:     "https://www.dublincore.org/specifications/dublin-core/dcmi-terms/dublin_core_terms.ttl",
		format:  "turtle",

		prefix:    "dcterms",
		namespace: nsDCTerms,
		iri:       "http://purl.org/dc/terms/",
		// The document references the DCAM and DCMI Type namespaces — as the
		// types of its encoding schemes, and for dcam:domainIncludes — but it
		// defines neither. Claiming them here would put four terms in the
		// package that it cannot say anything about; both are published
		// separately and would be their own source entries.
	},
	{
		pkg:     "dc",
		title:   "Dublin Core Metadata Element Set",
		summary: "The original fifteen Dublin Core elements. They are superseded by the dcterms package for new work, and are here because a great deal of published data still uses them.",
		url:     "https://www.dublincore.org/specifications/dublin-core/dcmi-terms/dublin_core_elements.ttl",
		format:  "turtle",

		prefix:    "dc",
		namespace: "http://purl.org/dc/elements/1.1/",
		iri:       "http://purl.org/dc/elements/1.1/",
	},
	{
		pkg:     "foaf",
		title:   "FOAF vocabulary",
		summary: "Friend of a Friend: people, the accounts and documents they hold, and the links between them. The oldest widely deployed vocabulary for describing agents on the web.",
		url:     "http://xmlns.com/foaf/spec/index.rdf",
		format:  "rdfxml",
		base:    "http://xmlns.com/foaf/0.1/",

		prefix:    "foaf",
		namespace: "http://xmlns.com/foaf/0.1/",
		iri:       "http://xmlns.com/foaf/0.1/",
	},
	{
		pkg:     "prov",
		title:   "PROV-O provenance ontology",
		summary: "PROV-O: entities, activities and agents, and the derivation, attribution and generation relations between them. The W3C's answer to where a piece of data came from.",
		url:     "https://www.w3.org/ns/prov-o.ttl",
		format:  "turtle",

		prefix:    "prov",
		namespace: "http://www.w3.org/ns/prov#",
		iri:       "http://www.w3.org/ns/prov#",
	},
	{
		pkg:     "dcat",
		title:   "Data Catalog Vocabulary",
		summary: "DCAT: catalogs, datasets, distributions and data services. The interchange format for open data portals, and the base of national profiles such as DCAT-AP.",
		url:     "https://www.w3.org/ns/dcat.ttl",
		format:  "turtle",

		prefix:    "dcat",
		namespace: "http://www.w3.org/ns/dcat#",
	},
	{
		pkg:     "org",
		title:   "Organization Ontology",
		summary: "The W3C Organization Ontology: organizations, their sub-units, the posts within them and the people holding those posts, over time.",
		url:     "https://www.w3.org/ns/org.ttl",
		format:  "turtle",

		prefix:    "org",
		namespace: "http://www.w3.org/ns/org#",
	},
	{
		pkg:     "time",
		title:   "OWL-Time ontology",
		summary: "OWL-Time: instants, intervals, the thirteen Allen interval relations, and durations in both clock and calendar terms.",
		url:     "https://www.w3.org/2006/time",
		format:  "turtle",

		prefix:    "time",
		namespace: "http://www.w3.org/2006/time#",
	},
	{
		pkg:     "schema",
		title:   "Schema.org vocabulary",
		summary: "Schema.org, the vocabulary search engines read. It is far larger than the others here and models the world loosely: its domainIncludes and rangeIncludes are kept as annotations rather than promoted to OWL domain and range, because that is what they mean.",
		url:     "https://schema.org/version/latest/schemaorg-current-https.ttl",
		format:  "turtle",

		prefix:    "schema",
		namespace: nsSchema,
		iri:       nsSchema,

		rangeHints:    []rdf.IRI{nsSchema + "rangeIncludes"},
		datatypeRoots: []rdf.IRI{nsSchema + "DataType"},
	},
}
