package gocode

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"sort"
	"strings"
)

// interfaceMethods returns the methods an exported interface declares, each as
// a normalized signature such as "Kind() owl.Kind", sorted. They are its
// contract.
//
// Unexported methods count: a method like owl.Axiom's isAxiom seals the
// interface, so that only its own package can implement it, and that is part
// of what it promises. An embedded interface of the module that has a class of
// its own is left out, because satisfying it is already a SubClassOf edge, and
// listing its methods again would make one change to it a change to every
// interface that embeds it. Any other embedded interface contributes its
// methods, since nothing else records them: one from outside the module, such
// as io.Reader, and one of the module's that is embedded with type arguments,
// such as Store[int], which is a different interface for each argument and a
// superclass for none.
func interfaceMethods(tn *types.TypeName, byObj map[*types.TypeName]*typeInfo) []string {
	iface, ok := tn.Type().Underlying().(*types.Interface)
	if !ok {
		return nil
	}
	var methods []*types.Func
	for i := 0; i < iface.NumExplicitMethods(); i++ {
		methods = append(methods, iface.ExplicitMethod(i))
	}
	for i := 0; i < iface.NumEmbeddeds(); i++ {
		e := iface.EmbeddedType(i)
		if named, ok := e.(*types.Named); ok && named.TypeArgs().Len() == 0 {
			if _, own := byObj[named.Obj()]; own {
				continue
			}
		}
		if inner, ok := e.Underlying().(*types.Interface); ok {
			for j := 0; j < inner.NumMethods(); j++ {
				methods = append(methods, inner.Method(j))
			}
		}
	}

	keep := map[string]bool{}
	if named, ok := tn.Type().(*types.Named); ok {
		for i := 0; i < named.TypeParams().Len(); i++ {
			keep[named.TypeParams().At(i).Obj().Name()] = true
		}
	}
	var out []string
	for _, m := range methods {
		out = append(out, signatureOf(m, tn.Pkg().Name(), keep, nil))
	}
	sort.Strings(out)
	return dedupe(out)
}

// declaredMethods returns the exported methods a type other than an interface
// declares, on itself or on its pointer, each as a normalized signature,
// sorted. They are what the type offers. A method it only gets from an
// embedded field is not listed: the field is recorded, and the method is that
// type's.
func declaredMethods(tn *types.TypeName) []string {
	named, ok := tn.Type().(*types.Named)
	if !ok {
		return nil
	}
	var out []string
	for i := 0; i < named.NumMethods(); i++ {
		m := named.Method(i)
		if !m.Exported() {
			continue
		}
		// A method names its receiver's type parameters itself, and may call
		// them something else than the type's declaration does. They are
		// written the type's way, so that the methods of one type agree and a
		// design has one set of names to use.
		keep, subst := map[string]bool{}, map[string]string{}
		declared := named.TypeParams()
		for params, j := m.Signature().RecvTypeParams(), 0; j < params.Len(); j++ {
			own := params.At(j).Obj().Name()
			name := own
			if j < declared.Len() {
				name = declared.At(j).Obj().Name()
			}
			subst[own] = name
			keep[name] = true
		}
		out = append(out, signatureOf(m, tn.Pkg().Name(), keep, subst))
	}
	sort.Strings(out)
	return dedupe(out)
}

// packageFuncs returns the functions a package exports, each as a normalized
// signature, sorted. With the methods of its types they are what it offers.
func packageFuncs(p *types.Package) []string {
	scope := p.Scope()
	var out []string
	for _, name := range scope.Names() {
		if f, ok := scope.Lookup(name).(*types.Func); ok && f.Exported() {
			out = append(out, signatureOf(f, p.Name(), nil, nil))
		}
	}
	sort.Strings(out)
	return out
}

// signatureOf writes a function or method declared in the package named pkg as
// a normalized signature. keep and subst are the type parameters of the type a
// method is on: the names to leave bare, and what the method's own names for
// them are to be written as.
func signatureOf(f *types.Func, pkg string, keep map[string]bool, subst map[string]string) string {
	qualify := func(p *types.Package) string { return p.Name() }
	raw := f.Name() + strings.TrimPrefix(types.TypeString(f.Type(), qualify), "func")
	sig, err := writeSignature(raw, sigWriter{pkg: pkg, keep: keep, subst: subst})
	if err != nil {
		return raw // go/types wrote something the normalizer cannot read; keep it as it is
	}
	return sig
}

// The kinds a named type can be, as the annotation :kind records them. Only
// the first two change what a design can say about a type: a struct has
// fields, and an interface's methods are a contract.
const (
	kindStruct    = "struct"
	kindInterface = "interface"
)

