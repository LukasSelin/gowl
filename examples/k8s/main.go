// Command k8s describes the structure of Go types as an OWL 2 EL ontology that
// a reasoner can answer questions with, and is tried on Kubernetes' API types.
//
// Fetch the API types and run it from the repository root:
//
//	go mod download k8s.io/api@v0.37.2 k8s.io/apimachinery@v0.37.2
//	M=$(go env GOMODCACHE)
//	go run ./examples/k8s -questions examples/k8s/questions.ofn -answers -o k8s.ofn \
//	    $M/k8s.io/api@v0.37.2 $M/k8s.io/apimachinery@v0.37.2/pkg/apis/meta/v1
//	gowl classify -explain "batchv1:CronJob :RunsPods" k8s.ofn
//
// examples/self transcribes a module: a class per type, a property per field,
// packages as individuals. Almost none of that is OWL 2 EL, so a reasoner has
// nothing to do with it. This generator keeps only the structure a reasoner can
// use, and leaves the questions to a hand-written file:
//
//   - Every exported named type is a class whose IRI is its pkg.go.dev URL, so
//     core/v1.Event and events/v1.Event stay two classes. Each package gets a
//     prefix such as appsv1: so the output stays readable.
//
//   - A field is an existential restriction over one of a few shared properties,
//     not a property of its own:
//
//     embeds    an embedded value field                ⊑ hasField
//     hasField  a value field                          ⊑ contains (transitive)
//     mayHave   a field through a pointer, slice or map ⊑ mayContain (transitive)
//
//     with contains ⊑ mayContain. T ⊑ ∃contains.X says every T value holds an X
//     somewhere inside it. T ⊑ ∃mayContain.X is weaker: T's definition reaches
//     X, but only through something that may be nil or empty.
//
//   - Fields of basic types (string, int32, ...) are left out, since no
//     question here is about them.
//
//   - A type defined over another named type (type List metav1.List) is a
//     SubClassOf it: it has the same shape, so what one contains the other does.
//
// Source is read syntactically. There is no type-checking, so no dependency has
// to be loaded; a qualified type name is resolved through its file's imports.
// Files carrying Go's generated-code marker are skipped.
//
// -questions merges a hand-written ontology of defined classes into the output.
// -answers classifies the result and prints, for each class that file declares,
// the types the reasoner placed under it, and any class the file's rules make
// unsatisfiable.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"go/ast"
	"go/build"
	"go/doc"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gowl/el"
	"gowl/owl"
)

const (
	ontologyIRI = "https://github.com/LukasSelin/gowl/examples/k8s"
	vocab       = ontologyIRI + "#"
	godoc       = "https://pkg.go.dev/"
)

func main() {
	questions := flag.String("questions", "", "merge the defined classes in this ontology document into the output")
	answers := flag.Bool("answers", false, "classify, and print what each class of -questions was found to contain")
	out := flag.String("o", "", "write the ontology to this file; without it, it goes to stdout unless -answers is set")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: k8s [-questions file.ofn] [-answers] [-o out.ofn] dir...")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 || (*answers && *questions == "") {
		flag.Usage()
		os.Exit(2)
	}

	r, err := Build(flag.Args(), *questions)
	if err != nil {
		fmt.Fprintln(os.Stderr, "k8s:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "k8s: %d packages, %d types, %d restrictions, %d types outside them, %d generated files skipped\n",
		r.Packages, r.Types, r.Restrictions, r.External, r.Generated)
	for _, w := range r.Warnings {
		fmt.Fprintln(os.Stderr, "k8s:", w)
	}

	if *out != "" || !*answers {
		if err := write(r.Ontology, *out); err != nil {
			fmt.Fprintln(os.Stderr, "k8s:", err)
			os.Exit(1)
		}
	}
	if *answers {
		w := bufio.NewWriter(os.Stdout)
		PrintAnswers(w, r)
		if err := w.Flush(); err != nil {
			fmt.Fprintln(os.Stderr, "k8s:", err)
			os.Exit(1)
		}
	}
}

