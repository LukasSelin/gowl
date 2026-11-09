package gocode

import (
	"fmt"
	"slices"
	"strings"

	"gowl/lint"
	"gowl/owl"
)

// The checks [Conform] runs. The missing ones read the plan and look in the
// code; the unplanned ones read what the change added to the code and look in
// the plan. Types and packages are errors in both directions, because each is
// a decision about the module's structure. A field or an import nobody planned
// is a warning: it is often just the plan being a little short.
const (
	// MissingType reports a type the plan names and the code does not have.
	MissingType = "missing-type"
	// MissingField reports a field the plan names and Build does not record.
	MissingField = "missing-field"
	// FieldMismatch reports a field the code has with another type, or on
	// another struct, than the plan says.
	FieldMismatch = "field-mismatch"
	// MissingInterface reports a type that does not satisfy an interface the
	// plan says it satisfies.
	MissingInterface = "missing-interface"
	// MissingPackage reports a package the plan names and the module lacks.
	MissingPackage = "missing-package"
	// MissingImport reports an import between packages the plan names and the
	// code does not have.
	MissingImport = "missing-import"
	// UnplannedType reports a type the change added that no plan names.
	UnplannedType = "unplanned-type"
	// UnplannedField reports a field the change added or retyped that no plan
	// names, on a type that is itself accounted for.
	UnplannedField = "unplanned-field"
	// UnplannedPackage reports a package the change added that no plan names.
	UnplannedPackage = "unplanned-package"
	// UnplannedImport reports an import the change added that no plan names.
	UnplannedImport = "unplanned-import"
	// MissingMethod reports a method the plan gives an interface and the code
	// does not.
	MissingMethod = "missing-method"
	// MethodMismatch reports a method the code has with another signature
	// than the plan says.
	MethodMismatch = "method-mismatch"
	// UnplannedMethod reports a method the change added to an interface, or
	// changed, that no plan names. An interface is a contract every
	// implementation depends on, so this is an error.
	UnplannedMethod = "unplanned-method"
	// MalformedMethod reports a method in a plan that is not a signature.
	MalformedMethod = "malformed-method"
)

// Conformance is how a module measures up against a plan: what was checked,
// what could not be, and where the two differ.
type Conformance struct {
	// Checked counts the plan's types, fields, interfaces, packages and
	// imports that were compared with the code.
	Checked int
	// Unchecked lists the terms the plan names outside the module's namespace,
	// which the code cannot say anything about.
	Unchecked []owl.IRI
	// Findings are ordered as [lint.Run] orders them.
	Findings []lint.Finding
}

// Namespace returns the namespace [Build] writes a module's terms in. A plan
// has to use it for its names to mean anything to [Conform].
func Namespace(module *owl.Ontology) owl.IRI { return module.IRI + "#" }

// Overlay returns a new ontology holding base's axioms and those of each plan,
// each distinct axiom once, under base's name and prefixes. A plan usually
// repeats a declaration of a type that already exists, and that repetition is
// not a mistake worth reporting.
func Overlay(base *owl.Ontology, plans ...*owl.Ontology) *owl.Ontology {
	out := owl.New(base.IRI)
	out.Prefixes = base.Prefixes
	seen := map[string]bool{}
	for _, o := range append([]*owl.Ontology{base}, plans...) {
		for ax := range o.All() {
			if key := owl.CanonicalKey(ax); !seen[key] {
				seen[key] = true
				out.Add(ax)
			}
		}
	}
	return out
}

// ReviewPlan checks a plan before any of it is built. It reads the module as
// though the plan were already code, and reports what [Compare] would say about
// that change, plus what the plan adds to the linter's findings. So a planned
// type that repeats an existing one is caught while it is still one line in a
// plan, and a term the plan uses but never declares, usually a typo, shows up
// as undeclared-entity.
//
// orphan-class and missing-label are left out. Types in Go rarely have a
// superclass, which new-orphan judges better, and labels come from Build once
// the code exists.
func ReviewPlan(plan, before *owl.Ontology) Delta {
	merged := Overlay(before, plan)
	d := Compare(before, merged)

	rules, _ := lint.Select(lint.Default(), []string{"orphan-class", "missing-label"})
	known := map[string]bool{}
	for _, f := range lint.Run(before, rules) {
		known[f.String()] = true
	}
	for _, f := range lint.Run(merged, rules) {
		if !known[f.String()] {
			d.Findings = append(d.Findings, f)
		}
	}
	SortFindings(d.Findings)
	return d
}

