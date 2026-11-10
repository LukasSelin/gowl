package gocode

import (
	"fmt"
	"slices"
	"strings"

	"gowl/internal/icv"
	"gowl/lint"
	"gowl/owl"
)

// The checks [CheckInvariants] runs, besides the method and function checks it
// shares with [Conform] for what the invariants say a type or a package
// offers.
const (
	// InvariantViolated reports a rule of the invariants the code breaks, once
	// for each package or type that breaks it.
	InvariantViolated = "invariant-violated"
	// UnsupportedInvariant reports a rule outside what the checker can check.
	// It is an error because a rule nobody checks reads as if it held.
	UnsupportedInvariant = "unsupported-invariant"
	// UnknownPackage reports a package the invariants name that is neither the
	// module's own, nor of the standard library, nor of a dependency, nor
	// declared an :ExternalPackage: usually a misspelling, which in a rule that
	// forbids a package quietly forbids nothing.
	UnknownPackage = "unknown-package"
	// UnknownType reports a type the invariants name that the module does not
	// export. A rule about a misspelled type is a rule about nothing.
	UnknownType = "unknown-type"
	// EmptyTerm reports a group or property the rules use that has nothing in
	// it, so that the rules about it hold, or fail, for nothing at all.
	EmptyTerm = "empty-term"
)

// The words the invariants' rules use about types, beside the ones [Build]
// describes packages with. Only the code states them. Of what Build wrote
// about a type, each is read as a fact about an individual that has the type's
// own name: its :kind puts it in :Struct or :Interface, its comment in
// :Documented, its :definedIn relates it to its package, the interfaces it
// satisfies are what it :implements, and the types its fields hold are what it
// :holds. A package with a comment is :Documented too.
const (
	factType       = "Type"
	factStruct     = "Struct"
	factInterface  = "Interface"
	factDocumented = "Documented"
	factImplements = "implements"
	factHolds      = "holds"
)

// groupFixed is the one group of the invariants' own that the checker reads:
// a package or a type in :Fixed offers exactly the functions or methods the
// invariants give it, and nothing more.
const groupFixed = "Fixed"

// PackageIndividual returns the individual that stands for the package with
// the given import path in the namespace of module, as [Build] names it.
func PackageIndividual(module *owl.Ontology, path string) owl.NamedIndividual {
	return owl.NamedIndividual(Namespace(module) + owl.IRI(packageLocalName(path)))
}

// reading is the invariants and a module's description, sorted into what the
// checks need.
type reading struct {
	ns   owl.IRI
	code facts
	// data holds the facts: the code's, about its packages and its types, and
	// the groups the invariants put those in. constraints holds the rules.
	data, constraints *owl.Ontology
	// types holds the individuals that stand for the code's types.
	types map[owl.NamedIndividual]bool
	// groups lists who the invariants put in each group of their own.
	groups map[owl.IRI][]owl.NamedIndividual
	// methods and funcs hold the signatures the invariants give a type, a
	// package or a group, as written.
	methods, funcs map[owl.IRI][]string
	// findings is what sorting the invariants already found wrong with them.
	findings []lint.Finding
	name     func(owl.Node) string
}

func (r *reading) report(rule string, sev lint.Severity, subject owl.IRI, format string, args ...any) {
	r.findings = append(r.findings, lint.Finding{
		Rule: rule, Severity: sev, Subject: subject, Message: fmt.Sprintf(format, args...),
	})
}

// refuse reports an axiom of the invariants that nothing will check.
func (r *reading) refuse(ax owl.Axiom, format string, args ...any) {
	r.report(UnsupportedInvariant, lint.Error, "", "not checked: %s: %s", r.name(ax), fmt.Sprintf(format, args...))
	r.findings[len(r.findings)-1].Axiom = ax
}

