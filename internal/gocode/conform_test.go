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

// What a change imports from outside the module is not a plan's business: the
// invariants say what the module may depend on. So a new import of fmt is no
// unplanned package and no unplanned import, and a planned package that
// imports it needs only the plan for the package.
func TestConformLeavesImportsFromOutsideToTheInvariants(t *testing.T) {
	t.Setenv("GOWORK", "off")
	core := "// Package core is the core.\npackage core\n\n// Thing is a thing.\ntype Thing struct{ Name string }\n"
	usesStrings := "package core\n\nimport \"strings\"\n\nvar _ = strings.ToUpper\n"
	billing := "// Package billing bills.\npackage billing\n\nimport \"fmt\"\n\nvar _ = fmt.Sprint\n"

	before := writeModule(t, map[string]string{"core/core.go": core})
	after := writeModule(t, map[string]string{"core/core.go": core, "core/use.go": usesStrings, "billing/billing.go": billing})

	wantFindings(t, Conform(shopPlan(t, "ClassAssertion(:Package :package.example.com.shop.billing)"), before, after).Findings)
	wantFindings(t, Conform(owl.New(""), before, after).Findings,
		"unplanned-package package.example.com.shop.billing")
}

// A plan that does name an import from outside the module has it checked.
func TestConformChecksAPlannedImportFromOutside(t *testing.T) {
	t.Setenv("GOWORK", "off")
	core := "// Package core is the core.\npackage core\n\n// Thing is a thing.\ntype Thing struct{ Name string }\n"
	withStrings := writeModule(t, map[string]string{"core/core.go": core, "core/use.go": "package core\n\nimport \"strings\"\n\nvar _ = strings.ToUpper\n"})
	without := writeModule(t, map[string]string{"core/core.go": core})
	plan := shopPlan(t, "ObjectPropertyAssertion(:imports :package.example.com.shop.core :package.strings)")

	c := Conform(plan, without, withStrings)
	wantFindings(t, c.Findings)
	if c.Checked != 2 { // the module's package, and its import; strings is no package of the module
		t.Errorf("checked = %d, want 2", c.Checked)
	}
	wantFindings(t, Conform(plan, without, without).Findings, "missing-import package.example.com.shop.core")
}

// An interface's methods are its contract: one the change adds needs a plan,
// and the plan's version of it is held to the code's.
func TestConformMethods(t *testing.T) {
	t.Setenv("GOWORK", "off")
	before := buildShop(t, shopBefore)
	after := buildShop(t, strings.Replace(shopBefore, "type Priced interface{ Price() int }",
		"type Priced interface {\n\tPrice() int\n\tCurrency() string\n}", 1))
	priced := "Declaration(Class(:shop.Priced))\n"

	for _, c := range []struct {
		name, plan string
		want       []string
	}{
		{"no plan", "", []string{"unplanned-method shop.Priced"}},
		// Names and spacing are the plan writer's own.
		{"planned", priced + `AnnotationAssertion(:method :shop.Priced "Currency() (code string)")`, nil},
		{"mismatch", priced + `AnnotationAssertion(:method :shop.Priced "Currency() int")`, []string{"method-mismatch shop.Priced"}},
		{"missing", priced + `AnnotationAssertion(:method :shop.Priced "Currency() string")
			AnnotationAssertion(:method :shop.Priced "Discount(pct float64) int")`, []string{"missing-method shop.Priced"}},
		{"malformed", priced + `AnnotationAssertion(:method :shop.Priced "Currency(")`,
			[]string{"malformed-method shop.Priced", "unplanned-method shop.Priced"}},
		// A plan that gives methods to a type it does not even declare still
		// gets told the type is missing.
		{"no such type", priced + `AnnotationAssertion(:method :shop.Priced "Currency() string")
			AnnotationAssertion(:method :shop.Ghost "Boo()")`, []string{"missing-type shop.Ghost"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			plan := owl.New("")
			if c.plan != "" {
				plan = shopPlan(t, c.plan)
			}
			wantFindings(t, Conform(plan, before, after).Findings, c.want...)
		})
	}

	// Methods that were there before the change are left alone.
	wantFindings(t, Conform(owl.New(""), after, after).Findings)
}

