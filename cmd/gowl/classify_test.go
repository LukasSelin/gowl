package main

import (
	"strings"
	"testing"
)

// pizzaDoc entails Margherita ⊑ CheesyPizza without asserting it, contains one
// unsatisfiable class, and carries one axiom outside EL.
const pizzaDoc = `Prefix(:=<http://example.org/pizza#>)
Ontology(<http://example.org/pizza>
    SubClassOf(:Margherita :Pizza)
    SubClassOf(:Margherita ObjectSomeValuesFrom(:hasTopping :Mozzarella))
    SubClassOf(:Mozzarella :CheeseTopping)
    EquivalentClasses(:CheesyPizza
        ObjectIntersectionOf(:Pizza ObjectSomeValuesFrom(:hasTopping :CheeseTopping)))
    DisjointClasses(:CheeseTopping :VegetableTopping)
    SubClassOf(:Impossible :CheeseTopping)
    SubClassOf(:Impossible :VegetableTopping)
    ObjectPropertyRange(:hasTopping :Topping)
)
`

func TestClassifyText(t *testing.T) {
	path := write(t, "pizza.ofn", pizzaDoc)
	out, code := capture(t, func() int { return run([]string{"classify", path}) })
	if code != exitOK {
		t.Errorf("exit = %d, want %d without -fail-on-incoherent", code, exitOK)
	}

	for _, want := range []string{
		"consistent          true",
		"coherent            false",
		"unsatisfiable classes (1)",
		":Impossible",
		"not classified (1 axiom outside OWL 2 EL)",
		"ObjectPropertyRange(:hasTopping :Topping)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
}

func TestClassifyFailOnIncoherent(t *testing.T) {
	path := write(t, "pizza.ofn", pizzaDoc)
	_, code := capture(t, func() int {
		return run([]string{"classify", "-fail-on-incoherent", path})
	})
	if code != exitFailed {
		t.Errorf("exit = %d, want %d for an incoherent ontology", code, exitFailed)
	}

	// A coherent ontology passes the same check.
	clean := write(t, "clean.ofn", cleanDoc)
	_, code = capture(t, func() int {
		return run([]string{"classify", "-fail-on-incoherent", clean})
	})
	if code != exitOK {
		t.Errorf("exit = %d, want %d for a coherent ontology", code, exitOK)
	}
}

func TestClassifyAxiomsEmitsAParseableOntology(t *testing.T) {
	path := write(t, "pizza.ofn", pizzaDoc)
	out, code := capture(t, func() int { return run([]string{"classify", "-axioms", path}) })
	if code != exitOK {
		t.Fatalf("exit = %d", code)
	}

	// The inference that motivates having a reasoner at all.
	if !strings.Contains(out, "SubClassOf(:Margherita :CheesyPizza)") {
		t.Errorf("inferred taxonomy is missing the derived subsumption:\n%s", out)
	}
	if !strings.Contains(out, "EquivalentClasses(:Impossible owl:Nothing)") {
		t.Errorf("an unsatisfiable class should be reported as owl:Nothing:\n%s", out)
	}

	// The output is an ontology document, so it must round-trip.
	rewritten := write(t, "inferred.ofn", out)
	_, code = capture(t, func() int { return run([]string{"stats", rewritten}) })
	if code != exitOK {
		t.Errorf("the emitted taxonomy does not parse back, exit = %d", code)
	}
}

func TestClassifyExplain(t *testing.T) {
	path := write(t, "pizza.ofn", pizzaDoc)
	out, code := capture(t, func() int {
		return run([]string{"classify", "-explain", ":Margherita :CheesyPizza", path})
	})
	if code != exitOK {
		t.Fatalf("exit = %d", code)
	}

	for _, want := range []string{
		"SubClassOf(:Margherita :Pizza)",
		"SubClassOf(:Mozzarella :CheeseTopping)",
		"EquivalentClasses(:CheesyPizza",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("explanation is missing %q:\n%s", want, out)
		}
	}
	// Axioms that play no part must not be cited.
	if strings.Contains(out, "Impossible") {
		t.Errorf("explanation cites an unrelated axiom:\n%s", out)
	}
}

