package main

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"gowl/el"
	"gowl/internal/gocode"
	"gowl/owl"
)

// shop is a miniature of the shape the doppel walkthrough has: a command, an
// engine of two packages and a foundation under it, and one presentation
// package on top.
var shop = map[string]string{
	"go.mod":             "module example.com/shop\n\ngo 1.21\n",
	"cmd/cmd.go":         "package cmd\n\nimport (\n\t\"example.com/shop/pricing\"\n\t\"example.com/shop/report\"\n)\n\nvar _ = pricing.Total\nvar _ = report.Render\n",
	"core/core.go":       "package core\n\n// ID names a thing.\ntype ID string\n",
	"engine/engine.go":   "package engine\n\nimport \"example.com/shop/core\"\n\n// Item is one thing for sale.\ntype Item struct {\n\tID    core.ID\n\tName  string\n\tSKU   string\n\tPrice int\n}\n",
	"pricing/pricing.go": "package pricing\n\nimport \"example.com/shop/engine\"\n\n// Total adds up prices.\nfunc Total(items []engine.Item) (n int) {\n\tfor _, it := range items {\n\t\tn += it.Price\n\t}\n\treturn n\n}\n",
	"report/report.go":   "package report\n\nimport \"example.com/shop/engine\"\n\n// Row is one line of a report.\ntype Row struct {\n\tItem  engine.Item\n\tCount int\n}\n\n// Render renders rows.\nfunc Render(rows []Row) string { return \"\" }\n",
}

// exporter adds a CSV export the quick way: a package of its own, a type
// mirroring engine.Item, and the engine reaching into it to format a price.
var exporter = map[string]string{
	"export/export.go": "package export\n\n// Item is one exported row.\ntype Item struct {\n\tName  string\n\tSKU   string\n\tPrice int\n}\n\n// Money formats a price.\nfunc Money(cents int) string { return \"\" }\n",
	"engine/engine.go": "package engine\n\nimport (\n\t\"example.com/shop/core\"\n\t\"example.com/shop/export\"\n)\n\n// Item is one thing for sale.\ntype Item struct {\n\tID    core.ID\n\tName  string\n\tSKU   string\n\tPrice int\n}\n\nvar _ = export.Money\n",
}

// tidy adds the same export as a function of the presentation package.
var tidy = map[string]string{
	"report/csv.go": "package report\n\n// CSV writes rows as comma-separated values.\nfunc CSV(rows []Row) string { return \"\" }\n",
}

const shopArch = `
Prefix(:=<https://github.com/LukasSelin/gowl/examples/sprawl#>)
Prefix(rdfs:=<http://www.w3.org/2000/01/rdf-schema#>)
Prefix(shop:=<https://example.com/shop/>)

Ontology(<https://example.com/shop/architecture>
    SubClassOf(:Entry :Layer)
    SubClassOf(:Presentation :Layer)
    SubClassOf(:Engine :Layer)
    AnnotationAssertion(rdfs:label :Engine "engine")
    AnnotationAssertion(rdfs:label :Presentation "presentation")
    DisjointClasses(Annotation(rdfs:comment "A package sits in one layer.") :Entry :Presentation :Engine)

    SubClassOf(shop:cmd :Entry)
    SubClassOf(shop:report :Presentation)
    SubClassOf(shop:engine :Engine)
    SubClassOf(shop:pricing :Engine)
    SubClassOf(shop:core :Engine)
    %s

    EquivalentClasses(:ReachesPresentation ObjectSomeValuesFrom(:dependsOn :Presentation))
    DisjointClasses(Annotation(rdfs:comment "The engine never depends on presentation.") :Engine :ReachesPresentation)
)
`

// arch parses the miniature's architecture with extra axioms added.
func arch(t *testing.T, extra ...string) *owl.Ontology {
	t.Helper()
	o, err := owl.ParseFunctionalString(strings.Replace(shopArch, "%s", strings.Join(extra, "\n    "), 1))
	if err != nil {
		t.Fatal(err)
	}
	return o
}