func write(o *owl.Ontology, path string) (err error) {
	f := os.Stdout
	if path != "" {
		if f, err = os.Create(path); err != nil {
			return err
		}
		defer func() {
			if cerr := f.Close(); err == nil {
				err = cerr
			}
		}()
	}
	w := bufio.NewWriter(f)
	if err := o.WriteFunctional(w); err != nil {
		return err
	}
	return w.Flush()
}

// Result is the ontology Build produced and what went into it.
type Result struct {
	Ontology  *owl.Ontology
	Questions []owl.Class // the classes the -questions document declares, in its order

	Packages, Types, Restrictions, External, Generated int
	Warnings                                           []string
}

// pkg is one parsed package.
type pkg struct {
	path  string
	name  string
	files []*file
	types map[string]*typeDecl // exported, non-alias type declarations by name
}

type file struct {
	ast     *ast.File
	imports map[string]string // local name -> import path
}

type typeDecl struct {
	name string
	spec *ast.TypeSpec
	doc  string
	file *file
}

// ref is one named type a type expression mentions, and whether the type
// expression can hold none of it (through a pointer, slice or map).
type ref struct {
	class    owl.Class
	optional bool
}

// Build reads the Go packages under each root and returns their type structure
// as an ontology, with the questions document merged in when one is named.
func Build(roots []string, questions string) (*Result, error) {
	pkgs, generated, err := load(roots)
	if err != nil {
		return nil, err
	}
	r := &Result{Packages: len(pkgs), Generated: generated}
	g := &generator{
		o:        owl.New(ontologyIRI),
		pkgs:     pkgs,
		prefixes: map[string]string{"": vocab, "owl": "", "rdf": "", "rdfs": "", "xsd": ""},
		declared: map[owl.Class]bool{},
	}
	g.o.Prefix("", vocab)
	g.o.Annotate(owl.RDFSComment, owl.Str("The structure of Go types, written by gowl's examples/k8s for an OWL 2 EL reasoner."))
	g.vocabulary()

	paths := make([]string, 0, len(pkgs))
	for p := range pkgs {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		pk := pkgs[p]
		names := make([]string, 0, len(pk.types))
		for n := range pk.types {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			g.describe(pk, pk.types[n])
			r.Types++
		}
	}
	r.Restrictions = g.restrictions
	r.External = len(g.declared) - r.Types

	if questions != "" {
		qs, warnings, err := g.merge(questions)
		if err != nil {
			return nil, err
		}
		r.Questions = qs
		r.Warnings = warnings
	}
	if err := g.o.Err(); err != nil {
		return nil, err
	}
	r.Ontology = g.o
	return r, nil
}

type generator struct {
	o            *owl.Ontology
	pkgs         map[string]*pkg
	prefixes     map[string]string // prefix name -> namespace, "" for reserved
	byNamespace  map[string]string // namespace -> prefix name
	declared     map[owl.Class]bool
	restrictions int

	embeds, hasField, contains, mayHave, mayContain owl.ObjectProperty
}

// vocabulary declares the five properties a type's structure is written with.
func (g *generator) vocabulary() {
	o := g.o
	g.embeds = o.ObjectProperty(":embeds")
	g.hasField = o.ObjectProperty(":hasField")
	g.contains = o.ObjectProperty(":contains")
	g.mayHave = o.ObjectProperty(":mayHave")
	g.mayContain = o.ObjectProperty(":mayContain")

	o.DefineObjectProperty(g.embeds).SubPropertyOf(g.hasField).
		Label("embeds").Comment("The subject type embeds a value of the object type as an anonymous field.")
	o.DefineObjectProperty(g.hasField).SubPropertyOf(g.contains).
		Label("has field").Comment("The subject type has a field holding a value of the object type, never nil.")
	o.DefineObjectProperty(g.contains).SubPropertyOf(g.mayContain).Transitive().
		Label("contains").Comment("Every value of the subject type holds a value of the object type somewhere inside it.")
	o.DefineObjectProperty(g.mayHave).SubPropertyOf(g.mayContain).
		Label("may have").Comment("The subject type has a field reaching the object type through a pointer, slice or map, which may be nil or empty.")
	o.DefineObjectProperty(g.mayContain).Transitive().
		Label("may contain").Comment("The subject type's definition reaches the object type, possibly only through fields that may be nil or empty.")
}