// readInvariants sorts the invariants and the module's description into facts
// and rules. See [CheckInvariants] for which axiom is which.
func readInvariants(invariants, module *owl.Ontology, known []owl.NamedIndividual) *reading {
	ns := Namespace(module)
	r := &reading{
		ns:      ns,
		code:    readFacts(module, ns),
		types:   map[owl.NamedIndividual]bool{},
		groups:  map[owl.IRI][]owl.NamedIndividual{},
		methods: map[owl.IRI][]string{},
		funcs:   map[owl.IRI][]string{},
		name:    func(n owl.Node) string { return invariants.Render(n) },
	}
	code := r.code
	extClass := owl.Class(ns + vocabExternalPackage)
	importsProp := owl.ObjectProperty(ns + vocabImports)

	// The words the code owns: its types and their fields, and the words its
	// facts are stated in. Not everything the description mentions: read as
	// though a plan were built, it mentions what the plan does, and a plan
	// that uses the name of one of the invariants' groups takes nothing from
	// them.
	owned := map[owl.IRI]bool{}
	for _, c := range code.types(ns) {
		owned[c.IRI()] = true
	}
	for p := range code.props {
		owned[p] = true
	}
	for _, v := range []string{
		vocabPackage, vocabExternalPackage, vocabImports, vocabDefinedIn,
		factType, factStruct, factInterface, factDocumented, factImplements, factHolds,
	} {
		owned[ns+owl.IRI(v)] = true
	}

	r.data = owl.New(module.IRI)
	r.data.Prefixes = invariants.Prefixes
	r.constraints = owl.New(invariants.IRI)
	r.constraints.Prefixes = invariants.Prefixes
	r.codeFacts(module)

	declaredExternal := map[owl.NamedIndividual]bool{}
	for ax := range invariants.All() {
		switch x := owl.Unwrap(ax).(type) {
		case owl.ClassAssertion:
			cl, ok1 := x.Class.(owl.Class)
			i, ok2 := x.Individual.(owl.NamedIndividual)
			switch {
			case ok1 && ok2 && cl == extClass:
				declaredExternal[i] = true
				r.data.Add(x)
			case ok1 && ok2 && !owned[cl.IRI()]:
				r.groups[cl.IRI()] = append(r.groups[cl.IRI()], i)
				r.data.Add(x)
			case ok1 && owned[cl.IRI()]:
				r.refuse(ax, "only the code says which packages and types there are, and what each is")
			default:
				r.constraints.Add(ax)
			}
		case owl.ObjectPropertyAssertion:
			p, ok := x.Property.(owl.ObjectProperty)
			switch {
			case ok && p == importsProp:
				r.refuse(ax, "only the code says what imports what; to forbid an import, use NegativeObjectPropertyAssertion")
			case ok && owned[p.IRI()]:
				r.refuse(ax, "only the code states %s; to forbid a fact, use NegativeObjectPropertyAssertion", r.name(p))
			case ok:
				r.data.Add(x)
			default:
				r.constraints.Add(ax)
			}
		case owl.AnnotationAssertion:
			lit, ok := x.Value.(owl.Literal)
			subject := r.name(owl.NamedIndividual(x.Subject))
			switch {
			case ok && x.Property.IRI() == ns+vocabMethod:
				r.methods[x.Subject] = append(r.methods[x.Subject], lit.Value)
			case ok && x.Property.IRI() == ns+vocabFunc:
				r.funcs[x.Subject] = append(r.funcs[x.Subject], lit.Value)
			// The rest of what Build says about a type or a package is a fact
			// here, and a fact is held to with a rule. Written as an
			// annotation it would be checked by nothing.
			case x.Property.IRI() == ns+vocabKind:
				r.refuse(ax, "what kind a type is, is a fact of the code; to hold %s to one, write a rule, such as SubClassOf(ObjectOneOf(%s) :%s)", subject, subject, factInterface)
			case x.Property.IRI() == ns+vocabDefinedIn:
				r.refuse(ax, "where a type is defined is a fact of the code; to hold %s to a package, write a rule, such as SubClassOf(ObjectOneOf(%s) ObjectHasValue(:%s :package...))", subject, subject, vocabDefinedIn)
			case x.Property.IRI() == ns+vocabName:
				r.refuse(ax, "what a package is called is a fact of the code, and no rule is about it")
			}
		default:
			r.constraints.Add(ax)
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
			r.data.Add(owl.ClassAssertion{Class: extClass, Individual: i})
			continue
		}
		if invariants.IsDeclared(i) || len(invariants.AxiomsReferencing(i)) > 0 {
			r.report(UnknownPackage, lint.Error, i.IRI(), "the invariants name %s, which is no package of the module, the standard library or a dependency; declare it with ClassAssertion(:%s %s) if it is one the module must never use", r.name(i), vocabExternalPackage, r.name(i))
		}
	}
	// Whatever else the invariants name is a type, or a slip of the pen.
	for _, i := range invariants.Individuals() {
		if strings.HasPrefix(string(i), packagePrefix) || r.types[i] || isKnown[i] {
			continue
		}
		r.report(UnknownType, lint.Error, i.IRI(), "the invariants name %s, which is no exported type of the module", r.name(i))
	}
	return r
}