// facts is what an ontology says about a module, read the way Build writes it.
type facts struct {
	classes map[owl.Class]bool
	fields  map[owl.IRI]fieldInfo
	props   map[owl.IRI]bool
	// packages are the module's own; external are the packages from outside
	// it that the module imports, or that a design says are from outside.
	packages map[owl.NamedIndividual]bool
	external map[owl.NamedIndividual]bool
	// named holds every individual the ontology mentions.
	named   map[owl.NamedIndividual]bool
	imports map[importEdge]bool
	// methods holds what each interface's :method annotations say, as written.
	methods map[owl.Class][]string
}

// importEdge is one package importing another.
type importEdge struct{ from, to owl.NamedIndividual }

func readFacts(o *owl.Ontology, ns owl.IRI) facts {
	f := facts{
		classes:  map[owl.Class]bool{},
		fields:   fieldsOf(o),
		props:    map[owl.IRI]bool{},
		packages: map[owl.NamedIndividual]bool{},
		external: map[owl.NamedIndividual]bool{},
		named:    map[owl.NamedIndividual]bool{},
		imports:  map[importEdge]bool{},
		methods:  map[owl.Class][]string{},
	}
	for _, c := range o.Classes() {
		f.classes[c] = true
	}
	for _, p := range o.ObjectProperties() {
		f.props[p.IRI()] = true
	}
	for _, p := range o.DataProperties() {
		f.props[p.IRI()] = true
	}
	for _, i := range o.Individuals() {
		f.named[i] = true
	}
	for ax := range o.All() {
		switch x := owl.Unwrap(ax).(type) {
		case owl.ClassAssertion:
			c, ok1 := x.Class.(owl.Class)
			i, ok2 := x.Individual.(owl.NamedIndividual)
			switch {
			case !ok1 || !ok2:
			case c.IRI() == ns+vocabPackage:
				f.packages[i] = true
			case c.IRI() == ns+vocabExternalPackage:
				f.external[i] = true
			}
		case owl.ObjectPropertyAssertion:
			p, ok1 := x.Property.(owl.ObjectProperty)
			from, ok2 := x.Subject.(owl.NamedIndividual)
			to, ok3 := x.Object.(owl.NamedIndividual)
			if ok1 && ok2 && ok3 && p.IRI() == ns+vocabImports {
				f.imports[importEdge{from, to}] = true
			}
		case owl.AnnotationAssertion:
			if lit, ok := x.Value.(owl.Literal); ok && x.Property.IRI() == ns+vocabMethod {
				iface := owl.Class(x.Subject)
				f.methods[iface] = append(f.methods[iface], lit.Value)
			}
		}
	}
	return f
}

// isVocabulary reports whether e is one of the words Build talks about code
// with, rather than anything in the code.
func isVocabulary(e owl.Entity, ns owl.IRI) bool {
	switch e.IRI() {
	case ns + vocabPackage, ns + vocabExternalPackage, ns + vocabImports, ns + vocabMethod, ns + vocabDefinedIn:
		return true
	}
	return false
}

// codeMethods reads an interface's methods as the code has them, by name. Build
// wrote them normalized already.
func codeMethods(sigs []string) map[string]string {
	out := map[string]string{}
	for _, s := range sigs {
		out[methodName(s)] = s
	}
	return out
}

// packageName is the Go package name in a class gocode wrote: store for
// :store.Item.
func packageName(c owl.Class, ns owl.IRI) string {
	local := strings.TrimPrefix(string(c.IRI()), string(ns))
	name, _, _ := strings.Cut(local, ".")
	return name
}

// designedMethod is one method a design gives an interface, normalized for
// comparison with the code.
type designedMethod struct {
	written, sig string
	err          error
}

// designedMethods normalizes the methods a design gives iface, reading bare
// names the way the code's own signatures use them.
func designedMethods(iface owl.Class, written []string, code []string, ns owl.IRI) []designedMethod {
	keep := map[string]bool{}
	for _, s := range code {
		for name := range bareNames(s) {
			keep[name] = true
		}
	}
	out := make([]designedMethod, 0, len(written))
	for _, w := range written {
		sig, err := normalizeSignature(w, packageName(iface, ns), keep)
		out = append(out, designedMethod{written: w, sig: sig, err: err})
	}
	return out
}

