// Package gocode describes a Go module's code as an OWL 2 ontology. It is what
// "gowl gocode" runs, and what examples/self runs on gowl itself.
//
// Build takes the root of a module — the directory holding its go.mod. The
// module has to build. Its own packages are read from source; everything they
// import, standard library and third-party modules alike, is read from the
// export data "go list -export" produces inside that module, so its
// dependencies must be in the module cache.
//
// The mapping is deliberately small:
//
//   - every exported named Go type becomes an OWL class, labelled with its
//     qualified Go name and commented with the first sentence of its doc, as
//     go doc shows it: its own, or else that of the declaration group it is in;
//   - a type that satisfies an interface of this module is a SubClassOf that
//     interface — so owl.SubClassOf ⊑ owl.Axiom ⊑ owl.Node, and gowl's type
//     hierarchy for OWL is itself an OWL class hierarchy;
//   - an exported struct field whose type is one of those classes becomes an
//     object property with the struct as domain and the field type as range,
//     and a field of a basic type becomes a data property with an XSD range.
//     An embedded field counts, under its type's name;
//   - every package of the module is an individual of :Package, linked by
//     :imports to each package it imports directly. A package from outside the
//     module that one of them imports, standard library or dependency, is an
//     individual of :ExternalPackage; only the module's own imports are
//     recorded, not what those packages import in turn;
//   - every class is tied to its package with the annotation :definedIn, and
//     says what it is underneath, a struct, an interface or something else,
//     with the annotation :kind;
//   - what the code offers is listed as Name(parameter types) result types:
//     the methods of a type with the annotation :method, and the functions a
//     package exports with the annotation :func, on the package. An interface
//     lists every method it declares, and any other type the exported ones it
//     declares itself. The package's own name, which those signatures qualify
//     its types with, is the annotation :name.
//
// Only SubClassOf edges that are not implied by others are written, so the
// class hierarchy reads as the direct one.
package gocode

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/build"
	"go/doc"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"gowl/owl"
)

// selfIRI names gowl's own description. gowl's module path is not a URL the way
// a published module's is, so it keeps the IRI it was first written with; any
// other module is named by its module path under https://.
const selfIRI = "https://github.com/LukasSelin/gowl/self"

// The local names of the words Build talks about code with, in the module's
// namespace. They describe the code rather than being part of it, so the
// checks that compare a plan with the code leave them out.
const (
	vocabPackage         = "Package"
	vocabExternalPackage = "ExternalPackage"
	vocabImports         = "imports"
	vocabMethod          = "method"
	vocabDefinedIn       = "definedIn"
	vocabFunc            = "func"
	vocabKind            = "kind"
	vocabName            = "name"
)

// packageLocalName is the local name of the individual standing for the
// package with the given import path: gowl/internal/gocode becomes
// package.gowl.internal.gocode. The mapping loses where the slashes were, so
// a/b.c and a.b/c share a name; Go module paths make that rare in practice.
func packageLocalName(path string) string {
	return "package." + strings.ReplaceAll(path, "/", ".")
}

// pkg is one type-checked package of the module.
type pkg struct {
	path  string // import path, e.g. gowl/owl
	files []*ast.File
	types *types.Package
}

// loader type-checks the module's packages, resolving imports of the module
// from source and everything else from compiled export data.
type loader struct {
	root   string
	module string // the module path declared in root/go.mod
	fset   *token.FileSet
	deps   types.Importer
	loaded map[string]*pkg
}

// inModule reports whether path names a package of the module being described.
func (l *loader) inModule(path string) bool {
	return path == l.module || strings.HasPrefix(path, l.module+"/")
}