// cheapest is a change to shopBefore: a function the package now offers.
const cheapest = `
// Cheapest finds the cheapest of some products.
func Cheapest(c *Catalog, among ...Product) (best Product, ok bool) { return }
`

// What a package offers is planned like its types: a function the change adds
// needs a plan, and the plan's version of it is held to the code's.
func TestConformFuncs(t *testing.T) {
	t.Setenv("GOWORK", "off")
	before, after := buildShop(t, shopBefore), buildShop(t, shopBefore+cheapest)
	const shop = ":package.example.com.shop"
	planned := `AnnotationAssertion(:func ` + shop + ` "Cheapest(c *Catalog, among ...Product) (best Product, ok bool)")`

	for _, c := range []struct {
		name, plan string
		want       []string
	}{
		{"no plan", "", []string{"unplanned-func package.example.com.shop"}},
		// Names and spacing are the plan writer's own, and a bare type is the
		// package's.
		{"planned", planned, nil},
		{"mismatch", `AnnotationAssertion(:func ` + shop + ` "Cheapest(c *Catalog) Product")`, []string{"func-mismatch package.example.com.shop"}},
		{"missing", planned + `
			AnnotationAssertion(:func ` + shop + ` "Dearest(c *Catalog) Product")`, []string{"missing-func package.example.com.shop"}},
		{"malformed", `AnnotationAssertion(:func ` + shop + ` "Cheapest(")`,
			[]string{"malformed-func package.example.com.shop", "unplanned-func package.example.com.shop"}},
		// A plan that gives functions to a package nobody has is told so, once.
		{"no such package", planned + `
			AnnotationAssertion(:func :package.example.com.shop.ghost "Boo()")
			AnnotationAssertion(:func :package.example.com.shop.ghost "Hoo()")`, []string{"missing-package package.example.com.shop.ghost"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			plan := owl.New("")
			if c.plan != "" {
				plan = shopPlan(t, c.plan)
			}
			got := Conform(plan, before, after)
			wantFindings(t, got.Findings, c.want...)
			for _, f := range got.Findings {
				want := lint.Error
				if f.Rule == UnplannedFunc {
					want = lint.Warning
				}
				if f.Severity != want {
					t.Errorf("%s: severity %v, want %v", f.Rule, f.Severity, want)
				}
			}
		})
	}

	// A function that was there before the change is left alone, until its
	// signature changes.
	wantFindings(t, Conform(owl.New(""), after, after).Findings)
	changed := buildShop(t, shopBefore+strings.Replace(cheapest, "(best Product, ok bool)", "(best *Product, ok bool)", 1))
	wantFindings(t, Conform(owl.New(""), after, changed).Findings, "unplanned-func package.example.com.shop")

	// What the plan checked is counted: the one function.
	if c := Conform(shopPlan(t, planned), before, after); c.Checked != 1 {
		t.Errorf("checked = %d, want 1", c.Checked)
	}
}

// A new package's functions are covered by the finding about the package. A
// plan names the package to plan it: describing its functions does not.
func TestConformFuncsOfANewPackage(t *testing.T) {
	t.Setenv("GOWORK", "off")
	core := "// Package core is the core.\npackage core\n\n// Thing is a thing.\ntype Thing struct{ Name string }\n"
	billing := "// Package billing bills.\npackage billing\n\n// Bill bills.\nfunc Bill(cents int) error { return nil }\n"
	before := writeModule(t, map[string]string{"core/core.go": core})
	after := writeModule(t, map[string]string{"core/core.go": core, "billing/billing.go": billing})
	const named = "ClassAssertion(:Package :package.example.com.shop.billing)\n"
	const bill = `AnnotationAssertion(:func :package.example.com.shop.billing "Bill(cents int) error")`

	wantFindings(t, Conform(owl.New(""), before, after).Findings, "unplanned-package package.example.com.shop.billing")
	wantFindings(t, Conform(shopPlan(t, named+bill), before, after).Findings)
	// A package that is planned still has to have its functions named.
	wantFindings(t, Conform(shopPlan(t, named), before, after).Findings,
		"unplanned-func package.example.com.shop.billing")
	// And one whose functions are described still has to be named.
	wantFindings(t, Conform(shopPlan(t, bill), before, after).Findings,
		"unplanned-package package.example.com.shop.billing")
}

