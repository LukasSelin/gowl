package gocode

import (
	"fmt"
	"strings"

	"gowl/internal/icv"
	"gowl/lint"
	"gowl/owl"
)

// The checks [CheckInvariants] runs, besides the method checks it shares with
// [Conform] for the interfaces whose methods the invariants fix.
const (
	// InvariantViolated reports a rule of the invariants the code breaks, once
	// for each package that breaks it.
	InvariantViolated = "invariant-violated"
	// UnsupportedInvariant reports a rule outside what the checker can check.
	// It is an error because a rule nobody checks reads as if it held.
	UnsupportedInvariant = "unsupported-invariant"
	// UnknownPackage reports a package the invariants name that is neither the
	// module's own, nor of the standard library, nor of a dependency, nor
	// declared an :ExternalPackage: usually a misspelling, which in a rule that
	// forbids a package quietly forbids nothing.
	UnknownPackage = "unknown-package"
	// EmptyTerm reports a group or property the rules use that has nothing in
	// it, so that the rules about it hold, or fail, for no package at all.
	EmptyTerm = "empty-term"
)

// PackageIndividual returns the individual that stands for the package with
// the given import path in the namespace of module, as [Build] names it.
func PackageIndividual(module *owl.Ontology, path string) owl.NamedIndividual {
	return owl.NamedIndividual(Namespace(module) + owl.IRI(packageLocalName(path)))
}

// CheckInvariants checks a module against its invariants: rules about its
// packages that hold on every change, unlike a plan, which is about one.
//
// The code says which packages there are and what each imports, and nothing
// else can: of what [Build] wrote, those facts alone are kept. The invariants
// add facts of their own about words the code does not use, such as
// ClassAssertion(:Library :package.gowl.owl) to put a package in a group, and
// may declare a package the module does not depend on as an :ExternalPackage,
// so that a rule can forbid it. Every other axiom of theirs is a rule, checked
// over those facts as they stand, under the closed-world assumption: see
// [icv.Validate] for which axioms that covers. A :method annotation on an
// interface fixes its methods: the code must have exactly those.
//
// known lists the packages from outside the code that the invariants may name
// without being told the name is unknown: the standard library, the module's
// dependencies, and anything a plan for the change names. A known package that
// the module does not import is still from outside it, and taken to be one.
func CheckInvariants(invariants, module *owl.Ontology, known []owl.NamedIndividual) Conformance {
	ns := Namespace(module)
	code := readFacts(module, ns)
	pkgClass, extClass := owl.Class(ns+vocabPackage), owl.Class(ns+vocabExternalPackage)
	importsProp := owl.ObjectProperty(ns + vocabImports)

	var c Conformance
	report := func(rule string, sev lint.Severity, subject owl.IRI, format string, args ...any) {
		c.Findings = append(c.Findings, lint.Finding{
			Rule: rule, Severity: sev, Subject: subject, Message: fmt.Sprintf(format, args...),
		})
	}
	name := func(n owl.Node) string { return invariants.Render(n) }

	// The words the code owns: whatever Build wrote about it.
	owned := map[owl.IRI]bool{}
	for _, e := range module.Signature() {
		owned[e.IRI()] = true
	}
	for _, v := range []string{vocabPackage, vocabExternalPackage, vocabImports} {
		owned[ns+owl.IRI(v)] = true
	}

	data := owl.New(module.IRI)
	data.Prefixes = invariants.Prefixes
	constraints := owl.New(invariants.IRI)
	constraints.Prefixes = invariants.Prefixes
	// The code's words are defined even where it has nothing to say with
	// them, such as :imports in a module that imports nothing.
	data.Declare(pkgClass, extClass, importsProp)
	for p := range code.packages {
		data.Add(owl.ClassAssertion{Class: pkgClass, Individual: p})
	}
	for p := range code.external {
		data.Add(owl.ClassAssertion{Class: extClass, Individual: p})
	}
	for e := range code.imports {
		data.Add(owl.ObjectPropertyAssertion{Property: importsProp, Subject: e.from, Object: e.to})
	}

	declaredExternal := map[owl.NamedIndividual]bool{}
	methods := map[owl.Class][]string{}
	for ax := range invariants.All() {
		switch x := owl.Unwrap(ax).(type) {
		case owl.ClassAssertion:
			cl, ok1 := x.Class.(owl.Class)
			i, ok2 := x.Individual.(owl.NamedIndividual)
			switch {
			case ok1 && ok2 && cl == extClass:
				declaredExternal[i] = true
				data.Add(x)
			case ok1 && ok2 && !owned[cl.IRI()]:
				data.Add(x)
			case ok1 && owned[cl.IRI()]:
				report(UnsupportedInvariant, lint.Error, "", "not checked: %s: only the code says which packages and types there are", name(ax))
				c.Findings[len(c.Findings)-1].Axiom = ax
			default:
				constraints.Add(ax)
			}
		case owl.ObjectPropertyAssertion:
			p, ok := x.Property.(owl.ObjectProperty)
			if ok && owned[p.IRI()] {
				report(UnsupportedInvariant, lint.Error, "", "not checked: %s: only the code says what imports what; to forbid an import, use NegativeObjectPropertyAssertion", name(ax))
				c.Findings[len(c.Findings)-1].Axiom = ax
				continue
			}
			if ok {
				data.Add(x)
				continue
			}
			constraints.Add(ax)
		case owl.AnnotationAssertion:
			if lit, ok := x.Value.(owl.Literal); ok && x.Property.IRI() == ns+vocabMethod {
				iface := owl.Class(x.Subject)
				methods[iface] = append(methods[iface], lit.Value)
			}
		default:
			constraints.Add(ax)
		}
	}

	// Packages named only by the invariants, or only as something a plan
	// imports, are from outside the module when they are known to exist.
	isKnown := map[owl.NamedIndividual]bool{}
	for _, k := range known {
		isKnown[k] = true
	}
	typed := func(i owl.NamedIndividual) bool { return code.packages[i] || code.external[i] || declaredExternal[i] }
	packagePrefix := string(ns) + "package."
	var untyped []owl.NamedIndividual
	seen := map[owl.NamedIndividual]bool{}
	for _, o := range []*owl.Ontology{invariants, module} {
		for _, i := range o.Individuals() {
			if !seen[i] && strings.HasPrefix(string(i), packagePrefix) && !typed(i) {
				seen[i] = true
				untyped = append(untyped, i)
			}
		}
	}
	for _, i := range untyped {
		if isKnown[i] {
			data.Add(owl.ClassAssertion{Class: extClass, Individual: i})
			continue
		}
		if invariants.IsDeclared(i) || len(invariants.AxiomsReferencing(i)) > 0 {
			report(UnknownPackage, lint.Error, i.IRI(), "the invariants name %s, which is no package of the module, the standard library or a dependency; declare it with ClassAssertion(:%s %s) if it is one the module must never use", name(i), vocabExternalPackage, name(i))
		}
	}

	r := icv.Validate(data, constraints)
	c.Checked = r.Checked
	for _, v := range r.Violations {
		report(InvariantViolated, lint.Error, v.Subject.IRI(), "%s: %s", ruleText(v.Axiom, name), v.Reason)
		c.Findings[len(c.Findings)-1].Axiom = v.Axiom
	}
	for _, u := range r.Unsupported {
		report(UnsupportedInvariant, lint.Error, "", "not checked: %s: %s", name(u.Axiom), u.Reason)
		c.Findings[len(c.Findings)-1].Axiom = u.Axiom
	}
	for _, e := range r.Empty {
		what := "nothing is in it"
		if _, ok := e.(owl.ObjectProperty); ok {
			what = "it relates nothing"
		}
		report(EmptyTerm, lint.Warning, e.IRI(), "the invariants use %s, and %s, so the rules about it hold or fail for no package; is the name right?", name(e), what)
	}

	// Interfaces whose methods the invariants fix.
	for iface, written := range methods {
		if !code.classes[iface] {
			report(MissingType, lint.Error, iface.IRI(), "the invariants fix the methods of %s, and the code has no such exported type", name(iface))
			continue
		}
		c.Checked += checkMethods(iface, written, code.methods[iface], ns, true, "the invariants", name, report)
	}

	SortFindings(c.Findings)
	return c
}

