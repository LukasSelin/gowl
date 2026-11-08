// Command design-gate is a Claude Code PreToolUse hook that keeps a plan ahead
// of the code. It refuses an Edit or Write that would add an exported Go type
// no design/*.ofn file names, and tells the agent to plan the type first.
//
// It gates types and nothing else. A bug fix adds none, so it never notices one.
// Fields, imports and packages are left to "gowl conform" at the end of the turn:
// telling whether a field is one gocode records takes type-checking the module,
// which is too slow to do before every edit.
//
// It only ever stops an edit it is sure about. Anything it cannot read lets the
// edit through, and the Stop hook still sees the result. Set GOWL_DESIGN_GATE=off
// to turn it off.
package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"gowl/owl"
)

type hookInput struct {
	Cwd       string `json:"cwd"`
	ToolInput struct {
		FilePath   string `json:"file_path"`
		Content    string `json:"content"`
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
	} `json:"tool_input"`
	ToolName string `json:"tool_name"`
}

func main() {
	os.Exit(run(os.Stdin, os.Stderr))
}

func run(stdin io.Reader, stderr io.Writer) int {
	if os.Getenv("GOWL_DESIGN_GATE") == "off" {
		return 0
	}
	var in hookInput
	if err := json.NewDecoder(stdin).Decode(&in); err != nil {
		return 0
	}
	unplanned := check(in)
	if len(unplanned) == 0 {
		return 0
	}
	fmt.Fprintf(stderr, `design-gate: this edit adds exported Go types that no design/*.ofn file names:
  %s

Plan them before writing them. Use the design-first skill, or add each one to a
design/<change>.ofn file under the names gowl gocode writes, for example:

  Declaration(Class(:%s))

Then check the plan and retry this edit:

  go run ./cmd/gowl plan -fail-on warning design/<change>.ofn

If the module already has a type that does this job, use it instead.
`, strings.Join(unplanned, ", "), unplanned[0])
	return 2
}

// check returns the exported types the edit would add and no plan names, as
// package.Type, the way gocode names them.
func check(in hookInput) []string {
	path := in.ToolInput.FilePath
	if !filepath.IsAbs(path) {
		path = filepath.Join(in.Cwd, path)
	}
	path = filepath.Clean(path)
	if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
		return nil
	}
	root, ok := moduleRoot(filepath.Dir(path))
	if !ok || !described(root, path) {
		return nil
	}

	var old []byte
	data, err := os.ReadFile(path)
	if err == nil {
		old = data
	} else if !os.IsNotExist(err) {
		return nil
	}
	proposed, ok := afterEdit(in, string(old))
	if !ok {
		return nil
	}

	pkg, added := parseTypes(path, proposed)
	if pkg == "" || len(added) == 0 {
		return nil
	}
	// The whole package, not just this file: moving a type adds nothing.
	existing := packageTypes(filepath.Dir(path), filepath.Base(path))
	if _, here := parseTypes(path, string(old)); here != nil {
		existing = append(existing, here...)
	}

	planned := plannedClasses(root)
	var out []string
	for _, name := range added {
		q := pkg + "." + name
		if !slices.Contains(existing, name) && !planned[q] {
			out = append(out, q)
		}
	}
	sort.Strings(out)
	return slices.Compact(out)
}

// afterEdit returns what the file will hold once the tool has run. Writing out
// the edit, rather than scanning new_string, means the parser sees a whole file
// and a field named like a type cannot be mistaken for one.
func afterEdit(in hookInput, old string) (string, bool) {
	if in.ToolName == "Write" {
		return in.ToolInput.Content, true
	}
	t := in.ToolInput
	if t.OldString == "" || !strings.Contains(old, t.OldString) {
		return "", false // the tool itself is about to fail
	}
	if t.ReplaceAll {
		return strings.ReplaceAll(old, t.OldString, t.NewString), true
	}
	return strings.Replace(old, t.OldString, t.NewString, 1), true
}

// moduleRoot finds the directory holding the go.mod that governs dir.
func moduleRoot(dir string) (string, bool) {
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, true
		}
		up := filepath.Dir(dir)
		if up == dir {
			return "", false
		}
		dir = up
	}
}

// described reports whether gocode would read path when describing the module
// at root. It skips the same directories: testdata, and anything beginning with
// a dot or an underscore.
func described(root, path string) bool {
	rel, err := filepath.Rel(root, filepath.Dir(path))
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if part == ".." || part == "testdata" || strings.HasPrefix(part, ".") || strings.HasPrefix(part, "_") {
			return false
		}
	}
	return true
}

// parseTypes returns a file's package name and exported types. A syntax error
// somewhere else in the file still leaves the declarations before it readable.
func parseTypes(path, src string) (string, []string) {
	if src == "" {
		return "", nil
	}
	f, _ := parser.ParseFile(token.NewFileSet(), path, src, parser.SkipObjectResolution)
	if f == nil || f.Name == nil {
		return "", nil
	}
	return f.Name.Name, exportedTypes(f)
}

// exportedTypes lists the file's exported named types, leaving out aliases,
// which gocode does not describe either.
func exportedTypes(f *ast.File) []string {
	var out []string
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts := spec.(*ast.TypeSpec)
			if ts.Name.IsExported() && !ts.Assign.IsValid() {
				out = append(out, ts.Name.Name)
			}
		}
	}
	return out
}

// packageTypes lists the exported types of the non-test files in dir, other
// than the one being edited.
func packageTypes(dir, except string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || name == except || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		_, types := parseTypes(filepath.Join(dir, name), string(data))
		out = append(out, types...)
	}
	return out
}

// plannedClasses reads every plan under root/design and returns the local names
// of their classes. It does not ask which plans belong to this change: a type an
// older plan named was already agreed on, and "gowl conform" is what holds a
// change to its own plan.
func plannedClasses(root string) map[string]bool {
	out := map[string]bool{}
	files, _ := filepath.Glob(filepath.Join(root, "design", "*.ofn"))
	for _, file := range files {
		f, err := os.Open(file)
		if err != nil {
			continue
		}
		plan, err := owl.ParseFunctional(f)
		f.Close()
		if err != nil {
			continue // the ontology-check hook reports a plan that does not parse
		}
		for _, c := range plan.Classes() {
			iri := string(c.IRI())
			if i := strings.LastIndexByte(iri, '#'); i >= 0 {
				out[iri[i+1:]] = true
			}
		}
	}
	return out
}
