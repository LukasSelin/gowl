package owl_test

import (
	"strings"
	"testing"

	"gowl/owl"
)

func mustParse(t *testing.T, src string) *owl.Ontology {
	t.Helper()
	o, err := owl.ParseFunctionalString(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return o
}

const diffHeader = "Prefix(:=<http://example.org/o#>)\nOntology(<http://example.org/o>\n"

func doc(body string) string { return diffHeader + body + "\n)" }

func TestDiffDetectsAddedAndRemoved(t *testing.T) {
	from := mustParse(t, doc("    SubClassOf(:A :B)\n    SubClassOf(:B :C)"))
	to := mustParse(t, doc("    SubClassOf(:B :C)\n    SubClassOf(:C :D)"))

	d := owl.DiffOntologies(from, to)
	if d.Empty() {
		t.Fatal("expected changes")
	}
	if len(d.RemovedAxioms) != 1 || owl.Functional(d.RemovedAxioms[0]) != "SubClassOf(<http://example.org/o#A> <http://example.org/o#B>)" {
		t.Errorf("removed = %v", d.RemovedAxioms)
	}
	if len(d.AddedAxioms) != 1 || owl.Functional(d.AddedAxioms[0]) != "SubClassOf(<http://example.org/o#C> <http://example.org/o#D>)" {
		t.Errorf("added = %v", d.AddedAxioms)
	}

	out := d.String()
	if !strings.Contains(out, "- SubClassOf(:A :B)") || !strings.Contains(out, "+ SubClassOf(:C :D)") {
		t.Errorf("String() = %q", out)
	}
}

func TestDiffIgnoresOperandOrder(t *testing.T) {
	from := mustParse(t, doc("    EquivalentClasses(:A :B)"))
	to := mustParse(t, doc("    EquivalentClasses(:B :A)"))

	if d := owl.DiffOntologies(from, to); !d.Empty() {
		t.Errorf("reordering operands should not be a change:\n%s", d)
	}
}

func TestDiffIgnoresAxiomOrder(t *testing.T) {
	from := mustParse(t, doc("    SubClassOf(:A :B)\n    SubClassOf(:B :C)"))
	to := mustParse(t, doc("    SubClassOf(:B :C)\n    SubClassOf(:A :B)"))

	if d := owl.DiffOntologies(from, to); !d.Empty() {
		t.Errorf("reordering axioms should not be a change:\n%s", d)
	}
}

func TestDiffIsMultiset(t *testing.T) {
	from := mustParse(t, doc("    SubClassOf(:A :B)\n    SubClassOf(:A :B)"))
	to := mustParse(t, doc("    SubClassOf(:A :B)"))

	d := owl.DiffOntologies(from, to)
	if len(d.RemovedAxioms) != 1 {
		t.Errorf("a document asserting an axiom twice differs from one asserting it once: removed = %d", len(d.RemovedAxioms))
	}
}

func TestDiffHeaderAndImports(t *testing.T) {
	from := mustParse(t, "Ontology(<http://example.org/a> <http://example.org/a/1>\n    Import(<http://example.org/x>)\n)")
	to := mustParse(t, "Ontology(<http://example.org/b> <http://example.org/b/2>\n    Import(<http://example.org/y>)\n)")

	d := owl.DiffOntologies(from, to)
	if !d.IRIChanged || !d.VersionChanged {
		t.Errorf("header changes missed: iri=%v version=%v", d.IRIChanged, d.VersionChanged)
	}
	if len(d.AddedImports) != 1 || len(d.RemovedImports) != 1 {
		t.Errorf("imports: added=%v removed=%v", d.AddedImports, d.RemovedImports)
	}
}

func TestDiffEntitiesAndSummary(t *testing.T) {
	from := mustParse(t, doc("    SubClassOf(:A :B)"))
	to := mustParse(t, doc("    SubClassOf(:A :C)"))

	d := owl.DiffOntologies(from, to)
	entities := d.Entities()
	if len(entities) != 3 {
		t.Errorf("Entities() = %v, want A, B and C", entities)
	}
	if s := d.Summary(); !strings.Contains(s, "1 axiom added") || !strings.Contains(s, "1 axiom removed") {
		t.Errorf("Summary() = %q", s)
	}
	if s := owl.DiffOntologies(from, from).Summary(); s != "no changes" {
		t.Errorf("identical ontologies: Summary() = %q", s)
	}
}

func TestDiffOfSelfIsEmpty(t *testing.T) {
	o, _ := pizza()
	if d := owl.DiffOntologies(o, o); !d.Empty() {
		t.Errorf("an ontology should not differ from itself:\n%s", d)
	}
}
