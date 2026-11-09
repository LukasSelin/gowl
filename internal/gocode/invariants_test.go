package gocode

import (
	"strings"
	"sync"
	"testing"

	"gowl/lint"
	"gowl/owl"
)

// The shop: core keeps things, api serves them over HTTP, billing bills.
const (
	invCore = `// Package core keeps things.
package core

import "strings"

// Thing is a thing.
type Thing struct{ Name string }

// Store keeps things.
type Store interface {
	Get(id int) (Thing, error)
}

var _ = strings.ToUpper
`
	invAPI = `// Package api serves.
package api

import (
	"net/http"

	"example.com/shop/core"
)

var _ core.Thing
var _ http.Handler
`
	invBilling = `// Package billing bills.
package billing

import (
	"fmt"

	"example.com/shop/core"
)

var _ core.Thing
var _ = fmt.Sprint
`
)

func shopFiles(extra map[string]string) map[string]string {
	files := map[string]string{"core/core.go": invCore, "api/api.go": invAPI, "billing/billing.go": invBilling}
	for k, v := range extra {
		files[k] = v
	}
	return files
}

// The rules the shop keeps: the core does no I/O, and every package is placed.
const shopRules = `
	ClassAssertion(:Core :package.example.com.shop.core)
	ClassAssertion(:Core :package.example.com.shop.billing)
	ClassAssertion(:Edge :package.example.com.shop.api)
	ClassAssertion(:Effectful :package.net.http)
	ClassAssertion(:Effectful :package.os)
	SubObjectPropertyOf(:imports :dependsOn)
	TransitiveObjectProperty(:dependsOn)
	DisjointClasses(Annotation(rdfs:comment "The core does no I/O.") :Core ObjectSomeValuesFrom(:dependsOn :Effectful))
	SubClassOf(:Package ObjectUnionOf(:Core :Edge))
`

func invariants(t *testing.T, axioms string) *owl.Ontology {
	t.Helper()
	o, err := owl.ParseFunctionalString(`Prefix(:=<https://example.com/shop#>)
Prefix(rdfs:=<http://www.w3.org/2000/01/rdf-schema#>)
Ontology(<https://example.com/shop/design/invariants>
` + axioms + "\n)\n")
	if err != nil {
		t.Fatal(err)
	}
	return o
}

var (
	stdOnce sync.Once
	std     []string
)

// known names the standard library in the shop's namespace, as the command
// would; it is listed once, because go list std is slow.
func known(t *testing.T, module *owl.Ontology) []owl.NamedIndividual {
	t.Helper()
	stdOnce.Do(func() {
		var err error
		if std, err = KnownPackages("."); err != nil {
			t.Fatal(err)
		}
	})
	var out []owl.NamedIndividual
	for _, p := range std {
		out = append(out, PackageIndividual(module, p))
	}
	return out
}

func check(t *testing.T, module *owl.Ontology, axioms string) Conformance {
	t.Helper()
	return CheckInvariants(invariants(t, axioms), module, known(t, module))
}

func TestInvariantsHold(t *testing.T) {
	t.Setenv("GOWORK", "off")
	o := writeModule(t, shopFiles(nil))
	c := check(t, o, shopRules)
	wantFindings(t, c.Findings)
	if c.Checked != 2 {
		t.Errorf("checked = %d, want 2", c.Checked)
	}
}

// When billing starts to use the api, the core reaches the network through it,
// and the finding says why the rule exists and which imports are to blame.
func TestInvariantViolatedThroughAnotherPackage(t *testing.T) {
	t.Setenv("GOWORK", "off")
	billing := strings.Replace(invBilling, `"fmt"`, "\"fmt\"\n\n\t\"example.com/shop/api\"", 1) + "\nvar _ = api.Serve\n"
	api := invAPI + "\n// Serve serves.\nfunc Serve() {}\n"
	o := writeModule(t, shopFiles(map[string]string{"billing/billing.go": billing, "api/api.go": api}))

	c := check(t, o, shopRules)
	wantFindings(t, c.Findings, "invariant-violated package.example.com.shop.billing")
	f := c.Findings[0]
	for _, want := range []string{
		"The core does no I/O: ",
		":package.example.com.shop.billing :dependsOn :package.net.http",
		"(through :package.example.com.shop.billing →:imports :package.example.com.shop.api →:imports :package.net.http)",
	} {
		if !strings.Contains(f.Message, want) {
			t.Errorf("message lacks %q:\n%s", want, f.Message)
		}
	}
	if f.Axiom == nil {
		t.Error("the finding does not carry the rule it breaks")
	}
}

