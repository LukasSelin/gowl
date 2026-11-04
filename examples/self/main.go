// Command self describes gowl's own Go code as an OWL 2 ontology, so that the
// repository's linter has a real, non-toy ontology of this repository to run on.
//
// Run it from the repository root:
//
//	go run ./examples/self > examples/self/gowl.ofn
//	go run ./cmd/gowl lint examples/self/gowl.ofn
//
// The mapping is deliberately small:
//
//   - every exported named Go type becomes an OWL class, labelled with its
//     qualified Go name and commented with the first sentence of its doc;
//   - a type that satisfies an interface of this module is a SubClassOf that
//     interface — so owl.SubClassOf ⊑ owl.Axiom ⊑ owl.Node, and gowl's type
//     hierarchy for OWL is itself an OWL class hierarchy;
//   - an exported struct field whose type is one of those classes becomes an
//     object property with the struct as domain and the field type as range,
//     and a field of a basic type becomes a data property with an XSD range;
//   - every package is an individual of :Package, linked by :imports to the
//     module packages it imports, and every class is tied to its package with
//     the annotation :definedIn.
//
// Only SubClassOf edges that are not implied by others are written, so the
// class hierarchy reads as the direct one.
package main

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/build"
	"go/doc"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gowl/owl"
)

const (
	module    = "gowl"
	ontology  = "https://github.com/LukasSelin/gowl/self"
	namespace = ontology + "#"
)

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	o, err := Build(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "self:", err)
		os.Exit(1)
	}
	w := bufio.NewWriter(os.Stdout)
	if err := o.WriteFunctional(w); err != nil {
		fmt.Fprintln(os.Stderr, "self:", err)
		os.Exit(1)
	}
	if err := w.Flush(); err != nil {
		fmt.Fprintln(os.Stderr, "self:", err)
		os.Exit(1)
	}
}

// pkg is one type-checked package of the module.
type pkg struct {
	path  string // import path, e.g. gowl/owl
	files []*ast.File
	types *types.Package
}

// loader type-checks the module's packages, resolving imports of the module
// from source and everything else from the standard library's export data.
type loader struct {
	root   string
	fset   *token.FileSet
	std    types.Importer
	loaded map[string]*pkg
}

func (l *loader) Import(path string) (*types.Package, error) {
	if path != module && !strings.HasPrefix(path, module+"/") {
		return l.std.Import(path)
	}
	p, err := l.load(path)
	if err != nil {
		return nil, err
	}
	return p.types, nil
}

