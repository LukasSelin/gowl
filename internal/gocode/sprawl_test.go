package gocode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gowl/owl"
)

// shopBefore is a small module as it stands before a change.
const shopBefore = `// Package shop sells things.
package shop

// Priced is anything with a price.
type Priced interface{ Price() int }

// Catalog lists products.
type Catalog struct{ Products []Product }

// Product is something for sale.
type Product struct {
	Name  string
	Cents int
}

// Price is the product's price.
func (p Product) Price() int { return p.Cents }

// Order is a customer's order.
type Order struct {
	Catalog  *Catalog
	Customer string
	Total    int
}

// Draft is an order being put together.
type Draft struct{ Catalog *Catalog }

// Shelf, Aisle and Wing overlap already: a Wing has every field of an Aisle.
type Shelf struct{ Size int }

// Aisle is a row of shelves.
type Aisle struct {
	Shelf  *Shelf
	Number int
}
`

// shopChange is what the change does: it adds types and reshapes Draft and
// Wing.
const shopChange = `
// Basket repeats Order under other names.
type Basket struct {
	From  *Catalog
	Buyer string
	Sum   int
}

// Invoice repeats Order and adds a field.
type Invoice struct {
	Catalog  *Catalog
	Customer string
	Total    int
	Paid     bool
}

// Receipt reuses Order by embedding it.
type Receipt struct {
	Order
	Paid bool
}

// Gift is new, and satisfies an interface that was already there.
type Gift struct{ Value int }

// Price is the gift's value.
func (g Gift) Price() int { return g.Value }

// Note relates to nothing.
type Note struct{ Text string }

// Tag and Label know only each other.
type Tag struct{ Label Label }

// Label is a tag's text.
type Label struct{ Text string }
`

// writeShop writes the shop module with the given source and returns its root.
func writeShop(t *testing.T, src string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range map[string]string{
		"go.mod":  "module example.com/shop\n\ngo 1.21\n",
		"shop.go": src,
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func buildShop(t *testing.T, src string) *owl.Ontology {
	t.Helper()
	o, err := Build(writeShop(t, src))
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func TestCompare(t *testing.T) {
	t.Setenv("GOWORK", "off")
	wing := "\n// Wing is a part of the shop.\ntype Wing struct {\n\tShelf  *Shelf\n\tNumber int\n\tFloor  string\n%s}\n"
	before := buildShop(t, shopBefore+strings.Replace(wing, "%s", "", 1))
	after := buildShop(t,
		strings.Replace(shopBefore, "type Draft struct{ Catalog *Catalog }",
			"type Draft struct {\n\tCatalog *Catalog\n\tCustomer string\n\tTotal int\n}", 1)+
			strings.Replace(wing, "%s", "\tOpen   bool\n", 1)+
			shopChange)

	d := Compare(before, after)

	names := func(cs []owl.Class) string {
		var out []string
		for _, c := range cs {
			out = append(out, after.Render(c))
		}
		return strings.Join(out, " ")
	}
	if got, want := names(d.Added), ":shop.Basket :shop.Gift :shop.Invoice :shop.Label :shop.Note :shop.Receipt :shop.Tag"; got != want {
		t.Errorf("added = %s\nwant    %s", got, want)
	}
	if got, want := names(d.Changed), ":shop.Draft :shop.Wing"; got != want {
		t.Errorf("changed = %s\nwant      %s", got, want)
	}

	found := map[string]string{} // "rule subject" -> message
	for _, f := range d.Findings {
		found[f.Rule+" "+after.Render(owl.Class(f.Subject))] = f.Message
	}
	for _, want := range []struct{ key, says string }{
		// A renamed copy, and an existing type reshaped into one.
		{"same-shape-type :shop.Basket", "has the same fields as :shop.Draft and :shop.Order (:shop.Catalog, xsd:integer, xsd:string); consider reusing one of them"},
		{"same-shape-type :shop.Draft", "has the same fields as :shop.Basket and :shop.Order"},
		// A copy that adds a field should embed what it copied.
		{"same-shape-type :shop.Invoice", "has every field of :shop.Basket, :shop.Draft and :shop.Order"},
		{"new-orphan :shop.Note", "connects to no type that existed before"},
		{"new-orphan :shop.Tag", "only to the new :shop.Label"},
		{"new-orphan :shop.Label", "only to the new :shop.Tag"},
	} {
		msg, ok := found[want.key]
		if !ok {
			t.Errorf("missing %s", want.key)
			continue
		}
		if !strings.Contains(msg, want.says) {
			t.Errorf("%s: message %q\nshould say %q", want.key, msg, want.says)
		}
		delete(found, want.key)
	}
	// Receipt embeds Order rather than copying it, Gift satisfies an existing
	// interface, and Wing had every field of Aisle before the change too.
	for key, msg := range found {
		t.Errorf("unexpected %s: %s", key, msg)
	}

	if len(d.Findings) > 0 && d.Findings[0].Rule != SameShapeType {
		t.Errorf("warnings should come before info, got %s first", d.Findings[0].Rule)
	}
}

// Embedding is the reuse Compare recommends, so the field has to be visible.
func TestEmbeddedFieldIsAProperty(t *testing.T) {
	t.Setenv("GOWORK", "off")
	o := buildShop(t, shopBefore+shopChange)
	if doc := o.Functional(); !strings.Contains(doc, "ObjectPropertyRange(:shop.Receipt.Order :shop.Order)") {
		t.Errorf("embedded Order should be a property of Receipt:\n%s", doc)
	}
}

func TestCompareUnchanged(t *testing.T) {
	t.Setenv("GOWORK", "off")
	o := buildShop(t, shopBefore+shopChange)
	d := Compare(o, o)
	if len(d.Added)+len(d.Changed)+len(d.Findings) != 0 {
		t.Errorf("comparing a module with itself should find nothing, got %+v", d)
	}
}
