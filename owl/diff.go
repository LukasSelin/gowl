package owl

import (
	"sort"
	"strconv"
	"strings"
)

// Diff is the difference between two ontologies. Axioms are matched by
// canonical form ([CanonicalKey]), so reordering the operands of a commutative
// construct is not reported as a change, while the axioms held here are the
// originals as asserted.
//
// A modified axiom appears as one removal plus one addition: OWL has no notion
// of editing an axiom in place, and guessing which removal pairs with which
// addition would invent structure the documents do not have.
type Diff struct {
	From, To *Ontology

	AddedAxioms   []Axiom
	RemovedAxioms []Axiom

	AddedImports   []IRI
	RemovedImports []IRI

	AddedAnnotations   []Annotation
	RemovedAnnotations []Annotation

	// IRIChanged and VersionChanged report header changes; the values are on
	// From and To.
	IRIChanged     bool
	VersionChanged bool
}

// DiffOntologies compares two ontologies.
func DiffOntologies(from, to *Ontology) *Diff {
	d := &Diff{
		From:           from,
		To:             to,
		IRIChanged:     from.IRI != to.IRI,
		VersionChanged: from.VersionIRI != to.VersionIRI,
	}

	d.RemovedAxioms, d.AddedAxioms = diffAxioms(from.Axioms(), to.Axioms())
	d.RemovedImports, d.AddedImports = diffSlices(from.Imports, to.Imports, func(i IRI) string { return string(i) })
	d.RemovedAnnotations, d.AddedAnnotations = diffSlices(from.Annotations, to.Annotations,
		func(a Annotation) string { return Functional(a) })
	return d
}

// Empty reports whether the two ontologies are equivalent under canonical
// comparison.
func (d *Diff) Empty() bool {
	return !d.IRIChanged && !d.VersionChanged &&
		len(d.AddedAxioms) == 0 && len(d.RemovedAxioms) == 0 &&
		len(d.AddedImports) == 0 && len(d.RemovedImports) == 0 &&
		len(d.AddedAnnotations) == 0 && len(d.RemovedAnnotations) == 0
}

// Entities returns the entities touched by any axiom change, sorted by kind
// then IRI. This is what a release note wants: which terms moved, not which
// axioms did.
func (d *Diff) Entities() []Entity {
	seen := make(map[Entity]bool)
	for _, ax := range d.AddedAxioms {
		Walk(ax, func(e Entity) { seen[e] = true })
	}
	for _, ax := range d.RemovedAxioms {
		Walk(ax, func(e Entity) { seen[e] = true })
	}
	return sortedEntities(seen)
}

// String renders the diff as a unified-diff-style listing: removals prefixed
// with '-', additions with '+'.
func (d *Diff) String() string {
	var b strings.Builder

	if d.IRIChanged {
		b.WriteString("- Ontology(<" + string(d.From.IRI) + ">)\n")
		b.WriteString("+ Ontology(<" + string(d.To.IRI) + ">)\n")
	}
	if d.VersionChanged {
		b.WriteString("- Version(<" + string(d.From.VersionIRI) + ">)\n")
		b.WriteString("+ Version(<" + string(d.To.VersionIRI) + ">)\n")
	}
	for _, i := range d.RemovedImports {
		b.WriteString("- Import(<" + string(i) + ">)\n")
	}
	for _, i := range d.AddedImports {
		b.WriteString("+ Import(<" + string(i) + ">)\n")
	}
	for _, a := range d.RemovedAnnotations {
		b.WriteString("- " + d.From.Render(a) + "\n")
	}
	for _, a := range d.AddedAnnotations {
		b.WriteString("+ " + d.To.Render(a) + "\n")
	}
	for _, ax := range d.RemovedAxioms {
		b.WriteString("- " + d.From.Render(ax) + "\n")
	}
	for _, ax := range d.AddedAxioms {
		b.WriteString("+ " + d.To.Render(ax) + "\n")
	}
	return b.String()
}

// Summary is a one-line count of the changes, suitable for a commit message or
// a CI log.
func (d *Diff) Summary() string {
	if d.Empty() {
		return "no changes"
	}
	var parts []string
	if n := len(d.AddedAxioms); n > 0 {
		parts = append(parts, plural(n, "axiom")+" added")
	}
	if n := len(d.RemovedAxioms); n > 0 {
		parts = append(parts, plural(n, "axiom")+" removed")
	}
	if n := len(d.AddedImports) + len(d.RemovedImports); n > 0 {
		parts = append(parts, plural(n, "import")+" changed")
	}
	if n := len(d.AddedAnnotations) + len(d.RemovedAnnotations); n > 0 {
		parts = append(parts, plural(n, "annotation")+" changed")
	}
	if d.IRIChanged {
		parts = append(parts, "ontology IRI changed")
	}
	if d.VersionChanged {
		parts = append(parts, "version IRI changed")
	}
	if n := len(d.Entities()); n > 0 {
		parts = append(parts, "affecting "+plural(n, "entity"))
	}
	return strings.Join(parts, ", ")
}

func plural(n int, noun string) string {
	if n == 1 {
		return strconv.Itoa(n) + " " + noun
	}
	if noun == "entity" {
		return strconv.Itoa(n) + " entities"
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

func diffAxioms(from, to []Axiom) (removed, added []Axiom) {
	return diffSlices(from, to, CanonicalKey)
}

// diffSlices computes a multiset difference keyed by key. Multiset rather than
// set, so that a document asserting the same axiom twice and one asserting it
// once differ by one.
func diffSlices[T any](from, to []T, key func(T) string) (removed, added []T) {
	fromCount := make(map[string]int, len(from))
	for _, v := range from {
		fromCount[key(v)]++
	}
	toCount := make(map[string]int, len(to))
	for _, v := range to {
		toCount[key(v)]++
	}

	seen := make(map[string]int)
	for _, v := range from {
		k := key(v)
		seen[k]++
		if seen[k] > toCount[k] {
			removed = append(removed, v)
		}
	}
	seen = make(map[string]int)
	for _, v := range to {
		k := key(v)
		seen[k]++
		if seen[k] > fromCount[k] {
			added = append(added, v)
		}
	}
	return removed, added
}

// SortAxioms orders axioms by their rendering, for stable output. Keys are
// rendered once up front rather than inside the comparison.
func SortAxioms(axioms []Axiom) {
	type keyed struct {
		key string
		val Axiom
	}
	ks := make([]keyed, len(axioms))
	for i, ax := range axioms {
		ks[i] = keyed{key: Functional(ax), val: ax}
	}
	sort.SliceStable(ks, func(i, j int) bool { return ks[i].key < ks[j].key })
	for i, e := range ks {
		axioms[i] = e.val
	}
}
