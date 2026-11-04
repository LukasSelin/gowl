package main

import (
	"os"
	"strings"
	"testing"

	"gowl/lint"
	"gowl/owl"
)

// The committed gowl.ofn must be what the generator produces from the code as
// it is now. A change to an exported type, a doc comment or an import shows up
// here; regenerate with:
//
//	go run ./examples/self > examples/self/gowl.ofn
func TestCommittedOntologyIsCurrent(t *testing.T) {
	o, err := Build("../..")
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
	o, err := Build("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range lint.Run(o, lint.Default()) {
		if f.Severity >= lint.Warning {
			t.Errorf("%s: %s: %s", f.Severity, f.Rule, f.Message)
		}
	}
}
