package lint

import (
	"fmt"
	"sort"

	"gowl/owl"
)

// Default returns the built-in rules. The set is deliberately conservative:
// every rule here flags something that is wrong or surprising in essentially
// any ontology, so it can be turned on without tuning.
func Default() []Rule {
	return []Rule{
		{
			Name:        "undeclared-entity",
			Description: "every entity used in an axiom should have a Declaration",
			Severity:    Warning,
			Check:       undeclaredEntity,
		},
		{
			Name:        "punned-entity",
			Description: "an IRI used as more than one kind of entity",
			Severity:    Warning,
			Check:       punnedEntity,
		},
		{
			Name:        "missing-label",
			Description: "classes and properties should carry an rdfs:label",
			Severity:    Info,
			Check:       missingLabel,
		},
		{
			Name:        "deprecated-reference",
			Description: "axioms should not reference entities marked owl:deprecated",
			Severity:    Warning,
			Check:       deprecatedReference,
		},
		{
			Name:        "duplicate-axiom",
			Description: "the same axiom asserted more than once",
			Severity:    Warning,
			Check:       duplicateAxiom,
		},
		{
			Name:        "trivial-axiom",
			Description: "axioms that assert nothing, such as SubClassOf(A A)",
			Severity:    Warning,
			Check:       trivialAxiom,
		},
		{
			Name:        "subclass-cycle",
			Description: "a cycle in the asserted class hierarchy makes its classes equivalent",
			Severity:    Error,
			Check:       subclassCycle,
		},
		{
			Name:        "orphan-class",
			Description: "a class with no asserted superclass and no equivalence",
			Severity:    Info,
			Check:       orphanClass,
		},
	}
}

// builtin reports whether an entity comes from the OWL, RDF, RDFS or XSD
// vocabulary, which no ontology is expected to declare.
func builtin(e owl.Entity) bool { return owl.IsBuiltin(e) }

func undeclaredEntity(o *owl.Ontology) []Finding {
	var out []Finding
	for _, e := range o.Signature() {
		if o.IsDeclared(e) || builtin(e) {
			continue
		}
		out = append(out, Finding{
			Subject: e.IRI(),
			Message: fmt.Sprintf("%s %s is used but never declared", e.Kind(), e.IRI()),
		})
	}
	return out
}

func punnedEntity(o *owl.Ontology) []Finding {
	kinds := make(map[owl.IRI][]owl.Kind)
	for _, e := range o.Signature() {
		if builtin(e) {
			continue
		}
		kinds[e.IRI()] = append(kinds[e.IRI()], e.Kind())
	}

	var out []Finding
	for iri, ks := range kinds {
		if len(ks) < 2 {
			continue
		}
		names := make([]string, len(ks))
		for i, k := range ks {
			names[i] = k.String()
		}
		sort.Strings(names)
		out = append(out, Finding{
			Subject: iri,
			Message: fmt.Sprintf("%s is used as %v", iri, names),
		})
	}
	return out
}

func missingLabel(o *owl.Ontology) []Finding {
	var out []Finding
	for _, e := range o.Signature() {
		switch e.Kind() {
		case owl.KindClass, owl.KindObjectProperty, owl.KindDataProperty:
		default:
			continue
		}
		if builtin(e) || o.Label(e) != "" {
			continue
		}
		out = append(out, Finding{
			Subject: e.IRI(),
			Message: fmt.Sprintf("%s %s has no rdfs:label", e.Kind(), e.IRI()),
		})
	}
	return out
}

func deprecatedReference(o *owl.Ontology) []Finding {
	deprecated := make(map[owl.IRI]bool)
	for ax := range o.All() {
		a, ok := owl.Unwrap(ax).(owl.AnnotationAssertion)
		if !ok || a.Property != owl.OWLDeprecated {
			continue
		}
		if lit, ok := a.Value.(owl.Literal); ok && lit.Value == "true" {
			deprecated[a.Subject] = true
		}
	}
	if len(deprecated) == 0 {
		return nil
	}

	var out []Finding
	for ax := range o.All() {
		// A declaration or an annotation *about* a deprecated term is how the
		// term stays documented; only other references are suspect.
		switch inner := owl.Unwrap(ax).(type) {
		case owl.Declaration:
			continue
		case owl.AnnotationAssertion:
			if deprecated[inner.Subject] {
				continue
			}
		}
		owl.Walk(ax, func(e owl.Entity) {
			if deprecated[e.IRI()] {
				out = append(out, Finding{
					Subject: e.IRI(),
					Axiom:   ax,
					Message: fmt.Sprintf("%s is deprecated but still referenced by %s", e.IRI(), o.Render(ax)),
				})
			}
		})
	}
	return out
}