// module writes the miniature with overlays applied to a temp directory and
// returns its root.
func module(t *testing.T, overlays ...map[string]string) string {
	t.Helper()
	t.Setenv("GOWORK", "off")
	files := maps.Clone(shop)
	for _, o := range overlays {
		maps.Copy(files, o)
	}
	root := t.TempDir()
	for name, src := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// tree reads the miniature, with overlays, as gowl gocode describes it.
func tree(t *testing.T, overlays ...map[string]string) *Tree {
	t.Helper()
	tr, err := Load(module(t, overlays...))
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestReadTree(t *testing.T) {
	tr := tree(t)
	if tr.Module != "example.com/shop" {
		t.Errorf("module = %q", tr.Module)
	}
	want := []string{"example.com/shop/cmd", "example.com/shop/core", "example.com/shop/engine", "example.com/shop/pricing", "example.com/shop/report"}
	if !slices.Equal(tr.Packages, want) {
		t.Errorf("packages = %v, want %v", tr.Packages, want)
	}
	if got := tr.Imports["example.com/shop/report"]; !slices.Equal(got, []string{"example.com/shop/engine"}) {
		t.Errorf("report imports %v", got)
	}
	var item *Type
	for _, ty := range tr.Types {
		if ty.Name == "engine.Item" {
			item = ty
		}
	}
	if item == nil {
		t.Fatalf("no engine.Item among %d types", len(tr.Types))
	}
	if want := []string{"ID core.ID", "Name string", "Price int", "SKU string"}; !slices.Equal(item.Fields, want) {
		t.Errorf("engine.Item fields = %v, want %v", item.Fields, want)
	}
}

// A document gowl gocode wrote reads back into the same tree as the module
// directory it describes.
func TestTreeFromDocument(t *testing.T) {
	dir := module(t)
	o, err := gocode.Build(dir)
	if err != nil {
		t.Fatal(err)
	}
	doc := filepath.Join(t.TempDir(), "shop.ofn")
	if err := writeFile(o, doc); err != nil {
		t.Fatal(err)
	}
	fromDir, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	fromDoc, err := Load(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fromDir, fromDoc) {
		t.Errorf("the document reads back differently:\n dir %+v\n doc %+v", fromDir, fromDoc)
	}
}

func TestBaseHolds(t *testing.T) {
	unplaced, violations := Check(tree(t), arch(t))
	if len(unplaced) > 0 || len(violations) > 0 {
		t.Errorf("unplaced %v, violations %+v; want none", unplaced, violations)
	}
}

func TestUnplacedPackageFailsTheGate(t *testing.T) {
	base, change := tree(t), tree(t, exporter)
	var out bytes.Buffer
	if ReportChange(&out, base, change, arch(t)) {
		t.Fatalf("gate passed:\n%s", out.String())
	}
	f := Compare(base, change)
	if !slices.Equal(f.Packages.Added, []string{"export"}) {
		t.Errorf("packages added = %v", f.Packages.Added)
	}
	if !slices.Equal(f.Imports.Added, []string{"engine → export"}) {
		t.Errorf("imports added = %v", f.Imports.Added)
	}
	for _, want := range []string{"unplaced packages (1)", "  export  new", "gate: failed (1 unplaced package)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report lacks %q:\n%s", want, out.String())
		}
	}
}

// Placing the new package is not enough when the engine imports it: the
// reasoner names every engine package that now reaches presentation, with the
// imports that take it there.
func TestViolationNamesTheImports(t *testing.T) {
	base, change := tree(t), tree(t, exporter)
	placed := arch(t, "SubClassOf(shop:export :Presentation)")
	unplaced, violations := Check(change, placed)
	if len(unplaced) > 0 {
		t.Errorf("unplaced = %v", unplaced)
	}
	paths := map[string][]string{}
	for _, v := range violations {
		if v.Rule != "The engine never depends on presentation." {
			t.Errorf("unexpected rule %q", v.Rule)
		}
		paths[change.Rel(v.Package)] = v.Path
	}
	want := map[string][]string{
		"engine":  {"example.com/shop/export"},
		"pricing": {"example.com/shop/engine", "example.com/shop/export"},
	}
	if !reflect.DeepEqual(paths, want) {
		t.Errorf("violations = %v, want %v", paths, want)
	}

	var out bytes.Buffer
	if ReportChange(&out, base, change, placed) {
		t.Fatalf("gate passed:\n%s", out.String())
	}
	for _, want := range []string{
		"    engine → export  new",
		"    pricing → engine → export  new",
		"gate: failed (2 new rule violations)",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report lacks %q:\n%s", want, out.String())
		}
	}
}

// A rule may name what it forbids inline, without defining a class for it.
func TestRuleWithAnonymousOperand(t *testing.T) {
	a := arch(t,
		"SubClassOf(shop:export :Presentation)",
		`DisjointClasses(Annotation(rdfs:comment "Inline.") :Engine ObjectSomeValuesFrom(:dependsOn :Presentation))`)
	_, violations := Check(tree(t, exporter), a)
	var got []string
	for _, v := range violations {
		if v.Rule == "Inline." {
			got = append(got, v.Package)
		}
	}
	if want := []string{"example.com/shop/engine", "example.com/shop/pricing"}; !slices.Equal(got, want) {
		t.Errorf("inline rule caught %v, want %v", got, want)
	}
}

