package gocode

import (
	"fmt"
	"sort"
	"strings"

	"gowl/lint"
	"gowl/owl"
)

// The checks [Compare] runs. Both look only at what a change did. A module's
// existing look-alikes stay quiet: gowl's own OWL types share field shapes
// because the OWL specification does, and repeating that on every run would
// bury the one look-alike a change has just introduced.
const (
	// SameShapeType reports a type the change added or reshaped whose
	// fields, taken by type rather than by name, match another type's: the
	// same fields, every field of the other, or a subset of the other's.
	SameShapeType = "same-shape-type"
	// NewOrphan reports a type the change added that connects to no type
	// that existed before, through an interface it satisfies, a type that
	// satisfies it, or a field in either direction.
	NewOrphan = "new-orphan"
)

// Delta is what a change did to a module's types, as [Compare] sees it.
type Delta struct {
	// Added are the types the change introduced.
	Added []owl.Class
	// Changed are the types that existed before and whose fields changed.
	Changed []owl.Class
	// Findings are ordered as [lint.Run] orders them: most severe first,
	// then by rule, then by subject.
	Findings []lint.Finding
}

// Compare checks a change to a module against the module as it was, given the
// ontologies [Build] produced before and after the change. It reports the two
// ways a change grows a module beside its existing types rather than through
// them: a type whose fields repeat another type's ([SameShapeType]), and a new
// type that connects to nothing that was there ([NewOrphan]).
//
// Fields are compared by type and not by name, because a copy is usually
// renamed. Only fields of the module's own types and of basic types are
// visible, since those are the fields Build records, and a match has to share
// at least two fields, one of them of a module type: two types that each hold
// a string and a bool have nothing worth sharing.
func Compare(before, after *owl.Ontology) Delta {
	was, now := shapes(before), shapes(after)
	existed := map[owl.Class]bool{}
	for _, c := range before.Classes() {
		existed[c] = true
	}

	classes := after.Classes()
	var d Delta
	for _, c := range classes {
		switch {
		case !existed[c]:
			d.Added = append(d.Added, c)
		case !equalShapes(was[c], now[c]):
			d.Changed = append(d.Changed, c)
		}
	}

	for _, c := range append(append([]owl.Class{}, d.Added...), d.Changed...) {
		if f, ok := sameShape(after, c, classes, was, now, existed); ok {
			d.Findings = append(d.Findings, f)
		}
	}
	d.Findings = append(d.Findings, newOrphans(after, d.Added, now, existed)...)
	sortFindings(d.Findings)
	return d
}

// sortFindings orders findings as [lint.Run] does: most severe first, then by
// rule, then by subject. The message settles the rest, because some checks walk
// maps and two findings can share a rule and a subject.
func sortFindings(fs []lint.Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		a, b := fs[i], fs[j]
		if a.Severity != b.Severity {
			return a.Severity > b.Severity
		}
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		if a.Subject != b.Subject {
			return a.Subject < b.Subject
		}
		return a.Message < b.Message
	})
}

// slot is one field as Build records it: the class or datatype it holds.
// Object marks a field of one of the module's types, which is a relation to
// that type; any other slot is a basic value.
type slot struct {
	iri    owl.IRI
	object bool
}

// shape is a type's fields, sorted so that equal shapes compare equal.
type shape []slot

// fieldInfo is what an ontology says about the property standing for one struct
// field: the struct it is on and what it holds. Build always writes both; a plan
// may leave either out, which leaves that part empty.
type fieldInfo struct {
	domain owl.Class
	held   slot
}

