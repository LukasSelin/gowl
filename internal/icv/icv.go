// Package icv checks facts against constraints written as OWL 2 axioms, under
// the closed-world assumption: integrity constraint validation.
//
// A reasoner reads axioms open-world. That a package is not said to import os
// does not mean it does not, so "this package never reaches os" is something it
// can never confirm. A description of code is different: it is complete for
// what it records, and what it does not say is false. Over such a finite model
// an axiom is something to check, the way a model checker checks a property,
// and a failure comes with the individual that breaks it and the edges to
// blame.
//
// # The fragment
//
// It is kept small on purpose, so that the checker is easy to trust, and
// anything outside it is listed in [Report.Unsupported] rather than skipped.
//
// Facts are the data's ClassAssertions of named classes and its
// ObjectPropertyAssertions of named properties, between named individuals. The
// data's Declarations say which terms it defines, even ones it states no fact
// about.
// Names are unique, and the domain is closed: it is every individual the data
// or the constraints name.
//
// Two kinds of property axiom in the constraints derive new facts:
// SubObjectPropertyOf between named properties, and TransitiveObjectProperty.
// They may only derive a property the data asserts nothing about, so a rule
// can define :dependsOn from :imports but cannot add an :imports fact.
//
// Every other logical axiom is a check: SubClassOf, DisjointClasses and
// NegativeObjectPropertyAssertion, over named classes, owl:Thing, owl:Nothing,
// ObjectIntersectionOf, ObjectUnionOf, ObjectComplementOf, ObjectOneOf,
// ObjectSomeValuesFrom, ObjectAllValuesFrom and ObjectHasValue, with a named
// property or its ObjectInverseOf.
//
// The subject side of a SubClassOf, and every operand of a DisjointClasses,
// must be anchored: bounded by the facts rather than by the domain. A named
// class, ObjectOneOf, ObjectHasValue and ObjectSomeValuesFrom are anchored, as
// is an intersection with an anchored operand and a union of anchored ones.
// owl:Thing, ObjectComplementOf and ObjectAllValuesFrom are not: who they
// include depends on which individuals happen to be named somewhere, so a
// check over them could change its answer when an unrelated name is added.
//
// Validate does no I/O and knows nothing about Go.
package icv

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"gowl/owl"
)

// Report is what checking facts against constraints found: what was checked,
// what broke, what could not be checked, and which terms had nothing in them.
type Report struct {
	// Checked counts the constraints evaluated.
	Checked int
	// Violations are ordered by constraint, then by subject.
	Violations []Violation
	// Unsupported lists the axioms outside the fragment, which were not
	// checked. Each has no Subject or Path; its Reason says what is outside.
	Unsupported []Violation
	// Empty lists the terms the checks and rules use that nothing defines: a
	// named class no fact puts anything in, and a property no fact states and
	// no rule derives, unless the data declares it. A check about one holds,
	// or fails, for no individual at all, which usually means a misspelled
	// name. A term the data declares, or a property a rule derives, is
	// defined even when it happens to be empty.
	Empty []owl.Entity
}

// Violation is a constraint the facts break, the individual that breaks it,
// and the edges to blame.
type Violation struct {
	// Axiom is the constraint as written, annotations and all: an
	// rdfs:comment on it is the place to say why the rule exists.
	Axiom owl.Axiom
	// Subject is the counterexample.
	Subject owl.NamedIndividual
	// Path lists the asserted facts that make the violation when an edge is to
	// blame: for a rule that a package never reaches :Effectful through
	// :dependsOn, the chain of :imports from the subject to it. It is always
	// one chain, each fact's object the next one's subject, running the way
	// the facts do, so through an ObjectInverseOf it ends at the subject.
	// Derived facts are traced back to the assertions behind them.
	Path []owl.ObjectPropertyAssertion
	// Reason says what is wrong in words.
	Reason string
}

// rule derives facts: sub's facts are super's too, or, when transitive is
// set, a chain of its facts makes one more.
type rule struct {
	sub, super owl.ObjectProperty
	transitive owl.ObjectProperty
}

// edge is one fact of a property.
type edge struct{ from, to owl.NamedIndividual }

