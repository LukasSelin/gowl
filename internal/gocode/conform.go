package gocode

import (
	"fmt"
	"go/token"
	"slices"
	"strings"

	"gowl/lint"
	"gowl/owl"
)

// The checks [Conform] runs. The missing ones read the plan and look in the
// code; the unplanned ones read what the change added to the code and look in
// the plan. Types and packages are errors in both directions, because each is
// a decision about the module's structure. A field, an import, a function or
// a method of a type that is no interface is a warning when nobody planned it:
// that is often just the plan being a little short.
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
	// MissingMethod reports a method the plan gives a type and the code does
	// not.
	MissingMethod = "missing-method"
	// MethodMismatch reports a method the code has with another signature
	// than the plan says.
	MethodMismatch = "method-mismatch"
	// UnplannedMethod reports a method the change added to a type, or
	// changed, that no plan names. An interface is a contract every
	// implementation depends on, so on one this is an error. On any other
	// type it is a warning, and a method an interface asks of the type needs
	// no plan of its own when the type already satisfied that interface, or
	// the plan says it will.
	UnplannedMethod = "unplanned-method"
	// MalformedMethod reports a method in a plan that is not a signature.
	MalformedMethod = "malformed-method"
	// MissingFunc reports a function the plan gives a package and the code
	// does not.
	MissingFunc = "missing-func"
	// FuncMismatch reports a function the code has with another signature
	// than the plan says.
	FuncMismatch = "func-mismatch"
	// UnplannedFunc reports a function the change added to a package, or
	// changed, that no plan names.
	UnplannedFunc = "unplanned-func"
	// MalformedFunc reports a function in a plan that is not a signature.
	MalformedFunc = "malformed-func"
	// KindMismatch reports a type that is another kind of type than the plan
	// says: a struct where it says interface.
	KindMismatch = "kind-mismatch"
	// MisplacedType reports a type the code defines in another package than
	// the plan says.
	MisplacedType = "misplaced-type"
	// UncheckedAxiom reports something a plan states that nothing reads, which
	// [ReviewPlan] says while it is still a plan: a statement nobody checks
	// reads as if it held.
	UncheckedAxiom = "unchecked-axiom"
	// UndeclaredEntity reports a type or a package a plan describes with an
	// annotation, such as a :method or a :func, that the module does not have
	// and the plan does not declare: a new one left undeclared, or a slip in
	// the name of an old one. It is the linter's rule of the same name, which
	// cannot see the subject of an annotation, and [ReviewPlan] reports it.
	UndeclaredEntity = "undeclared-entity"
)