func TestClassifyExplainNonEntailment(t *testing.T) {
	path := write(t, "pizza.ofn", pizzaDoc)
	out, code := capture(t, func() int {
		return run([]string{"classify", "-explain", ":Pizza :Margherita", path})
	})
	if code != exitOK {
		t.Errorf("exit = %d", code)
	}
	if !strings.Contains(out, "is not subsumed by") {
		t.Errorf("output should say the subsumption does not hold:\n%s", out)
	}
}

func TestClassifyExplainBadArgument(t *testing.T) {
	path := write(t, "pizza.ofn", pizzaDoc)
	for _, arg := range []string{":OnlyOne", ""} {
		_, code := capture(t, func() int {
			return run([]string{"classify", "-explain", arg, path})
		})
		if arg != "" && code != exitProblem {
			t.Errorf("-explain %q: exit = %d, want %d", arg, code, exitProblem)
		}
	}
}

func TestClassifyJSON(t *testing.T) {
	path := write(t, "pizza.ofn", pizzaDoc)
	out, code := capture(t, func() int { return run([]string{"classify", "-json", path}) })
	if code != exitOK {
		t.Fatalf("exit = %d", code)
	}

	doc := decode[classifyDoc](t, out)
	if !doc.Consistent {
		t.Error("consistent = false")
	}
	if doc.Coherent {
		t.Error("coherent = true, but Impossible cannot have instances")
	}
	if len(doc.Unsatisfiable) != 1 || !strings.HasSuffix(doc.Unsatisfiable[0], "#Impossible") {
		t.Errorf("unsatisfiable = %v", doc.Unsatisfiable)
	}
	if len(doc.Unsupported) != 1 {
		t.Errorf("unsupported = %v, want the range axiom", doc.Unsupported)
	}
	if len(doc.Notes) == 0 {
		t.Error("an incomplete classification should carry a note saying so")
	}
	// Detail is opt-in, matching the text mode.
	if len(doc.InferredAxioms) != 0 {
		t.Errorf("inferred_axioms should be empty without -axioms: %v", doc.InferredAxioms)
	}
}

func TestClassifyJSONWithAxioms(t *testing.T) {
	path := write(t, "pizza.ofn", pizzaDoc)
	out, _ := capture(t, func() int { return run([]string{"classify", "-json", "-axioms", path}) })

	doc := decode[classifyDoc](t, out)
	if len(doc.InferredAxioms) != doc.InferredAxiomCount {
		t.Errorf("inferred_axioms has %d entries but the count says %d",
			len(doc.InferredAxioms), doc.InferredAxiomCount)
	}
	var found bool
	for _, ax := range doc.InferredAxioms {
		if strings.Contains(ax, "SubClassOf(:Margherita :CheesyPizza)") {
			found = true
		}
	}
	if !found {
		t.Errorf("inferred_axioms is missing the derived subsumption: %v", doc.InferredAxioms)
	}
}

func TestClassifyExplainJSON(t *testing.T) {
	path := write(t, "pizza.ofn", pizzaDoc)
	out, _ := capture(t, func() int {
		return run([]string{"classify", "-json", "-explain", ":Margherita :CheesyPizza", path})
	})

	doc := decode[explainDoc](t, out)
	if !doc.Entailed {
		t.Error("entailed = false for a subsumption that holds")
	}
	if len(doc.Axioms) != 4 {
		t.Errorf("axioms = %v, want the 4 that participate", doc.Axioms)
	}
	if len(doc.Notes) == 0 {
		t.Error("the justification caveat should be recorded")
	}
}

func TestClassifyJSONErrorIsADocument(t *testing.T) {
	path := write(t, "bad.ofn", "Ontology(Frobnicate(<http://x/A>))")
	out, code := capture(t, func() int { return run([]string{"classify", "-json", path}) })
	if code != exitProblem {
		t.Errorf("exit = %d, want %d", code, exitProblem)
	}
	if doc := decode[errorDoc](t, out); !strings.Contains(doc.Error, "Frobnicate") {
		t.Errorf("error = %q", doc.Error)
	}
}
