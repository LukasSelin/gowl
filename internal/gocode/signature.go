package gocode

import (
	"fmt"
	"go/ast"
	"go/parser"
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
// interface that embeds it. Any other embedded interface, such as io.Reader,
// contributes its methods, since nothing else records them.
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
		if named, ok := e.(*types.Named); ok {
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
	qualify := func(p *types.Package) string { return p.Name() }
	var out []string
	for _, m := range methods {
		raw := m.Name() + strings.TrimPrefix(types.TypeString(m.Type(), qualify), "func")
		sig, err := normalizeSignature(raw, tn.Pkg().Name(), keep)
		if err != nil {
			sig = raw // go/types wrote something the normalizer cannot read; keep it as it is
		}
		out = append(out, sig)
	}
	sort.Strings(out)
	return dedupe(out)
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

// methodName is the name a signature such as "Price(store.Item) int" starts
// with.
func methodName(sig string) string {
	name, _, _ := strings.Cut(strings.TrimSpace(sig), "(")
	return strings.TrimSpace(name)
}

// normalizeSignature rewrites a method signature, "Name(parameters) results",
// into the one form both the code and a plan are compared in, so that a plan
// can be written as a person would write it:
//
//   - parameter and result names are dropped, at every depth;
//   - interface{} becomes any, byte uint8 and rune int32;
//   - a bare name that is not built in, such as IRI written in package owl,
//     is qualified with pkg, except the names in keep, which are the
//     interface's type parameters;
//   - spacing is Go's.
//
// "Write(p []byte) (n int, err error)" in package io and
// "Write([]uint8) (int, error)" come out the same. Aliases are not resolved,
// and an array length is kept as written.
func normalizeSignature(sig, pkg string, keep map[string]bool) (string, error) {
	sig = strings.TrimSpace(sig)
	name := methodName(sig)
	open := strings.Index(sig, "(")
	if open < 0 || !token.IsIdentifier(name) {
		return "", fmt.Errorf("%q is not a method signature: want Name(parameters) results", sig)
	}
	expr, err := parser.ParseExpr("func" + sig[open:])
	if err != nil {
		return "", fmt.Errorf("%q is not a method signature: %v", sig, err)
	}
	ft, ok := expr.(*ast.FuncType)
	if !ok {
		return "", fmt.Errorf("%q is not a method signature", sig)
	}
	w := sigWriter{pkg: pkg, keep: keep}
	w.b.WriteString(name)
	w.funcType(ft)
	if w.err != nil {
		return "", fmt.Errorf("%q: %v", sig, w.err)
	}
	return w.b.String(), nil
}

// bareNames lists the unqualified names a normalized signature uses that are
// not built in. In a signature gocode wrote, those can only be the interface's
// type parameters, so they are what a plan's signature has to leave bare too.
func bareNames(sig string) map[string]bool {
	out := map[string]bool{}
	open := strings.Index(sig, "(")
	if open < 0 {
		return out
	}
	expr, err := parser.ParseExpr("func" + sig[open:])
	if ft, ok := expr.(*ast.FuncType); err == nil && ok {
		w := sigWriter{bare: out}
		w.funcType(ft)
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
	err  error
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
		switch {
		case x.Name == "byte":
			w.b.WriteString("uint8")
		case x.Name == "rune":
			w.b.WriteString("int32")
		case builtinType(x.Name) || w.keep[x.Name]:
			w.b.WriteString(x.Name)
		default:
			if w.bare != nil {
				w.bare[x.Name] = true
			}
			w.b.WriteString(w.pkg + "." + x.Name)
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
		w.b.WriteString("interface{")
		for i, m := range x.Methods.List {
			if i > 0 {
				w.b.WriteString("; ")
			}
			if ft, ok := m.Type.(*ast.FuncType); ok && len(m.Names) == 1 {
				w.b.WriteString(m.Names[0].Name)
				w.funcType(ft)
			} else {
				w.expr(m.Type)
			}
		}
		w.b.WriteByte('}')
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
