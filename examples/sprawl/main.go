// Command sprawl gates the part of a Go change that doppel does not measure:
// the packages it adds, the imports between them, and the exported types it
// mints. doppel finds a function that already exists. sprawl finds a package
// nobody placed, an import that crosses a layer, and a type that already
// exists under another name.
//
// It compares two descriptions of a module written by gowl gocode, the base a
// change started from and the tree with the change in it, against an
// architecture written by hand in OWL:
//
//	gowl gocode -o base.ofn ../doppel        # before starting the change
//	...                                      # make the change
//	go run ./examples/sprawl -arch examples/sprawl/doppel.ofn base.ofn ../doppel
//
// Either argument may be a module directory, which is described on the spot,
// or a document gowl gocode wrote. Given one argument, sprawl checks that tree
// on its own and lists the type twins already in it.
//
// The report has four parts:
//
//   - Footprint: packages, import edges, exported types and fields, added and
//     removed.
//   - Unplaced packages: a package the architecture does not put in a layer.
//     A new one fails the gate.
//   - Rule violations: a package the reasoner places under two classes that
//     one of the architecture's DisjointClasses axioms keeps apart. A new one
//     fails the gate. One the base already had is listed, but does not fail it.
//   - Twins: a new exported type with the same name as an existing type, or
//     with mostly the same fields. Twins are advisory and do not fail the gate.
//
// # The architecture
//
// The architecture is an ontology document. Each package is a class, named by
// its import path under https://, so doppel's analyzer is
// <https://github.com/LukasSelin/doppel/internal/analyzer>. sprawl writes
// these axioms for the tree being checked, in its own vocabulary:
//
//	SubClassOf(P :Package)                          every package
//	SubClassOf(P ObjectSomeValuesFrom(:imports Q))  P imports Q, both in the module
//	SubObjectPropertyOf(:imports :dependsOn)
//	TransitiveObjectProperty(:dependsOn)
//
// The architecture adds the rest. It places each package in a layer, which is
// a subclass of :Layer. It defines the classes its rules need, such as
// EquivalentClasses(:ReachesPresentation ObjectSomeValuesFrom(:dependsOn
// :Presentation)). Each rule is a DisjointClasses axiom, such as
// DisjointClasses(:Engine :ReachesPresentation). An rdfs:comment annotation on
// the axiom is the text the report prints for it.
//
// All of this is OWL 2 EL, so gowl's classifier decides it. A rule is checked
// by classifying without the DisjointClasses axioms and asking, for every
// package, which of the rule's classes it falls under. With the axioms left
// in, a violating package would be unsatisfiable. So would every package that
// imports it, and the report would name the importers instead of the cause.
// -o writes the tree with the axioms left in, for gowl classify.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"gowl/el"
	"gowl/internal/gocode"
	"gowl/owl"
)

// vocab is the namespace of the classes and properties sprawl writes, and of
// the ones an architecture document uses.
const vocab = "https://github.com/LukasSelin/gowl/examples/sprawl#"

var (
	classPackage  = owl.Class(vocab + "Package")
	classLayer    = owl.Class(vocab + "Layer")
	propImports   = owl.ObjectProperty(vocab + "imports")
	propDependsOn = owl.ObjectProperty(vocab + "dependsOn")
)

// maxListed caps each list in the report.
const maxListed = 12

const (
	exitOK     = 0
	exitFailed = 1
	exitUsage  = 2
)

func main() {
	arch := flag.String("arch", "", "the intended architecture, an ontology document (required)")
	out := flag.String("o", "", "write the checked tree's packages and imports, merged with the architecture, to this file")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: sprawl -arch architecture.ofn [-o tree.ofn] [base] tree")
		fmt.Fprintln(os.Stderr, "  base, tree: a module directory, or a document written by gowl gocode")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *arch == "" || flag.NArg() < 1 || flag.NArg() > 2 {
		flag.Usage()
		os.Exit(exitUsage)
	}
	os.Exit(run(*arch, *out, flag.Args(), os.Stdout))
}