// describe writes one type declaration as a class and its restrictions.
func (g *generator) describe(p *pkg, t *typeDecl) {
	c := g.class(p.path, t.name)
	b := g.o.Define(c).Label(g.label(p.path, t.name))
	if t.doc != "" {
		b.Comment(t.doc)
	}

	seen := map[string]bool{}
	restrict := func(prop owl.ObjectProperty, target owl.Class) {
		key := string(prop) + " " + string(target)
		if seen[key] || target == c {
			return
		}
		seen[key] = true
		b.SubClassOf(owl.Some(prop, target))
		g.restrictions++
	}

	st, ok := t.spec.Type.(*ast.StructType)
	if !ok {
		refs := g.refs(p, t.file, t.spec.Type, false)
		if len(refs) == 1 && !refs[0].optional {
			// type List metav1.List: a new name for the same shape.
			b.SubClassOf(refs[0].class)
			g.restrictions++
			return
		}
		for _, r := range refs {
			restrict(g.field(false, r.optional), r.class)
		}
		return
	}
	for _, f := range st.Fields.List {
		embedded := len(f.Names) == 0
		if !embedded && !anyExported(f.Names) {
			continue
		}
		for _, r := range g.refs(p, t.file, f.Type, false) {
			restrict(g.field(embedded, r.optional), r.class)
		}
	}
}

func (g *generator) field(embedded, optional bool) owl.ObjectProperty {
	switch {
	case optional:
		return g.mayHave
	case embedded:
		return g.embeds
	default:
		return g.hasField
	}
}

func anyExported(names []*ast.Ident) bool {
	for _, n := range names {
		if n.IsExported() {
			return true
		}
	}
	return false
}

// refs returns the named types a type expression mentions. Basic types, type
// parameters and unexported types mention nothing a question can ask about.
func (g *generator) refs(p *pkg, f *file, e ast.Expr, optional bool) []ref {
	switch t := e.(type) {
	case *ast.Ident:
		if _, ok := p.types[t.Name]; ok {
			return []ref{{g.class(p.path, t.Name), optional}}
		}
	case *ast.SelectorExpr:
		x, ok := t.X.(*ast.Ident)
		if !ok || !t.Sel.IsExported() {
			return nil
		}
		path, ok := f.imports[x.Name]
		if !ok {
			return nil
		}
		if q, ok := g.pkgs[path]; ok {
			if _, ok := q.types[t.Sel.Name]; !ok {
				return nil // declared in a generated file, or not a type
			}
			return []ref{{g.class(path, t.Sel.Name), optional}}
		}
		c := g.class(path, t.Sel.Name)
		if !g.declared[c] {
			g.declared[c] = true
			g.o.Define(c).Label(g.label(path, t.Sel.Name))
		}
		return []ref{{c, optional}}
	case *ast.StarExpr:
		return g.refs(p, f, t.X, true)
	case *ast.ArrayType:
		return g.refs(p, f, t.Elt, true)
	case *ast.MapType:
		return append(g.refs(p, f, t.Key, true), g.refs(p, f, t.Value, true)...)
	case *ast.ParenExpr:
		return g.refs(p, f, t.X, optional)
	case *ast.IndexExpr:
		return append(g.refs(p, f, t.X, optional), g.refs(p, f, t.Index, true)...)
	case *ast.IndexListExpr:
		out := g.refs(p, f, t.X, optional)
		for _, i := range t.Indices {
			out = append(out, g.refs(p, f, i, true)...)
		}
		return out
	case *ast.StructType:
		var out []ref
		for _, fl := range t.Fields.List {
			out = append(out, g.refs(p, f, fl.Type, optional)...)
		}
		return out
	}
	return nil
}

// class returns the class for a type, registering its package's prefix.
func (g *generator) class(path, name string) owl.Class {
	ns := godoc + path + "#"
	if _, ok := g.byNamespace[ns]; !ok {
		g.prefix(path, ns)
	}
	c := owl.Class(ns + name)
	if _, ok := g.pkgs[path]; ok {
		g.declared[c] = true
	}
	return c
}

func (g *generator) label(path, name string) string {
	return g.byNamespace[godoc+path+"#"] + "." + name
}

var versionElem = regexp.MustCompile(`^v[0-9]+((alpha|beta)[0-9]+)?$`)

