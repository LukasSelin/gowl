package gocode

import (
	"slices"
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

// A plan that puts its new package in a group itself has put the group in the
// wrong file. Reviewing the plan says so. Here the package is one nobody
// placed, and the invariants' own groups are read as they were: the plan using
// their name takes nothing from them.
func TestReviewInvariantsWithAGroupInThePlan(t *testing.T) {
	t.Setenv("GOWORK", "off")
	before := writeModule(t, shopFiles(nil))
	plan := shopPlan(t, `
		Declaration(Class(:Core))
		ClassAssertion(:Package :package.example.com.shop.audit)
		ClassAssertion(:Core :package.example.com.shop.audit)
	`)
	wantFindings(t, ReviewInvariants(invariants(t, shopRules), plan, before, known(t, before)).Findings,
		"invariant-violated package.example.com.shop.audit")
}

// What the shop's types look like, for the rules about types to be about.
const (
	// The core gains a type with no comment that holds a Thing, and a store.
	invCoreTypes = invCore + `
type Bare struct{ Of Thing }

// Memory keeps things in memory.
type Memory struct{ Things []Thing }

// Get gets a thing.
func (Memory) Get(id int) (Thing, error) { return Thing{}, nil }
`
	// The api gains a store of its own, and an interface.
	invAPITypes = invAPI + `
// Remote keeps things far away.
type Remote struct{}

// Get fetches a thing.
func (Remote) Get(id int) (core.Thing, error) { return core.Thing{}, nil }

// Handler is something the api asks for.
type Handler interface{ Handle() }
`
)

// The rules reach the module's types as well as its packages: what each is,
// whether it is documented, where it is defined, what it implements and what
// it holds.
func TestInvariantsAboutTypes(t *testing.T) {
	t.Setenv("GOWORK", "off")
	o := writeModule(t, shopFiles(map[string]string{"core/core.go": invCoreTypes, "api/api.go": invAPITypes}))
	c := check(t, o, shopRules+`
		SubClassOf(Annotation(rdfs:comment "Every type says what it is for") :Type :Documented)
		SubClassOf(Annotation(rdfs:comment "Interfaces belong to the core") :Interface ObjectSomeValuesFrom(:definedIn :Core))
		SubClassOf(Annotation(rdfs:comment "A store lives in the core") ObjectHasValue(:implements :core.Store) ObjectSomeValuesFrom(:definedIn :Core))
		NegativeObjectPropertyAssertion(Annotation(rdfs:comment "Nothing bare holds a thing") :holds :core.Bare :core.Thing)
		DisjointClasses(Annotation(rdfs:comment "A struct is no interface") :Struct :Interface)
	`)
	wantFindings(t, c.Findings,
		"invariant-violated api.Handler",
		"invariant-violated api.Remote",
		"invariant-violated core.Bare",
		"invariant-violated core.Bare",
	)
	if c.Checked != 7 {
		t.Errorf("checked = %d, want 7: the shop's two rules and these five", c.Checked)
	}
	said := map[string]string{}
	for _, f := range c.Findings {
		_, local, _ := strings.Cut(string(f.Subject), "#")
		said[local] += f.Message + "\n"
	}
	for subject, want := range map[string][]string{
		"api.Handler": {"Interfaces belong to the core: :api.Handler is :Interface, and :api.Handler has no :definedIn to anything that is :Core"},
		"api.Remote":  {"A store lives in the core: ", "ObjectHasValue(:implements :core.Store)"},
		"core.Bare":   {"Every type says what it is for: :core.Bare is :Type, and :core.Bare is not :Documented", "Nothing bare holds a thing: :core.Bare :holds :core.Thing"},
	} {
		for _, w := range want {
			if !strings.Contains(said[subject], w) {
				t.Errorf("%s: message lacks %q:\n%s", subject, w, said[subject])
			}
		}
	}
}

// Only the code says what its types are, and a type the invariants name has to
// be one the module exports.
func TestInvariantsOwnNoFactsAboutTypes(t *testing.T) {
	t.Setenv("GOWORK", "off")
	o := writeModule(t, shopFiles(map[string]string{"core/core.go": invCoreTypes}))
	c := check(t, o, shopRules+`
		ClassAssertion(:Documented :core.Bare)
		ObjectPropertyAssertion(:implements :core.Thing :core.Store)
		ObjectPropertyAssertion(:definedIn :core.Thing :package.example.com.shop.api)
		SubClassOf(ObjectOneOf(:core.Thng) :Documented)
	`)
	var got []string
	for _, f := range c.Findings {
		_, local, _ := strings.Cut(string(f.Subject), "#")
		got = append(got, strings.TrimSpace(f.Rule+" "+local))
	}
	want := "unknown-type core.Thng, unsupported-invariant, unsupported-invariant, unsupported-invariant, invariant-violated core.Thng"
	// The rule about the misspelled type also fails, for the type nobody has.
	slices.Sort(got)
	wantSorted := strings.Split(want, ", ")
	slices.Sort(wantSorted)
	if !slices.Equal(got, wantSorted) {
		t.Errorf("findings = %q\nwant       %q", got, wantSorted)
		for _, f := range c.Findings {
			t.Log(f)
		}
	}

	// A group of the invariants' own can hold types, and a rule can be about it.
	wantFindings(t, check(t, o, shopRules+`
		ClassAssertion(:Value :core.Thing)
		ClassAssertion(:Value :core.Bare)
		SubClassOf(Annotation(rdfs:comment "A value is documented") :Value :Documented)
	`).Findings, "invariant-violated core.Bare")

	// A plan says what kind a type is, and where it lives, with an annotation.
	// Here those are facts, held to with a rule, and the annotation would be
	// checked by nothing: it is refused, with the rule to write instead.
	c = check(t, o, shopRules+`
		AnnotationAssertion(:kind :core.Store "struct")
		AnnotationAssertion(:definedIn :core.Store :package.example.com.shop.api)
		AnnotationAssertion(:name :package.example.com.shop.core "kernel")
	`)
	wantFindings(t, c.Findings, "unsupported-invariant ", "unsupported-invariant ", "unsupported-invariant ")
	for _, f := range c.Findings {
		if !strings.Contains(f.Message, "not checked: ") || !strings.Contains(f.Message, "a fact of the code") || f.Axiom == nil {
			t.Errorf("message = %q", f.Message)
		}
	}
	// The rules it points to say the same thing, and are checked.
	wantFindings(t, check(t, o, shopRules+`
		SubClassOf(ObjectOneOf(:core.Store) :Struct)
		SubClassOf(ObjectOneOf(:core.Store) ObjectHasValue(:definedIn :package.example.com.shop.api))
	`).Findings, "invariant-violated core.Store", "invariant-violated core.Store")
}

// The facts are there to read: each type an individual under its own name,
// beside the packages and the invariants' groups.
func TestFacts(t *testing.T) {
	t.Setenv("GOWORK", "off")
	o := writeModule(t, shopFiles(map[string]string{"core/core.go": invCoreTypes, "api/api.go": invAPITypes}))
	facts := Facts(invariants(t, shopRules), o, known(t, o)).Functional()
	for _, want := range []string{
		"ClassAssertion(:Package :package.example.com.shop.core)",
		"ClassAssertion(:Documented :package.example.com.shop.core)",
		"ClassAssertion(:Core :package.example.com.shop.core)",
		"ObjectPropertyAssertion(:imports :package.example.com.shop.api :package.net.http)",
		"ClassAssertion(:Type :core.Bare)",
		"ClassAssertion(:Struct :core.Thing)",
		"ClassAssertion(:Interface :core.Store)",
		"ClassAssertion(:Documented :core.Thing)",
		"ObjectPropertyAssertion(:definedIn :core.Memory :package.example.com.shop.core)",
		"ObjectPropertyAssertion(:implements :core.Memory :core.Store)",
		"ObjectPropertyAssertion(:implements :api.Remote :core.Store)",
		"ObjectPropertyAssertion(:holds :core.Memory :core.Thing)",
	} {
		if !strings.Contains(facts, want) {
			t.Errorf("missing %s", want)
		}
	}
	for _, unwanted := range []string{"ClassAssertion(:Documented :core.Bare)", "SubClassOf(", "DisjointClasses(", ":dependsOn"} {
		if strings.Contains(facts, unwanted) {
			t.Errorf("the facts hold %s, which is no fact of the code or group of the invariants", unwanted)
		}
	}
	if _, err := owl.ParseFunctionalString(facts); err != nil {
		t.Errorf("the facts do not parse back: %v", err)
	}
	if t.Failed() {
		t.Log(facts)
	}
}

// What the shop offers, for the invariants to hold it to.
const invCoreOffers = invCore + `
// Open opens a store.
func Open(name string) (Store, error) { return nil, nil }

// Memory keeps things in memory.
type Memory struct{}

// Get gets a thing.
func (Memory) Get(id int) (Thing, error) { return Thing{}, nil }

// Len counts them.
func (*Memory) Len() int { return 0 }
`

// The invariants say what the code offers, by signature: a function of a
// package, a method of a type. The code has to have each, and may have more.
func TestInvariantsCapabilities(t *testing.T) {
	t.Setenv("GOWORK", "off")
	o := writeModule(t, shopFiles(map[string]string{"core/core.go": invCoreOffers}))
	const core = ":package.example.com.shop.core"
	offers := shopRules + `
		AnnotationAssertion(:func ` + core + ` "Open(name string) (Store, error)")
		AnnotationAssertion(:method :core.Memory "Len() int")
	`
	c := check(t, o, offers)
	wantFindings(t, c.Findings)
	if c.Checked != 4 {
		t.Errorf("checked = %d, want 4: two rules, a function and a method", c.Checked)
	}

	for _, tc := range []struct {
		name, axioms string
		want         []string
	}{
		{"a function the code lacks", `AnnotationAssertion(:func ` + core + ` "Close() error")`,
			[]string{"missing-func package.example.com.shop.core"}},
		{"a function with another signature", `AnnotationAssertion(:func ` + core + ` "Open(name string) Store")`,
			[]string{"func-mismatch package.example.com.shop.core"}},
		{"a method the type lacks", `AnnotationAssertion(:method :core.Memory "Cap() int")`,
			[]string{"missing-method core.Memory"}},
		{"a method with another signature", `AnnotationAssertion(:method :core.Memory "Get(id string) (Thing, error)")`,
			[]string{"method-mismatch core.Memory"}},
		{"not a signature", `AnnotationAssertion(:func ` + core + ` "Open(")`,
			[]string{"malformed-func package.example.com.shop.core"}},
		{"a package nobody has", `AnnotationAssertion(:func :package.example.com.shop.ghost "Boo()")`,
			[]string{"missing-package package.example.com.shop.ghost"}},
		{"a package from outside", `AnnotationAssertion(:func :package.net.http "Get(url string)")`,
			[]string{"missing-package package.net.http"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantFindings(t, check(t, o, offers+tc.axioms).Findings, tc.want...)
		})
	}
}

// Given to a group, a capability is asked of every package or type in it.
func TestInvariantsCapabilitiesOfAGroup(t *testing.T) {
	t.Setenv("GOWORK", "off")
	version := "\n// Version says which version this is.\nfunc Version() string { return \"1\" }\n"
	o := writeModule(t, shopFiles(map[string]string{"core/core.go": invCoreOffers + version}))

	// Both core and billing are :Core, and only core says its version.
	wantFindings(t, check(t, o, shopRules+`AnnotationAssertion(:func :Core "Version() string")`).Findings,
		"missing-func package.example.com.shop.billing")
	withBilling := writeModule(t, shopFiles(map[string]string{"core/core.go": invCoreOffers + version, "billing/billing.go": invBilling + version}))
	c := check(t, withBilling, shopRules+`AnnotationAssertion(:func :Core "Version() string")`)
	wantFindings(t, c.Findings)
	if c.Checked != 4 {
		t.Errorf("checked = %d, want 4: two rules, and the function in two packages", c.Checked)
	}

	// A group of types: everything that keeps things can get one. A bare type
	// in the signature is each member's own package's.
	keepers := shopRules + `
		ClassAssertion(:Keeper :core.Memory)
		ClassAssertion(:Keeper :core.Thing)
		AnnotationAssertion(:method :Keeper "Get(id int) (Thing, error)")
	`
	wantFindings(t, check(t, o, keepers).Findings, "missing-method core.Thing")

	// A group's members have to be what the capability is a capability of.
	wantFindings(t, check(t, o, shopRules+`AnnotationAssertion(:method :Core "Get(id int) (Thing, error)")`).Findings,
		"unsupported-invariant package.example.com.shop.billing",
		"unsupported-invariant package.example.com.shop.core",
	)
	// And a name that is neither a type nor a group with anything in it is told so.
	wantFindings(t, check(t, o, shopRules+`AnnotationAssertion(:method :Keepr "Get(id int) (Thing, error)")`).Findings,
		"missing-type Keepr")
}

// A package or a type put in :Fixed offers exactly what the invariants list.
func TestInvariantsFixed(t *testing.T) {
	t.Setenv("GOWORK", "off")
	const core = ":package.example.com.shop.core"
	fixed := shopRules + `
		ClassAssertion(:Fixed ` + core + `)
		AnnotationAssertion(:func ` + core + ` "Open(name string) (Store, error)")
		ClassAssertion(:Fixed :core.Memory)
		AnnotationAssertion(:method :core.Memory "Get(id int) (Thing, error)")
	`
	// Len is one method too many, and Open is all the package may offer.
	o := writeModule(t, shopFiles(map[string]string{"core/core.go": invCoreOffers}))
	c := check(t, o, fixed)
	wantFindings(t, c.Findings, "unplanned-method core.Memory")
	if want := "the invariants fix the methods of :core.Memory, and the code adds Len() int"; !strings.Contains(c.Findings[0].Message, want) {
		t.Errorf("message lacks %q:\n%s", want, c.Findings[0].Message)
	}

	grown := writeModule(t, shopFiles(map[string]string{"core/core.go": strings.Replace(invCoreOffers,
		"// Len counts them.\nfunc (*Memory) Len() int { return 0 }\n", "// Close closes it.\nfunc Close() error { return nil }\n", 1)}))
	c = check(t, grown, fixed)
	wantFindings(t, c.Findings, "unplanned-func package.example.com.shop.core")
	if want := "the invariants fix the functions of " + core + ", and the code adds Close() error"; !strings.Contains(c.Findings[0].Message, want) {
		t.Errorf("message lacks %q:\n%s", want, c.Findings[0].Message)
	}

	// Fixed with nothing listed offers nothing: the api exports no function,
	// and the core does.
	wantFindings(t, check(t, o, shopRules+"ClassAssertion(:Fixed :package.example.com.shop.api)").Findings)
	wantFindings(t, check(t, o, shopRules+"ClassAssertion(:Fixed "+core+")").Findings,
		"unplanned-func package.example.com.shop.core")

	// Only what the module has can be fixed.
	wantFindings(t, check(t, o, shopRules+"ClassAssertion(:Fixed :package.os)").Findings, "unsupported-invariant package.os")
}

// A plan is held to what the invariants say the code offers before it is
// built, and may restate what is there in its own words.
func TestReviewInvariantsCapabilities(t *testing.T) {
	t.Setenv("GOWORK", "off")
	before := writeModule(t, shopFiles(map[string]string{"core/core.go": invCoreOffers}))
	ks := known(t, before)
	const core = ":package.example.com.shop.core"
	inv := invariants(t, shopRules+`
		ClassAssertion(:Fixed `+core+`)
		AnnotationAssertion(:func `+core+` "Open(name string) (Store, error)")
		AnnotationAssertion(:method :core.Store "Get(id int) (Thing, error)")
	`)

	same := shopPlan(t, `
		AnnotationAssertion(:func `+core+` "Open(n string) (s Store, err error)")
		AnnotationAssertion(:method :core.Store "Get(i int) (Thing, error)")
	`)
	wantFindings(t, ReviewInvariants(inv, same, before, ks).Findings)

	// A function added to a fixed package, a method added to a fixed
	// interface, and a method of it changed.
	breaks := shopPlan(t, `
		AnnotationAssertion(:func `+core+` "Close() error")
		AnnotationAssertion(:method :core.Store "Get(id string) (Thing, error)")
		AnnotationAssertion(:method :core.Store "Put(t Thing) error")
	`)
	wantFindings(t, ReviewInvariants(inv, breaks, before, ks).Findings,
		"method-mismatch core.Store",
		"unplanned-func package.example.com.shop.core",
		"unplanned-method core.Store",
	)

	// A method the plan writes that is no signature cannot be built, so it
	// takes nothing's place. The review of the plan is what reports it.
	malformed := shopPlan(t, `AnnotationAssertion(:method :core.Store "Get(")`)
	wantFindings(t, ReviewInvariants(inv, malformed, before, ks).Findings)
}

// A plan is checked against the rules about types too: a planned type is read
// as though it were built, with what the plan says about it.
func TestReviewInvariantsAboutTypes(t *testing.T) {
	t.Setenv("GOWORK", "off")
	before := writeModule(t, shopFiles(nil))
	ks := known(t, before)
	inv := invariants(t, shopRules+`
		SubClassOf(Annotation(rdfs:comment "Interfaces belong to the core") :Interface ObjectSomeValuesFrom(:definedIn :Core))
		SubClassOf(Annotation(rdfs:comment "What the core defines says what it is for") ObjectSomeValuesFrom(:definedIn :Core) :Documented)
	`)

	plan := shopPlan(t, `
		Declaration(Class(:api.Handler))
		AnnotationAssertion(:kind :api.Handler "interface")
		AnnotationAssertion(:definedIn :api.Handler :package.example.com.shop.api)
		Declaration(Class(:core.Shelf))
		AnnotationAssertion(:definedIn :core.Shelf :package.example.com.shop.core)
	`)
	wantFindings(t, ReviewInvariants(inv, plan, before, ks).Findings,
		"invariant-violated api.Handler",
		"invariant-violated core.Shelf",
	)

	kept := shopPlan(t, `
		Declaration(Class(:core.Shelf))
		AnnotationAssertion(rdfs:comment :core.Shelf "Shelf is where things are kept.")
		AnnotationAssertion(:definedIn :core.Shelf :package.example.com.shop.core)
	`)
	wantFindings(t, ReviewInvariants(inv, kept, before, ks).Findings)

	// A class in a plan that is not named as a type is, such as a group put in
	// the wrong file, is not taken for a type nobody documented.
	documented := invariants(t, shopRules+`SubClassOf(Annotation(rdfs:comment "Every type says what it is for") :Type :Documented)`)
	stray := shopPlan(t, `
		Declaration(Class(:Sellable))
		ClassAssertion(:Sellable :package.example.com.shop.core)
	`)
	wantFindings(t, ReviewInvariants(documented, stray, before, ks).Findings)
}