func (l *loader) Import(path string) (*types.Package, error) {
	if !l.inModule(path) {
		return l.deps.Import(path)
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

	dir := filepath.Join(l.root, filepath.FromSlash(strings.TrimPrefix(strings.TrimPrefix(path, l.module), "/")))
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

// packages lists the import paths of every package of module under root,
// skipping testdata, dot- or underscore-prefixed directories and nested modules
// as the go tool does.
func packages(root, module string) ([]string, error) {
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
		if path != root {
			if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
				return filepath.SkipDir // another module
			}
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

// modulePath returns the module path declared in root/go.mod.
func modulePath(root string) (string, error) {
	name := filepath.Join(root, "go.mod")
	data, err := os.ReadFile(name)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if f := strings.Fields(line); len(f) >= 2 && f[0] == "module" {
			return strings.Trim(f[1], "\"`"), nil
		}
	}
	return "", fmt.Errorf("%s: no module directive", name)
}

// exportData asks the go command, run in root, where the compiled export data
// of every package the module depends on lives, standard library included.
// Reading them all through one importer keeps one copy of each package, so the
// context.Context a dependency was compiled against is the one the module sees.
func exportData(root string) (map[string]string, error) {
	cmd := exec.Command("go", "list", "-deps", "-export",
		"-f", "{{if .Export}}{{.ImportPath}}\t{{.Export}}{{end}}", "./...")
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list -export in %s: %v\n%s", root, err, bytes.TrimSpace(stderr.Bytes()))
	}
	files := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if path, file, ok := strings.Cut(strings.TrimSpace(line), "\t"); ok {
			files[path] = file
		}
	}
	return files, nil
}

// KnownPackages lists the import paths, besides the module's own, that a
// module's invariants can name and expect to mean something: every package of
// the standard library, and every package the module depends on. A name that
// is none of these is usually misspelled, and a misspelled package in a rule
// that forbids it quietly forbids nothing.
func KnownPackages(root string) ([]string, error) {
	seen := map[string]bool{}
	for _, args := range [][]string{{"list", "std"}, {"list", "-deps", "./..."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = root
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("go %s in %s: %v\n%s", strings.Join(args, " "), root, err, bytes.TrimSpace(stderr.Bytes()))
		}
		for _, path := range strings.Fields(string(out)) {
			seen[path] = true
		}
	}
	out := make([]string, 0, len(seen))
	for path := range seen {
		out = append(out, path)
	}
	sort.Strings(out)
	return out, nil
}