// model is the facts, closed: every relation complete, every individual known.
type model struct {
	render  func(owl.Node) string
	inds    []owl.NamedIndividual // sorted
	members map[owl.Class]map[owl.NamedIndividual]bool
	// proofs holds each property's facts, asserted or derived, with the
	// asserted facts behind each.
	proofs map[owl.ObjectProperty]map[edge][]owl.ObjectPropertyAssertion
	succ   map[owl.ObjectProperty]map[owl.NamedIndividual][]owl.NamedIndividual // sorted
	pred   map[owl.ObjectProperty]map[owl.NamedIndividual][]owl.NamedIndividual // sorted
}

// Validate checks the constraints against the facts in data. See the package
// documentation for what each kind of axiom means.
func Validate(data, constraints *owl.Ontology) Report {
	var r Report
	m := &model{
		render:  constraints.Render,
		members: map[owl.Class]map[owl.NamedIndividual]bool{},
		proofs:  map[owl.ObjectProperty]map[edge][]owl.ObjectPropertyAssertion{},
	}
	named := map[owl.NamedIndividual]bool{}
	declared := map[owl.Entity]bool{}
	for ax := range data.All() {
		switch x := owl.Unwrap(ax).(type) {
		case owl.Declaration:
			declared[x.Entity] = true
		case owl.ClassAssertion:
			c, ok1 := x.Class.(owl.Class)
			i, ok2 := x.Individual.(owl.NamedIndividual)
			if ok1 && ok2 {
				m.add(c, i)
				named[i] = true
			}
		case owl.ObjectPropertyAssertion:
			p, ok1 := x.Property.(owl.ObjectProperty)
			from, ok2 := x.Subject.(owl.NamedIndividual)
			to, ok3 := x.Object.(owl.NamedIndividual)
			if ok1 && ok2 && ok3 {
				fact := owl.ObjectPropertyAssertion{Property: p, Subject: from, Object: to}
				m.relate(p, edge{from, to}, []owl.ObjectPropertyAssertion{fact})
				named[from], named[to] = true, true
			}
		}
	}
	asserted := map[owl.ObjectProperty]bool{}
	for p := range m.proofs {
		asserted[p] = true
	}

	// Sort the constraints into rules and checks, each distinct axiom once and
	// in a fixed order, so that the report does not depend on the order they
	// were written in.
	var rules []rule
	var checks []owl.Axiom
	seen := map[string]bool{}
	var axioms []owl.Axiom
	for ax := range constraints.All() {
		if key := owl.CanonicalKey(ax); !seen[key] {
			seen[key] = true
			axioms = append(axioms, ax)
		}
	}
	sort.SliceStable(axioms, func(i, j int) bool { return owl.CanonicalKey(axioms[i]) < owl.CanonicalKey(axioms[j]) })
	refuse := func(ax owl.Axiom, format string, args ...any) {
		r.Unsupported = append(r.Unsupported, Violation{Axiom: ax, Reason: fmt.Sprintf(format, args...)})
	}
	for _, ax := range axioms {
		switch x := owl.Unwrap(ax).(type) {
		case owl.Declaration, owl.AnnotationAssertion, owl.SubAnnotationPropertyOf,
			owl.AnnotationPropertyDomain, owl.AnnotationPropertyRange:
			// Not logical: nothing to check.
		case owl.SubObjectPropertyOf:
			sub, ok1 := x.Sub.(owl.ObjectProperty)
			super, ok2 := x.Super.(owl.ObjectProperty)
			switch {
			case !ok1 || !ok2:
				refuse(ax, "only a named property can be a sub-property here")
			case asserted[super]:
				refuse(ax, "it would add facts about %s, which the data states; derive a new property instead", m.render(super))
			default:
				rules = append(rules, rule{sub: sub, super: super})
			}
		case owl.TransitiveObjectProperty:
			p, ok := x.Property.(owl.ObjectProperty)
			switch {
			case !ok:
				refuse(ax, "only a named property can be made transitive here")
			case asserted[p]:
				refuse(ax, "it would add facts about %s, which the data states; make a property it is a sub-property of transitive instead", m.render(p))
			default:
				rules = append(rules, rule{transitive: p})
			}
		case owl.SubClassOf, owl.DisjointClasses, owl.NegativeObjectPropertyAssertion:
			if why := m.unsupported(x); why != "" {
				refuse(ax, "%s", why)
				continue
			}
			checks = append(checks, ax)
		case owl.EquivalentClasses:
			refuse(ax, "every axiom here is a check, and this one reads like a definition: name a group's members with ClassAssertion, and check an equivalence as two SubClassOf")
		case owl.ClassAssertion, owl.ObjectPropertyAssertion:
			refuse(ax, "an assertion is a fact, and facts belong in the data")
		default:
			refuse(ax, "outside the fragment icv checks")
		}
	}

	// The closed domain: every individual the facts or the checks name.
	for _, ax := range checks {
		owl.Walk(ax, func(e owl.Entity) {
			if i, ok := e.(owl.NamedIndividual); ok {
				named[i] = true
			}
		})
	}
	for i := range named {
		m.inds = append(m.inds, i)
	}
	slices.Sort(m.inds)

	m.saturate(rules)

	derived := map[owl.ObjectProperty]bool{}
	for _, r := range rules {
		derived[r.super], derived[r.transitive] = true, true
	}
	empty := map[owl.Entity]bool{}
	// A rule from a property nothing states, such as a misspelled :imprts,
	// leaves the property it defines empty too.
	for _, r := range rules {
		if r.sub != "" && len(m.proofs[r.sub]) == 0 && !derived[r.sub] && !declared[r.sub] {
			empty[r.sub] = true
		}
	}
	for _, ax := range checks {
		r.Checked++
		r.Violations = append(r.Violations, m.check(ax)...)
		owl.Walk(owl.Unwrap(ax), func(e owl.Entity) {
			switch x := e.(type) {
			case owl.Class:
				if x != owl.Thing && x != owl.Nothing && len(m.members[x]) == 0 && !declared[x] {
					empty[x] = true
				}
			case owl.ObjectProperty:
				if len(m.proofs[x]) == 0 && !derived[x] && !declared[x] {
					empty[x] = true
				}
			}
		})
	}
	for e := range empty {
		r.Empty = append(r.Empty, e)
	}
	sort.Slice(r.Empty, func(i, j int) bool { return r.Empty[i].IRI() < r.Empty[j].IRI() })
	return r
}