// A new package has to be placed on purpose.
func TestNewPackageMustBePlaced(t *testing.T) {
	t.Setenv("GOWORK", "off")
	o := writeModule(t, shopFiles(map[string]string{"util/util.go": "// Package util helps.\npackage util\n"}))
	wantFindings(t, check(t, o, shopRules).Findings, "invariant-violated package.example.com.shop.util")
}

// A misspelled package is caught, whether the module's own or the standard
// library's; one the module must never use can be declared instead.
func TestUnknownPackages(t *testing.T) {
	t.Setenv("GOWORK", "off")
	o := writeModule(t, shopFiles(nil))
	wantFindings(t, check(t, o, shopRules+`
		ClassAssertion(:Effectful :package.os.exce)
		ClassAssertion(:Core :package.example.com.shop.cor)
		ClassAssertion(:ExternalPackage :package.github.com.evil.logger)
		ClassAssertion(:Effectful :package.github.com.evil.logger)
	`).Findings,
		"unknown-package package.example.com.shop.cor",
		"unknown-package package.os.exce",
	)
}

func TestEmptyTermAndRefusals(t *testing.T) {
	t.Setenv("GOWORK", "off")
	o := writeModule(t, shopFiles(nil))
	c := check(t, o, shopRules+`
		DisjointClasses(:Cor ObjectSomeValuesFrom(:dependsOn :Effectful))
		ObjectPropertyAssertion(:imports :package.example.com.shop.core :package.example.com.shop.api)
		ClassAssertion(:Package :package.example.com.shop.core)
		EquivalentClasses(:Core ObjectOneOf(:package.example.com.shop.core))
	`)
	var rules []string
	for _, f := range c.Findings {
		rules = append(rules, f.Rule+" "+f.Severity.String())
	}
	got := strings.Join(rules, ", ")
	want := "unsupported-invariant error, unsupported-invariant error, unsupported-invariant error, empty-term warning"
	if got != want {
		t.Errorf("findings = %s\nwant       %s", got, want)
		for _, f := range c.Findings {
			t.Log(f)
		}
	}
	for _, f := range c.Findings {
		if f.Rule == UnsupportedInvariant && !strings.Contains(f.Message, "not checked") {
			t.Errorf("message = %q", f.Message)
		}
	}
}

// The invariants can fix an interface's methods: then the code must have
// exactly those, on every change.
func TestFrozenContract(t *testing.T) {
	t.Setenv("GOWORK", "off")
	frozen := shopRules + `AnnotationAssertion(:method :core.Store "Get(id int) (Thing, error)")`

	o := writeModule(t, shopFiles(nil))
	c := check(t, o, frozen)
	wantFindings(t, c.Findings)
	if c.Checked != 3 {
		t.Errorf("checked = %d, want 3: two rules and one method", c.Checked)
	}

	grown := strings.Replace(invCore, "Get(id int) (Thing, error)\n", "Get(id int) (Thing, error)\n\tPut(t Thing)\n", 1)
	wantFindings(t, check(t, writeModule(t, shopFiles(map[string]string{"core/core.go": grown})), frozen).Findings,
		"unplanned-method core.Store")

	changed := strings.Replace(invCore, "Get(id int) (Thing, error)", "Get(id string) (Thing, error)", 1)
	wantFindings(t, check(t, writeModule(t, shopFiles(map[string]string{"core/core.go": changed})), frozen).Findings,
		"method-mismatch core.Store")

	wantFindings(t, check(t, o, shopRules+`AnnotationAssertion(:method :core.Shelf "Get() int")`).Findings,
		"missing-type core.Shelf")
}

// A plan is checked against the invariants before it is built, and is told
// only about what it would break.
func TestReviewInvariants(t *testing.T) {
	t.Setenv("GOWORK", "off")
	// The base already has a package nobody placed; that is not the plan's doing.
	before := writeModule(t, shopFiles(map[string]string{"util/util.go": "// Package util helps.\npackage util\n"}))
	inv := invariants(t, shopRules)
	ks := known(t, before)

	plan := shopPlan(t, `
		ObjectPropertyAssertion(:imports :package.example.com.shop.core :package.os)
		ClassAssertion(:Package :package.example.com.shop.audit)
	`)
	// audit is not placed, and core reaching os takes billing, which imports
	// core, with it.
	c := ReviewInvariants(inv, plan, before, ks)
	wantFindings(t, c.Findings,
		"invariant-violated package.example.com.shop.audit",
		"invariant-violated package.example.com.shop.billing",
		"invariant-violated package.example.com.shop.core",
	)
	for _, f := range c.Findings {
		if f.Severity != lint.Error {
			t.Errorf("%v: severity %v", f, f.Severity)
		}
	}
	// A plan that keeps to the rules hears nothing, not even about util.
	wantFindings(t, ReviewInvariants(inv, shopPlan(t, "Declaration(Class(:core.Thing))"), before, ks).Findings)
}