func duplicateAxiom(o *owl.Ontology) []Finding {
	seen := make(map[string]bool)
	var out []Finding
	for ax := range o.All() {
		key := owl.CanonicalKey(ax)
		if seen[key] {
			out = append(out, Finding{
				Axiom:   ax,
				Message: "duplicate axiom " + o.Render(ax),
			})
			continue
		}
		seen[key] = true
	}
	return out
}

func trivialAxiom(o *owl.Ontology) []Finding {
	var out []Finding
	add := func(ax owl.Axiom, why string) {
		out = append(out, Finding{Axiom: ax, Message: why + ": " + o.Render(ax)})
	}
	for ax := range o.All() {
		switch x := owl.Unwrap(ax).(type) {
		case owl.SubClassOf:
			if owl.Equal(x.Sub, x.Super) {
				add(ax, "a class is trivially its own subclass")
			}
		case owl.EquivalentClasses:
			if len(owl.CanonicalAxiom(x).(owl.EquivalentClasses)) < 2 {
				add(ax, "equivalence between fewer than two distinct classes")
			}
		case owl.DisjointClasses:
			if len(owl.CanonicalAxiom(x).(owl.DisjointClasses)) < 2 {
				add(ax, "disjointness between fewer than two distinct classes")
			}
		case owl.SameIndividual:
			if len(owl.CanonicalAxiom(x).(owl.SameIndividual)) < 2 {
				add(ax, "identity between fewer than two distinct individuals")
			}
		}
	}
	return out
}

// subclassCycle finds cycles in the asserted named-class hierarchy. A cycle
// makes every class on it equivalent, which is almost never intended.
func subclassCycle(o *owl.Ontology) []Finding {
	supers := make(map[owl.Class][]owl.Class)
	for ax := range o.All() {
		x, ok := owl.Unwrap(ax).(owl.SubClassOf)
		if !ok {
			continue
		}
		sub, subOK := x.Sub.(owl.Class)
		super, superOK := x.Super.(owl.Class)
		if subOK && superOK && sub != super {
			supers[sub] = append(supers[sub], super)
		}
	}

	const (
		unvisited = 0
		onStack   = 1
		done      = 2
	)
	state := make(map[owl.Class]int)
	var stack []owl.Class
	var out []Finding

	var visit func(owl.Class)
	visit = func(c owl.Class) {
		state[c] = onStack
		stack = append(stack, c)
		for _, super := range supers[c] {
			switch state[super] {
			case unvisited:
				visit(super)
			case onStack:
				// Report the cycle from where it closes.
				start := 0
				for i, v := range stack {
					if v == super {
						start = i
						break
					}
				}
				cycle := append(append([]owl.Class{}, stack[start:]...), super)
				names := make([]string, len(cycle))
				for i, v := range cycle {
					names[i] = o.Render(v)
				}
				out = append(out, Finding{
					Subject: super.IRI(),
					Message: "subclass cycle: " + joinArrows(names),
				})
			}
		}
		stack = stack[:len(stack)-1]
		state[c] = done
	}

	for _, c := range o.Classes() {
		if state[c] == unvisited {
			visit(c)
		}
	}
	return out
}

func joinArrows(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += " -> "
		}
		out += n
	}
	return out
}

func orphanClass(o *owl.Ontology) []Finding {
	// A class is anchored if it has an asserted superclass, an equivalence, or
	// participates in a disjoint union.
	anchored := make(map[owl.Class]bool)
	for ax := range o.All() {
		switch x := owl.Unwrap(ax).(type) {
		case owl.SubClassOf:
			if sub, ok := x.Sub.(owl.Class); ok {
				anchored[sub] = true
			}
		case owl.EquivalentClasses:
			for _, ce := range x {
				if c, ok := ce.(owl.Class); ok {
					anchored[c] = true
				}
			}
		case owl.DisjointUnion:
			anchored[x.Class] = true
			for _, ce := range x.Operands {
				if c, ok := ce.(owl.Class); ok {
					anchored[c] = true
				}
			}
		}
	}

	var out []Finding
	for _, c := range o.Classes() {
		if anchored[c] || c == owl.Thing || c == owl.Nothing || builtin(c) {
			continue
		}
		out = append(out, Finding{
			Subject: c.IRI(),
			Message: fmt.Sprintf("class %s has no asserted superclass or equivalence", c.IRI()),
		})
	}
	return out
}