func (m *model) add(c owl.Class, i owl.NamedIndividual) {
	if m.members[c] == nil {
		m.members[c] = map[owl.NamedIndividual]bool{}
	}
	m.members[c][i] = true
}

// relate records a fact of p unless it is already known, and reports whether
// it was new.
func (m *model) relate(p owl.ObjectProperty, e edge, proof []owl.ObjectPropertyAssertion) bool {
	if m.proofs[p] == nil {
		m.proofs[p] = map[edge][]owl.ObjectPropertyAssertion{}
	}
	if _, ok := m.proofs[p][e]; ok {
		return false
	}
	m.proofs[p][e] = proof
	return true
}

// saturate applies the rules until no new fact appears. Each round derives
// only from the facts of earlier rounds, and of the derivations of one fact in
// a round it keeps the one resting on the fewest assertions, so a fact's proof
// is as shallow and as short as the rules allow, and the same on every run.
func (m *model) saturate(rules []rule) {
	for {
		found := map[owl.ObjectProperty]map[edge][]owl.ObjectPropertyAssertion{}
		offer := func(p owl.ObjectProperty, e edge, proof []owl.ObjectPropertyAssertion) {
			if _, known := m.proofs[p][e]; known {
				return
			}
			if found[p] == nil {
				found[p] = map[edge][]owl.ObjectPropertyAssertion{}
			}
			if old, ok := found[p][e]; !ok || len(proof) < len(old) {
				found[p][e] = proof
			}
		}
		m.index()
		for _, r := range rules {
			if r.transitive == "" {
				for _, from := range m.inds {
					for _, to := range m.succ[r.sub][from] {
						offer(r.super, edge{from, to}, m.proofs[r.sub][edge{from, to}])
					}
				}
				continue
			}
			p := r.transitive
			for _, x := range m.inds {
				for _, y := range m.succ[p][x] {
					for _, z := range m.succ[p][y] {
						first, second := m.proofs[p][edge{x, y}], m.proofs[p][edge{y, z}]
						offer(p, edge{x, z}, append(slices.Clip(first), second...))
					}
				}
			}
		}
		if len(found) == 0 {
			return
		}
		for p, es := range found {
			for e, proof := range es {
				m.relate(p, e, proof)
			}
		}
	}
}

