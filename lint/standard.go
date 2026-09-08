package lint

import (
	"fmt"
	"strings"
	"sync"

	"gowl/owl"
)

// PreferStandardTerms returns a rule that reports a term an ontology defines
// for itself when one of the reference vocabularies already names it. Reusing
// foaf:Person rather than minting :Person is what makes an ontology join up
// with everybody else's data, and it is the easiest thing to forget.
//
// The rule is not in [Default] because it needs those references, and because
// which vocabularies count as standard is a project's decision rather than
// this package's. Pass the ones you mean:
//
//	rules := append(lint.Default(), lint.PreferStandardTerms(
//		skos.Vocabulary(), dcterms.Vocabulary(), foaf.Vocabulary(),
//	))
//
// It matches on the local name of an IRI and on rdfs:label, and only within
// one entity kind, so a class is never reported against a property. It is
// Info, because minting your own term is often the right call — a project's
// :Person may genuinely not be foaf:Person.
func PreferStandardTerms(refs ...*owl.Vocabulary) Rule {
	// The index is built on the first check rather than here, so that listing
	// the rules costs nothing: a caller passing every standard vocabulary is
	// otherwise paying to index schema.org just to print a description.
	index := sync.OnceValue(func() *standardIndex { return newStandardIndex(refs) })
	return Rule{
		Name:        "prefer-standard-term",
		Description: "a locally defined term that a standard vocabulary already names",
		Severity:    Info,
		Check:       func(o *owl.Ontology) []Finding { return index().check(o) },
	}
}

// standardIndex maps a folded name to the reference terms that answer to it.
// It is built once, when the rule is constructed, because the reference
// vocabularies do not change between runs and one of them is schema.org.
type standardIndex struct {
	refs  []*owl.Vocabulary
	byKey map[standardKey][]owl.Term
}

// standardKey is a name folded for comparison, together with the entity kind
// it was seen as. Kind is part of the key so that a class is only ever
// reported against a class.
type standardKey struct {
	kind owl.Kind
	name string
}

func newStandardIndex(refs []*owl.Vocabulary) *standardIndex {
	idx := &standardIndex{refs: refs, byKey: make(map[standardKey][]owl.Term)}
	for _, ref := range refs {
		for _, t := range ref.Terms() {
			for _, name := range namesOf(t.Entity.IRI(), t.Label) {
				key := standardKey{kind: t.Entity.Kind(), name: name}
				idx.byKey[key] = append(idx.byKey[key], t)
			}
		}
	}
	// Candidates keep the order they were indexed in — reference by reference,
	// and within one, the vocabulary's own signature order. So the caller
	// expresses which vocabulary it would rather reuse by the order it passes
	// them, and the suggestion never moves between runs.
	return idx
}

func (idx *standardIndex) check(o *owl.Ontology) []Finding {
	var out []Finding
	for _, e := range o.Signature() {
		if builtin(e) || !o.IsDeclared(e) || idx.defines(e) {
			continue
		}
		match, how, ok := idx.match(e, o.Label(e))
		if !ok || mentions(o, e, match.Entity.IRI()) {
			continue
		}
		out = append(out, Finding{
			Subject: e.IRI(),
			Message: fmt.Sprintf("%s %s %s %s; consider using it instead",
				strings.ToLower(e.Kind().String()), o.Render(e), how, match.Name),
		})
	}
	return out
}

// defines reports whether the reference vocabularies already contain an
// entity, which is the case when an ontology reuses a standard term properly.
func (idx *standardIndex) defines(e owl.Entity) bool {
	for _, ref := range idx.refs {
		if ref.Contains(e) {
			return true
		}
	}
	return false
}

// match finds the reference term an entity duplicates, preferring a match on
// the IRI's local name over one on the label: the same local name is a much
// stronger signal than the same prose.
func (idx *standardIndex) match(e owl.Entity, label string) (owl.Term, string, bool) {
	local := fold(localName(string(e.IRI())))
	if terms := idx.byKey[standardKey{e.Kind(), local}]; len(terms) > 0 && usable(local) {
		return terms[0], "has the same name as", true
	}
	if folded := fold(label); usable(folded) && folded != local {
		if terms := idx.byKey[standardKey{e.Kind(), folded}]; len(terms) > 0 {
			return terms[0], fmt.Sprintf("is labelled %q, which matches", label), true
		}
	}
	return owl.Term{}, "", false
}

// usable rejects names too short to mean anything, which otherwise collide
// across vocabularies and drown the real findings.
func usable(folded string) bool { return len(folded) >= 3 }

// mentions reports whether the ontology already relates an entity to an IRI.
// An ontology that says EquivalentClasses(:Person foaf:Person) has made its
// alignment explicit and does not need telling.
func mentions(o *owl.Ontology, e owl.Entity, other owl.IRI) bool {
	for _, ax := range o.AxiomsReferencing(e) {
		found := false
		owl.Walk(ax, func(seen owl.Entity) {
			if seen.IRI() == other {
				found = true
			}
		})
		if found {
			return true
		}
	}
	return false
}

// namesOf returns the keys a reference term should be found by: its local name
// and its label.
func namesOf(iri owl.IRI, label string) []string {
	local := fold(localName(string(iri)))
	out := make([]string, 0, 2)
	if usable(local) {
		out = append(out, local)
	}
	if folded := fold(label); usable(folded) && folded != local {
		out = append(out, folded)
	}
	return out
}

// localName is the part of an IRI after its last separator.
func localName(iri string) string {
	if i := strings.LastIndexAny(iri, "#/:"); i >= 0 {
		return iri[i+1:]
	}
	return iri
}

// fold reduces a name to its letters and digits, lowercased, so that
// :familyName, "family name" and foaf:family_name all compare equal.
func fold(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