// codeFacts states what the code says, as facts: which packages there are and
// what each imports, and which types there are, what each is, where it is
// defined, and what it implements and holds.
func (r *reading) codeFacts(module *owl.Ontology) {
	ns, code, data := r.ns, r.code, r.data
	class := func(local string) owl.Class { return owl.Class(ns + owl.IRI(local)) }
	property := func(local string) owl.ObjectProperty { return owl.ObjectProperty(ns + owl.IRI(local)) }
	pkgClass, extClass, typeClass := class(vocabPackage), class(vocabExternalPackage), class(factType)
	structClass, ifaceClass, documented := class(factStruct), class(factInterface), class(factDocumented)
	imports, definedIn := property(vocabImports), property(vocabDefinedIn)
	implements, holds := property(factImplements), property(factHolds)

	// The code's words are defined even where it has nothing to say with
	// them, such as :imports in a module that imports nothing.
	data.Declare(pkgClass, extClass, typeClass, structClass, ifaceClass, documented, imports, definedIn, implements, holds)

	for _, p := range sortedKeys(code.packages) {
		data.Add(owl.ClassAssertion{Class: pkgClass, Individual: p})
		if code.documented[p.IRI()] {
			data.Add(owl.ClassAssertion{Class: documented, Individual: p})
		}
	}
	for _, p := range sortedKeys(code.external) {
		data.Add(owl.ClassAssertion{Class: extClass, Individual: p})
	}
	for e := range code.imports {
		data.Add(owl.ObjectPropertyAssertion{Property: imports, Subject: e.from, Object: e.to})
	}

	// In the description a type is a class. A rule is about things, so here
	// each type is an individual, under the same name.
	types := code.types(ns)
	for _, c := range types {
		r.types[owl.NamedIndividual(c.IRI())] = true
	}
	for _, c := range types {
		t := owl.NamedIndividual(c.IRI())
		data.Add(owl.ClassAssertion{Class: typeClass, Individual: t})
		switch code.kinds[c] {
		case kindStruct:
			data.Add(owl.ClassAssertion{Class: structClass, Individual: t})
		case kindInterface:
			data.Add(owl.ClassAssertion{Class: ifaceClass, Individual: t})
		}
		if code.documented[c.IRI()] {
			data.Add(owl.ClassAssertion{Class: documented, Individual: t})
		}
		if home, ok := code.homes[c]; ok {
			data.Add(owl.ObjectPropertyAssertion{Property: definedIn, Subject: t, Object: home})
		}
		// Every interface the type satisfies, not only the direct ones Build
		// writes: the facts are closed, so they are stated in full.
		for _, super := range module.AncestorsOf(c) {
			if i := owl.NamedIndividual(super.IRI()); r.types[i] {
				data.Add(owl.ObjectPropertyAssertion{Property: implements, Subject: t, Object: i})
			}
		}
	}
	// Two fields of one type make one fact.
	held := map[importEdge]bool{}
	for _, f := range code.fields {
		from, to := owl.NamedIndividual(f.domain.IRI()), owl.NamedIndividual(f.held.iri)
		if f.held.object && r.types[from] && r.types[to] {
			held[importEdge{from, to}] = true
		}
	}
	for e := range held {
		data.Add(owl.ObjectPropertyAssertion{Property: holds, Subject: e.from, Object: e.to})
	}
}