// Conform checks a module against the plan for a change, given the ontologies
// [Build] produced before and after the change. It looks both ways: at what the
// plan promised and the code does not have, and at what the change added and
// the plan does not mention. Anything that was there before the change is left
// alone unless the plan names it, so a plan only has to cover the change.
//
// A plan is about the module's own structure. A planned import of a package
// from outside the module is checked, but the change's other imports from
// outside it are not reported: what the module may depend on is for the
// invariants to say (see [CheckInvariants]).
//
// Removing something is not reported. A plan can say what should exist, and has
// no way to say what should not.
func Conform(plan, before, after *owl.Ontology) Conformance {
	ns := Namespace(after)
	want, was, now := readFacts(plan, ns), readFacts(before, ns), readFacts(after, ns)

	var c Conformance
	report := func(rule string, sev lint.Severity, subject owl.IRI, format string, args ...any) {
		c.Findings = append(c.Findings, lint.Finding{
			Rule: rule, Severity: sev, Subject: subject, Message: fmt.Sprintf(format, args...),
		})
	}
	name := func(n owl.Node) string { return after.Render(n) }

	// A plan names a package of the module by typing it :Package, by giving it
	// imports, or by naming it at all; a package it names only as something
	// imported, and the code does not have, is taken to be from outside.
	targets, sources := map[owl.NamedIndividual]bool{}, map[owl.NamedIndividual]bool{}
	for edge := range want.imports {
		sources[edge.from] = true
		targets[edge.to] = true
	}
	plannedPackage := func(x owl.NamedIndividual) bool {
		switch {
		case want.packages[x] || now.packages[x]:
			return true
		case want.external[x] || now.external[x]:
			return false
		}
		return !targets[x] || sources[x]
	}

	// What the plan promised.
	for _, e := range plan.Signature() {
		switch e.(type) {
		case owl.Class, owl.ObjectProperty, owl.DataProperty, owl.NamedIndividual:
		default:
			continue // annotation properties and datatypes describe nothing in the code
		}
		// :Package, :imports and the rest are the words Build talks about code
		// with, not anything in the code.
		if owl.IsBuiltin(e) || isVocabulary(e, ns) {
			continue
		}
		if !strings.HasPrefix(string(e.IRI()), string(ns)) {
			c.Unchecked = append(c.Unchecked, e.IRI())
			continue
		}
		switch x := e.(type) {
		case owl.Class:
			c.Checked++
			if !now.classes[x] {
				report(MissingType, lint.Error, x.IRI(), "the plan names type %s, and the code has no such exported type", name(x))
			}
			for _, super := range plan.SuperClassesOf(x) {
				if !now.classes[x] || !now.classes[super] {
					continue // the missing type is what to report
				}
				c.Checked++
				if !slices.Contains(after.AncestorsOf(x), super) {
					report(MissingInterface, lint.Error, x.IRI(), "the plan says %s satisfies %s, and it does not", name(x), name(super))
				}
			}
		case owl.ObjectProperty, owl.DataProperty:
			c.Checked++
			planned := want.fields[e.IRI()]
			got, ok := now.fields[e.IRI()]
			switch {
			case !ok && planned.domain != "" && !now.classes[planned.domain]:
				// The struct is missing; that finding covers its fields.
			case !ok:
				report(MissingField, lint.Error, e.IRI(), "the plan names field %s, and the code does not have it as an exported field of a module or basic type", name(e))
			case planned.domain != "" && planned.domain != got.domain:
				report(FieldMismatch, lint.Error, e.IRI(), "the plan puts field %s on %s, and the code has it on %s", name(e), name(planned.domain), name(got.domain))
			case planned.held.iri != "" && planned.held != got.held:
				report(FieldMismatch, lint.Error, e.IRI(), "the plan says field %s holds %s, and the code has %s", name(e), renderSlot(after, planned.held), renderSlot(after, got.held))
			}
		case owl.NamedIndividual:
			if !plannedPackage(x) {
				continue // from outside the module; its import is what the plan says about it
			}
			c.Checked++
			if !now.packages[x] {
				report(MissingPackage, lint.Error, x.IRI(), "the plan names package %s, and the module has no such package", name(x))
			}
		}
	}
	for edge := range want.imports {
		if !now.packages[edge.from] || (plannedPackage(edge.to) && !now.packages[edge.to]) {
			continue // the missing package is what to report
		}
		c.Checked++
		if !now.imports[edge] {
			report(MissingImport, lint.Error, edge.from.IRI(), "the plan says %s imports %s, and it does not", name(edge.from), name(edge.to))
		}
	}
	for iface, written := range want.methods {
		if !strings.HasPrefix(string(iface), string(ns)) {
			continue
		}
		if !now.classes[iface] {
			if !want.classes[iface] { // otherwise the missing type is reported above
				report(MissingType, lint.Error, iface.IRI(), "the plan gives methods to type %s, and the code has no such exported type", name(iface))
			}
			continue
		}
		c.Checked += checkMethods(iface, written, now.methods[iface], ns, false, "the plan", name, report)
	}

	// What the change added that nobody planned.
	unplannedType := func(cl owl.Class) bool { return !was.classes[cl] && !want.classes[cl] }
	for cl := range now.classes {
		if unplannedType(cl) {
			report(UnplannedType, lint.Error, cl.IRI(), "type %s is new, and no plan names it", name(cl))
		}
	}
	for iri, f := range now.fields {
		if old, ok := was.fields[iri]; ok && old == f {
			continue
		}
		if !want.props[iri] && !unplannedType(f.domain) {
			report(UnplannedField, lint.Warning, iri, "field %s is new or retyped, and no plan names it", after.Render(owl.DataProperty(iri)))
		}
	}
	for iface, sigs := range now.methods {
		if unplannedType(iface) {
			continue // the type is what to report
		}
		old := codeMethods(was.methods[iface])
		planned := map[string]bool{}
		for _, m := range designedMethods(iface, want.methods[iface], sigs, ns) {
			if m.err == nil {
				planned[methodName(m.sig)] = true
			}
		}
		for mname, sig := range codeMethods(sigs) {
			if old[mname] == sig || planned[mname] {
				continue
			}
			report(UnplannedMethod, lint.Error, iface.IRI(), "%s now has the method %s, and no plan names it", name(iface), sig)
		}
	}
	unplannedPackage := func(p owl.NamedIndividual) bool { return !was.packages[p] && !want.named[p] }
	for p := range now.packages {
		if unplannedPackage(p) {
			report(UnplannedPackage, lint.Error, p.IRI(), "package %s is new, and no plan names it", name(p))
		}
	}
	for edge := range now.imports {
		if !now.packages[edge.to] {
			continue // an import from outside the module: the invariants' business
		}
		if !was.imports[edge] && !want.imports[edge] && !unplannedPackage(edge.from) {
			report(UnplannedImport, lint.Warning, edge.from.IRI(), "%s now imports %s, and no plan says so", name(edge.from), name(edge.to))
		}
	}

	slices.Sort(c.Unchecked)
	SortFindings(c.Findings)
	return c
}