func run(archPath, out string, args []string, stdout io.Writer) int {
	fail := func(err error) int {
		fmt.Fprintln(os.Stderr, "sprawl:", err)
		return exitUsage
	}
	arch, err := parseFile(archPath)
	if err != nil {
		return fail(err)
	}
	var trees []*Tree
	for _, a := range args {
		t, err := Load(a)
		if err != nil {
			return fail(err)
		}
		trees = append(trees, t)
	}
	tree := trees[len(trees)-1]
	if out != "" {
		if err := writeFile(Structure(tree, arch), out); err != nil {
			return fail(err)
		}
	}

	w := bufio.NewWriter(stdout)
	var passed bool
	if len(trees) == 1 {
		passed = ReportTree(w, tree, arch)
	} else {
		passed = ReportChange(w, trees[0], tree, arch)
	}
	if err := w.Flush(); err != nil {
		return fail(err)
	}
	if !passed {
		return exitFailed
	}
	return exitOK
}

// --- reading a tree ----------------------------------------------------------

// Tree is one gowl gocode description of a module, read back into the facts
// the gate compares.
type Tree struct {
	Module   string
	Packages []string            // import paths, sorted
	Imports  map[string][]string // import path -> the module packages it imports, sorted
	Types    []*Type             // sorted by package, then name
}

// Type is one exported named type.
type Type struct {
	Class   owl.Class // the class gowl gocode wrote for it
	Name    string    // its qualified Go name, e.g. snapshot.Unit
	Package string    // import path of the package that declares it
	Fields  []string  // its exported fields, as "Name type", sorted
}

// Load reads a tree from a module directory, by describing it with gowl
// gocode, or from a document gowl gocode wrote.
func Load(path string) (*Tree, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	var o *owl.Ontology
	if fi.IsDir() {
		if o, err = gocode.Build(path); err != nil {
			return nil, err
		}
	} else if o, err = parseFile(path); err != nil {
		return nil, err
	}
	t, err := ReadTree(o)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return t, nil
}

// ReadTree reads the packages, imports, types and fields out of a gowl gocode
// description.
func ReadTree(o *owl.Ontology) (*Tree, error) {
	ns, ok := o.Prefixes.Namespace("")
	if !ok {
		return nil, errors.New("not written by gowl gocode: no default prefix")
	}
	pkgClass := owl.Class(ns + "Package")
	imports := owl.ObjectProperty(ns + "imports")
	definedIn := owl.AnnotationProperty(ns + "definedIn")

	// gocode sorts its output, so a fact can arrive before what it refers to.
	// Collect everything first, then assemble.
	pathOf := map[owl.IRI]string{}
	var edges [][2]owl.IRI
	home := map[owl.Class]owl.IRI{}
	domain := map[owl.IRI]owl.Class{}
	rangeOf := map[owl.IRI]string{}
	for ax := range o.All() {
		switch x := owl.Unwrap(ax).(type) {
		case owl.ClassAssertion:
			if x.Class == pkgClass {
				if ind, ok := x.Individual.(owl.NamedIndividual); ok {
					pathOf[ind.IRI()] = o.Label(ind)
				}
			}
		case owl.ObjectPropertyAssertion:
			s, sok := x.Subject.(owl.NamedIndividual)
			v, vok := x.Object.(owl.NamedIndividual)
			if x.Property == imports && sok && vok {
				edges = append(edges, [2]owl.IRI{s.IRI(), v.IRI()})
			}
		case owl.AnnotationAssertion:
			if v, ok := x.Value.(owl.IRI); ok && x.Property == definedIn {
				home[owl.Class(x.Subject)] = v
			}
		case owl.DataPropertyDomain:
			if p, ok := x.Property.(owl.DataProperty); ok {
				if c, ok := x.Domain.(owl.Class); ok {
					domain[p.IRI()] = c
				}
			}
		case owl.DataPropertyRange:
			if p, ok := x.Property.(owl.DataProperty); ok {
				if d, ok := x.Range.(owl.Datatype); ok {
					rangeOf[p.IRI()] = goType(d)
				}
			}
		case owl.ObjectPropertyDomain:
			if p, ok := x.Property.(owl.ObjectProperty); ok {
				if c, ok := x.Domain.(owl.Class); ok {
					domain[p.IRI()] = c
				}
			}
		case owl.ObjectPropertyRange:
			if p, ok := x.Property.(owl.ObjectProperty); ok {
				if c, ok := x.Range.(owl.Class); ok {
					rangeOf[p.IRI()] = o.Label(c)
				}
			}
		}
	}
	if len(pathOf) == 0 {
		return nil, errors.New("not written by gowl gocode: no packages")
	}

	t := &Tree{Imports: map[string][]string{}}
	for _, p := range pathOf {
		t.Packages = append(t.Packages, p)
	}
	sort.Strings(t.Packages)
	t.Module = commonPath(t.Packages)
	for _, e := range edges {
		from, to := pathOf[e[0]], pathOf[e[1]]
		if from != "" && to != "" {
			t.Imports[from] = append(t.Imports[from], to)
		}
	}
	for _, deps := range t.Imports {
		sort.Strings(deps)
	}

	byClass := map[owl.Class]*Type{}
	for c, ind := range home {
		ty := &Type{Class: c, Name: o.Label(c), Package: pathOf[ind]}
		if ty.Name == "" {
			ty.Name = localName(owl.IRI(c))
		}
		byClass[c] = ty
		t.Types = append(t.Types, ty)
	}
	for p, c := range domain {
		ty, ok := byClass[c]
		r, rok := rangeOf[p]
		if !ok || !rok {
			continue
		}
		name := o.Label(owl.DataProperty(p)) // labels are keyed by IRI, so this finds an object property's too
		if name == "" {
			name = localName(p)
		}
		ty.Fields = append(ty.Fields, name[strings.LastIndexByte(name, '.')+1:]+" "+r)
	}
	for _, ty := range t.Types {
		sort.Strings(ty.Fields)
	}
	sort.Slice(t.Types, func(i, j int) bool {
		a, b := t.Types[i], t.Types[j]
		if a.Package != b.Package {
			return a.Package < b.Package
		}
		return a.Name < b.Name
	})
	return t, nil
}

