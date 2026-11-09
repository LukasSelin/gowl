package main

import (
	"os"
	"strings"
	"testing"

	"gowl/internal/gocode"
	"gowl/lint"
	"gowl/owl"
)

// The committed gowl.ofn must be what the generator produces from the code as
// it is now. A change to an exported type, a doc comment or an import shows up
// here; regenerate with:
//
//	go run ./examples/self > examples/self/gowl.ofn
func TestCommittedOntologyIsCurrent(t *testing.T) {
	o, err := gocode.Build("../..")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("gowl.ofn")
	if err != nil {
		t.Fatal(err)
	}
	got := o.Functional()
	if strings.ReplaceAll(string(want), "\r\n", "\n") != got {
		t.Fatal("examples/self/gowl.ofn is stale; regenerate with: go run ./examples/self > examples/self/gowl.ofn")
	}
	if _, err := owl.ParseFunctionalString(got); err != nil {
		t.Fatalf("generated document does not parse back: %v", err)
	}
}

// The repository's own linter holds the repository's own description to its
// default rules: nothing at warning or above. The info-level orphan-class
// findings are expected — they are the types that satisfy no interface of the
// module, such as the builders and option structs.
func TestSelfLintsClean(t *testing.T) {
	o, err := gocode.Build("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range lint.Run(o, lint.Default()) {
		if f.Severity >= lint.Warning {
			t.Errorf("%s: %s: %s", f.Severity, f.Rule, f.Message)
		}
	}
}

// gowl keeps the rules in design/invariants.ofn on every change: its layers,
// effects at the edges, and the methods of the interfaces everything is written
// against. The repository has no CI, so this test is what keeps the rules alive
// outside a Claude Code session, where the invariants-check hook runs them.
func TestOwnInvariantsHold(t *testing.T) {
	o, err := gocode.Build("../..")
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile("../../design/invariants.ofn")
	if err != nil {
		t.Fatal(err)
	}
	inv, err := owl.ParseFunctionalString(string(src))
	if err != nil {
		t.Fatal(err)
	}
	paths, err := gocode.KnownPackages("../..")
	if err != nil {
		t.Fatal(err)
	}
	var known []owl.NamedIndividual
	for _, p := range paths {
		known = append(known, gocode.PackageIndividual(o, p))
	}
	c := gocode.CheckInvariants(inv, o, known)
	for _, f := range c.Findings {
		t.Errorf("%s: %s: %s", f.Severity, f.Rule, f.Message)
	}
	if c.Checked == 0 {
		t.Error("no rule was checked")
	}
}