// index rebuilds the sorted successor and predecessor lists from the facts.
func (m *model) index() {
	m.succ = map[owl.ObjectProperty]map[owl.NamedIndividual][]owl.NamedIndividual{}
	m.pred = map[owl.ObjectProperty]map[owl.NamedIndividual][]owl.NamedIndividual{}
	for p, es := range m.proofs {
		s, q := map[owl.NamedIndividual][]owl.NamedIndividual{}, map[owl.NamedIndividual][]owl.NamedIndividual{}
		for e := range es {
			s[e.from] = append(s[e.from], e.to)
			q[e.to] = append(q[e.to], e.from)
		}
		for _, l := range s {
			slices.Sort(l)
		}
		for _, l := range q {
			slices.Sort(l)
		}
		m.succ[p], m.pred[p] = s, q
	}
}

// unsupported says why a check is outside the fragment, or returns "".
func (m *model) unsupported(ax owl.Axiom) string {
	switch x := ax.(type) {
	case owl.SubClassOf:
		if why := expressionUnsupported(x.Sub); why != "" {
			return why
		}
		if why := expressionUnsupported(x.Super); why != "" {
			return why
		}
		if !anchored(x.Sub) {
			return fmt.Sprintf("%s is not anchored: who it includes depends on which individuals happen to be named, so check a named class, ObjectOneOf, ObjectHasValue or ObjectSomeValuesFrom instead", m.render(x.Sub))
		}
	case owl.DisjointClasses:
		if len(x) < 2 {
			return "DisjointClasses needs two operands"
		}
		for _, ce := range x {
			if why := expressionUnsupported(ce); why != "" {
				return why
			}
			if !anchored(ce) {
				return fmt.Sprintf("%s is not anchored: who it includes depends on which individuals happen to be named", m.render(ce))
			}
		}
	case owl.NegativeObjectPropertyAssertion:
		_, ok1 := x.Subject.(owl.NamedIndividual)
		_, ok2 := x.Object.(owl.NamedIndividual)
		if !ok1 || !ok2 {
			return "only named individuals can be checked"
		}
		return propertyUnsupported(x.Property)
	}
	return ""
}

func propertyUnsupported(p owl.ObjectPropertyExpression) string {
	switch p.(type) {
	case owl.ObjectProperty, owl.ObjectInverseOf:
		return ""
	}
	return fmt.Sprintf("the property %s is outside the fragment", owl.Functional(p))
}

// expressionUnsupported says why a class expression is outside the fragment,
// or returns "".
func expressionUnsupported(ce owl.ClassExpression) string {
	switch x := ce.(type) {
	case owl.Class:
		return ""
	case owl.ObjectIntersectionOf:
		return firstUnsupported(x)
	case owl.ObjectUnionOf:
		return firstUnsupported(x)
	case owl.ObjectComplementOf:
		return expressionUnsupported(x.Operand)
	case owl.ObjectOneOf:
		for _, i := range x {
			if _, ok := i.(owl.NamedIndividual); !ok {
				return "only named individuals can be enumerated"
			}
		}
		return ""
	case owl.ObjectSomeValuesFrom:
		if why := propertyUnsupported(x.Property); why != "" {
			return why
		}
		return expressionUnsupported(x.Filler)
	case owl.ObjectAllValuesFrom:
		if why := propertyUnsupported(x.Property); why != "" {
			return why
		}
		return expressionUnsupported(x.Filler)
	case owl.ObjectHasValue:
		if _, ok := x.Value.(owl.NamedIndividual); !ok {
			return "only a named individual can be a value"
		}
		return propertyUnsupported(x.Property)
	}
	return fmt.Sprintf("the class expression %s is outside the fragment", owl.Functional(ce))
}

func firstUnsupported(ces []owl.ClassExpression) string {
	for _, ce := range ces {
		if why := expressionUnsupported(ce); why != "" {
			return why
		}
	}
	return ""
}

// anchored reports whether everything in ce is bounded by the facts, rather
// than by which individuals the domain happens to hold.
func anchored(ce owl.ClassExpression) bool {
	switch x := ce.(type) {
	case owl.Class:
		return x != owl.Thing
	case owl.ObjectOneOf, owl.ObjectHasValue, owl.ObjectSomeValuesFrom:
		return true
	case owl.ObjectIntersectionOf:
		return slices.ContainsFunc(x, anchored)
	case owl.ObjectUnionOf:
		return len(x) > 0 && !slices.ContainsFunc(x, func(ce owl.ClassExpression) bool { return !anchored(ce) })
	}
	return false
}