// goType names the Go kind gocode mapped to an XSD datatype.
func goType(d owl.Datatype) string {
	switch d {
	case owl.XSDString:
		return "string"
	case owl.XSDBoolean:
		return "bool"
	case owl.XSDInteger:
		return "int"
	}
	if strings.HasSuffix(string(d), "#double") {
		return "float"
	}
	return localName(owl.IRI(d))
}

func localName(i owl.IRI) string {
	s := string(i)
	return s[strings.LastIndexAny(s, "#/")+1:]
}

// commonPath is the longest run of leading path elements every import path
// shares: the module path, when the module has a package at its root or more
// than one package.
func commonPath(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	common := strings.Split(paths[0], "/")
	for _, p := range paths[1:] {
		parts := strings.Split(p, "/")
		n := 0
		for n < len(common) && n < len(parts) && common[n] == parts[n] {
			n++
		}
		common = common[:n]
	}
	return strings.Join(common, "/")
}

// Rel names a package of the tree for a reader: relative to the module, or
// the module path itself for the package at its root.
func (t *Tree) Rel(path string) string {
	if rest, ok := strings.CutPrefix(path, t.Module+"/"); ok {
		return rest
	}
	return path
}

// --- the ontology the reasoner reads -------------------------------------

// PackageClass is the class standing for the package with this import path.
func PackageClass(path string) owl.Class { return owl.Class("https://" + path) }

func packagePath(c owl.Class) string { return strings.TrimPrefix(string(c), "https://") }

// Structure writes a tree's packages and imports as OWL 2 EL axioms, merged
// with the architecture's.
func Structure(t *Tree, arch *owl.Ontology) *owl.Ontology {
	o := owl.New(owl.IRI("https://" + t.Module + "/architecture"))
	for _, name := range arch.Prefixes.Names() {
		if ns, ok := arch.Prefixes.Namespace(name); ok {
			o.Prefix(name, ns)
		}
	}
	if _, ok := o.Prefixes.Namespace(""); !ok {
		o.Prefix("", vocab)
	}
	o.Annotate(owl.RDFSComment, owl.Str(t.Module+"'s packages and imports, checked by gowl's examples/sprawl against an architecture."))

	o.Define(classPackage).Label("Go package").Comment("A package of the module.")
	o.Define(classLayer).Label("layer").Comment("A layer of the architecture. Every package is placed in one.")
	o.DefineObjectProperty(propImports).SubPropertyOf(propDependsOn).
		Label("imports").Comment("The subject package imports the object package directly.")
	o.DefineObjectProperty(propDependsOn).Transitive().
		Label("depends on").Comment("The subject package imports the object package, directly or through others.")

	for _, p := range t.Packages {
		b := o.Define(PackageClass(p)).Label(t.Rel(p)).SubClassOf(classPackage)
		for _, q := range t.Imports[p] {
			b.SubClassOf(owl.Some(propImports, PackageClass(q)))
		}
	}
	for ax := range arch.All() {
		o.Add(ax)
	}
	return o
}

