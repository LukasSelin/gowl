package gocode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