// typeKind says what a named type is underneath.
func typeKind(t types.Type) string {
	switch t.Underlying().(type) {
	case *types.Struct:
		return kindStruct
	case *types.Interface:
		return kindInterface
	case *types.Basic:
		return "basic"
	case *types.Slice:
		return "slice"
	case *types.Array:
		return "array"
	case *types.Map:
		return "map"
	case *types.Chan:
		return "chan"
	case *types.Signature:
		return "func"
	case *types.Pointer:
		return "pointer"
	}
	return "other"
}

func dedupe(sorted []string) []string {
	var out []string
	for i, s := range sorted {
		if i == 0 || s != sorted[i-1] {
			out = append(out, s)
		}
	}
	return out
}

// methodName is the name a signature such as "Price(store.Item) int" or
// "Map[T any]([]T) []T" starts with.
func methodName(sig string) string {
	sig = strings.TrimSpace(sig)
	if i := strings.IndexAny(sig, "(["); i >= 0 {
		sig = sig[:i]
	}
	return strings.TrimSpace(sig)
}

// errNotSignature is what is wrong with anything that does not start the way
// a signature does.
var errNotSignature = errors.New("want Name(parameters) results")

// parseSignature reads "Name(parameters) results", with type parameters after
// the name if it has any, the way Go reads a function declaration that has no
// body. The error says only what is wrong: whoever reports it has the
// signature, and says whose it is.
func parseSignature(sig string) (string, *ast.FuncType, error) {
	sig = strings.TrimSpace(sig)
	name := methodName(sig)
	if !token.IsIdentifier(name) || !strings.Contains(sig, "(") {
		return "", nil, errNotSignature
	}
	f, err := parser.ParseFile(token.NewFileSet(), "", "package p; func "+sig, parser.SkipObjectResolution)
	if err != nil {
		// Where in the wrapped source the parser stopped helps nobody.
		var list scanner.ErrorList
		if errors.As(err, &list) && len(list) > 0 {
			return "", nil, errors.New(list[0].Msg)
		}
		return "", nil, err
	}
	if len(f.Decls) != 1 {
		return "", nil, errors.New("want one signature")
	}
	fd, ok := f.Decls[0].(*ast.FuncDecl)
	if !ok || fd.Recv != nil || fd.Body != nil {
		return "", nil, errNotSignature
	}
	return name, fd.Type, nil
}

// normalizeSignature rewrites the signature of a method or a function,
// "Name(parameters) results", into the one form both the code and a design are
// compared in, so that a design can be written as a person would write it:
//
//   - parameter and result names are dropped, at every depth;
//   - interface{} becomes any, byte uint8 and rune int32;
//   - a bare name that is not built in, such as IRI written in package owl,
//     is qualified with pkg, except the names in keep, which are the type
//     parameters of the type the method is on, and the signature's own;
//   - type parameters are written one constraint to a name;
//   - spacing is Go's.
//
// "Write(p []byte) (n int, err error)" in package io and
// "Write([]uint8) (int, error)" come out the same. Aliases are not resolved,
// and an array length is kept as written.
func normalizeSignature(sig, pkg string, keep map[string]bool) (string, error) {
	return writeSignature(sig, sigWriter{pkg: pkg, keep: keep})
}

// writeSignature reads a signature and writes it the way w is set up to.
func writeSignature(sig string, w sigWriter) (string, error) {
	name, ft, err := parseSignature(sig)
	if err != nil {
		return "", err
	}
	w.b.WriteString(name)
	w.typeParams(ft.TypeParams)
	w.funcType(ft)
	if w.err != nil {
		return "", w.err
	}
	return w.b.String(), nil
}

// bareNames lists the unqualified names a signature uses that are neither
// built in nor type parameters of its own. In a signature gocode wrote, those
// can only be the type parameters of the type a method is on, so they are
// what a design's signature has to leave bare too. In one a design wrote, they
// are those and the types of the design's own package.
func bareNames(sig string) map[string]bool {
	out := map[string]bool{}
	_, _ = writeSignature(sig, sigWriter{bare: out})
	return out
}

// sameSignature reports whether two normalized signatures are the same but
// for what they call the type parameters they declare, which is the writer's
// own, as what a signature calls its parameters is: Map[T any]([]T) and Map[E
// any]([]E) are one function.
//
// The parameters of the type a method is on are another matter. The method
// does not declare them, so nothing in its signature says which is which, and
// they are compared by name: the name the type's declaration gives each,
// which is how Build writes them whatever the method calls them.
func sameSignature(a, b string) bool {
	return a == b || positional(a) == positional(b)
}