// An annotation describes a type or a package and does not bring it into being.
// One the module does not have is declared by the plan, or its name is a slip,
// and the plan is told while it is still a plan.
func TestReviewPlanReportsWhatItDescribesAndNeverDeclares(t *testing.T) {
	t.Setenv("GOWORK", "off")
	before := buildShop(t, shopBefore)

	d := ReviewPlan(shopPlan(t, `
	AnnotationAssertion(:method :shop.Prodct "Price() int")
	AnnotationAssertion(:kind :shop.Hook "interface")
	AnnotationAssertion(:definedIn :shop.Order :package.example.com.shpo)
	AnnotationAssertion(:func :package.example.com.shop.billing "Bill(cents int) error")
	`), before)
	wantFindings(t, d.Findings,
		"undeclared-entity package.example.com.shop.billing",
		"undeclared-entity package.example.com.shpo",
		"undeclared-entity shop.Hook",
		"undeclared-entity shop.Prodct",
	)
	for _, f := range d.Findings {
		if f.Severity != lint.Warning {
			t.Errorf("%v: severity %v, want a warning", f, f.Severity)
		}
	}
	if len(d.Added) != 0 {
		t.Errorf("added = %v: a type the plan only describes is not planned", d.Added)
	}

	// Declared, they are the plan's to describe, and what exists already needs
	// no declaring.
	d = ReviewPlan(shopPlan(t, `
	Declaration(Class(:shop.Hook))
	AnnotationAssertion(:kind :shop.Hook "interface")
	Declaration(NamedIndividual(:package.example.com.shop.billing))
	ClassAssertion(:Package :package.example.com.shop.billing)
	AnnotationAssertion(:definedIn :shop.Hook :package.example.com.shop.billing)
	AnnotationAssertion(:func :package.example.com.shop.billing "Bill(cents int) error")
	AnnotationAssertion(:method :shop.Product "Discount(pct int) int")
	AnnotationAssertion(:func :package.example.com.shop "Cheapest(c *Catalog) Product")
	`), before)
	wantFindings(t, d.Findings, "new-orphan shop.Hook")
}

// The methods of a type that is no interface are what it offers. One the
// change adds is a warning, not the error it is on an interface, and one an
// interface asks for is planned by planning that the type satisfies it.
func TestConformMethodsOfOtherTypes(t *testing.T) {
	t.Setenv("GOWORK", "off")
	before := buildShop(t, shopBefore)
	after := buildShop(t, shopBefore+`
// Discount lowers the price.
func (p *Product) Discount(pct int) int { return p.Cents }

// Price is what everything in the catalog costs.
func (c Catalog) Price() int { return c.total() }

func (c Catalog) total() int { return 0 }
`)
	discount := `AnnotationAssertion(:method :shop.Product "Discount(pct int) int")`
	priced := "SubClassOf(:shop.Catalog :shop.Priced)\n"

	for _, c := range []struct {
		name, plan string
		want       []string
	}{
		{"no plan", "", []string{"unplanned-method shop.Catalog", "unplanned-method shop.Product"}},
		{"planned", priced + discount, nil},
		{"planned by name", `AnnotationAssertion(:method :shop.Catalog "Price() int")` + discount, nil},
		{"mismatch", priced + `AnnotationAssertion(:method :shop.Product "Discount() int")`, []string{"method-mismatch shop.Product"}},
		{"missing", priced + discount + `AnnotationAssertion(:method :shop.Product "Refund() int")`, []string{"missing-method shop.Product"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			plan := owl.New("")
			if c.plan != "" {
				plan = shopPlan(t, c.plan)
			}
			got := Conform(plan, before, after)
			wantFindings(t, got.Findings, c.want...)
			for _, f := range got.Findings {
				if f.Rule == UnplannedMethod && f.Severity != lint.Warning {
					t.Errorf("%s on %s: severity %v, want a warning for a type that is no interface", f.Rule, f.Subject, f.Severity)
				}
			}
		})
	}
}

