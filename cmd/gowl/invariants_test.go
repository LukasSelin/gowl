package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// storeInvariants keeps the store free of I/O and names its one package.
const storeInvariants = `Prefix(:=<https://example.com/store#>)
Prefix(rdfs:=<http://www.w3.org/2000/01/rdf-schema#>)

Ontology(<https://example.com/store/design/invariants>
    ClassAssertion(:Core :package.example.com.store)
    ClassAssertion(:Effectful :package.os)
    SubObjectPropertyOf(:imports :dependsOn)
    TransitiveObjectProperty(:dependsOn)
    DisjointClasses(Annotation(rdfs:comment "The store does no I/O.") :Core ObjectSomeValuesFrom(:dependsOn :Effectful))
)
`

// useOS makes the store read the environment.
func useOS(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "env.go"), []byte("package store\n\nimport \"os\"\n\nvar _ = os.Getenv\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInvariantsWithNoneIsQuiet(t *testing.T) {
	dir := storeRepo(t)
	out, code := capture(t, func() int { return run([]string{"invariants", dir}) })
	if code != exitOK || !strings.Contains(out, "no invariants") {
		t.Errorf("exit = %d, want 0 and a note that there are none\n%s", code, out)
	}
}

func TestInvariantsHoldThenBreak(t *testing.T) {
	dir := storeRepo(t)
	putPlan(t, dir, "invariants.ofn", storeInvariants)

	out, code := capture(t, func() int { return run([]string{"invariants", "-fail-on", "warning", dir}) })
	if code != exitOK {
		t.Errorf("exit = %d, want 0\n%s", code, out)
	}
	for _, want := range []string{"design/invariants.ofn: 1 rule(s)", "no findings"} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q:\n%s", want, out)
		}
	}

	useOS(t, dir)
	out, code = capture(t, func() int { return run([]string{"invariants", dir}) })
	if code != exitFailed {
		t.Errorf("exit = %d, want 1\n%s", code, out)
	}
	want := "error: invariant-violated: The store does no I/O: :package.example.com.store is :Core, and :package.example.com.store :dependsOn :package.os, which is :Effectful"
	if !strings.Contains(out, want) {
		t.Errorf("output should contain %q:\n%s", want, out)
	}
}