// checkMethods compares the methods a design gives an interface with the ones
// the code declares, reporting a method the code lacks or has with another
// signature, and one the design writes that is not a signature at all. With
// exact set, a method the code has and the design does not name is reported
// too: that is how the invariants pin a contract. It returns how many methods
// it compared.
func checkMethods(iface owl.Class, written, code []string, ns owl.IRI, exact bool, source string,
	name func(owl.Node) string, report func(rule string, sev lint.Severity, subject owl.IRI, format string, args ...any)) int {
	have := codeMethods(code)
	named := map[string]bool{}
	checked := 0
	for _, m := range designedMethods(iface, written, code, ns) {
		if m.err != nil {
			report(MalformedMethod, lint.Error, iface.IRI(), "in %s, %s has the method %q, which is not a signature: %v", source, name(iface), m.written, m.err)
			continue
		}
		checked++
		mname := methodName(m.sig)
		named[mname] = true
		got, ok := have[mname]
		switch {
		case !ok:
			report(MissingMethod, lint.Error, iface.IRI(), "in %s, %s has the method %s, and the code does not declare it", source, name(iface), m.sig)
		case got != m.sig:
			report(MethodMismatch, lint.Error, iface.IRI(), "in %s, %s has the method %s, and the code has %s", source, name(iface), m.sig, got)
		}
	}
	if exact {
		for mname, sig := range have {
			if !named[mname] {
				report(UnplannedMethod, lint.Error, iface.IRI(), "%s fix the methods of %s, and the code adds %s", source, name(iface), sig)
			}
		}
	}
	return checked
}