// positional rewrites a normalized signature with each type parameter it
// declares named after its place in the list.
func positional(sig string) string {
	out, err := writeSignature(sig, sigWriter{rename: map[string]string{}})
	if err != nil {
		return sig
	}
	return out
}

func builtinType(name string) bool {
	_, ok := types.Universe.Lookup(name).(*types.TypeName)
	return ok
}

// sigWriter prints type expressions in the normalized form.
type sigWriter struct {
	b    strings.Builder
	pkg  string
	keep map[string]bool
	// bare, when set, collects the names that would be qualified with pkg.
	bare map[string]bool
	// subst names the type parameters of the type a method is on as the type's
	// declaration does, whatever the method's receiver calls them.
	subst map[string]string
	// rename, when set, names each type parameter the signature declares
	// after its place in the list, and qualifies nothing, because the
	// signature being written is normalized already.
	rename map[string]string
	err    error
}

// placed is the name rename gives a type parameter the signature declares:
// its place in the list.
func (w *sigWriter) placed(name string) string {
	if _, ok := w.rename[name]; !ok {
		w.rename[name] = fmt.Sprintf("$%d", len(w.rename)+1)
	}
	return w.rename[name]
}

// part writes with a writer of its own that names things as w does, and
// returns what was written.
func (w *sigWriter) part(write func(*sigWriter)) string {
	p := sigWriter{pkg: w.pkg, keep: w.keep, bare: w.bare, subst: w.subst, rename: w.rename}
	write(&p)
	if p.err != nil && w.err == nil {
		w.err = p.err
	}
	return p.b.String()
}

// typeParams writes a signature's type parameters, one constraint to a name,
// so that [K, V any] is [K any, V any], and from there on leaves their names
// bare.
func (w *sigWriter) typeParams(list *ast.FieldList) {
	if list == nil || len(list.List) == 0 {
		return
	}
	// The names are in scope for the whole list, constraints included. The
	// caller's set is left as it was.
	keep := map[string]bool{}
	for name := range w.keep {
		keep[name] = true
	}
	for _, f := range list.List {
		for _, n := range f.Names {
			keep[n.Name] = true
			if w.rename != nil {
				w.placed(n.Name)
			}
		}
	}
	w.keep = keep

	w.b.WriteByte('[')
	first := true
	for _, f := range list.List {
		for _, n := range f.Names {
			if !first {
				w.b.WriteString(", ")
			}
			first = false
			if w.rename != nil {
				w.b.WriteString(w.placed(n.Name) + " ")
			} else {
				w.b.WriteString(n.Name + " ")
			}
			w.expr(constraint(f.Type))
		}
	}
	w.b.WriteByte(']')
}

// constraint is a type parameter's constraint without the interface Go lets
// a writer leave out: interface{ ~int | ~string } constrains as ~int |
// ~string does, and go/types writes whichever the code has.
func constraint(e ast.Expr) ast.Expr {
	if it, ok := e.(*ast.InterfaceType); ok && it.Methods != nil && len(it.Methods.List) == 1 {
		if only := it.Methods.List[0]; len(only.Names) == 0 {
			return only.Type
		}
	}
	return e
}

func (w *sigWriter) funcType(ft *ast.FuncType) {
	w.b.WriteByte('(')
	w.fields(ft.Params)
	w.b.WriteByte(')')
	if ft.Results == nil || len(ft.Results.List) == 0 {
		return
	}
	n := 0
	for _, f := range ft.Results.List {
		n += max(1, len(f.Names))
	}
	w.b.WriteByte(' ')
	if n == 1 {
		w.expr(ft.Results.List[0].Type)
		return
	}
	w.b.WriteByte('(')
	w.fields(ft.Results)
	w.b.WriteByte(')')
}

// fields writes a parameter or result list without names, repeating the type
// once per name: (a, b int) is (int, int).
func (w *sigWriter) fields(list *ast.FieldList) {
	if list == nil {
		return
	}
	first := true
	for _, f := range list.List {
		for i := 0; i < max(1, len(f.Names)); i++ {
			if !first {
				w.b.WriteString(", ")
			}
			first = false
			w.expr(f.Type)
		}
	}
}