// related lists the individuals x relates to by p, sorted.
func (m *model) related(p owl.ObjectPropertyExpression, x owl.NamedIndividual) []owl.NamedIndividual {
	switch q := p.(type) {
	case owl.ObjectProperty:
		return m.succ[q][x]
	case owl.ObjectInverseOf:
		return m.pred[q.Property][x]
	}
	return nil
}

// proof returns the asserted facts behind p(x, y), in their own direction.
func (m *model) proof(p owl.ObjectPropertyExpression, x, y owl.NamedIndividual) ([]owl.ObjectPropertyAssertion, bool) {
	switch q := p.(type) {
	case owl.ObjectProperty:
		pr, ok := m.proofs[q][edge{x, y}]
		return pr, ok
	case owl.ObjectInverseOf:
		pr, ok := m.proofs[q.Property][edge{y, x}]
		return pr, ok
	}
	return nil, false
}

// has reports whether x is in ce, evaluated over the closed model.
func (m *model) has(ce owl.ClassExpression, x owl.NamedIndividual) bool {
	switch c := ce.(type) {
	case owl.Class:
		switch c {
		case owl.Thing:
			return slices.Contains(m.inds, x)
		case owl.Nothing:
			return false
		}
		return m.members[c][x]
	case owl.ObjectIntersectionOf:
		for _, op := range c {
			if !m.has(op, x) {
				return false
			}
		}
		return true
	case owl.ObjectUnionOf:
		for _, op := range c {
			if m.has(op, x) {
				return true
			}
		}
		return false
	case owl.ObjectComplementOf:
		return !m.has(c.Operand, x)
	case owl.ObjectOneOf:
		return slices.Contains(c, owl.Individual(x))
	case owl.ObjectSomeValuesFrom:
		for _, y := range m.related(c.Property, x) {
			if m.has(c.Filler, y) {
				return true
			}
		}
		return false
	case owl.ObjectAllValuesFrom:
		for _, y := range m.related(c.Property, x) {
			if !m.has(c.Filler, y) {
				return false
			}
		}
		return true
	case owl.ObjectHasValue:
		v, _ := c.Value.(owl.NamedIndividual)
		return slices.Contains(m.related(c.Property, x), v)
	}
	return false
}

// extension lists the members of ce, sorted.
func (m *model) extension(ce owl.ClassExpression) []owl.NamedIndividual {
	var out []owl.NamedIndividual
	for _, x := range m.inds {
		if m.has(ce, x) {
			out = append(out, x)
		}
	}
	return out
}

// check evaluates one check and returns its violations, one per
// counterexample.
func (m *model) check(ax owl.Axiom) []Violation {
	var out []Violation
	switch x := owl.Unwrap(ax).(type) {
	case owl.SubClassOf:
		for _, i := range m.extension(x.Sub) {
			if !m.has(x.Super, i) {
				why, path := m.whyNot(x.Super, i)
				// A rule about individuals it names needs no reminder of who
				// they are.
				if _, named := x.Sub.(owl.ObjectOneOf); !named {
					why = fmt.Sprintf("%s is %s, and %s", m.render(i), m.render(x.Sub), why)
				}
				out = append(out, Violation{Axiom: ax, Subject: i, Path: path, Reason: why})
			}
		}
	case owl.DisjointClasses:
		reported := map[owl.NamedIndividual]bool{}
		for a := 0; a < len(x); a++ {
			for b := a + 1; b < len(x); b++ {
				for _, i := range m.extension(x[a]) {
					if reported[i] || !m.has(x[b], i) {
						continue
					}
					reported[i] = true
					// A path is one chain of facts; the reason gives both.
					whyA, path := m.whyIn(x[a], i)
					whyB, pathB := m.whyIn(x[b], i)
					if len(path) == 0 {
						path = pathB
					}
					out = append(out, Violation{Axiom: ax, Subject: i, Path: path,
						Reason: fmt.Sprintf("%s, and %s", whyA, whyB)})
				}
			}
		}
	case owl.NegativeObjectPropertyAssertion:
		from := x.Subject.(owl.NamedIndividual)
		to := x.Object.(owl.NamedIndividual)
		if path, ok := m.proof(x.Property, from, to); ok {
			out = append(out, Violation{Axiom: ax, Subject: from, Path: path,
				Reason: fmt.Sprintf("%s %s %s%s", m.render(from), m.render(x.Property), m.render(to), m.via(path))})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Subject < out[j].Subject })
	return out
}