// --- checking a tree ---------------------------------------------------------

// Violation is one package found on both sides of a rule.
type Violation struct {
	Rule    string    // the rule's rdfs:comment, or the axiom itself
	Package string    // import path
	Under   [2]string // labels of two of the rule's classes the package falls under
	Path    []string  // the imports behind it, from Package outward; empty when none are involved
	New     bool      // the base did not have it
}

// Check classifies a tree against the architecture. It returns the packages
// no layer holds and the rules each package breaks.
func Check(t *Tree, arch *owl.Ontology) (unplaced []string, violations []Violation) {
	full := Structure(t, arch)
	label := func(c owl.Class) string {
		if l := full.Label(c); l != "" {
			return l
		}
		if s, ok := full.Prefixes.Compact(owl.IRI(c)); ok {
			return s
		}
		return string(c)
	}

	// Classify without the rules; a rule operand that is not a named class
	// gets a fresh name, so subsumption can be asked of it.
	o := owl.New(full.IRI)
	type rule struct {
		text    string
		classes []owl.Class
	}
	var rules []rule
	for ax := range full.All() {
		d, ok := owl.Unwrap(ax).(owl.DisjointClasses)
		if !ok {
			o.Add(ax)
			continue
		}
		r := rule{text: ruleText(ax, full.Prefixes)}
		for i, ce := range d {
			c, ok := ce.(owl.Class)
			if !ok {
				c = owl.Class(fmt.Sprintf("%srule%d.%d", vocab, len(rules), i))
				o.Add(owl.EquivalentClasses{c, ce})
			}
			r.classes = append(r.classes, c)
		}
		rules = append(rules, r)
	}
	c := el.Classify(o)

	for _, p := range t.Packages {
		pc := PackageClass(p)
		if !c.IsSubsumedBy(pc, classLayer) {
			unplaced = append(unplaced, p)
		}
		for _, r := range rules {
			var under []owl.Class
			for _, rc := range r.classes {
				if c.IsSubsumedBy(pc, rc) {
					under = append(under, rc)
				}
			}
			if len(under) < 2 {
				continue
			}
			why := append(c.Explain(pc, under[0]), c.Explain(pc, under[1])...)
			violations = append(violations, Violation{
				Rule:    r.text,
				Package: p,
				Under:   [2]string{label(under[0]), label(under[1])},
				Path:    importPath(p, why),
			})
		}
	}
	return unplaced, violations
}

// ruleText is what the report says a rule is: the rdfs:comment it was
// annotated with, or the axiom.
func ruleText(ax owl.Axiom, pre *owl.Prefixes) string {
	if a, ok := ax.(owl.Annotated); ok {
		for _, an := range a.Annotations {
			if l, ok := an.Value.(owl.Literal); ok && an.Property == owl.RDFSComment {
				return l.Value
			}
		}
	}
	return compact(owl.Functional(owl.Unwrap(ax)), pre)
}