// Build type-checks the module rooted at root — the directory holding its
// go.mod — and returns the ontology that describes it.
func Build(root string) (*owl.Ontology, error) {
	module, err := modulePath(root)
	if err != nil {
		return nil, err
	}
	files, err := exportData(root)
	if err != nil {
		return nil, err
	}
	lookup := func(path string) (io.ReadCloser, error) {
		file, ok := files[path]
		if !ok {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(file)
	}
	fset := token.NewFileSet()
	l := &loader{root: root, module: module, fset: fset,
		deps: importer.ForCompiler(fset, "gc", lookup), loaded: map[string]*pkg{}}

	paths, err := packages(root, module)
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

	iri := owl.IRI(selfIRI)
	about := "gowl's own Go code, described as an OWL 2 ontology by examples/self."
	if module != "gowl" {
		iri = owl.IRI("https://" + module)
		about = module + "'s Go code, described as an OWL 2 ontology by gowl gocode."
	}
	o := owl.New(iri)
	o.Prefix("", iri+"#")
	o.Annotate(owl.RDFSComment, owl.Str(about))

	// The vocabulary for talking about Go code.
	pkgClass := o.Class(":" + vocabPackage)
	o.Define(pkgClass).Label("Go package").Comment("A Go package of the " + module + " module.")
	extClass := o.Class(":" + vocabExternalPackage)
	o.Define(extClass).Label("external Go package").
		Comment("A package from outside the " + module + " module that one of its packages imports: part of the standard library, or of a dependency.").
		DisjointWith(pkgClass)
	imports := o.ObjectProperty(":" + vocabImports)
	// No range: the package imported is a :Package or an :ExternalPackage.
	o.DefineObjectProperty(imports).Domain(pkgClass).Irreflexive().
		Label("imports").Comment("The subject package imports the object package directly. The object is a :Package or an :ExternalPackage.")
	// The words that describe a term rather than relate two of them.
	describe := func(name, label, comment string) owl.AnnotationProperty {
		p := o.AnnotationProperty(":" + name)
		o.Declare(p)
		o.Add(owl.AnnotationAssertion{Property: owl.RDFSLabel, Subject: p.IRI(), Value: owl.Str(label)})
		o.Add(owl.AnnotationAssertion{Property: owl.RDFSComment, Subject: p.IRI(), Value: owl.Str(comment)})
		return p
	}
	definedIn := describe(vocabDefinedIn, "defined in", "The package whose source declares the subject Go type.")
	method := describe(vocabMethod, "method", "A method the subject Go type declares, as Name(parameter types) result types: every method of an interface, and the exported methods of any other type.")
	function := describe(vocabFunc, "function", "A function the subject Go package exports, as Name(parameter types) result types.")
	kind := describe(vocabKind, "kind", "What the subject Go type is underneath: struct, interface, basic, slice, array, map, chan, func or pointer.")
	pkgName := describe(vocabName, "name", "The name the subject Go package declares, which qualifies what it exports: owl in owl.Class.")

	pkgIndividual := func(path string) owl.NamedIndividual {
		return o.Individual(":" + packageLocalName(path))
	}

	// Packages, and what they import. Whether an import is the module's own is
	// decided by the packages found above rather than by its path, so a nested
	// module, which has the module's path as a prefix, counts as external.
	own := map[string]bool{}
	for _, p := range pkgs {
		own[p.path] = true
	}
	external := map[string]bool{}
	for _, p := range pkgs {
		ind := pkgIndividual(p.path)
		b := o.DefineIndividual(ind).Type(pkgClass).Label(p.path)
		if s := synopsis(p); s != "" {
			b.Comment(s)
		}
		var deps []string
		for _, imp := range p.types.Imports() {
			deps = append(deps, imp.Path())
			if !own[imp.Path()] {
				external[imp.Path()] = true
			}
		}
		sort.Strings(deps)
		for _, d := range deps {
			b.Fact(imports, pkgIndividual(d))
		}
		// What the package is called where it is used, and what it offers.
		o.Add(owl.AnnotationAssertion{Property: pkgName, Subject: ind.IRI(), Value: owl.Str(p.types.Name())})
		for _, sig := range packageFuncs(p.types) {
			o.Add(owl.AnnotationAssertion{Property: function, Subject: ind.IRI(), Value: owl.Str(sig)})
		}
	}
	for path := range external {
		o.DefineIndividual(pkgIndividual(path)).Type(extClass).Label(path)
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
		b.Annotate(kind, owl.Str(typeKind(ti.obj.Type())))
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

	// An interface's methods are its contract, and any other type's exported
	// methods are what it offers, so each is written down.
	for _, t := range all {
		sigs := interfaceMethods(t.obj, byObj)
		if sigs == nil {
			sigs = declaredMethods(t.obj)
		}
		for _, sig := range sigs {
			builders[t].Annotate(method, owl.Str(sig))
		}
	}

	// Struct fields become properties.
	for _, t := range all {
		st, ok := t.obj.Type().Underlying().(*types.Struct)
		if !ok {
			continue
		}
		for i := 0; i < st.NumFields(); i++ {
			// An embedded field is a field named after its type, and embedding
			// is how one type reuses another, so it is a relation like any other.
			f := st.Field(i)
			if !f.Exported() {
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

// typeDocs returns the first sentence of each type's doc comment, keyed by
// name. A type declared in a group with no comment of its own has the group's,
// which is what go doc shows for it.
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
				if text == "" {
					text = gd.Doc.Text()
				}
				if s := firstSentence(text, false); s != "" {
					out[ts.Name.Name] = s
				}
			}
		}
	}
	return out
}

// synopsis is the first sentence of a package's doc comment: of the first of
// its files that has one. A comment that is all directives, such as
// //go:generate above a package clause, has no text and is not one.
func synopsis(p *pkg) string {
	for _, f := range p.files {
		if s := firstSentence(f.Doc.Text(), true); s != "" {
			return s
		}
	}
	return ""
}

// firstSentence is the first sentence of a doc comment, as go doc lists it.
//
// go/doc says nothing for a comment that begins the way a licence header
// does, with "copyright", "all rights" or "author", because such a header is
// often what sits above a package clause. A comment that only happens to
// begin so, as the one on a type called Author does, is documentation all the
// same, so its first line stands in. header says the comment is where a
// licence header could be, and then one that begins with "copyright" or "all
// rights" is still taken for one.
func firstSentence(text string, header bool) string {
	var pr doc.Package
	if s := strings.TrimSpace(pr.Synopsis(text)); s != "" {
		return s
	}
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	if lower := strings.ToLower(line); header && (strings.HasPrefix(lower, "copyright") || strings.HasPrefix(lower, "all rights")) {
		return ""
	}
	return strings.TrimSpace(line)
}
