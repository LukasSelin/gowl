package vocab_test

import (
	"strings"
	"testing"

	"gowl/owl"
	"gowl/vocab"
	"gowl/vocab/dcterms"
	"gowl/vocab/foaf"
	"gowl/vocab/prov"
	"gowl/vocab/schema"
	"gowl/vocab/skos"
	"gowl/vocab/time"
)

// Every generated .ofn file must parse, and must actually say something. This
// is the test that catches a generator change that produces plausible but
// broken output.
func TestEveryVocabularyParses(t *testing.T) {
	for _, e := range vocab.All() {
		t.Run(e.Package, func(t *testing.T) {
			o := e.Ontology()
			if err := o.Err(); err != nil {
				t.Fatalf("Err = %v", err)
			}
			if o.Len() == 0 {
				t.Fatal("no axioms")
			}
			if o.IRI == "" {
				t.Error("no ontology IRI")
			}
			if len(o.Classes())+len(o.ObjectProperties())+len(o.DataProperties())+
				len(o.AnnotationProperties()) == 0 {
				t.Error("no classes and no properties")
			}
		})
	}
}

// A vocabulary's constants and its ontology must agree: every entity the
// ontology declares in the vocabulary's own namespace should also be reachable
// through its Vocabulary, by both IRI and compact name.
func TestVocabularyResolvesItsOwnTerms(t *testing.T) {
	for _, e := range vocab.All() {
		t.Run(e.Package, func(t *testing.T) {
			v := e.Vocabulary()
			o := e.Ontology()
			checked := 0
			for _, entity := range o.Signature() {
				if !strings.HasPrefix(string(entity.IRI()), string(e.Namespace)) {
					continue
				}
				if !o.IsDeclared(entity) {
					continue
				}
				checked++
				if !v.Contains(entity) {
					t.Errorf("%s %s is declared but not in the vocabulary", entity.Kind(), entity.IRI())
					continue
				}
				if _, ok := v.LookupKind(string(entity.IRI()), entity.Kind()); !ok {
					t.Errorf("%s is not resolvable by IRI", entity.IRI())
				}
			}
			if checked == 0 {
				t.Fatal("no declared terms in the vocabulary's own namespace")
			}
		})
	}
}

func TestOntologyReturnsAFreshCopy(t *testing.T) {
	before := skos.Ontology().Len()
	skos.Ontology().Add(owl.Declaration{Entity: owl.Class("http://example.org/Intruder")})
	if after := skos.Ontology().Len(); after != before {
		t.Errorf("adding to one copy changed the next: %d then %d", before, after)
	}
}

func TestOwnerAndPrefixes(t *testing.T) {
	if e, ok := vocab.Owner(skos.Concept.IRI()); !ok || e.Package != "skos" {
		t.Errorf("Owner(skos:Concept) = %v, %v", e.Package, ok)
	}
	if _, ok := vocab.Owner("http://example.org/Nothing"); ok {
		t.Error("Owner claimed an IRI no vocabulary defines")
	}
	if e, ok := vocab.ByPrefix("dcterms:"); !ok || e.Namespace != dcterms.Namespace {
		t.Errorf("ByPrefix(dcterms:) = %v, %v", e, ok)
	}

	iri, err := vocab.Prefixes().Expand("foaf:Person")
	if err != nil || iri != foaf.Person.IRI() {
		t.Errorf("Expand(foaf:Person) = %q, %v", iri, err)
	}
}

// The point of a shared prefix table: an axiom typed by a person, or produced
// by a model, parses against every standard vocabulary at once.
func TestParseAxiomAcrossVocabularies(t *testing.T) {
	ax, err := owl.ParseAxiom("SubClassOf(foaf:Person prov:Agent)", vocab.Prefixes())
	if err != nil {
		t.Fatalf("ParseAxiom: %v", err)
	}
	sub, ok := owl.Unwrap(ax).(owl.SubClassOf)
	if !ok {
		t.Fatalf("got %T", owl.Unwrap(ax))
	}
	if sub.Sub != owl.ClassExpression(foaf.Person) || sub.Super != owl.ClassExpression(prov.Agent) {
		t.Errorf("parsed as %s", ax)
	}
}