// When an interface grows, the plan names its new method, and the types that
// already satisfied it need no plan for implementing it.
func TestConformMethodAnInterfaceAsksFor(t *testing.T) {
	t.Setenv("GOWORK", "off")
	before := buildShop(t, shopBefore)
	after := buildShop(t, strings.Replace(shopBefore, "type Priced interface{ Price() int }",
		"type Priced interface {\n\tPrice() int\n\tCurrency() string\n}", 1)+`
// Currency is the product's currency.
func (p Product) Currency() string { return "SEK" }
`)
	plan := shopPlan(t, `AnnotationAssertion(:method :shop.Priced "Currency() string")`)
	wantFindings(t, Conform(plan, before, after).Findings)
	// With no plan, the interface is what is reported: its implementation follows.
	wantFindings(t, Conform(owl.New(""), before, after).Findings, "unplanned-method shop.Priced")
}

// An interface can also grow by embedding another. What the new one asks of a
// type that already satisfied the old is planned by planning the embedding.
func TestConformMethodAnEmbeddedInterfaceAsksFor(t *testing.T) {
	t.Setenv("GOWORK", "off")
	before := buildShop(t, shopBefore)
	after := buildShop(t, strings.Replace(shopBefore, "type Priced interface{ Price() int }",
		"type Priced interface {\n\tStamped\n\tPrice() int\n}", 1)+`
// Stamped is anything with a stamp.
type Stamped interface{ Stamp() string }

// Stamp is the product's stamp.
func (p Product) Stamp() string { return p.Name }
`)
	plan := shopPlan(t, `
	Declaration(Class(:shop.Priced))
	Declaration(Class(:shop.Stamped))
	AnnotationAssertion(:method :shop.Stamped "Stamp() string")
	SubClassOf(:shop.Priced :shop.Stamped)
	`)
	wantFindings(t, Conform(plan, before, after).Findings)
}

// What a plan calls a type parameter a function declares is its own. The
// parameters of a generic type are called what the type's declaration calls
// them, whatever one of its methods calls them.
func TestConformTypeParameters(t *testing.T) {
	t.Setenv("GOWORK", "off")
	const cache = `
// Cache keeps things by key.
type Cache[Key comparable, Val any] struct{ Size int }
`
	before := buildShop(t, shopBefore+cache)
	after := buildShop(t, shopBefore+cache+`
// Put keeps a thing. The receiver has its own names for the cache's parameters.
func (c *Cache[K, V]) Put(k K, v V) {}

// Each calls f for every one of some priced things.
func Each[P Priced](ps []P, f func(P) int) int { return 0 }
`)
	plan := shopPlan(t, `
	AnnotationAssertion(:method :shop.Cache "Put(k Key, v Val)")
	AnnotationAssertion(:func :package.example.com.shop "Each[T Priced](ts []T, f func(T) int) int")
	`)
	wantFindings(t, Conform(plan, before, after).Findings)

	// A method that only renames its receiver's parameters has not changed.
	renamed := buildShop(t, shopBefore+cache+`
// Put keeps a thing.
func (c *Cache[Key, Val]) Put(k Key, v Val) {}

// Each calls f for every one of some priced things.
func Each[Q Priced](qs []Q, f func(Q) int) int { return 0 }
`)
	wantFindings(t, Conform(owl.New(""), after, renamed).Findings)

	// Swapping two parameters of the type is a different method.
	swapped := shopPlan(t, `AnnotationAssertion(:method :shop.Cache "Put(v Val, k Key)")`)
	wantFindings(t, Conform(swapped, before, after).Findings,
		"method-mismatch shop.Cache", "unplanned-func package.example.com.shop")
}

