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

// Stray lets the cat out.
func (c *Cat) Stray(far bool) (home pen.Pen, err error) { return c.Home, nil }

func (Cat) purr() {}

// Feed hands on the keeper's context, so the module and its dependency have to
// agree on what a context.Context is.
func Feed() context.Context { return keeper.Context() }

// First picks the first of a litter.
func First[T Animal](litter []T) (T, bool) {
	var none T
	return none, false
}

func hidden() {}

// Pair is two of a kind.
type Pair[K comparable, V any] struct {
	Left  K
	Right V
}

// Both returns the two. The method has its own names for the type's
// parameters, and they are described under the type's.
func (p Pair[A, B]) Both() (A, B) { return p.Left, p.Right }

// Swap returns them the other way round, in a method that calls the type's
// parameters by each other's names.
func (p Pair[V, K]) Swap() (K, V) { return p.Right, p.Left }

// Enclosures.
type (
	Cage struct{ Bars int }
	// Tank holds water.
	Tank struct{ Litres int }
)

// Sound is what an animal says.
type Sound string

// Author is who wrote the zoo's guide book.
type Author struct{ Name string }

// Keeper keeps animals of one kind.
type Keeper[T Animal] interface{ Keep(t T) error }

// CatKeeper keeps cats, and can say how many.
type CatKeeper interface {
	Keeper[Cat]
	Count() int
}

var _ = hidden
`)
	// A directive above a package clause is no package comment, and does not
	// hide the one another file has.
	write("zoo/a_gen.go", "//go:generate echo generated\npackage zoo\n")
	// A licence header that runs into the package clause is not one either.
	write("zoo/licensed/licensed.go", "// Copyright 2024 The Zoo Authors. All rights reserved.\npackage licensed\n")
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
		// An embedded interface of the module with type arguments is no
		// superclass, because nothing satisfies a generic interface as such.
		// Its methods are listed, for the arguments it was given.
		`AnnotationAssertion(:method :zoo.Keeper "Keep(T) error"^^xsd:string)`,
		`AnnotationAssertion(:method :zoo.CatKeeper "Count() int"^^xsd:string)`,
		`AnnotationAssertion(:method :zoo.CatKeeper "Keep(zoo.Cat) error"^^xsd:string)`,
		// Any other type lists the exported methods it declares, on itself or
		// on its pointer, and a package the functions it exports, under the
		// name it declares.
		`AnnotationAssertion(:method :zoo.Cat "Name() string"^^xsd:string)`,
		`AnnotationAssertion(:method :zoo.Cat "Stray(bool) (pen.Pen, error)"^^xsd:string)`,
		`AnnotationAssertion(:method :zoo.Pair "Both() (K, V)"^^xsd:string)`,
		`AnnotationAssertion(:method :zoo.Pair "Swap() (V, K)"^^xsd:string)`,
		`AnnotationAssertion(:func :package.example.com.zoo "Feed() context.Context"^^xsd:string)`,
		`AnnotationAssertion(:func :package.example.com.zoo "First[T zoo.Animal]([]T) (T, bool)"^^xsd:string)`,
		`AnnotationAssertion(:name :package.example.com.zoo "zoo"^^xsd:string)`,
		`AnnotationAssertion(:name :package.example.com.zoo.pen "pen"^^xsd:string)`,
		// What each type is underneath.
		`AnnotationAssertion(:kind :zoo.Animal "interface"^^xsd:string)`,
		`AnnotationAssertion(:kind :zoo.Cat "struct"^^xsd:string)`,
		`AnnotationAssertion(:kind :zoo.Sound "basic"^^xsd:string)`,
		// A type declared in a group has the group's comment when it has none
		// of its own, as go doc shows it.
		`AnnotationAssertion(rdfs:comment :zoo.Cage "Enclosures."^^xsd:string)`,
		`AnnotationAssertion(rdfs:comment :zoo.Tank "Tank holds water."^^xsd:string)`,
		// go doc's synopsis says nothing for a comment that begins like a
		// licence header, with "author" among others, and a type called
		// Author is documented all the same.
		`AnnotationAssertion(rdfs:comment :zoo.Author "Author is who wrote the zoo's guide book."^^xsd:string)`,
		`AnnotationAssertion(rdfs:comment :package.example.com.zoo "Package zoo keeps animals."^^xsd:string)`,
		"ClassAssertion(:Package :package.example.com.zoo.licensed)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s", want)
		}
	}
	for _, unwanted := range []string{"Ghost", "package.example.com.zoo.nested", `:zoo.Feeder "Name() string"`, "ClassAssertion(:Package :package.example.com.keeper)", ":package.context :",
		// Unexported methods and functions are nobody else's business, and a
		// package from outside the module is not described.
		"purr", "hidden", ":name :package.context", ":func :package.example.com.keeper",
		"rdfs:comment :package.example.com.zoo.licensed"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("describes %s, which is not part of the module", unwanted)
		}
	}
	if t.Failed() {
		t.Log(got)
	}
}