func (l *loader) load(path string) (*pkg, error) {
	if p, ok := l.loaded[path]; ok {
		if p == nil {
			return nil, fmt.Errorf("import cycle through %s", path)
		}
		return p, nil
	}
	l.loaded[path] = nil

	dir := filepath.Join(l.root, filepath.FromSlash(strings.TrimPrefix(strings.TrimPrefix(path, module), "/")))
	bp, err := build.ImportDir(dir, 0)
	if err != nil {
		return nil, err
	}
	p := &pkg{path: path}
	for _, name := range bp.GoFiles {
		f, err := parser.ParseFile(l.fset, filepath.Join(dir, name), nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		p.files = append(p.files, f)
	}
	conf := types.Config{Importer: l}
	p.types, err = conf.Check(path, l.fset, p.files, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	l.loaded[path] = p
	return p, nil
}

// packages lists the import paths of every package under root, skipping
// testdata and dot- or underscore-prefixed directories as the go tool does.
func packages(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		if path != root && (name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
			return filepath.SkipDir
		}
		if _, err := build.ImportDir(path, 0); err != nil {
			return nil // no buildable Go files here
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			out = append(out, module)
		} else {
			out = append(out, module+"/"+filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

// typeInfo is one exported named type and the class standing for it.
type typeInfo struct {
	obj   *types.TypeName
	class owl.Class
	pkg   string
	doc   string
}

// Build type-checks the module rooted at root and returns the ontology that
// describes it.
func Build(root string) (*owl.Ontology, error) {
	fset := token.NewFileSet()
	l := &loader{root: root, fset: fset, std: importer.Default(), loaded: map[string]*pkg{}}

	paths, err := packages(root)
	if err != nil {
		return nil, err
	}
	var pkgs []*pkg
	for _, path := range paths {
		p, err := l.load(path)
		if err != nil {
			return nil, err
		}
		pkgs = append(pkgs, p)
	}

	o := owl.New(ontology)
	o.Prefix("", namespace)
	o.Annotate(owl.RDFSComment, owl.Str("gowl's own Go code, described as an OWL 2 ontology by examples/self."))

	// The vocabulary for talking about Go code.
	pkgClass := o.Class(":Package")
	o.Define(pkgClass).Label("Go package").Comment("A Go package of the gowl module.")
	imports := o.ObjectProperty(":imports")
	o.DefineObjectProperty(imports).Domain(pkgClass).Range(pkgClass).Irreflexive().
		Label("imports").Comment("The subject package imports the object package directly.")
	definedIn := o.AnnotationProperty(":definedIn")
	o.Declare(definedIn)
	o.Add(owl.AnnotationAssertion{Property: owl.RDFSLabel, Subject: definedIn.IRI(), Value: owl.Str("defined in")})
	o.Add(owl.AnnotationAssertion{Property: owl.RDFSComment, Subject: definedIn.IRI(), Value: owl.Str("The package whose source declares the subject Go type.")})

	pkgIndividual := func(path string) owl.NamedIndividual {
		return o.Individual(":package." + strings.ReplaceAll(path, "/", "."))
	}

	// Packages, and the imports among them.
	for _, p := range pkgs {
		ind := pkgIndividual(p.path)
		b := o.DefineIndividual(ind).Type(pkgClass).Label(p.path)
		if s := synopsis(p); s != "" {
			b.Comment(s)
		}
		var deps []string
		for _, imp := range p.types.Imports() {
			if imp.Path() == module || strings.HasPrefix(imp.Path(), module+"/") {
				deps = append(deps, imp.Path())
			}
		}
		sort.Strings(deps)
		for _, d := range deps {
			b.Fact(imports, pkgIndividual(d))
		}
	}

	// Exported named types become classes.
	var all []*typeInfo
	byObj := map[*types.TypeName]*typeInfo{}
	for _, p := range pkgs {
		docs := typeDocs(p)
		scope := p.types.Scope()
		for _, name := range scope.Names() {
			tn, ok := scope.Lookup(name).(*types.TypeName)
			if !ok || !tn.Exported() || tn.IsAlias() {
				continue
			}
			ti := &typeInfo{
				obj:   tn,
				class: o.Class(":" + qualified(tn)),
				pkg:   p.path,
				doc:   docs[name],
			}
			all = append(all, ti)
			byObj[tn] = ti
		}
	}
	// One builder per class: Define declares, so calling it again per axiom
	// would assert the Declaration twice (duplicate-axiom catches exactly that).
	builders := map[*typeInfo]*owl.ClassBuilder{}
	for _, ti := range all {
		b := o.Define(ti.class).Label(qualified(ti.obj))
		if ti.doc != "" {
			b.Comment(ti.doc)
		}
		b.Annotate(definedIn, pkgIndividual(ti.pkg).IRI())
		builders[ti] = b
	}

	// Interface satisfaction becomes the class hierarchy.
	supers := map[*typeInfo][]*typeInfo{}
	for _, t := range all {
		for _, i := range all {
			if t == i || !satisfies(t.obj, i.obj) {
				continue
			}
			supers[t] = append(supers[t], i)
		}
	}
	for _, t := range all {
		for _, s := range direct(t, supers) {
			builders[t].SubClassOf(s.class)
		}
	}

	// Struct fields become properties.
	for _, t := range all {
		st, ok := t.obj.Type().Underlying().(*types.Struct)
		if !ok {
			continue
		}
		for i := 0; i < st.NumFields(); i++ {
			f := st.Field(i)
			if !f.Exported() || f.Embedded() {
				continue
			}
			name := ":" + qualified(t.obj) + "." + f.Name()
			label := qualified(t.obj) + "." + f.Name()
			if r, ok := fieldClass(f.Type(), byObj); ok {
				p := o.ObjectProperty(name)
				o.DefineObjectProperty(p).Domain(t.class).Range(r.class).Label(label)
				continue
			}
			if dt, ok := fieldDatatype(f.Type()); ok {
				p := o.DataProperty(name)
				o.DefineDataProperty(p).Domain(t.class).Range(dt).Label(label)
			}
		}
	}

	if err := o.Err(); err != nil {
		return nil, err
	}
	return o.Sort(), nil
}

// qualified names a type the way Go code refers to it: owl.Class.
func qualified(tn *types.TypeName) string {
	return tn.Pkg().Name() + "." + tn.Name()
}

// satisfies reports whether t (or *t) implements the non-empty interface i.
// An empty interface is satisfied by everything and says nothing.
func satisfies(t, i *types.TypeName) bool {
	iface, ok := i.Type().Underlying().(*types.Interface)
	if !ok || iface.NumMethods() == 0 {
		return false
	}
	if types.Implements(t.Type(), iface) {
		return true
	}
	if _, isIface := t.Type().Underlying().(*types.Interface); isIface {
		return false
	}
	return types.Implements(types.NewPointer(t.Type()), iface)
}

// direct drops a supertype that is reachable through another supertype, so
// only the edges of the transitive reduction are written. Two interfaces
// with the same method set imply each other; both edges are kept, and the
// linter's subclass-cycle rule is what reports them.
func direct(t *typeInfo, supers map[*typeInfo][]*typeInfo) []*typeInfo {
	strictlyBelow := func(a, b *typeInfo) bool { // a ⊑ b and not b ⊑ a
		return contains(supers[a], b) && !contains(supers[b], a)
	}
	var out []*typeInfo
	for _, s := range supers[t] {
		implied := false
		for _, m := range supers[t] {
			if m != s && strictlyBelow(m, s) {
				implied = true
				break
			}
		}
		if !implied {
			out = append(out, s)
		}
	}
	return out
}

func contains(xs []*typeInfo, x *typeInfo) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// fieldClass resolves a field type — T, *T, []T or []*T — to the class of a
// module type.
func fieldClass(t types.Type, byObj map[*types.TypeName]*typeInfo) (*typeInfo, bool) {
	for {
		switch u := t.(type) {
		case *types.Pointer:
			t = u.Elem()
			continue
		case *types.Slice:
			t = u.Elem()
			continue
		case *types.Named:
			ti, ok := byObj[u.Obj()]
			return ti, ok
		}
		return nil, false
	}
}

// fieldDatatype maps a field of a basic type to its XSD datatype.
func fieldDatatype(t types.Type) (owl.Datatype, bool) {
	b, ok := t.(*types.Basic)
	if !ok {
		return "", false
	}
	switch {
	case b.Info()&types.IsString != 0:
		return owl.XSDString, true
	case b.Info()&types.IsBoolean != 0:
		return owl.XSDBoolean, true
	case b.Info()&types.IsInteger != 0:
		return owl.XSDInteger, true
	case b.Info()&types.IsFloat != 0:
		return owl.Datatype(owl.NamespaceXSD + "double"), true
	}
	return "", false
}

// typeDocs returns the first sentence of each type's doc comment, keyed by name.
func typeDocs(p *pkg) map[string]string {
	out := map[string]string{}
	for _, f := range p.files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts := spec.(*ast.TypeSpec)
				text := ts.Doc.Text()
				if text == "" && len(gd.Specs) == 1 {
					text = gd.Doc.Text()
				}
				if s := firstSentence(text); s != "" {
					out[ts.Name.Name] = s
				}
			}
		}
	}
	return out
}

// synopsis is the first sentence of a package's doc comment.
func synopsis(p *pkg) string {
	for _, f := range p.files {
		if f.Doc != nil {
			return firstSentence(f.Doc.Text())
		}
	}
	return ""
}

func firstSentence(text string) string {
	var pr doc.Package
	return strings.TrimSpace(pr.Synopsis(text))
}