// A plan can say what kind of type one is, and which package defines it.
func TestConformKindAndPackage(t *testing.T) {
	t.Setenv("GOWORK", "off")
	o := buildShop(t, shopBefore)

	c := Conform(shopPlan(t, `
	AnnotationAssertion(:kind :shop.Priced "interface")
	AnnotationAssertion(:kind :shop.Order "struct")
	AnnotationAssertion(:definedIn :shop.Order :package.example.com.shop)
	`), o, o)
	wantFindings(t, c.Findings)
	if c.Checked != 3 {
		t.Errorf("checked = %d, want 3", c.Checked)
	}

	c = Conform(shopPlan(t, `
	AnnotationAssertion(:kind :shop.Priced "struct")
	AnnotationAssertion(:definedIn :shop.Order :package.example.com.shop.billing)
	AnnotationAssertion(:kind :shop.Ghost "struct")
	AnnotationAssertion(:definedIn :shop.Ghost :package.example.com.shop)
	`), o, o)
	wantFindings(t, c.Findings, "kind-mismatch shop.Priced", "misplaced-type shop.Order", "missing-type shop.Ghost")
	if want := `the kind "struct", and in the code its kind is "interface"`; !strings.Contains(c.Findings[0].Message, want) {
		t.Errorf("message lacks %q:\n%s", want, c.Findings[0].Message)
	}
}

// A plan says only what Build writes about code. Anything else it states is
// checked by nothing, and it is told so while it is still a plan.
func TestReviewPlanReportsWhatNothingChecks(t *testing.T) {
	t.Setenv("GOWORK", "off")
	before := buildShop(t, shopBefore)

	d := ReviewPlan(shopPlan(t, `
	Declaration(Class(:shop.Order))
	Declaration(Class(:shop.Draft))
	Declaration(Class(:Core))
	SubClassOf(:shop.Draft :shop.Priced)
	EquivalentClasses(:shop.Order :shop.Draft)
	DisjointClasses(:shop.Order :shop.Draft)
	SubClassOf(:shop.Order ObjectSomeValuesFrom(:shop.Order.Catalog :shop.Catalog))
	EquivalentClasses(:shop.Draft ObjectSomeValuesFrom(:shop.Order.Catalog :shop.Catalog))
	ClassAssertion(:Core :package.example.com.shop)
	ObjectPropertyAssertion(:shop.Order.Catalog :package.example.com.shop :package.example.com.shop)
	AnnotationAssertion(:name :package.example.com.shop "store")
	`), before)

	// Two named types said to be equivalent are read as satisfying each other,
	// which conform checks. The other equivalence is with a class expression.
	var unchecked []string
	for _, f := range d.Findings {
		if f.Rule != UncheckedAxiom {
			continue
		}
		if f.Severity != lint.Warning || f.Axiom == nil {
			t.Errorf("%v: want a warning that carries its axiom", f)
		}
		unchecked = append(unchecked, strings.SplitN(owl.Functional(f.Axiom), "(", 2)[0])
	}
	slices.Sort(unchecked)
	if got, want := strings.Join(unchecked, " "), "AnnotationAssertion ClassAssertion DisjointClasses EquivalentClasses ObjectPropertyAssertion SubClassOf"; got != want {
		t.Errorf("unchecked = %s\nwant        %s", got, want)
	}
	// The group the plan declares is no type, so it is not a new one.
	if len(d.Added) != 0 || slices.Contains(findingsOf(d.Findings), "new-orphan Core") {
		t.Errorf("added = %v, findings = %q: a class that is not named as a type is, is not a new type", d.Added, findingsOf(d.Findings))
	}

	// Everything the gift plan says is something conform reads.
	for _, f := range ReviewPlan(shopPlan(t, giftPlan+`
	ClassAssertion(:Package :package.example.com.shop)
	ObjectPropertyAssertion(:imports :package.example.com.shop :package.strings)
	AnnotationAssertion(:method :shop.Gift "Price() int")
	AnnotationAssertion(:kind :shop.Gift "struct")
	`), before).Findings {
		if f.Rule == UncheckedAxiom {
			t.Errorf("unexpected %v", f)
		}
	}
}

// A method or function a plan writes that is not a signature is found when the
// plan is reviewed, not once the code exists.
func TestReviewPlanReportsMalformedSignatures(t *testing.T) {
	t.Setenv("GOWORK", "off")
	before := buildShop(t, shopBefore)
	d := ReviewPlan(shopPlan(t, `
	AnnotationAssertion(:method :shop.Priced "Currency(")
	AnnotationAssertion(:method :shop.Priced "Price() int")
	AnnotationAssertion(:func :package.example.com.shop "Cheapest")
	`), before)
	wantFindings(t, d.Findings, "malformed-func package.example.com.shop", "malformed-method shop.Priced")
}
