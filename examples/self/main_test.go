package main

import (
	"os"
	"path/filepath"
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

// Another module is described under its own module path. The fixture has what
// a real repository has and gowl does not: a dependency outside the standard
// library, here reached through a local replace so the test needs no network,
// whose context.Context crosses into the module; and a nested module, which
// belongs to no one but itself.
func TestOtherModule(t *testing.T) {
	t.Setenv("GOWORK", "off")
	tmp := t.TempDir()
	write := func(name, src string) {
		t.Helper()
		path := filepath.Join(tmp, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("keeper/go.mod", "module example.com/keeper\n\ngo 1.21\n")
	write("keeper/keeper.go", `package keeper

import "context"

// Context is the keeper's context.
func Context() context.Context { return context.Background() }
`)
	write("zoo/go.mod", `module example.com/zoo

go 1.21

require example.com/keeper v0.0.0

replace example.com/keeper => ../keeper
`)
	write("zoo/zoo.go", `// Package zoo keeps animals.
package zoo

import (
	"context"

	"example.com/keeper"
	"example.com/zoo/pen"
)

// Animal is anything kept in the zoo.
type Animal interface{ Name() string }

// Cat is an animal that lives in a pen.
type Cat struct {
	Lives int
	Home  pen.Pen
}

// Name names the cat.
func (Cat) Name() string { return "cat" }

// Feed hands on the keeper's context, so the module and its dependency have to
// agree on what a context.Context is.
func Feed() context.Context { return keeper.Context() }
`)
	write("zoo/pen/pen.go", `// Package pen holds enclosures.
package pen

// Pen is an enclosure.
type Pen struct{ Size int }
`)
	write("zoo/nested/go.mod", "module example.com/zoo/nested\n\ngo 1.21\n")
	write("zoo/nested/nested.go", `package nested

// Ghost belongs to another module.
type Ghost struct{}
`)

	o, err := Build(filepath.Join(tmp, "zoo"))
	if err != nil {
		t.Fatal(err)
	}
	got := o.Functional()
	for _, want := range []string{
		"Ontology(<https://example.com/zoo>",
		"SubClassOf(:zoo.Cat :zoo.Animal)",
		"ObjectPropertyAssertion(:imports :package.example.com.zoo :package.example.com.zoo.pen)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s", want)
		}
	}
	for _, unwanted := range []string{"Ghost", "package.example.com.keeper"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("describes %s, which is not part of the module", unwanted)
		}
	}
	if t.Failed() {
		t.Log(got)
	}
}