// prefix names a package the way Kubernetes code imports it: appsv1 for
// k8s.io/api/apps/v1, metav1 for .../apis/meta/v1, resource for .../api/resource.
func (g *generator) prefix(path, ns string) {
	if g.byNamespace == nil {
		g.byNamespace = map[string]string{}
	}
	elems := strings.Split(path, "/")
	base := elems[len(elems)-1]
	if versionElem.MatchString(base) && len(elems) > 1 {
		base = elems[len(elems)-2] + base
	}
	var b strings.Builder
	for _, r := range base {
		if r < 128 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	base = b.String()
	if base == "" || base[0] >= '0' && base[0] <= '9' {
		base = "p" + base
	}
	name := base
	for n := 2; ; n++ {
		if _, taken := g.prefixes[name]; !taken {
			break
		}
		name = base + strconv.Itoa(n)
	}
	g.prefixes[name] = ns
	g.byNamespace[ns] = name
	g.o.Prefix(name, owl.IRI(ns))
}

// merge adds the questions document's axioms and prefixes to the ontology and
// returns the classes it declares. A class the questions mention that is
// neither declared there nor a type of the code is reported, since a question
// about a type that is not there silently answers nothing.
func (g *generator) merge(path string) ([]owl.Class, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	q, err := owl.ParseFunctional(f)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	for _, name := range q.Prefixes.Names() {
		ns, _ := q.Prefixes.Namespace(name)
		if _, taken := g.prefixes[name]; !taken {
			g.prefixes[name] = string(ns)
			g.o.Prefix(name, ns)
		}
	}
	var classes []owl.Class
	own := map[owl.Class]bool{}
	for ax := range q.All() {
		if d, ok := ax.(owl.Declaration); ok {
			if c, ok := d.Entity.(owl.Class); ok && !own[c] {
				own[c] = true
				classes = append(classes, c)
			}
		}
		g.o.Add(ax)
	}
	var warnings []string
	for _, e := range q.Signature() {
		c, ok := e.(owl.Class)
		if !ok || own[c] || g.declared[c] || c == owl.Thing || c == owl.Nothing {
			continue
		}
		warnings = append(warnings, fmt.Sprintf("%s: %s is not a type in the code read", path, c))
	}
	return classes, warnings, nil
}

// Answer is what classification placed under one question class.
type Answer struct {
	Question owl.Class
	Members  []owl.Class // the code's types, sorted by IRI
	Includes []owl.Class // other question classes it subsumes
}

// Answers classifies r's ontology. It returns the classes the questions' rules
// make unsatisfiable, and an answer per question class in the questions'
// order. An unsatisfiable class is entailed under every class, so it is left
// out of the answers and reported once as a violation instead.
func Answers(r *Result) (violations []owl.Class, answers []Answer) {
	c := el.Classify(r.Ontology, el.WithoutExplanations())
	isQuestion := map[owl.Class]bool{}
	for _, q := range r.Questions {
		isQuestion[q] = true
	}
	unsat := map[owl.Class]bool{}
	for _, cl := range c.UnsatisfiableClasses() {
		unsat[cl] = true
		violations = append(violations, cl)
	}
	for _, q := range r.Questions {
		a := Answer{Question: q}
		for _, s := range c.SubClassesOf(q) {
			switch {
			case unsat[s]:
			case isQuestion[s]:
				a.Includes = append(a.Includes, s)
			default:
				a.Members = append(a.Members, s)
			}
		}
		answers = append(answers, a)
	}
	return violations, answers
}

// PrintAnswers writes Answers as text, naming classes by their prefixes.
func PrintAnswers(w io.Writer, r *Result) {
	violations, answers := Answers(r)
	name := func(cl owl.Class) string {
		if s, ok := r.Ontology.Prefixes.Compact(owl.IRI(cl)); ok {
			return s
		}
		return "<" + string(cl) + ">"
	}
	if len(violations) > 0 {
		fmt.Fprintf(w, "violations (%d): unsatisfiable under the questions' rules\n", len(violations))
		for _, cl := range violations {
			fmt.Fprintf(w, "  %s\n", name(cl))
		}
		fmt.Fprintln(w)
	}
	for _, a := range answers {
		fmt.Fprintf(w, "%s (%d)", name(a.Question), len(a.Members))
		if len(a.Includes) > 0 {
			names := make([]string, len(a.Includes))
			for i, cl := range a.Includes {
				names[i] = name(cl)
			}
			fmt.Fprintf(w, "  includes %s", strings.Join(names, ", "))
		}
		fmt.Fprintln(w)
		for _, m := range a.Members {
			fmt.Fprintf(w, "  %s\n", name(m))
		}
		fmt.Fprintln(w)
	}
}

// load parses the packages under each root, skipping testdata, vendor, and
// dot- or underscore-prefixed directories as the go tool does, and files that
// carry the generated-code marker. It returns the packages by import path and
// the number of generated files skipped.
func load(roots []string) (map[string]*pkg, int, error) {
	pkgs := map[string]*pkg{}
	fset := token.NewFileSet()
	generated := 0
	for _, root := range roots {
		root, err := filepath.Abs(root)
		if err != nil {
			return nil, 0, err
		}
		modDir, modPath, err := findModule(root)
		if err != nil {
			return nil, 0, err
		}
		err = filepath.WalkDir(root, func(dir string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return err
			}
			name := d.Name()
			if dir != root && (name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			bp, err := build.ImportDir(dir, 0)
			if err != nil {
				return nil // no buildable Go files here
			}
			rel, err := filepath.Rel(modDir, dir)
			if err != nil {
				return err
			}
			path := modPath
			if rel != "." {
				path += "/" + filepath.ToSlash(rel)
			}
			if _, done := pkgs[path]; done {
				return nil
			}
			p := &pkg{path: path, name: bp.Name, types: map[string]*typeDecl{}}
			for _, fn := range bp.GoFiles {
				f, err := parser.ParseFile(fset, filepath.Join(dir, fn), nil, parser.ParseComments)
				if err != nil {
					return err
				}
				if ast.IsGenerated(f) {
					generated++
					continue
				}
				p.files = append(p.files, &file{ast: f})
			}
			pkgs[path] = p
			return nil
		})
		if err != nil {
			return nil, 0, err
		}
	}
	for _, p := range pkgs {
		for _, f := range p.files {
			f.imports = imports(f.ast, pkgs)
			collect(p, f)
		}
	}
	return pkgs, generated, nil
}

// imports maps each name a file can qualify a type with to its import path. An
// unnamed import of a package that was not read is named by its last path
// element, which is the convention and true of everything Kubernetes imports.
func imports(f *ast.File, pkgs map[string]*pkg) map[string]string {
	m := map[string]string{}
	for _, spec := range f.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		var name string
		switch {
		case spec.Name != nil:
			name = spec.Name.Name
		case pkgs[path] != nil:
			name = pkgs[path].name
		default:
			name = path[strings.LastIndex(path, "/")+1:]
		}
		if name != "_" && name != "." {
			m[name] = path
		}
	}
	return m
}

// collect records a file's exported, non-alias type declarations.
func collect(p *pkg, f *file) {
	for _, decl := range f.ast.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, s := range gd.Specs {
			ts := s.(*ast.TypeSpec)
			if !ts.Name.IsExported() || ts.Assign.IsValid() {
				continue
			}
			text := ts.Doc.Text()
			if text == "" && len(gd.Specs) == 1 {
				text = gd.Doc.Text()
			}
			p.types[ts.Name.Name] = &typeDecl{name: ts.Name.Name, spec: ts, doc: firstSentence(text), file: f}
		}
	}
}

func firstSentence(text string) string {
	var pr doc.Package
	return strings.TrimSpace(pr.Synopsis(text))
}

// findModule returns the directory and path of the module enclosing dir.
func findModule(dir string) (string, string, error) {
	for d := dir; ; {
		data, err := os.ReadFile(filepath.Join(d, "go.mod"))
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if f := strings.Fields(line); len(f) >= 2 && f[0] == "module" {
					return d, strings.Trim(f[1], `"`), nil
				}
			}
			return "", "", fmt.Errorf("%s: no module line", filepath.Join(d, "go.mod"))
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", "", fmt.Errorf("%s: not inside a Go module", dir)
		}
		d = parent
	}
}