// fieldsOf reads every field back out of the property domains and ranges,
// keyed by the property's IRI.
func fieldsOf(o *owl.Ontology) map[owl.IRI]fieldInfo {
	out := map[owl.IRI]fieldInfo{}
	set := func(iri owl.IRI, change func(*fieldInfo)) {
		f := out[iri]
		change(&f)
		out[iri] = f
	}
	for ax := range o.All() {
		switch x := owl.Unwrap(ax).(type) {
		case owl.ObjectPropertyDomain:
			p, ok1 := x.Property.(owl.ObjectProperty)
			c, ok2 := x.Domain.(owl.Class)
			if ok1 && ok2 {
				set(p.IRI(), func(f *fieldInfo) { f.domain = c })
			}
		case owl.ObjectPropertyRange:
			p, ok1 := x.Property.(owl.ObjectProperty)
			c, ok2 := x.Range.(owl.Class)
			if ok1 && ok2 {
				set(p.IRI(), func(f *fieldInfo) { f.held = slot{iri: c.IRI(), object: true} })
			}
		case owl.DataPropertyDomain:
			p, ok1 := x.Property.(owl.DataProperty)
			c, ok2 := x.Domain.(owl.Class)
			if ok1 && ok2 {
				set(p.IRI(), func(f *fieldInfo) { f.domain = c })
			}
		case owl.DataPropertyRange:
			p, ok1 := x.Property.(owl.DataProperty)
			dt, ok2 := x.Range.(owl.Datatype)
			if ok1 && ok2 {
				set(p.IRI(), func(f *fieldInfo) { f.held = slot{iri: dt.IRI()} })
			}
		}
	}
	return out
}

// shapes groups the fields Build writes by the type they are on, one property
// per exported field.
func shapes(o *owl.Ontology) map[owl.Class]shape {
	out := map[owl.Class]shape{}
	for _, f := range fieldsOf(o) {
		if f.domain != "" && f.held.iri != "" {
			out[f.domain] = append(out[f.domain], f.held)
		}
	}
	for c, s := range out {
		sort.Slice(s, func(i, j int) bool {
			if s[i].object != s[j].object {
				return s[i].object // relations first, so they lead the message
			}
			return s[i].iri < s[j].iri
		})
		out[c] = s
	}
	return out
}

func equalShapes(a, b shape) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// common is the fields two shapes share, counting repeats: two ints in a and
// three in b share two.
func common(a, b shape) shape {
	left := map[slot]int{}
	for _, s := range a {
		left[s]++
	}
	var out shape
	for _, s := range b {
		if left[s] > 0 {
			left[s]--
			out = append(out, s)
		}
	}
	return out
}

// How one type's fields relate to another's.
type overlap int

const (
	sameFields overlap = iota // the same fields
	superset                  // every field of the other, and more
	subset                    // only fields the other has too
)

type match struct {
	other  owl.Class
	how    overlap
	shared shape
	extra  int // fields one side has and the other lacks
}

// compareShapes reports whether a and b overlap in a way worth reporting.
// Partial overlaps are left out: two types that share some fields and each
// add their own are too common to be a signal.
func compareShapes(a, b shape) (overlap, shape, bool) {
	shared := common(a, b)
	if len(shared) < 2 || !shared[0].object { // sorted: relations first
		return 0, nil, false
	}
	switch {
	case len(shared) == len(a) && len(shared) == len(b):
		return sameFields, shared, true
	case len(shared) == len(b):
		return superset, shared, true
	case len(shared) == len(a):
		return subset, shared, true
	}
	return 0, nil, false
}

// sameShape compares c's fields with every other type's and reports the
// closest matches of the strongest kind, skipping any match that already held
// before the change.
func sameShape(o *owl.Ontology, c owl.Class, classes []owl.Class, was, now map[owl.Class]shape, existed map[owl.Class]bool) (lint.Finding, bool) {
	var found []match
	for _, other := range classes {
		if other == c {
			continue
		}
		how, shared, ok := compareShapes(now[c], now[other])
		if !ok {
			continue
		}
		if existed[c] && existed[other] {
			if old, _, ok := compareShapes(was[c], was[other]); ok && old == how {
				continue
			}
		}
		found = append(found, match{other: other, how: how, shared: shared,
			extra: len(now[c]) + len(now[other]) - 2*len(shared)})
	}
	if len(found) == 0 {
		return lint.Finding{}, false
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].how != found[j].how {
			return found[i].how < found[j].how
		}
		if found[i].extra != found[j].extra {
			return found[i].extra < found[j].extra
		}
		return found[i].other < found[j].other
	})
	best := found[0].how
	var others []owl.Class
	for _, m := range found {
		if m.how == best {
			others = append(others, m.other)
		}
	}

	it := "it"
	if len(others) > 1 {
		it = "one of them"
	}
	name, list, fields := o.Render(c), renderList(o, others), renderShape(o, found[0].shared)
	var msg string
	switch best {
	case sameFields:
		msg = fmt.Sprintf("%s has the same fields as %s (%s); consider reusing %s", name, list, fields, it)
	case superset:
		msg = fmt.Sprintf("%s has every field of %s (%s); consider embedding %s", name, list, fields, it)
	case subset:
		msg = fmt.Sprintf("every field of %s is also in %s (%s); consider using %s instead", name, list, fields, it)
	}
	return lint.Finding{Rule: SameShapeType, Severity: lint.Warning, Subject: c.IRI(), Message: msg}, true
}