// Conformance is how a module measures up against a plan: what was checked,
// what could not be, and where the two differ.
type Conformance struct {
	// Checked counts the plan's types, fields, interfaces, methods, functions,
	// packages and imports that were compared with the code.
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

// asBuilt returns the module as [Build] would describe it once the plan is
// built: [Overlay], with what the plan says a type or a package offers written
// the way Build writes a signature, and in place of the signature the base has
// under the same name. A plan writes Get(id int) (Thing, error) where Build
// writes Get(int) (core.Thing, error), and read side by side the two would
// look like two methods, or like one that changed.
func asBuilt(before, plan *owl.Ontology) *owl.Ontology {
	ns := Namespace(before)
	was, want := readFacts(before, ns), readFacts(plan, ns)

	// The plan's signatures as Build would write them, by whose and by name.
	type offer struct {
		of   owl.IRI
		name string
	}
	rewritten := map[offer]string{}
	for typ, written := range want.methods {
		for _, d := range designed(packageName(typ, ns), written, was.methods[typ]) {
			if d.err == nil {
				rewritten[offer{typ.IRI(), methodName(d.sig)}] = d.sig
			}
		}
	}
	for p, written := range want.funcs {
		for _, d := range designed(nameOfPackage(p, was.names), written, was.funcs[p]) {
			if d.err == nil {
				rewritten[offer{p.IRI(), methodName(d.sig)}] = d.sig
			}
		}
	}
	offers := func(ax owl.Axiom) (offer, owl.AnnotationAssertion, bool) {
		x, ok := owl.Unwrap(ax).(owl.AnnotationAssertion)
		lit, isLiteral := x.Value.(owl.Literal)
		if !ok || !isLiteral || (x.Property.IRI() != ns+vocabMethod && x.Property.IRI() != ns+vocabFunc) {
			return offer{}, x, false
		}
		return offer{x.Subject, methodName(lit.Value)}, x, true
	}

	// The base without the signatures the plan replaces, and the plan with its
	// own written Build's way. One the plan writes that is no signature is
	// left out: it cannot be built, and [ReviewPlan] says so.
	kept, restated := owl.New(before.IRI), owl.New(plan.IRI)
	kept.Prefixes = before.Prefixes
	for ax := range before.All() {
		if of, _, ok := offers(ax); ok {
			if _, replaced := rewritten[of]; replaced {
				continue
			}
		}
		kept.Add(ax)
	}
	for ax := range plan.All() {
		if of, x, ok := offers(ax); ok {
			sig, valid := rewritten[of]
			if !valid {
				continue
			}
			x.Value = owl.Str(sig)
			ax = x
		}
		restated.Add(ax)
	}
	return Overlay(kept, restated)
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
//
// A plan can say only what Build writes about code, and anything else it
// states, such as a class expression, a disjointness or a group, is checked
// by nothing. Each such axiom is reported as [UncheckedAxiom], so that it is
// reworded, or moved to the invariants, while it is still a plan. A method or
// a function the plan writes that is not a signature is reported too, rather
// than left for [Conform] to find once the code exists.
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
	ns := Namespace(before)
	for ax := range plan.All() {
		if !planReads(ax, ns) {
			d.Findings = append(d.Findings, lint.Finding{
				Rule: UncheckedAxiom, Severity: lint.Warning, Axiom: ax,
				Message: fmt.Sprintf("the plan states %s, which nothing checks: a plan names types, fields, interfaces, methods, functions, packages and imports, and groups and rules go in the invariants", merged.Render(ax)),
			})
		}
	}
	// An annotation describes a type or a package, and its subject is no part
	// of what the plan's axioms name, so the linter cannot say it is
	// undeclared. A type the plan describes has to be one the module has or
	// the plan declares, and a package likewise.
	was, want := readFacts(before, ns), readFacts(plan, ns)
	undeclared := func(subject owl.Entity, there bool, what, how string) {
		if there || !strings.HasPrefix(string(subject.IRI()), string(ns)) {
			return
		}
		d.Findings = append(d.Findings, lint.Finding{
			Rule: UndeclaredEntity, Severity: lint.Warning, Subject: subject.IRI(),
			Message: fmt.Sprintf("the plan describes %s %s, which the module does not have and the plan does not declare: correct the name, or add %s", what, merged.Render(subject), how),
		})
	}
	described := map[owl.Class]bool{}
	for typ := range want.methods {
		described[typ] = true
	}
	for typ := range want.kinds {
		described[typ] = true
	}
	packages := map[owl.NamedIndividual]bool{}
	for typ, home := range want.homes {
		described[typ] = true
		packages[home] = true
	}
	for p := range want.funcs {
		packages[p] = true
	}
	for typ := range described {
		undeclared(typ, was.classes[typ] || want.classes[typ], "type", "Declaration(Class("+merged.Render(typ)+"))")
	}
	for p := range packages {
		undeclared(p, was.packages[p] || want.named[p], "package", "ClassAssertion(:"+vocabPackage+" "+merged.Render(p)+")")
	}

	malformed := func(kind offered, subject owl.Entity, written []string) {
		for _, w := range written {
			if _, err := normalizeSignature(w, "", nil); err != nil {
				d.Findings = append(d.Findings, lint.Finding{
					Rule: kind.malformed, Severity: lint.Error, Subject: subject.IRI(),
					Message: fmt.Sprintf(malformedFormat, "the plan", merged.Render(subject), kind.what, w, err),
				})
			}
		}
	}
	for typ, written := range want.methods {
		malformed(offeredMethods, typ, written)
	}
	for p, written := range want.funcs {
		malformed(offeredFuncs, p, written)
	}
	SortFindings(d.Findings)
	return d
}

// planReads reports whether ax is something a plan can say and have checked:
// a declaration, an annotation, or one of the axioms Build writes about a
// type, a field, a package or an import. It is the list of what [readFacts]
// and [fieldsOf] read, and [Conform] the plan's superclasses.
func planReads(ax owl.Axiom, ns owl.IRI) bool {
	named := func(ok ...bool) bool { return !slices.Contains(ok, false) }
	switch x := owl.Unwrap(ax).(type) {
	case owl.Declaration:
		return true
	case owl.AnnotationAssertion:
		// What a package is called is the code's to say, and nothing holds
		// the code to a plan's word for it.
		return x.Property.IRI() != ns+vocabName
	case owl.SubClassOf:
		_, sub := x.Sub.(owl.Class)
		_, super := x.Super.(owl.Class)
		return named(sub, super)
	case owl.EquivalentClasses:
		// Two types that satisfy each other: read as a SubClassOf each way.
		for _, ce := range x {
			if _, ok := ce.(owl.Class); !ok {
				return false
			}
		}
		return len(x) > 1
	case owl.ObjectPropertyDomain:
		_, p := x.Property.(owl.ObjectProperty)
		_, c := x.Domain.(owl.Class)
		return named(p, c)
	case owl.ObjectPropertyRange:
		_, p := x.Property.(owl.ObjectProperty)
		_, c := x.Range.(owl.Class)
		return named(p, c)
	case owl.DataPropertyDomain:
		_, p := x.Property.(owl.DataProperty)
		_, c := x.Domain.(owl.Class)
		return named(p, c)
	case owl.DataPropertyRange:
		_, p := x.Property.(owl.DataProperty)
		_, dt := x.Range.(owl.Datatype)
		return named(p, dt)
	case owl.ClassAssertion:
		c, isClass := x.Class.(owl.Class)
		_, i := x.Individual.(owl.NamedIndividual)
		return named(isClass, i) && (c.IRI() == ns+vocabPackage || c.IRI() == ns+vocabExternalPackage)
	case owl.ObjectPropertyAssertion:
		p, isProperty := x.Property.(owl.ObjectProperty)
		_, from := x.Subject.(owl.NamedIndividual)
		_, to := x.Object.(owl.NamedIndividual)
		return named(isProperty, from, to) && p.IRI() == ns+vocabImports
	}
	return false
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
	// named holds every individual the ontology mentions in an axiom. The
	// subject of an annotation is not one: an annotation describes a thing,
	// and does not name it into being.
	named   map[owl.NamedIndividual]bool
	imports map[importEdge]bool
	// methods holds what each type's :method annotations say, as written, and
	// funcs what each package's :func annotations do.
	methods map[owl.Class][]string
	funcs   map[owl.NamedIndividual][]string
	// kinds holds each type's :kind, homes the package its :definedIn names,
	// and names the name each package's :name gives it.
	kinds map[owl.Class]string
	homes map[owl.Class]owl.NamedIndividual
	names map[owl.NamedIndividual]string
	// documented holds the terms that carry an rdfs:comment.
	documented map[owl.IRI]bool
}

// importEdge is one package importing another.
type importEdge struct{ from, to owl.NamedIndividual }

func readFacts(o *owl.Ontology, ns owl.IRI) facts {
	f := facts{
		classes:    map[owl.Class]bool{},
		fields:     fieldsOf(o),
		props:      map[owl.IRI]bool{},
		packages:   map[owl.NamedIndividual]bool{},
		external:   map[owl.NamedIndividual]bool{},
		named:      map[owl.NamedIndividual]bool{},
		imports:    map[importEdge]bool{},
		methods:    map[owl.Class][]string{},
		funcs:      map[owl.NamedIndividual][]string{},
		kinds:      map[owl.Class]string{},
		homes:      map[owl.Class]owl.NamedIndividual{},
		names:      map[owl.NamedIndividual]string{},
		documented: map[owl.IRI]bool{},
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
			lit, isLiteral := x.Value.(owl.Literal)
			switch {
			case x.Property == owl.RDFSComment:
				f.documented[x.Subject] = true
			case isLiteral && x.Property.IRI() == ns+vocabMethod:
				typ := owl.Class(x.Subject)
				f.methods[typ] = append(f.methods[typ], lit.Value)
			case isLiteral && x.Property.IRI() == ns+vocabFunc:
				p := owl.NamedIndividual(x.Subject)
				f.funcs[p] = append(f.funcs[p], lit.Value)
			case isLiteral && x.Property.IRI() == ns+vocabKind:
				f.kinds[owl.Class(x.Subject)] = lit.Value
			case isLiteral && x.Property.IRI() == ns+vocabName:
				f.names[owl.NamedIndividual(x.Subject)] = lit.Value
			case x.Property.IRI() == ns+vocabDefinedIn:
				if home, ok := x.Value.(owl.IRI); ok {
					f.homes[owl.Class(x.Subject)] = owl.NamedIndividual(home)
				}
			}
		}
	}
	return f
}