// whyIn explains why x is in ce, with the facts that put it there.
func (m *model) whyIn(ce owl.ClassExpression, x owl.NamedIndividual) (string, []owl.ObjectPropertyAssertion) {
	switch c := ce.(type) {
	case owl.ObjectSomeValuesFrom:
		y, path := m.witness(c.Property, x, func(y owl.NamedIndividual) bool { return m.has(c.Filler, y) })
		return fmt.Sprintf("%s %s %s, which is %s%s", m.render(x), m.render(c.Property), m.render(y), m.render(c.Filler), m.via(path)), path
	case owl.ObjectHasValue:
		v := c.Value.(owl.NamedIndividual)
		path, _ := m.proof(c.Property, x, v)
		return fmt.Sprintf("%s %s %s%s", m.render(x), m.render(c.Property), m.render(v), m.via(path)), path
	case owl.ObjectIntersectionOf:
		for _, op := range c {
			if why, path := m.whyIn(op, x); len(path) > 0 {
				return why, path
			}
		}
	case owl.ObjectUnionOf:
		for _, op := range c {
			if m.has(op, x) {
				return m.whyIn(op, x)
			}
		}
	case owl.ObjectComplementOf:
		return m.whyNot(c.Operand, x)
	}
	return fmt.Sprintf("%s is %s", m.render(x), m.render(ce)), nil
}

// whyNot explains why x is not in ce, with the facts that keep it out.
func (m *model) whyNot(ce owl.ClassExpression, x owl.NamedIndividual) (string, []owl.ObjectPropertyAssertion) {
	switch c := ce.(type) {
	case owl.ObjectAllValuesFrom:
		y, path := m.witness(c.Property, x, func(y owl.NamedIndividual) bool { return !m.has(c.Filler, y) })
		return fmt.Sprintf("%s %s %s, which is not %s%s", m.render(x), m.render(c.Property), m.render(y), m.render(c.Filler), m.via(path)), path
	case owl.ObjectIntersectionOf:
		for _, op := range c {
			if !m.has(op, x) {
				return m.whyNot(op, x)
			}
		}
	case owl.ObjectComplementOf:
		return m.whyIn(c.Operand, x)
	case owl.ObjectSomeValuesFrom:
		return fmt.Sprintf("%s has no %s to anything that is %s", m.render(x), m.render(c.Property), m.render(c.Filler)), nil
	case owl.ObjectHasValue:
		return fmt.Sprintf("%s has no %s to %s", m.render(x), m.render(c.Property), m.render(c.Value)), nil
	}
	return fmt.Sprintf("%s is not %s", m.render(x), m.render(ce)), nil
}

// witness finds the individual x relates to by p that satisfies ok and rests
// on the fewest asserted facts; of equals, the first by name.
func (m *model) witness(p owl.ObjectPropertyExpression, x owl.NamedIndividual, ok func(owl.NamedIndividual) bool) (owl.NamedIndividual, []owl.ObjectPropertyAssertion) {
	var best owl.NamedIndividual
	var bestPath []owl.ObjectPropertyAssertion
	found := false
	for _, y := range m.related(p, x) {
		if !ok(y) {
			continue
		}
		path, _ := m.proof(p, x, y)
		if !found || len(path) < len(bestPath) {
			best, bestPath, found = y, path, true
		}
	}
	return best, bestPath
}

// via renders a proof that took more than one fact: " (through a →p b →p c)".
func (m *model) via(path []owl.ObjectPropertyAssertion) string {
	if len(path) < 2 {
		return ""
	}
	var b strings.Builder
	b.WriteString(" (through ")
	b.WriteString(m.render(path[0].Subject))
	for _, f := range path {
		b.WriteString(" →" + m.render(f.Property) + " " + m.render(f.Object))
	}
	b.WriteString(")")
	return b.String()
}
