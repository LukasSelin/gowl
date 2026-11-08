package gocode

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gowl/lint"
	"gowl/owl"
)

const shopPrefixes = `Prefix(:=<https://example.com/shop#>)
Prefix(xsd:=<http://www.w3.org/2001/XMLSchema#>)
Prefix(foaf:=<http://xmlns.com/foaf/0.1/>)
`

// shopPlan parses a plan for the shop module from the axioms it holds.
func shopPlan(t *testing.T, axioms string) *owl.Ontology {
	t.Helper()
	o, err := owl.ParseFunctionalString(shopPrefixes + "Ontology(<https://example.com/shop/design>\n" + axioms + "\n)\n")
	if err != nil {
		t.Fatal(err)
	}
	return o
}

// findingsOf renders findings as "rule subject-local-name" for comparison.
func findingsOf(fs []lint.Finding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		_, local, _ := strings.Cut(string(f.Subject), "#")
		out = append(out, f.Rule+" "+local)
	}
	return out
}

func wantFindings(t *testing.T, fs []lint.Finding, want ...string) {
	t.Helper()
	got := findingsOf(fs)
	if !slices.Equal(got, want) {
		var messages []string
		for _, f := range fs {
			messages = append(messages, f.String())
		}
		t.Errorf("findings = %q\nwant       %q\n%s", got, want, strings.Join(messages, "\n"))
	}
}

// gift is a change to shopBefore: a new type satisfying an existing interface,
// holding an existing type.
const gift = `
// Gift is free.
type Gift struct {
	For   *Order
	Value int
}

// Price is the gift's value.
func (g Gift) Price() int { return g.Value }
`

const giftPlan = `
	Declaration(Class(:shop.Order))
	Declaration(Class(:shop.Priced))
	Declaration(Class(:shop.Gift))
	SubClassOf(:shop.Gift :shop.Priced)
	Declaration(ObjectProperty(:shop.Gift.For))
	ObjectPropertyDomain(:shop.Gift.For :shop.Gift)
	ObjectPropertyRange(:shop.Gift.For :shop.Order)
	Declaration(DataProperty(:shop.Gift.Value))
	DataPropertyDomain(:shop.Gift.Value :shop.Gift)
	DataPropertyRange(:shop.Gift.Value xsd:integer)
`

func TestConformCodeMatchesPlan(t *testing.T) {
	t.Setenv("GOWORK", "off")
	before, after := buildShop(t, shopBefore), buildShop(t, shopBefore+gift)

	c := Conform(shopPlan(t, giftPlan), before, after)

	wantFindings(t, c.Findings)
	// Order, Priced, Gift, the SubClassOf, For and Value.
	if c.Checked != 6 {
		t.Errorf("checked = %d, want 6", c.Checked)
	}
	if len(c.Unchecked) != 0 {
		t.Errorf("unchecked = %v", c.Unchecked)
	}
}

func TestConformImpliedInterfaceCounts(t *testing.T) {
	t.Setenv("GOWORK", "off")
	src := `// Package shop sells things.
package shop

// Node is the top.
type Node interface{ ID() int }

// Priced is a node with a price.
type Priced interface {
	Node
	Price() int
}

// Gift is priced, and so a node.
type Gift struct{ Value int }

// ID identifies it.
func (Gift) ID() int { return 1 }

// Price is its value.
func (Gift) Price() int { return 1 }
`
	// gocode writes only the direct edge, Gift to Priced. The plan's Gift to Node
	// still holds.
	plan := shopPlan(t, "Declaration(Class(:shop.Gift))\nDeclaration(Class(:shop.Node))\nSubClassOf(:shop.Gift :shop.Node)")
	o := buildShop(t, src)
	wantFindings(t, Conform(plan, o, o).Findings)
}

func TestConformReportsWhatThePlanPromised(t *testing.T) {
	t.Setenv("GOWORK", "off")
	before, after := buildShop(t, shopBefore), buildShop(t, shopBefore+gift)

	plan := shopPlan(t, giftPlan+`
	# The code has none of these.
	Declaration(Class(:shop.Refund))
	Declaration(DataProperty(:shop.Refund.Reason))
	DataPropertyDomain(:shop.Refund.Reason :shop.Refund)
	DataPropertyRange(:shop.Refund.Reason xsd:string)
	Declaration(DataProperty(:shop.Gift.Wrapped))
	DataPropertyDomain(:shop.Gift.Wrapped :shop.Gift)
	DataPropertyRange(:shop.Gift.Wrapped xsd:boolean)

	# Product does not take these from Priced's side.
	SubClassOf(:shop.Note :shop.Priced)
	SubClassOf(:shop.Draft :shop.Priced)

	# Packages.
	ClassAssertion(:Package :package.example.com.shop.billing)
	ObjectPropertyAssertion(:imports :package.example.com.shop :package.example.com.shop)
	`)
	c := Conform(plan, before, after)

	wantFindings(t, c.Findings,
		"missing-field shop.Gift.Wrapped",
		"missing-import package.example.com.shop",
		"missing-interface shop.Draft",
		"missing-package package.example.com.shop.billing",
		"missing-type shop.Note",
		"missing-type shop.Refund",
	)
}