// isVocabulary reports whether e is one of the words Build talks about code
// with, rather than anything in the code.
func isVocabulary(e owl.Entity, ns owl.IRI) bool {
	switch e.IRI() {
	case ns + vocabPackage, ns + vocabExternalPackage, ns + vocabImports, ns + vocabMethod, ns + vocabDefinedIn,
		ns + vocabFunc, ns + vocabKind, ns + vocabName:
		return true
	}
	return false
}

// isTypeName reports whether c is named the way Build names a type:
// package.Type, in the module's namespace. A class of any other name is one of
// the words Build talks about code with, or something a plan put in the wrong
// file, such as a group.
func isTypeName(c owl.Class, ns owl.IRI) bool {
	local, inModule := strings.CutPrefix(string(c), string(ns))
	pkg, name, ok := strings.Cut(local, ".")
	return inModule && ok && token.IsIdentifier(pkg) && token.IsIdentifier(name)
}

// types lists the classes that stand for Go types, sorted.
func (f facts) types(ns owl.IRI) []owl.Class {
	var out []owl.Class
	for c := range f.classes {
		if isTypeName(c, ns) {
			out = append(out, c)
		}
	}
	slices.Sort(out)
	return out
}

// codeMethods reads the methods of a type, or the functions of a package, as
// the code has them, by name. Build wrote them normalized already.
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