// compact abbreviates the bracketed IRIs in rendered functional syntax.
func compact(s string, pre *owl.Prefixes) string {
	var b strings.Builder
	for {
		i := strings.IndexByte(s, '<')
		j := strings.IndexByte(s, '>')
		if i < 0 || j < i {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		if c, ok := pre.Compact(owl.IRI(s[i+1 : j])); ok {
			b.WriteString(c)
		} else {
			b.WriteString(s[i : j+1])
		}
		s = s[j+1:]
	}
}

// importPath follows the import axioms in an explanation from the package
// outward, and returns the packages it passes through.
func importPath(from string, why []owl.Axiom) []string {
	next := map[string][]string{}
	for _, ax := range why {
		sub, ok := owl.Unwrap(ax).(owl.SubClassOf)
		if !ok {
			continue
		}
		some, ok := sub.Super.(owl.ObjectSomeValuesFrom)
		if !ok || some.Property != propImports {
			continue
		}
		s, sok := sub.Sub.(owl.Class)
		f, fok := some.Filler.(owl.Class)
		if sok && fok {
			next[packagePath(s)] = append(next[packagePath(s)], packagePath(f))
		}
	}
	var path []string
	seen := map[string]bool{from: true}
	for at := from; ; {
		cands := next[at]
		sort.Strings(cands)
		step := ""
		for _, c := range cands {
			if !seen[c] {
				step = c
				break
			}
		}
		if step == "" {
			return path
		}
		path = append(path, step)
		seen[step] = true
		at = step
	}
}

// --- comparing two trees -----------------------------------------------------

// Change is what was added and removed from one kind of thing.
type Change struct{ Added, Removed []string }

// Footprint is everything a change added to or removed from the module's
// structure.
type Footprint struct {
	Packages, Imports, Types, Fields Change
}

// Compare measures the footprint of the change from before to after.
func Compare(before, after *Tree) Footprint {
	var f Footprint
	f.Packages = diff(names(before, packageNames), names(after, packageNames))
	f.Imports = diff(names(before, importNames), names(after, importNames))
	f.Types = diff(names(before, typeNames), names(after, typeNames))
	f.Fields = diff(names(before, fieldNames), names(after, fieldNames))
	return f
}

func names(t *Tree, f func(*Tree, func(string))) map[string]bool {
	m := map[string]bool{}
	f(t, func(s string) { m[s] = true })
	return m
}

func packageNames(t *Tree, add func(string)) {
	for _, p := range t.Packages {
		add(t.Rel(p))
	}
}

func importNames(t *Tree, add func(string)) {
	for p, deps := range t.Imports {
		for _, q := range deps {
			add(t.Rel(p) + " → " + t.Rel(q))
		}
	}
}

func typeNames(t *Tree, add func(string)) {
	for _, ty := range t.Types {
		add(ty.Name)
	}
}

func fieldNames(t *Tree, add func(string)) {
	for _, ty := range t.Types {
		for _, f := range ty.Fields {
			add(ty.Name + "." + f)
		}
	}
}

func diff(before, after map[string]bool) Change {
	var c Change
	for s := range after {
		if !before[s] {
			c.Added = append(c.Added, s)
		}
	}
	for s := range before {
		if !after[s] {
			c.Removed = append(c.Removed, s)
		}
	}
	sort.Strings(c.Added)
	sort.Strings(c.Removed)
	return c
}

// Twin is a type that resembles an existing one.
type Twin struct {
	Type, Of *Type
	Shared   []string // the fields both declare
	Score    float64  // shared fields over all fields of the two
	SameName bool     // the same name in another package
}

// Twins pairs each of types with the existing types it resembles, and keeps
// each type's three closest. A type resembles another when
//
//   - it has the same name in another package and a field in common (a shared
//     name alone is not enough: every Go package may have its own Options);
//   - at least two fields are common, and they are at least half of the two
//     types' fields; or
//   - at least three fields are common, and they are all of the type's own:
//     it is a projection of the other.
func Twins(types, existing []*Type) []Twin {
	var out []Twin
	for _, t := range types {
		var mine []Twin
		for _, e := range existing {
			if e.Class == t.Class {
				continue
			}
			tw := Twin{Type: t, Of: e, Shared: intersect(t.Fields, e.Fields),
				SameName: simpleName(t.Name) == simpleName(e.Name)}
			if union := len(t.Fields) + len(e.Fields) - len(tw.Shared); union > 0 {
				tw.Score = float64(len(tw.Shared)) / float64(union)
			}
			n := len(tw.Shared)
			if (tw.SameName && n > 0) || (n >= 2 && tw.Score >= 0.5) || (n >= 3 && n == len(t.Fields)) {
				mine = append(mine, tw)
			}
		}
		sort.SliceStable(mine, func(i, j int) bool { return closer(mine[i], mine[j]) })
		if len(mine) > 3 {
			mine = mine[:3]
		}
		out = append(out, mine...)
	}
	return out
}

// closer orders twins by how much of a shape they share: the most fields in
// common first, then the larger share of the two types' fields.
func closer(a, b Twin) bool {
	if len(a.Shared) != len(b.Shared) {
		return len(a.Shared) > len(b.Shared)
	}
	if a.Score != b.Score {
		return a.Score > b.Score
	}
	if a.SameName != b.SameName {
		return a.SameName
	}
	return a.Of.Name < b.Of.Name
}

func simpleName(qualified string) string {
	return qualified[strings.LastIndexByte(qualified, '.')+1:]
}

func intersect(a, b []string) []string {
	in := map[string]bool{}
	for _, s := range b {
		in[s] = true
	}
	var out []string
	for _, s := range a {
		if in[s] {
			out = append(out, s)
		}
	}
	return out
}

// --- the report --------------------------------------------------------------

// ReportChange writes the report on a change, and says whether it passes the
// gate.
func ReportChange(w io.Writer, before, after *Tree, arch *owl.Ontology) bool {
	fmt.Fprintf(w, "sprawl: %s\n\n", after.Module)

	f := Compare(before, after)
	fmt.Fprintln(w, "footprint")
	footLine(w, "packages", f.Packages, true)
	footLine(w, "imports", f.Imports, true)
	footLine(w, "types", f.Types, true)
	footLine(w, "fields", f.Fields, false)

	_, baseViolations := Check(before, arch)
	unplaced, violations := Check(after, arch)
	had := map[string]bool{}
	for _, v := range baseViolations {
		had[v.Rule+"\x00"+v.Package] = true
	}
	for i := range violations {
		violations[i].New = !had[violations[i].Rule+"\x00"+violations[i].Package]
	}
	newPackage := map[string]bool{}
	for _, p := range f.Packages.Added {
		newPackage[p] = true
	}
	newUnplaced := 0
	for _, p := range unplaced {
		if newPackage[after.Rel(p)] {
			newUnplaced++
		}
	}
	newViolations := 0
	for _, v := range violations {
		if v.New {
			newViolations++
		}
	}
	printUnplaced(w, after, unplaced, newPackage)
	printViolations(w, after, violations)

	known := map[owl.Class]bool{}
	for _, t := range before.Types {
		known[t.Class] = true
	}
	var minted []*Type
	for _, t := range after.Types {
		if !known[t.Class] {
			minted = append(minted, t)
		}
	}
	twins := Twins(minted, before.Types)
	if len(twins) > 0 {
		fmt.Fprintf(w, "\nnew types with twins (%d) — reuse the existing type, or say why not\n", countTypes(twins))
		printTwins(w, twins)
	}

	fmt.Fprintln(w)
	if newUnplaced == 0 && newViolations == 0 {
		fmt.Fprintln(w, "gate: passed")
		return true
	}
	var why []string
	if newUnplaced > 0 {
		why = append(why, plural(newUnplaced, "unplaced package"))
	}
	if newViolations > 0 {
		why = append(why, plural(newViolations, "new rule violation"))
	}
	fmt.Fprintf(w, "gate: failed (%s)\n", strings.Join(why, ", "))
	return false
}

// ReportTree writes the report on one tree, and says whether it passes: every
// package placed, and no rule broken.
func ReportTree(w io.Writer, t *Tree, arch *owl.Ontology) bool {
	fmt.Fprintf(w, "sprawl: %s — %d packages, %d import edges, %d exported types\n",
		t.Module, len(t.Packages), len(names(t, importNames)), len(t.Types))
	unplaced, violations := Check(t, arch)
	printUnplaced(w, t, unplaced, nil)
	printViolations(w, t, violations)

	// Each pair once, closest first. A pair can be in one type's closest three
	// and not the other's, so dedupe on the pair rather than on its order.
	var twins []Twin
	seen := map[[2]owl.Class]bool{}
	for _, tw := range Twins(t.Types, t.Types) {
		key := [2]owl.Class{tw.Type.Class, tw.Of.Class}
		if key[1] < key[0] {
			key[0], key[1] = key[1], key[0]
		}
		if !seen[key] {
			seen[key] = true
			twins = append(twins, tw)
		}
	}
	sort.SliceStable(twins, func(i, j int) bool { return closer(twins[i], twins[j]) })
	if len(twins) > 0 {
		fmt.Fprintf(w, "\ntype twins already here (%d)", len(twins))
		if len(twins) > maxListed {
			fmt.Fprintf(w, ", the %d closest", maxListed)
			twins = twins[:maxListed]
		}
		fmt.Fprintln(w)
		printTwins(w, twins)
	}

	fmt.Fprintln(w)
	if len(unplaced) == 0 && len(violations) == 0 {
		fmt.Fprintln(w, "architecture: holds")
		return true
	}
	fmt.Fprintf(w, "architecture: broken (%s, %s)\n",
		plural(len(unplaced), "unplaced package"), plural(len(violations), "rule violation"))
	return false
}

func footLine(w io.Writer, what string, c Change, list bool) {
	line := fmt.Sprintf("  %-9s %-8s", what, fmt.Sprintf("+%d −%d", len(c.Added), len(c.Removed)))
	if list && len(c.Added) > 0 {
		line += "  " + capped(c.Added)
	}
	fmt.Fprintln(w, strings.TrimRight(line, " "))
	if list && len(c.Removed) > 0 {
		fmt.Fprintf(w, "  %-9s %-8s  removed: %s\n", "", "", capped(c.Removed))
	}
}

// printUnplaced lists the unplaced packages, marking those in isNew.
func printUnplaced(w io.Writer, t *Tree, unplaced []string, isNew map[string]bool) {
	if len(unplaced) == 0 {
		return
	}
	fmt.Fprintf(w, "\nunplaced packages (%d) — place each in a layer of the architecture, or put its code in a package that has one\n", len(unplaced))
	for _, p := range unplaced {
		mark := ""
		if isNew[t.Rel(p)] {
			mark = "  new"
		}
		fmt.Fprintf(w, "  %s%s\n", t.Rel(p), mark)
	}
}

func printViolations(w io.Writer, t *Tree, vs []Violation) {
	if len(vs) == 0 {
		return
	}
	fmt.Fprintf(w, "\nrule violations (%d)\n", len(vs))
	byRule := map[string][]Violation{}
	var order []string
	for _, v := range vs {
		if _, ok := byRule[v.Rule]; !ok {
			order = append(order, v.Rule)
		}
		byRule[v.Rule] = append(byRule[v.Rule], v)
	}
	for _, r := range order {
		fmt.Fprintf(w, "  %s\n", r)
		group := byRule[r]
		sort.SliceStable(group, func(i, j int) bool { return len(group[i].Path) < len(group[j].Path) })
		for _, v := range group {
			line := t.Rel(v.Package)
			if len(v.Path) > 0 {
				for _, p := range v.Path {
					line += " → " + t.Rel(p)
				}
			} else {
				line += fmt.Sprintf(" is both %s and %s", v.Under[0], v.Under[1])
			}
			mark := ""
			if v.New {
				mark = "  new"
			}
			fmt.Fprintf(w, "    %s%s\n", line, mark)
		}
	}
}

func printTwins(w io.Writer, twins []Twin) {
	width := 0
	for _, tw := range twins {
		width = max(width, len(tw.Type.Name))
	}
	var last *Type
	for _, tw := range twins {
		name := tw.Type.Name
		if tw.Type == last {
			name = ""
		}
		last = tw.Type
		what := fmt.Sprintf("%d of %d fields", len(tw.Shared), len(tw.Type.Fields))
		if tw.SameName {
			what = "same name, " + what
		}
		fmt.Fprintf(w, "  %-*s  ~ %s  (%s", width, name, tw.Of.Name, what)
		if len(tw.Shared) > 0 {
			fmt.Fprintf(w, ": %s", strings.Join(fieldNamesOnly(tw.Shared), ", "))
		}
		fmt.Fprintln(w, ")")
	}
}

func fieldNamesOnly(fields []string) []string {
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i], _, _ = strings.Cut(f, " ")
	}
	return out
}

func countTypes(twins []Twin) int {
	seen := map[*Type]bool{}
	for _, tw := range twins {
		seen[tw.Type] = true
	}
	return len(seen)
}

func capped(xs []string) string {
	if len(xs) <= maxListed {
		return strings.Join(xs, ", ")
	}
	return strings.Join(xs[:maxListed], ", ") + fmt.Sprintf(", and %d more", len(xs)-maxListed)
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}

// --- files -------------------------------------------------------------------

func parseFile(path string) (*owl.Ontology, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	o, err := owl.ParseFunctional(bufio.NewReader(f))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return o, nil
}

func writeFile(o *owl.Ontology, path string) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	w := bufio.NewWriter(f)
	if err := o.WriteFunctional(w); err != nil {
		return err
	}
	return w.Flush()
}
