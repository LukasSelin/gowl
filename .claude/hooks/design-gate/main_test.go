package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const storeSrc = `// Package store keeps stock.
package store

// Item is something in stock.
type Item struct{ Name string }

// Count returns one.
func Count() int { return 1 }
`

const plan = `Prefix(:=<https://example.com/store#>)
Ontology(<https://example.com/store/design/holding>
    Declaration(Class(:store.Holding))
)
`

// module writes a small module and returns its root.
func module(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	put(t, root, "go.mod", "module example.com/store\n\ngo 1.21\n")
	put(t, root, "store/store.go", storeSrc)
	return root
}

func put(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func edit(root, name, old, new string) hookInput {
	var in hookInput
	in.ToolName = "Edit"
	in.ToolInput.FilePath = filepath.Join(root, filepath.FromSlash(name))
	in.ToolInput.OldString = old
	in.ToolInput.NewString = new
	return in
}

func write(root, name, content string) hookInput {
	var in hookInput
	in.ToolName = "Write"
	in.ToolInput.FilePath = filepath.Join(root, filepath.FromSlash(name))
	in.ToolInput.Content = content
	return in
}

// addAfterItem is an Edit that puts decl after the Item type.
func addAfterItem(root, decl string) hookInput {
	return edit(root, "store/store.go", "type Item struct{ Name string }\n", "type Item struct{ Name string }\n\n"+decl+"\n")
}

func TestCheck(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   func(root string) hookInput
		want string
	}{
		{"new exported type", func(r string) hookInput { return addAfterItem(r, "type Holding struct{}") }, "store.Holding"},
		{"two new types", func(r string) hookInput { return addAfterItem(r, "type Memo struct{}\ntype Note struct{}") }, "store.Memo store.Note"},
		{"grouped types", func(r string) hookInput { return addAfterItem(r, "type (\n\tMemo struct{}\n\tnote struct{}\n)") }, "store.Memo"},
		{"generic type", func(r string) hookInput { return addAfterItem(r, "type Box[T any] struct{ V T }") }, "store.Box"},
		{"interface", func(r string) hookInput { return addAfterItem(r, "type Priced interface{ Price() int }") }, "store.Priced"},
		{"defined from a basic type", func(r string) hookInput { return addAfterItem(r, "type ID string") }, "store.ID"},
		{"new file in the package", func(r string) hookInput {
			return write(r, "store/memo.go", "package store\n\ntype Memo struct{}\n")
		}, "store.Memo"},
		{"new package", func(r string) hookInput {
			return write(r, "billing/billing.go", "package billing\n\ntype Invoice struct{}\n")
		}, "billing.Invoice"},
		{"package name differs from its directory", func(r string) hookInput {
			return write(r, "cmd/tool/main.go", "package main\n\ntype Report struct{}\n")
		}, "main.Report"},

		{"unexported type", func(r string) hookInput { return addAfterItem(r, "type holding struct{}") }, ""},
		{"alias", func(r string) hookInput { return addAfterItem(r, "type Stock = Item") }, ""},
		{"a bug fix", func(r string) hookInput {
			return edit(r, "store/store.go", "return 1", "return 2")
		}, ""},
		{"a new function", func(r string) hookInput { return addAfterItem(r, "func Other() int { return 2 }") }, ""},
		{"a field named like a type", func(r string) hookInput {
			return edit(r, "store/store.go", "type Item struct{ Name string }", "type Item struct {\n\tName string\n\tMeta struct{}\n}")
		}, ""},
		{"rewriting a file keeps its types", func(r string) hookInput {
			return write(r, "store/store.go", "package store\n\n// Item changed.\ntype Item struct{ Label string }\n")
		}, ""},
		{"a test file", func(r string) hookInput {
			return write(r, "store/store_test.go", "package store\n\ntype Fixture struct{}\n")
		}, ""},
		{"testdata", func(r string) hookInput {
			return write(r, "store/testdata/x.go", "package x\n\ntype Sample struct{}\n")
		}, ""},
		{"a dot directory", func(r string) hookInput {
			return write(r, ".claude/hooks/x/main.go", "package main\n\ntype Tool struct{}\n")
		}, ""},
		{"an underscore directory", func(r string) hookInput {
			return write(r, "_old/old.go", "package old\n\ntype Old struct{}\n")
		}, ""},
		{"not Go", func(r string) hookInput { return write(r, "README.md", "type Holding struct{}") }, ""},
		{"an edit that will fail", func(r string) hookInput {
			return edit(r, "store/store.go", "no such text", "type Holding struct{}")
		}, ""},
		{"a file that does not parse", func(r string) hookInput {
			return write(r, "store/broken.go", "this is not Go")
		}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := module(t)
			got := strings.Join(check(tc.in(root)), " ")
			if got != tc.want {
				t.Errorf("unplanned = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCheckAcceptsAPlannedType(t *testing.T) {
	root := module(t)
	put(t, root, "design/holding.ofn", plan)
	if got := check(addAfterItem(root, "type Holding struct{}")); len(got) != 0 {
		t.Errorf("unplanned = %v, want none", got)
	}
	// The plan covers Holding and nothing else.
	if got := check(addAfterItem(root, "type Holding struct{}\ntype Memo struct{}")); strings.Join(got, " ") != "store.Memo" {
		t.Errorf("unplanned = %v, want store.Memo", got)
	}
}

// Moving a type to another file adds nothing to the package.
func TestCheckAcceptsATypeThePackageHas(t *testing.T) {
	root := module(t)
	put(t, root, "store/extra.go", "package store\n\ntype Holding struct{}\n")
	if got := check(addAfterItem(root, "type Holding struct{}")); len(got) != 0 {
		t.Errorf("unplanned = %v, want none", got)
	}
}

func TestCheckOutsideAModule(t *testing.T) {
	dir := t.TempDir()
	if got := check(write(dir, "x.go", "package x\n\ntype X struct{}\n")); len(got) != 0 {
		t.Errorf("unplanned = %v, want none outside a module", got)
	}
}

func TestReplaceAll(t *testing.T) {
	root := module(t)
	put(t, root, "store/store.go", "package store\n\n// marker\n\n// marker\n")
	in := edit(root, "store/store.go", "// marker\n", "type Holding struct{}\n")
	in.ToolInput.ReplaceAll = true
	if got := check(in); len(got) != 1 {
		t.Errorf("unplanned = %v", got)
	}
}

// A plan that does not parse names nothing. The ontology-check hook is the one
// that says so; here it must not crash, and the type still needs a plan.
func TestAPlanThatDoesNotParse(t *testing.T) {
	root := module(t)
	put(t, root, "design/bad.ofn", "this does not parse")
	if got := check(addAfterItem(root, "type Holding struct{}")); len(got) != 1 {
		t.Errorf("unplanned = %v, want Holding", got)
	}
}

func runPayload(t *testing.T, in hookInput) (int, string) {
	t.Helper()
	payload, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	return run(bytes.NewReader(payload), &stderr), stderr.String()
}

func TestRunRefusesWithInstructions(t *testing.T) {
	root := module(t)
	code, msg := runPayload(t, addAfterItem(root, "type Holding struct{}"))
	if code != 2 {
		t.Errorf("exit = %d, want 2 to refuse the edit", code)
	}
	for _, want := range []string{"store.Holding", "design/<change>.ofn", "Declaration(Class(:store.Holding))", "gowl plan", "design-first"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message should contain %q:\n%s", want, msg)
		}
	}
}

func TestRunLetsThroughWhatItCannotRead(t *testing.T) {
	var stderr bytes.Buffer
	if code := run(strings.NewReader("not json"), &stderr); code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
}

func TestRunCanBeTurnedOff(t *testing.T) {
	t.Setenv("GOWL_DESIGN_GATE", "off")
	root := module(t)
	if code, msg := runPayload(t, addAfterItem(root, "type Holding struct{}")); code != 0 {
		t.Errorf("exit = %d, want 0\n%s", code, msg)
	}
}