// A violation the base already had is reported, but is the base's to fix: it
// does not fail a change that did not add it.
func TestExistingViolationDoesNotFail(t *testing.T) {
	broken := tree(t, exporter)
	placed := arch(t, "SubClassOf(shop:export :Presentation)")
	var out bytes.Buffer
	if !ReportChange(&out, broken, tree(t, exporter, tidy), placed) {
		t.Fatalf("gate failed:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "    engine → export\n") {
		t.Errorf("existing violation not listed, or marked new:\n%s", out.String())
	}
}

func TestTidyChangePasses(t *testing.T) {
	var out bytes.Buffer
	if !ReportChange(&out, tree(t), tree(t, tidy), arch(t)) {
		t.Fatalf("gate failed:\n%s", out.String())
	}
	want := "footprint\n  packages  +0 −0\n  imports   +0 −0\n  types     +0 −0\n  fields    +0 −0\n\ngate: passed\n"
	if !strings.HasSuffix(out.String(), want) {
		t.Errorf("report:\n%s\nwant it to end with:\n%s", out.String(), want)
	}
}

// With one tree there is no base, so nothing is marked new and the verdict is
// about the architecture as it stands.
func TestReportTree(t *testing.T) {
	var out bytes.Buffer
	if !ReportTree(&out, tree(t), arch(t)) {
		t.Errorf("base reported broken:\n%s", out.String())
	}
	out.Reset()
	if ReportTree(&out, tree(t, exporter), arch(t)) {
		t.Fatalf("broken tree reported as holding:\n%s", out.String())
	}
	for _, want := range []string{"\n  export\n", "architecture: broken (1 unplaced package, 0 rule violations)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "new") {
		t.Errorf("single-tree report marks something new:\n%s", out.String())
	}
}

func TestPackageInTwoLayers(t *testing.T) {
	_, violations := Check(tree(t), arch(t, "SubClassOf(shop:core :Presentation)"))
	var got []string
	for _, v := range violations {
		if v.Rule == "A package sits in one layer." {
			got = append(got, v.Package)
			if len(v.Path) > 0 || v.Under != [2]string{"presentation", "engine"} {
				t.Errorf("%s: under %v by %v, want presentation and engine and no imports", v.Package, v.Under, v.Path)
			}
		}
	}
	if !slices.Equal(got, []string{"example.com/shop/core"}) {
		t.Errorf("packages in two layers = %v", got)
	}
}

func TestTwins(t *testing.T) {
	ty := func(name string, fields ...string) *Type {
		return &Type{Class: owl.Class("urn:" + name), Name: name, Fields: fields}
	}
	unit := ty("snapshot.Unit", "File string", "Key string", "Line int", "Name string", "Package string", "Role string", "WL string")
	pair := ty("snapshot.Pair", "A string", "B string", "Kind string", "Score float")
	opts := ty("retriever.Options", "K int")
	existing := []*Type{unit, pair, opts}

	cases := []struct {
		t    *Type
		want []string
	}{
		// All of its fields are on snapshot.Unit, though they are under half
		// of the two types' fields together.
		{ty("sarif.Function", "File string", "Line int", "Name string", "Package string"), []string{"snapshot.Unit"}},
		// The same name with a field in common.
		{ty("sarif.Pair", "Kind string", "Score float", "Rule string", "Level string", "Message string"), []string{"snapshot.Pair"}},
		// The same name alone is not a twin: every package has Options.
		{ty("calibrate.Options", "Rate float"), nil},
		// Two fields of a projection are too few to say.
		{ty("sarif.Location", "File string", "Line int"), nil},
	}
	for _, c := range cases {
		var got []string
		for _, tw := range Twins([]*Type{c.t}, existing) {
			got = append(got, tw.Of.Name)
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: twins %v, want %v", c.t.Name, got, c.want)
		}
	}
}

// doppelPackages is every package of doppel at commit 9807c32, the commit
// doppel.ofn was written against.
var doppelPackages = []string{
	"", "cmd", "internal/analyzer", "internal/bench", "internal/calibrate",
	"internal/canon", "internal/clique", "internal/comparator", "internal/concepter",
	"internal/culture", "internal/dashboard", "internal/detmath", "internal/family",
	"internal/fingerprint", "internal/gofront", "internal/identity", "internal/lexbridge",
	"internal/lexfront", "internal/lexicon", "internal/mapper", "internal/moves",
	"internal/ontology", "internal/parallel", "internal/parser", "internal/reporter",
	"internal/retriever", "internal/snapshot", "internal/syntax", "internal/tagger",
}

// doppel.ofn is the architecture the walkthrough checks doppel against. Every
// axiom in it is one the EL classifier uses, it is coherent, and it places
// each of doppel's packages in exactly one layer. (Whether doppel's imports
// keep its rules is for a run on doppel itself; see the README.)
func TestDoppelArchitecture(t *testing.T) {
	a, err := parseFile("doppel.ofn")
	if err != nil {
		t.Fatal(err)
	}
	tr := &Tree{Module: "github.com/LukasSelin/doppel"}
	for _, p := range doppelPackages {
		tr.Packages = append(tr.Packages, strings.TrimSuffix(tr.Module+"/"+p, "/"))
	}
	c := el.Classify(Structure(tr, a))
	if u := c.Unsupported(); len(u) > 0 {
		t.Errorf("%d axioms outside OWL 2 EL, first %v", len(u), u[0])
	}
	if !c.IsCoherent() {
		t.Errorf("unsatisfiable: %v", c.UnsatisfiableClasses())
	}
	unplaced, violations := Check(tr, a)
	if len(unplaced) > 0 {
		t.Errorf("unplaced: %v", unplaced)
	}
	for _, v := range violations {
		t.Errorf("%s breaks %q: it is %s and %s", v.Package, v.Rule, v.Under[0], v.Under[1])
	}
}