// sortedKeys lists the keys of a set, sorted.
func sortedKeys[K ~string](set map[K]bool) []K {
	out := make([]K, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Facts returns the facts [CheckInvariants] checks the rules of invariants
// against, as an ontology: what the code says about its packages and its
// types, each type an individual under its own name, and the groups the
// invariants put them in. It is for a person to read when a rule does not say
// what they expected it to.
func Facts(invariants, module *owl.Ontology, known []owl.NamedIndividual) *owl.Ontology {
	data := readInvariants(invariants, module, known).data
	data.Annotate(owl.RDFSComment, owl.Str("The facts the invariants' rules are checked against: what the code says about its packages and types, and the groups the invariants put them in."))
	return data.Sort()
}

// CheckInvariants checks a module against its invariants: rules about its
// packages and types that hold on every change, unlike a plan, which is about
// one.
//
// The code says which packages there are and what each imports, which types
// there are and what kind each is, whether each is documented, where it is
// defined, what it implements and what its fields hold, and nothing else can:
// of what [Build] wrote, those facts alone are kept, each type as an
// individual under its own name. The invariants add facts of their own about
// words the code does not use, such as ClassAssertion(:Library
// :package.gowl.owl) to put a package in a group, and may declare a package
// the module does not depend on as an :ExternalPackage, so that a rule can
// forbid it. Every other logical axiom of theirs is a rule, checked over
// those facts as they stand, under the closed-world assumption: see
// [icv.Validate] for which axioms that covers.
//
// The invariants also say what the code offers, by signature. A :func on a
// package is a function it exports, and a :method on a type a method it has:
// the code must have each, as written, and may have more. On a group, every
// package or type in it must. Two things offer exactly what the invariants
// list and nothing more: an interface they give methods to, because every
// implementation depends on the whole of it, and a package or type they put
// in :Fixed.
//
// known lists the names from outside the code that the invariants may use
// without being told the name is unknown: the packages of the standard
// library and of the module's dependencies, and any package or type a plan
// for the change names. A known package that the module does not import is
// still from outside it, and taken to be one.
func CheckInvariants(invariants, module *owl.Ontology, known []owl.NamedIndividual) Conformance {
	r := readInvariants(invariants, module, known)

	checked := icv.Validate(r.data, r.constraints)
	c := Conformance{Checked: checked.Checked}
	for _, v := range checked.Violations {
		r.report(InvariantViolated, lint.Error, v.Subject.IRI(), "%s: %s", ruleText(v.Axiom, r.name), v.Reason)
		r.findings[len(r.findings)-1].Axiom = v.Axiom
	}
	for _, u := range checked.Unsupported {
		r.refuse(u.Axiom, "%s", u.Reason)
	}
	for _, e := range checked.Empty {
		what := "nothing is in it"
		if _, ok := e.(owl.ObjectProperty); ok {
			what = "it relates nothing"
		}
		r.report(EmptyTerm, lint.Warning, e.IRI(), "the invariants use %s, and %s, so the rules about it hold or fail for nothing; is the name right?", r.name(e), what)
	}

	c.Checked += r.checkOffers()
	c.Findings = SortFindings(r.findings)
	return c
}

// checkOffers holds the code to what the invariants say each type and package
// offers, and returns how many signatures it compared.
func (r *reading) checkOffers() int {
	fixed := map[owl.NamedIndividual]bool{}
	for _, m := range r.groups[r.ns+groupFixed] {
		fixed[m] = true
		if !r.types[m] && !r.code.packages[m] {
			r.report(UnsupportedInvariant, lint.Error, m.IRI(), "not checked: %s is :%s, and is no package or type of the module, so there is nothing to fix", r.name(m), groupFixed)
		}
	}

	checked := 0
	// Methods, of types.
	methods, direct := r.spread(r.methods, offeredMethods, "type", func(i owl.NamedIndividual) bool { return r.types[i] },
		func(subject owl.IRI) {
			r.report(MissingType, lint.Error, subject, "the invariants give methods to %s, which is neither an exported type of the module nor a group the invariants put anything in", r.name(owl.Class(subject)))
		})
	for _, t := range sortedSubjects(methods, fixed, func(i owl.NamedIndividual) bool { return r.types[i] }) {
		c := owl.Class(t.IRI())
		// An interface the invariants give methods to is a contract, and a
		// contract is the whole of it.
		exact := fixed[t] || (direct[t] && r.code.kinds[c] == kindInterface)
		checked += checkOffered(offeredMethods, t.IRI(), r.name(t), packageName(c, r.ns), methods[t], r.code.methods[c], exact, "the invariants", r.report)
	}
	// Functions, of packages.
	funcs, _ := r.spread(r.funcs, offeredFuncs, "package", func(i owl.NamedIndividual) bool { return r.code.packages[i] },
		func(subject owl.IRI) {
			r.report(MissingPackage, lint.Error, subject, "the invariants give functions to %s, which is neither a package of the module nor a group the invariants put anything in", r.name(owl.NamedIndividual(subject)))
		})
	for _, p := range sortedSubjects(funcs, fixed, func(i owl.NamedIndividual) bool { return r.code.packages[i] }) {
		checked += checkOffered(offeredFuncs, p.IRI(), r.name(p), nameOfPackage(p, r.code.names), funcs[p], r.code.funcs[p], fixed[p], "the invariants", r.report)
	}
	return checked
}

// spread hands the signatures the invariants give each subject to the types,
// or the packages, they are about: the subject itself when it is one, or else
// every member of the group it names. It returns each one's signatures,
// sorted and without repeats, and which were given theirs by name.
func (r *reading) spread(given map[owl.IRI][]string, kind offered, noun string, is func(owl.NamedIndividual) bool, neither func(owl.IRI)) (map[owl.NamedIndividual][]string, map[owl.NamedIndividual]bool) {
	out, direct := map[owl.NamedIndividual][]string{}, map[owl.NamedIndividual]bool{}
	for subject, sigs := range given {
		self := owl.NamedIndividual(subject)
		switch members := r.groups[subject]; {
		case is(self):
			out[self] = append(out[self], sigs...)
			direct[self] = true
		case len(members) > 0:
			for _, m := range members {
				if !is(m) {
					r.report(UnsupportedInvariant, lint.Error, m.IRI(), "not checked: the invariants give %ss to the group %s, and %s in it is no %s of the module", kind.what, r.name(owl.Class(subject)), r.name(m), noun)
					continue
				}
				out[m] = append(out[m], sigs...)
			}
		default:
			neither(subject)
		}
	}
	for i, sigs := range out {
		slices.Sort(sigs)
		out[i] = slices.Compact(sigs)
	}
	return out, direct
}

// sortedSubjects lists everything that has signatures to check: whoever was
// given some, and whoever is fixed, which with none listed offers nothing.
func sortedSubjects(given map[owl.NamedIndividual][]string, fixed map[owl.NamedIndividual]bool, is func(owl.NamedIndividual) bool) []owl.NamedIndividual {
	all := map[owl.NamedIndividual]bool{}
	for i := range given {
		all[i] = true
	}
	for i := range fixed {
		if is(i) {
			all[i] = true
		}
	}
	return sortedKeys(all)
}

// ReviewInvariants checks a plan against the invariants before any of it is
// built, by reading the module as though the plan were already code. It reports
// only what the plan would break: a finding the module at the base already
// has is not the plan's doing.
func ReviewInvariants(invariants, plan, before *owl.Ontology, known []owl.NamedIndividual) Conformance {
	base := CheckInvariants(invariants, before, known)
	planned := CheckInvariants(invariants, asBuilt(before, plan), known)
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