// nameOfPackage is the name the package p declares, as its :name records it.
// A package only a design names has no :name yet, and is taken to be called
// after the last element of its path, as nearly all are.
func nameOfPackage(p owl.NamedIndividual, names map[owl.NamedIndividual]string) string {
	if name, ok := names[p]; ok {
		return name
	}
	local := string(p)
	return local[strings.LastIndexAny(local, ".#/")+1:]
}

// designedSig is one method or function a design lists, normalized for
// comparison with the code.
type designedSig struct {
	written, sig string
	err          error
}

// designed normalizes the signatures a design gives a type or a package whose
// Go package is called pkg. A bare name in one is a type of that package, and
// is qualified, unless the code's own signatures leave that name bare: then it
// is a type parameter of the type a method is on, which a design calls what
// the type's declaration calls it.
func designed(pkg string, written, code []string) []designedSig {
	keep := map[string]bool{}
	for _, s := range code {
		for name := range bareNames(s) {
			keep[name] = true
		}
	}
	out := make([]designedSig, 0, len(written))
	for _, w := range written {
		sig, err := normalizeSignature(w, pkg, keep)
		out = append(out, designedSig{written: w, sig: sig, err: err})
	}
	return out
}

// designedNames lists the names of the signatures a design gives a type or a
// package, leaving out any that is no signature.
func designedNames(written []string) map[string]bool {
	out := map[string]bool{}
	for _, d := range designed("", written, nil) {
		if d.err == nil {
			out[methodName(d.sig)] = true
		}
	}
	return out
}