func TestMerge(t *testing.T) {
	merged := vocab.Merge("http://example.org/app", mustEntry(t, "skos"), mustEntry(t, "dcterms"))
	if merged.Len() < skos.Ontology().Len() {
		t.Errorf("merged ontology has %d axioms, fewer than skos alone", merged.Len())
	}
	if !merged.IsDeclared(skos.Concept) || !merged.IsDeclared(dcterms.Title) {
		t.Error("a merged vocabulary lost one of its declarations")
	}
	if len(merged.Imports) != 2 {
		t.Errorf("Imports = %v, want one per vocabulary", merged.Imports)
	}
}

func mustEntry(t *testing.T, name string) vocab.Entry {
	t.Helper()
	e, ok := vocab.ByPrefix(name)
	if !ok {
		t.Fatalf("no vocabulary %q", name)
	}
	return e
}

// Spot checks against what these vocabularies are known to say. They guard the
// mapping decisions — entity kinds especially — that a silent generator change
// could reverse without breaking anything else.
func TestKnownFacts(t *testing.T) {
	t.Run("skos hierarchy", func(t *testing.T) {
		o := skos.Ontology()
		if got := o.Label(skos.Concept); got != "Concept" {
			t.Errorf("label of skos:Concept = %q", got)
		}
		if supers := o.SuperClassesOf(skos.OrderedCollection); len(supers) == 0 {
			t.Error("skos:OrderedCollection has no asserted superclass")
		}
		// broaderTransitive is transitive, and broader sits under it.
		if !hasAxiom(o, owl.TransitiveObjectProperty{Property: skos.BroaderTransitive}) {
			t.Error("skos:broaderTransitive is not transitive")
		}
		if !hasAxiom(o, owl.SubObjectPropertyOf{Sub: skos.Broader, Super: skos.BroaderTransitive}) {
			t.Error("skos:broader is not below skos:broaderTransitive")
		}
	})

	t.Run("prov inverses", func(t *testing.T) {
		o := prov.Ontology()
		if !hasAxiom(o, owl.InverseObjectProperties{First: prov.Generated, Second: prov.WasGeneratedBy}) &&
			!hasAxiom(o, owl.InverseObjectProperties{First: prov.WasGeneratedBy, Second: prov.Generated}) {
			t.Error("prov:generated and prov:wasGeneratedBy are not stated as inverses")
		}
	})

	t.Run("owl-time is not flattened", func(t *testing.T) {
		// OWL-Time leans on anonymous class expressions; the mapping must keep
		// them rather than dropping the axioms that use them.
		found := false
		for ax := range time.Ontology().All() {
			if sub, ok := owl.Unwrap(ax).(owl.SubClassOf); ok {
				if _, named := sub.Super.(owl.Class); !named {
					found = true
					break
				}
			}
		}
		if !found {
			t.Error("no anonymous superclass survived the conversion")
		}
	})

	t.Run("schema.org kinds", func(t *testing.T) {
		// schema:name ranges over schema:Text, a datatype, so it is a data
		// property; schema:author points at a class, so it is an object one.
		var _ owl.DataProperty = schema.Name
		var _ owl.ObjectProperty = schema.Author
		var _ owl.Datatype = schema.Text
		var _ owl.Class = schema.Person

		o := schema.Ontology()
		if !hasAxiom(o, owl.SubClassOf{Sub: schema.Person, Super: schema.Thing}) {
			t.Error("schema:Person is not below schema:Thing")
		}
		// domainIncludes is documentation, not an OWL domain.
		for ax := range o.All() {
			if d, ok := owl.Unwrap(ax).(owl.ObjectPropertyDomain); ok {
				t.Fatalf("schema.org should assert no OWL domains, got %s", d)
			}
		}
	})
}

func hasAxiom(o *owl.Ontology, want owl.Axiom) bool {
	key := owl.CanonicalKey(want)
	for ax := range o.All() {
		if owl.CanonicalKey(ax) == key {
			return true
		}
	}
	return false
}