func TestConformFieldMismatch(t *testing.T) {
	t.Setenv("GOWORK", "off")
	before, after := buildShop(t, shopBefore), buildShop(t, shopBefore+gift)

	plan := shopPlan(t, strings.NewReplacer(
		"DataPropertyRange(:shop.Gift.Value xsd:integer)", "DataPropertyRange(:shop.Gift.Value xsd:string)",
		"ObjectPropertyDomain(:shop.Gift.For :shop.Gift)", "ObjectPropertyDomain(:shop.Gift.For :shop.Order)",
	).Replace(giftPlan))
	c := Conform(plan, before, after)

	wantFindings(t, c.Findings, "field-mismatch shop.Gift.For", "field-mismatch shop.Gift.Value")
	if !strings.Contains(c.Findings[1].Message, "holds xsd:string, and the code has xsd:integer") {
		t.Errorf("message should name both types: %s", c.Findings[1].Message)
	}
}

// A plan may name a field without saying what it holds. That is a promise that
// the field exists, and nothing more.
func TestConformFieldWithoutARange(t *testing.T) {
	t.Setenv("GOWORK", "off")
	before, after := buildShop(t, shopBefore), buildShop(t, shopBefore+gift)
	plan := shopPlan(t, `
	Declaration(Class(:shop.Gift))
	SubClassOf(:shop.Gift :shop.Priced)
	Declaration(ObjectProperty(:shop.Gift.For))
	Declaration(DataProperty(:shop.Gift.Value))
	`)
	wantFindings(t, Conform(plan, before, after).Findings)
}

func TestConformReportsWhatNobodyPlanned(t *testing.T) {
	t.Setenv("GOWORK", "off")
	before := buildShop(t, shopBefore)
	after := buildShop(t, strings.Replace(shopBefore, "type Draft struct{ Catalog *Catalog }",
		"type Draft struct {\n\tCatalog *Catalog\n\tBuyer string\n}", 1)+gift)

	c := Conform(owl.New(""), before, after)

	// Gift's own fields are covered by the finding about Gift. What was already
	// there is not mentioned at all.
	wantFindings(t, c.Findings, "unplanned-type shop.Gift", "unplanned-field shop.Draft.Buyer")
	if c.Checked != 0 {
		t.Errorf("checked = %d, want 0 with no plan", c.Checked)
	}
}

func TestConformRetypedFieldNeedsAPlan(t *testing.T) {
	t.Setenv("GOWORK", "off")
	before := buildShop(t, shopBefore)
	after := buildShop(t, strings.Replace(shopBefore, "Customer string", "Customer int", 1))

	wantFindings(t, Conform(owl.New(""), before, after).Findings, "unplanned-field shop.Order.Customer")

	plan := shopPlan(t, "Declaration(DataProperty(:shop.Order.Customer))\nDataPropertyRange(:shop.Order.Customer xsd:integer)")
	wantFindings(t, Conform(plan, before, after).Findings)
}