// offered is one of the two kinds of thing a design lists by signature: the
// methods of a type, or the functions of a package. The checks on both are
// the same, and differ only in what they are called.
type offered struct {
	what                                    string // "method" or "function"
	missing, mismatch, unplanned, malformed string // the rules that report it
}

var (
	offeredMethods = offered{"method", MissingMethod, MethodMismatch, UnplannedMethod, MalformedMethod}
	offeredFuncs   = offered{"function", MissingFunc, FuncMismatch, UnplannedFunc, MalformedFunc}
)

// malformedFormat words the finding about a method or function a design
// writes that is not a signature: where, whose, which kind, as written, why.
const malformedFormat = "in %s, %s has the %s %q, which is not a signature: %v"

// reporter records one finding.
type reporter func(rule string, sev lint.Severity, subject owl.IRI, format string, args ...any)

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
// What the code offers is part of that structure: the functions a package
// exports and the exported methods of its types. A plan names them by
// signature, with :func on the package and :method on the type, and may say
// what kind of type one is with :kind and which package defines it with
// :definedIn. All of it is held to the code.
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
	inModule := func(iri owl.IRI) bool { return strings.HasPrefix(string(iri), string(ns)) }

	// A type or a package the code lacks is reported once, however many
	// things the plan says about it.
	missing := map[owl.IRI]bool{}
	missingType := func(typ owl.Class, format string) {
		if !missing[typ.IRI()] {
			missing[typ.IRI()] = true
			report(MissingType, lint.Error, typ.IRI(), format, name(typ))
		}
	}
	missingPackage := func(p owl.NamedIndividual, format string) {
		if !missing[p.IRI()] {
			missing[p.IRI()] = true
			report(MissingPackage, lint.Error, p.IRI(), format, name(p))
		}
	}

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
		if !inModule(e.IRI()) {
			c.Unchecked = append(c.Unchecked, e.IRI())
			continue
		}
		switch x := e.(type) {
		case owl.Class:
			c.Checked++
			if !now.classes[x] {
				missingType(x, "the plan names type %s, and the code has no such exported type")
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
				missingPackage(x, "the plan names package %s, and the module has no such package")
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
	for typ, written := range want.methods {
		if !inModule(typ.IRI()) {
			continue
		}
		if !now.classes[typ] {
			missingType(typ, "the plan gives methods to type %s, and the code has no such exported type")
			continue
		}
		c.Checked += checkOffered(offeredMethods, typ.IRI(), name(typ), packageName(typ, ns), written, now.methods[typ], false, "the plan", report)
	}
	for p, written := range want.funcs {
		if !inModule(p.IRI()) {
			continue
		}
		if !now.packages[p] {
			missingPackage(p, "the plan gives functions to package %s, and the module has no such package")
			continue
		}
		c.Checked += checkOffered(offeredFuncs, p.IRI(), name(p), nameOfPackage(p, now.names), written, now.funcs[p], false, "the plan", report)
	}
	for typ, kind := range want.kinds {
		if !inModule(typ.IRI()) {
			continue
		}
		if !now.classes[typ] {
			missingType(typ, "the plan gives a kind to type %s, and the code has no such exported type")
			continue
		}
		c.Checked++
		if got := now.kinds[typ]; got != kind {
			report(KindMismatch, lint.Error, typ.IRI(), "the plan gives %s the kind %q, and in the code its kind is %q", name(typ), kind, got)
		}
	}
	for typ, home := range want.homes {
		if !inModule(typ.IRI()) {
			continue
		}
		if !now.classes[typ] {
			missingType(typ, "the plan says which package defines type %s, and the code has no such exported type")
			continue
		}
		c.Checked++
		if got := now.homes[typ]; got != home {
			report(MisplacedType, lint.Error, typ.IRI(), "the plan defines %s in %s, and the code defines it in %s", name(typ), name(home), name(got))
		}
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
	// has reports whether sigs, by name, holds this signature: the same but
	// for what it calls its type parameters.
	has := func(sigs map[string]string, sname, sig string) bool {
		other, ok := sigs[sname]
		return ok && sameSignature(other, sig)
	}
	// The methods an interface asks of a type are planned by planning that the
	// type satisfies it, and were planned long ago where it already did. What
	// such an interface embeds, it asks for too.
	asked := func(typ owl.Class) map[string]string {
		out := map[string]string{}
		for _, iface := range slices.Concat(before.AncestorsOf(typ), plan.SuperClassesOf(typ)) {
			for _, i := range slices.Concat(after.AncestorsOf(iface), []owl.Class{iface}) {
				for mname, sig := range codeMethods(now.methods[i]) {
					out[mname] = sig
				}
			}
		}
		return out
	}
	for typ, sigs := range now.methods {
		if unplannedType(typ) {
			continue // the type is what to report
		}
		old := codeMethods(was.methods[typ])
		planned := designedNames(want.methods[typ])
		// An interface is a contract; what any other type offers is its own.
		sev, implied := lint.Error, map[string]string{}
		if now.kinds[typ] != kindInterface {
			sev, implied = lint.Warning, asked(typ)
		}
		for mname, sig := range codeMethods(sigs) {
			if has(old, mname, sig) || planned[mname] || has(implied, mname, sig) {
				continue
			}
			report(UnplannedMethod, sev, typ.IRI(), "%s now has the method %s, and no plan names it", name(typ), sig)
		}
	}
	unplannedPackage := func(p owl.NamedIndividual) bool { return !was.packages[p] && !want.named[p] }
	for p := range now.packages {
		if unplannedPackage(p) {
			report(UnplannedPackage, lint.Error, p.IRI(), "package %s is new, and no plan names it", name(p))
		}
	}
	for p, sigs := range now.funcs {
		if unplannedPackage(p) {
			continue // the package is what to report
		}
		old := codeMethods(was.funcs[p])
		planned := designedNames(want.funcs[p])
		for fname, sig := range codeMethods(sigs) {
			if has(old, fname, sig) || planned[fname] {
				continue
			}
			report(UnplannedFunc, lint.Warning, p.IRI(), "%s now offers the function %s, and no plan names it", name(p), sig)
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

// checkOffered compares the signatures a design gives a type or a package
// with the ones the code declares, reporting one the code lacks or has with
// another signature, and one the design writes that is not a signature at all.
// With exact set, one the code has and the design does not name is reported
// too: that is how the invariants fix what something offers. It returns how
// many signatures it compared.
//
// subject is the type or the package, shown as its name in messages, and pkg
// the name of the Go package a bare type in a signature belongs to.
func checkOffered(kind offered, subject owl.IRI, shown, pkg string, written, code []string, exact bool, source string, report reporter) int {
	have := codeMethods(code)
	named := map[string]bool{}
	checked := 0
	for _, d := range designed(pkg, written, code) {
		if d.err != nil {
			report(kind.malformed, lint.Error, subject, malformedFormat, source, shown, kind.what, d.written, d.err)
			continue
		}
		checked++
		sname := methodName(d.sig)
		named[sname] = true
		got, ok := have[sname]
		switch {
		case !ok:
			report(kind.missing, lint.Error, subject, "in %s, %s has the %s %s, and the code does not declare it", source, shown, kind.what, d.sig)
		case !sameSignature(got, d.sig):
			report(kind.mismatch, lint.Error, subject, "in %s, %s has the %s %s, and the code has %s", source, shown, kind.what, d.sig, got)
		}
	}
	if exact {
		for sname, sig := range have {
			if !named[sname] {
				report(kind.unplanned, lint.Error, subject, "%s fix the %ss of %s, and the code adds %s", source, kind.what, shown, sig)
			}
		}
	}
	return checked
}
