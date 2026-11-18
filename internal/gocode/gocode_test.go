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
// writeZooModule writes a two-module tree under a temporary directory and
// returns the root of example.com/zoo: a module with a sub-package, an
// external dependency, a nested module that is not part of it, and types
// that exercise interfaces, embedding and struct fields. Shared so that the
// tests of prefixed and unprefixed output describe the same code.
func writeZooModule(t *testing.T) string {
	t.Helper()
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
	"io"

	"example.com/keeper"
	"example.com/zoo/pen"
)

// Animal is anything kept in the zoo.
type Animal interface{ Name() string }

// Feeder is an animal that can be fed. Only the zoo can make one.
type Feeder interface {
	Animal
	io.Reader
	Feed(ctx context.Context, portions ...int) (left int, err error)
	seal()
}

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

	return filepath.Join(tmp, "zoo")
}

// Build writes every package IRI in full. The prefix is opt-in, so a caller
// that does not ask for one gets a document naming nothing it did not name
// before: the same ontology, spelled the long way.
func TestWithoutPrefixNothingAbbreviated(t *testing.T) {
	root := writeZooModule(t)

	o, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}
	got := o.Functional()

	for _, want := range []string{
		"ObjectPropertyAssertion(:imports :package.example.com.zoo :package.example.com.zoo.pen)",
		`AnnotationAssertion(rdfs:label :package.example.com.zoo.pen "example.com/zoo/pen"^^xsd:string)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s", want)
		}
	}
	// Not merely that no name is abbreviated: that no prefix was declared to
	// abbreviate one with. A stray Prefix() line would be harmless to a reader
	// and invisible to the checks above.
	for _, unwanted := range []string{"pkg:", "Prefix(pkg:"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("declares or uses %s, though no prefix was asked for", unwanted)
		}
	}
	// The same guard TestOtherModule makes, in the spelling this document uses:
	// a package of another module is not described.
	if strings.Contains(got, "package.example.com.zoo.nested") {
		t.Error("describes the nested module, which is not part of this one")
	}
}

// An empty name is how Build asks for no prefix, so the two must agree
// exactly. Tested against the whole document rather than a sample of it,
// because the claim is that nothing differs.
func TestEmptyPrefixSameAsNone(t *testing.T) {
	root := writeZooModule(t)

	none, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := BuildPrefixed(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if none.Functional() != empty.Functional() {
		t.Error(`BuildPrefixed(root, "") differs from Build(root)`)
	}
}

func TestOtherModule(t *testing.T) {
	root := writeZooModule(t)

	o, err := BuildPrefixed(root, "pkg")
	if err != nil {
		t.Fatal(err)
	}
	got := o.Functional()
	for _, want := range []string{
		"Ontology(<https://example.com/zoo>",
		"SubClassOf(:zoo.Cat :zoo.Animal)",
		"ObjectPropertyAssertion(:imports :package.example.com.zoo pkg:pen)",
		// What the module imports from outside it is recorded too, dependency
		// and standard library alike, but not what those import in turn.
		"ObjectPropertyAssertion(:imports :package.example.com.zoo :package.example.com.keeper)",
		"ObjectPropertyAssertion(:imports :package.example.com.zoo :package.context)",
		"ClassAssertion(:ExternalPackage :package.example.com.keeper)",
		"ClassAssertion(:ExternalPackage :package.context)",
		"DisjointClasses(:ExternalPackage :Package)",
		// An interface lists the methods it declares, its seal included, and
		// those of an embedded interface from outside the module; an embedded
		// interface of the module is a superclass instead.
		"SubClassOf(:zoo.Feeder :zoo.Animal)",
		`AnnotationAssertion(:method :zoo.Animal "Name() string"^^xsd:string)`,
		`AnnotationAssertion(:method :zoo.Feeder "Feed(context.Context, ...int) (int, error)"^^xsd:string)`,
		`AnnotationAssertion(:method :zoo.Feeder "Read([]uint8) (int, error)"^^xsd:string)`,
		`AnnotationAssertion(:method :zoo.Feeder "seal()"^^xsd:string)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s", want)
		}
	}
	for _, unwanted := range []string{"Ghost", "pkg:nested", `:zoo.Feeder "Name() string"`, "ClassAssertion(:Package :package.example.com.keeper)", ":package.context :"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("describes %s, which is not part of the module", unwanted)
		}
	}
	if t.Failed() {
		t.Log(got)
	}
}