// writeModule writes files into a fresh module and describes it.
func writeModule(t *testing.T, files map[string]string) *owl.Ontology {
	t.Helper()
	root := t.TempDir()
	files["go.mod"] = "module example.com/shop\n\ngo 1.21\n"
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	o, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func TestConformPackagesAndImports(t *testing.T) {
	t.Setenv("GOWORK", "off")
	core := "// Package core is the core.\npackage core\n\n// Thing is a thing.\ntype Thing struct{ Name string }\n"
	api := "// Package api serves.\npackage api\n"
	apiUsingCore := api + "\nimport \"example.com/shop/core\"\n\nvar _ core.Thing\n"
	billing := "// Package billing bills.\npackage billing\n\nimport \"example.com/shop/core\"\n\nvar _ core.Thing\n"

	before := writeModule(t, map[string]string{"core/core.go": core, "api/api.go": api})
	after := writeModule(t, map[string]string{"core/core.go": core, "api/api.go": apiUsingCore, "billing/billing.go": billing})

	// The new package's own import is covered by the finding about the package.
	wantFindings(t, Conform(owl.New(""), before, after).Findings,
		"unplanned-package package.example.com.shop.billing",
		"unplanned-import package.example.com.shop.api",
	)

	plan := shopPlan(t, `
	ClassAssertion(:Package :package.example.com.shop.billing)
	ObjectPropertyAssertion(:imports :package.example.com.shop.billing :package.example.com.shop.core)
	ObjectPropertyAssertion(:imports :package.example.com.shop.api :package.example.com.shop.core)
	`)
	c := Conform(plan, before, after)
	wantFindings(t, c.Findings)
	if c.Checked != 5 { // the three packages named, and the two imports
		t.Errorf("checked = %d, want 5", c.Checked)
	}
}

func TestConformLeavesOutsideTermsUnchecked(t *testing.T) {
	t.Setenv("GOWORK", "off")
	o := buildShop(t, shopBefore)
	plan := shopPlan(t, "Declaration(Class(:shop.Order))\nDeclaration(Class(foaf:Person))\nSubClassOf(:shop.Order owl:Thing)")

	c := Conform(plan, o, o)

	wantFindings(t, c.Findings)
	if len(c.Unchecked) != 1 || c.Unchecked[0] != "http://xmlns.com/foaf/0.1/Person" {
		t.Errorf("unchecked = %v, want foaf:Person alone: owl:Thing is built in", c.Unchecked)
	}
}

func TestReviewPlanReadsThePlanAsCode(t *testing.T) {
	t.Setenv("GOWORK", "off")
	before := buildShop(t, shopBefore)

	// Basket repeats Order: the same three fields under other names. Nothing has
	// been written yet, and this is the moment to find out.
	d := ReviewPlan(shopPlan(t, `
	Declaration(Class(:shop.Catalog))
	Declaration(Class(:shop.Basket))
	Declaration(ObjectProperty(:shop.Basket.From))
	ObjectPropertyDomain(:shop.Basket.From :shop.Basket)
	ObjectPropertyRange(:shop.Basket.From :shop.Catalog)
	Declaration(DataProperty(:shop.Basket.Buyer))
	DataPropertyDomain(:shop.Basket.Buyer :shop.Basket)
	DataPropertyRange(:shop.Basket.Buyer xsd:string)
	Declaration(DataProperty(:shop.Basket.Sum))
	DataPropertyDomain(:shop.Basket.Sum :shop.Basket)
	DataPropertyRange(:shop.Basket.Sum xsd:integer)

	Declaration(Class(:shop.Note))
	`), before)

	if len(d.Added) != 2 || d.Added[0] != owl.Class("https://example.com/shop#shop.Basket") {
		t.Errorf("added = %v", d.Added)
	}
	wantFindings(t, d.Findings, "same-shape-type shop.Basket", "new-orphan shop.Note")
}

func TestReviewPlanCatchesATypo(t *testing.T) {
	t.Setenv("GOWORK", "off")
	before := buildShop(t, shopBefore)

	d := ReviewPlan(shopPlan(t, `
	Declaration(Class(:shop.Gift))
	Declaration(ObjectProperty(:shop.Gift.For))
	ObjectPropertyDomain(:shop.Gift.For :shop.Gift)
	ObjectPropertyRange(:shop.Gift.For :shop.Ordr)
	`), before)

	if got := findingsOf(d.Findings); !slices.Contains(got, "undeclared-entity shop.Ordr") {
		t.Errorf("findings = %q, want undeclared-entity for the typo", got)
	}
}

// A plan declares the existing types it mentions, and missing-label and
// orphan-class would fire on every planned type. None of that is news.
func TestReviewPlanIsQuietAboutTheExpected(t *testing.T) {
	t.Setenv("GOWORK", "off")
	before := buildShop(t, shopBefore)

	d := ReviewPlan(shopPlan(t, giftPlan), before)

	wantFindings(t, d.Findings)
	if len(d.Added) != 1 {
		t.Errorf("added = %v, want only Gift", d.Added)
	}
}

func TestOverlayKeepsEachAxiomOnce(t *testing.T) {
	a := shopPlan(t, "Declaration(Class(:shop.A))\nDeclaration(Class(:shop.B))")
	b := shopPlan(t, "Declaration(Class(:shop.B))\nDeclaration(Class(:shop.C))")

	o := Overlay(a, b)

	if o.Len() != 3 {
		t.Errorf("len = %d, want 3", o.Len())
	}
	if a.Len() != 2 || b.Len() != 2 {
		t.Errorf("the inputs should be left alone: %d and %d", a.Len(), b.Len())
	}
	if o.IRI != a.IRI {
		t.Errorf("iri = %s, want the base's", o.IRI)
	}
}