// ReviewInvariants checks a plan against the invariants before any of it is
// built, by reading the module as though the plan were already code. It reports
// only what the plan would break: a finding the module at the base already
// has is not the plan's doing.
func ReviewInvariants(invariants, plan, before *owl.Ontology, known []owl.NamedIndividual) Conformance {
	base := CheckInvariants(invariants, before, known)
	planned := CheckInvariants(invariants, Overlay(before, plan), known)
	had := map[string]bool{}
	for _, f := range base.Findings {
		had[findingKey(f)] = true
	}
	out := Conformance{Checked: planned.Checked}
	for _, f := range planned.Findings {
		if !had[findingKey(f)] {
			out.Findings = append(out.Findings, f)
		}
	}
	return out
}

// findingKey identifies a finding by what it is about rather than by its
// wording, which for a broken rule includes a path that can change without the
// finding changing.
func findingKey(f lint.Finding) string {
	key := f.Rule + "\x00" + string(f.Subject)
	if f.Axiom != nil {
		return key + "\x00" + owl.CanonicalKey(f.Axiom)
	}
	return key + "\x00" + f.Message
}

// ruleText is what a broken rule is called in a finding: the rdfs:comment it
// carries, which says why it exists, or else the rule itself.
func ruleText(ax owl.Axiom, name func(owl.Node) string) string {
	if a, ok := ax.(owl.Annotated); ok {
		for _, an := range a.Annotations {
			if lit, ok := an.Value.(owl.Literal); ok && an.Property == owl.RDFSComment {
				return strings.TrimRight(strings.TrimSpace(lit.Value), ".")
			}
		}
	}
	return name(owl.Unwrap(ax))
}
