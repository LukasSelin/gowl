package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gowl/owl"
)

// tinyModule writes a one-package Go module to a temp directory and returns its
// root. It is small on purpose: capture reads stdout only once the command has
// returned, so the document has to fit in a pipe's buffer.
func tinyModule(t *testing.T) string {
	t.Helper()
	t.Setenv("GOWORK", "off")
	root := t.TempDir()
	files := map[string]string{
		"go.mod":  "module example.com/tiny\n\ngo 1.21\n",
		"tiny.go": "// Package tiny is tiny.\npackage tiny\n\n// Thing is a thing.\ntype Thing struct{ N int }\n",
	}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestGoCodeWritesToStdout(t *testing.T) {
	root := tinyModule(t)
	out, code := capture(t, func() int { return run([]string{"gocode", root}) })
	if code != exitOK {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	o, err := owl.ParseFunctionalString(out)
	if err != nil {
		t.Fatalf("output does not parse: %v\n%s", err, out)
	}
	if o.IRI != "https://example.com/tiny" {
		t.Errorf("ontology IRI = %s, want https://example.com/tiny", o.IRI)
	}
	if !strings.Contains(out, "Declaration(Class(:tiny.Thing))") {
		t.Errorf("Thing should be a class:\n%s", out)
	}
}

func TestGoCodeWritesToFile(t *testing.T) {
	root := tinyModule(t)
	path := filepath.Join(t.TempDir(), "tiny.ofn")
	// The flag after the directory binds too, as it does for every command.
	out, code := capture(t, func() int { return run([]string{"gocode", root, "-o", path}) })
	if code != exitOK {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if out != "" {
		t.Errorf("-o should leave stdout empty, got:\n%s", out)
	}
	if _, code := capture(t, func() int { return run([]string{"lint", path}) }); code != exitOK {
		t.Errorf("gowl lint on the written file: exit = %d, want 0", code)
	}
}

func TestGoCodeProblems(t *testing.T) {
	out, code := capture(t, func() int { return run([]string{"gocode", t.TempDir()}) })
	if code != exitProblem {
		t.Errorf("directory without go.mod: exit = %d, want 2", code)
	}
	if !strings.Contains(out, "go.mod") {
		t.Errorf("error should name go.mod:\n%s", out)
	}
	if _, code := capture(t, func() int { return run([]string{"gocode", "a", "b"}) }); code != exitProblem {
		t.Errorf("two directories: exit = %d, want 2", code)
	}
}