func (w *sigWriter) expr(e ast.Expr) {
	switch x := e.(type) {
	case *ast.Ident:
		name := x.Name
		if to, ok := w.subst[name]; ok {
			name = to
		}
		place, declared := w.rename[name]
		switch {
		case declared:
			w.b.WriteString(place)
		case name == "byte":
			w.b.WriteString("uint8")
		case name == "rune":
			w.b.WriteString("int32")
		case builtinType(name) || w.keep[name] || w.rename != nil:
			w.b.WriteString(name)
		default:
			if w.bare != nil {
				w.bare[name] = true
			}
			w.b.WriteString(w.pkg + "." + name)
		}
	case *ast.SelectorExpr:
		pkg, ok := x.X.(*ast.Ident)
		if !ok {
			w.fail(e)
			return
		}
		w.b.WriteString(pkg.Name + "." + x.Sel.Name)
	case *ast.ParenExpr:
		w.expr(x.X)
	case *ast.StarExpr:
		w.b.WriteByte('*')
		w.expr(x.X)
	case *ast.Ellipsis:
		w.b.WriteString("...")
		w.expr(x.Elt)
	case *ast.ArrayType:
		w.b.WriteByte('[')
		if x.Len != nil {
			w.b.WriteString(types.ExprString(x.Len))
		}
		w.b.WriteByte(']')
		w.expr(x.Elt)
	case *ast.MapType:
		w.b.WriteString("map[")
		w.expr(x.Key)
		w.b.WriteByte(']')
		w.expr(x.Value)
	case *ast.ChanType:
		switch x.Dir {
		case ast.SEND:
			w.b.WriteString("chan<- ")
		case ast.RECV:
			w.b.WriteString("<-chan ")
		default:
			w.b.WriteString("chan ")
			// chan (<-chan T) needs its parentheses to keep its meaning.
			if inner, ok := unparen(x.Value).(*ast.ChanType); ok && inner.Dir == ast.RECV {
				w.b.WriteByte('(')
				w.expr(inner)
				w.b.WriteByte(')')
				return
			}
		}
		w.expr(x.Value)
	case *ast.FuncType:
		w.b.WriteString("func")
		w.funcType(x)
	case *ast.InterfaceType:
		if x.Methods == nil || len(x.Methods.List) == 0 {
			w.b.WriteString("any")
			return
		}
		// An interface is the same whatever order its members are written
		// in, so they are sorted: its methods, then what it embeds. That is
		// also the order go/types writes the code's in.
		var methods, embedded []string
		for _, m := range x.Methods.List {
			if ft, ok := m.Type.(*ast.FuncType); ok && len(m.Names) == 1 {
				methods = append(methods, w.part(func(p *sigWriter) {
					p.b.WriteString(m.Names[0].Name)
					p.funcType(ft)
				}))
			} else {
				embedded = append(embedded, w.part(func(p *sigWriter) { p.expr(m.Type) }))
			}
		}
		sort.Strings(methods)
		sort.Strings(embedded)
		w.b.WriteString("interface{" + strings.Join(append(methods, embedded...), "; ") + "}")
	case *ast.StructType:
		// A struct's field names are part of its type, so they stay.
		w.b.WriteString("struct{")
		first := true
		for _, f := range x.Fields.List {
			names := f.Names
			if len(names) == 0 {
				names = []*ast.Ident{nil}
			}
			for _, n := range names {
				if !first {
					w.b.WriteString("; ")
				}
				first = false
				if n != nil {
					w.b.WriteString(n.Name + " ")
				}
				w.expr(f.Type)
				if f.Tag != nil {
					w.b.WriteString(" " + f.Tag.Value)
				}
			}
		}
		w.b.WriteByte('}')
	case *ast.IndexExpr:
		w.expr(x.X)
		w.b.WriteByte('[')
		w.expr(x.Index)
		w.b.WriteByte(']')
	case *ast.IndexListExpr:
		w.expr(x.X)
		w.b.WriteByte('[')
		for i, idx := range x.Indices {
			if i > 0 {
				w.b.WriteString(", ")
			}
			w.expr(idx)
		}
		w.b.WriteByte(']')
	case *ast.UnaryExpr:
		// ~int, in a constraint.
		if x.Op != token.TILDE {
			w.fail(e)
			return
		}
		w.b.WriteByte('~')
		w.expr(x.X)
	case *ast.BinaryExpr:
		// ~int | ~string, in a constraint.
		if x.Op != token.OR {
			w.fail(e)
			return
		}
		w.expr(x.X)
		w.b.WriteString(" | ")
		w.expr(x.Y)
	default:
		w.fail(e)
	}
}

func (w *sigWriter) fail(e ast.Expr) {
	if w.err == nil {
		w.err = fmt.Errorf("cannot read the type %s", types.ExprString(e))
	}
}

func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}