func TestInvariantsJSON(t *testing.T) {
	dir := storeRepo(t)
	putPlan(t, dir, "invariants.ofn", storeInvariants)
	useOS(t, dir)
	out, code := capture(t, func() int { return run([]string{"invariants", "-json", dir}) })
	if code != exitFailed {
		t.Fatalf("exit = %d\n%s", code, out)
	}
	var doc struct {
		Invariants string `json:"invariants"`
		Checked    int    `json:"checked"`
		Findings   []struct {
			Rule    string `json:"rule"`
			Subject string `json:"subject"`
			Axiom   string `json:"axiom"`
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if !strings.HasSuffix(doc.Invariants, "design/invariants.ofn") || doc.Checked != 1 || len(doc.Findings) != 1 {
		t.Fatalf("doc = %+v", doc)
	}
	f := doc.Findings[0]
	if f.Rule != "invariant-violated" || f.Subject != "https://example.com/store#package.example.com.store" || !strings.HasPrefix(f.Axiom, "DisjointClasses(") {
		t.Errorf("finding = %+v", f)
	}
}

// A plan that would break an invariant fails before any code exists.
func TestPlanChecksTheInvariants(t *testing.T) {
	dir := storeRepo(t)
	putPlan(t, dir, "invariants.ofn", storeInvariants)
	path := putPlan(t, dir, "env.ofn", `Prefix(:=<https://example.com/store#>)
Ontology(<https://example.com/store/design/env>
    ObjectPropertyAssertion(:imports :package.example.com.store :package.os)
)
`)
	out, code := capture(t, func() int { return run([]string{"plan", "-base", "HEAD", path, dir}) })
	if code != exitFailed {
		t.Errorf("exit = %d, want 1\n%s", code, out)
	}
	for _, want := range []string{"checked against", "design/invariants.ofn: 1 rule(s)", "error: invariant-violated: The store does no I/O"} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q:\n%s", want, out)
		}
	}

	out, code = capture(t, func() int { return run([]string{"plan", "-json", "-base", "HEAD", path, dir}) })
	var doc struct {
		Invariants        string `json:"invariants"`
		InvariantsChecked int    `json:"invariants_checked"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil || code != exitFailed {
		t.Fatalf("exit = %d, err = %v\n%s", code, err, out)
	}
	if !strings.HasSuffix(doc.Invariants, "design/invariants.ofn") || doc.InvariantsChecked != 1 {
		t.Errorf("doc = %+v", doc)
	}
}

// The invariants are not a plan: conform leaves them out of the plans it
// compares with the code, and plan and conform -design refuse them.
func TestInvariantsAreNotAPlan(t *testing.T) {
	dir := storeRepo(t)
	inv := putPlan(t, dir, "invariants.ofn", storeInvariants)
	putPlan(t, dir, "holdings.ofn", holdingsPlan)

	out, code := capture(t, func() int { return run([]string{"conform", dir}) })
	if code != exitOK || !strings.Contains(out, "1 plan(s)") || strings.Contains(out, "plans:     design/holdings.ofn, design/invariants.ofn") {
		t.Errorf("exit = %d, want 0 with holdings as the one plan\n%s", code, out)
	}
	for name, args := range map[string][]string{
		"plan":            {"plan", inv, dir},
		"conform -design": {"conform", "-design", inv, dir},
	} {
		if out, code := capture(t, func() int { return run(args) }); code != exitProblem {
			t.Errorf("%s: exit = %d, want 2\n%s", name, code, out)
		}
	}
}

// A package a plan for the change names is not yet in the code, and the
// invariants may already place it.
func TestInvariantsKnowWhatThePlansName(t *testing.T) {
	dir := storeRepo(t)
	putPlan(t, dir, "invariants.ofn", strings.Replace(storeInvariants,
		"    ClassAssertion(:Effectful :package.os)",
		"    ClassAssertion(:Effectful :package.os)\n    ClassAssertion(:Core :package.example.com.store.audit)", 1))
	putPlan(t, dir, "audit.ofn", `Prefix(:=<https://example.com/store#>)
Ontology(<https://example.com/store/design/audit>
    ClassAssertion(:Package :package.example.com.store.audit)
)
`)
	out, code := capture(t, func() int { return run([]string{"invariants", "-fail-on", "warning", dir}) })
	if code != exitOK || strings.Contains(out, "unknown-package") {
		t.Errorf("exit = %d, want 0 with no unknown package\n%s", code, out)
	}
}

// storeShape adds rules about the store's types, and what the store offers.
const storeShape = `    SubClassOf(Annotation(rdfs:comment "Every type says what it is for") :Type :Documented)
    AnnotationAssertion(:func :package.example.com.store "Count(s *Stock) int")
`

// withShape adds storeShape to the store's invariants.
func withShape(extra string) string {
	return strings.Replace(storeInvariants, "    ClassAssertion(:Effectful :package.os)\n",
		"    ClassAssertion(:Effectful :package.os)\n"+storeShape+extra, 1)
}

// The invariants reach types and what the code offers, and a broken one names
// the type or the package and says what is missing.
func TestInvariantsAboutTypesAndCapabilities(t *testing.T) {
	dir := storeRepo(t)
	putPlan(t, dir, "invariants.ofn", withShape(""))
	if err := os.WriteFile(filepath.Join(dir, "bare.go"), []byte("package store\n\ntype Bare struct{ Of *Item }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := capture(t, func() int { return run([]string{"invariants", dir}) })
	if code != exitFailed {
		t.Errorf("exit = %d, want 1\n%s", code, out)
	}
	for _, want := range []string{
		"design/invariants.ofn: 3 rule(s)",
		"error: invariant-violated: Every type says what it is for: :store.Bare is :Type, and :store.Bare is not :Documented",
		"error: missing-func: in the invariants, :package.example.com.store has the function Count(*store.Stock) int, and the code does not declare it",
		"2 finding(s)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q:\n%s", want, out)
		}
	}
}

// -facts prints what the rules are checked against instead of checking them.
func TestInvariantsFacts(t *testing.T) {
	dir := storeRepo(t)

	// With no invariants yet, the code's facts are still there to read.
	out, code := capture(t, func() int { return run([]string{"invariants", "-facts", dir}) })
	if code != exitOK || !strings.Contains(out, "ClassAssertion(:Type :store.Item)") {
		t.Errorf("exit = %d, want 0 and the code's facts\n%s", code, out)
	}

	putPlan(t, dir, "invariants.ofn", withShape(""))
	useOS(t, dir)
	out, code = capture(t, func() int { return run([]string{"invariants", "-facts", dir}) })
	if code != exitOK {
		t.Errorf("exit = %d, want 0: -facts checks nothing\n%s", code, out)
	}
	for _, want := range []string{
		"Ontology(<https://example.com/store>",
		"ClassAssertion(:Package :package.example.com.store)",
		"ClassAssertion(:Core :package.example.com.store)",
		"ObjectPropertyAssertion(:imports :package.example.com.store :package.os)",
		"ClassAssertion(:Struct :store.Holding)",
		"ObjectPropertyAssertion(:definedIn :store.Holding :package.example.com.store)",
		"ObjectPropertyAssertion(:holds :store.Holding :store.Item)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "invariant-violated") || strings.Contains(out, "finding") {
		t.Errorf("-facts should report no findings:\n%s", out)
	}

	if out, code := capture(t, func() int { return run([]string{"invariants", "-facts", "-json", dir}) }); code != exitProblem {
		t.Errorf("exit = %d, want 2: the facts are an ontology, not a report\n%s", code, out)
	}
}

// A type a plan for the change names is not yet in the code, and a rule may
// already be about it.
func TestInvariantsKnowTheTypesThePlansName(t *testing.T) {
	dir := storeRepo(t)
	rule := "    SubClassOf(ObjectOneOf(:store.Ledger) ObjectSomeValuesFrom(:holds ObjectOneOf(:store.Item)))\n"
	putPlan(t, dir, "invariants.ofn", strings.Replace(storeInvariants,
		"    ClassAssertion(:Effectful :package.os)\n", "    ClassAssertion(:Effectful :package.os)\n"+rule, 1))

	out, code := capture(t, func() int { return run([]string{"invariants", dir}) })
	if code != exitFailed || !strings.Contains(out, "error: unknown-type: the invariants name :store.Ledger, which is no exported type of the module") {
		t.Errorf("exit = %d, want 1 and the type nobody has\n%s", code, out)
	}

	putPlan(t, dir, "ledger.ofn", `Prefix(:=<https://example.com/store#>)
Ontology(<https://example.com/store/design/ledger>
    Declaration(Class(:store.Ledger))
)
`)
	out, _ = capture(t, func() int { return run([]string{"invariants", dir}) })
	if strings.Contains(out, "unknown-type") {
		t.Errorf("a type a plan names is not unknown:\n%s", out)
	}
}

// A plan is checked against the rules about types and against what the
// invariants say the code offers, before any code exists.
func TestPlanChecksShapeAndCapabilities(t *testing.T) {
	dir := storeRepo(t)
	putPlan(t, dir, "invariants.ofn", withShape("    ClassAssertion(:Fixed :package.example.com.store)\n"))
	path := putPlan(t, dir, "ledger.ofn", `Prefix(:=<https://example.com/store#>)
Ontology(<https://example.com/store/design/ledger>
    Declaration(Class(:store.Ledger))
    AnnotationAssertion(:func :package.example.com.store "Count(s *Stock) int")
    AnnotationAssertion(:func :package.example.com.store "Audit(s *Stock) error")
    DisjointClasses(:store.Ledger :store.Stock)
)
`)
	out, code := capture(t, func() int { return run([]string{"plan", "-base", "HEAD", path, dir}) })
	if code != exitFailed {
		t.Errorf("exit = %d, want 1\n%s", code, out)
	}
	for _, want := range []string{
		// The planned type has no comment, and the plan adds a function to a
		// package whose functions are fixed. Count is what the invariants ask
		// for, so planning it mends what the base lacks.
		"error: invariant-violated: Every type says what it is for: :store.Ledger is :Type, and :store.Ledger is not :Documented",
		"error: unplanned-func: the invariants fix the functions of :package.example.com.store, and the code adds Audit(*store.Stock) error",
		"warning: unchecked-axiom: the plan states DisjointClasses(:store.Ledger :store.Stock), which nothing checks",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "missing-func") {
		t.Errorf("the plan adds Count, so it is not missing once the plan is built:\n%s", out)
	}
}