// newOrphans reports the added types that connect to no type that existed
// before. Connections run both ways along interfaces and fields, and through
// other new types, so a new interface with a new implementation that holds an
// existing type is connected, and a pair of new types that only know each
// other is not.
func newOrphans(o *owl.Ontology, added []owl.Class, now map[owl.Class]shape, existed map[owl.Class]bool) []lint.Finding {
	links := map[owl.Class][]owl.Class{}
	link := func(a, b owl.Class) {
		if a != b {
			links[a] = append(links[a], b)
			links[b] = append(links[b], a)
		}
	}
	for c, s := range now {
		for _, f := range s {
			if f.object {
				link(c, owl.Class(f.iri))
			}
		}
	}
	for _, c := range o.Classes() {
		for _, super := range o.SuperClassesOf(c) {
			link(c, super)
		}
	}

	var out []lint.Finding
	for _, c := range added {
		group, joined := component(c, links, existed)
		if joined {
			continue
		}
		msg := fmt.Sprintf("new type %s connects to no type that existed before: no interface, implementation or field links it to one", o.Render(c))
		var peers []owl.Class
		for _, p := range group {
			if p != c {
				peers = append(peers, p)
			}
		}
		if len(peers) > 0 {
			msg += fmt.Sprintf(", only to the new %s", renderList(o, peers))
		}
		out = append(out, lint.Finding{Rule: NewOrphan, Severity: lint.Info, Subject: c.IRI(), Message: msg})
	}
	return out
}

// component walks the types linked to start, returning them sorted and
// whether any of them existed before. It stops at the first that did.
func component(start owl.Class, links map[owl.Class][]owl.Class, existed map[owl.Class]bool) ([]owl.Class, bool) {
	seen := map[owl.Class]bool{start: true}
	queue := []owl.Class{start}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		for _, n := range links[c] {
			if existed[n] {
				return nil, true
			}
			if !seen[n] {
				seen[n] = true
				queue = append(queue, n)
			}
		}
	}
	group := make([]owl.Class, 0, len(seen))
	for c := range seen {
		group = append(group, c)
	}
	sort.Slice(group, func(i, j int) bool { return group[i] < group[j] })
	return group, false
}

// renderList names up to three classes, then counts the rest.
func renderList(o *owl.Ontology, cs []owl.Class) string {
	const shown = 3
	names := make([]string, 0, shown)
	for i, c := range cs {
		if i == shown {
			break
		}
		names = append(names, o.Render(c))
	}
	switch rest := len(cs) - len(names); {
	case rest > 0:
		return strings.Join(names, ", ") + fmt.Sprintf(" and %d more", rest)
	case len(names) == 1:
		return names[0]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
}

// renderSlot names what a field holds: a class or a datatype.
func renderSlot(o *owl.Ontology, s slot) string {
	if s.object {
		return o.Render(owl.Class(s.iri))
	}
	return o.Render(owl.Datatype(s.iri))
}

// renderShape lists fields by type, folding repeats: ":owl.Ontology,
// xsd:integer ×2".
func renderShape(o *owl.Ontology, s shape) string {
	var parts []string
	for i := 0; i < len(s); {
		j := i
		for j < len(s) && s[j] == s[i] {
			j++
		}
		name := renderSlot(o, s[i])
		if n := j - i; n > 1 {
			name += fmt.Sprintf(" ×%d", n)
		}
		parts = append(parts, name)
		i = j
	}
	return strings.Join(parts, ", ")
}
