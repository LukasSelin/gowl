package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const cleanDoc = `Prefix(:=<http://example.org/o#>)
Ontology(<http://example.org/o>
    Declaration(Class(:Root))
    Declaration(Class(:Child))
    AnnotationAssertion(rdfs:label :Root "Root"^^xsd:string)
    AnnotationAssertion(rdfs:label :Child "Child"^^xsd:string)
    SubClassOf(:Child :Root)
)
`

const cyclicDoc = `Prefix(:=<http://example.org/o#>)
Ontology(<http://example.org/o>
    Declaration(Class(:A))
    Declaration(Class(:B))
    SubClassOf(:A :B)
    SubClassOf(:B :A)
)
`

// write puts content in a temp file and returns its path.
func write(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// capture runs f with stdout and stderr redirected, returning what it printed.
func capture(t *testing.T, f func() int) (string, int) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	defer func() { os.Stdout, os.Stderr = stdout, stderr }()

	code := f()
	w.Close()
	out, _ := io.ReadAll(r)
	return string(out), code
}

func TestLintCleanOntologyExitsZero(t *testing.T) {
	path := write(t, "clean.ofn", cleanDoc)
	out, code := capture(t, func() int { return run([]string{"lint", path}) })
	if code != exitOK {
		t.Errorf("exit = %d, want 0\n%s", code, out)
	}
}

func TestLintFailsOnError(t *testing.T) {
	path := write(t, "cyclic.ofn", cyclicDoc)
	out, code := capture(t, func() int { return run([]string{"lint", path}) })
	if code != exitFailed {
		t.Errorf("exit = %d, want 1\n%s", code, out)
	}
	if !strings.Contains(out, "subclass-cycle") {
		t.Errorf("output should name the rule:\n%s", out)
	}
}

func TestLintFailOnThreshold(t *testing.T) {
	path := write(t, "cyclic.ofn", cyclicDoc)
	// Disabling the only error-severity rule should bring it back to zero.
	_, code := capture(t, func() int {
		return run([]string{"lint", "-disable", "subclass-cycle", path})
	})
	if code != exitOK {
		t.Errorf("exit = %d, want 0 once the error rule is disabled", code)
	}
	// Lowering the threshold makes warnings fail.
	_, code = capture(t, func() int {
		return run([]string{"lint", "-disable", "subclass-cycle", "-fail-on", "info", path})
	})
	if code != exitFailed {
		t.Errorf("exit = %d, want 1 with -fail-on info", code)
	}
}

func TestLintRejectsUnknownRule(t *testing.T) {
	path := write(t, "clean.ofn", cleanDoc)
	out, code := capture(t, func() int {
		return run([]string{"lint", "-disable", "nope", path})
	})
	if code != exitProblem {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(out, "nope") {
		t.Errorf("output should name the bad rule:\n%s", out)
	}
}

func TestLintList(t *testing.T) {
	out, code := capture(t, func() int { return run([]string{"lint", "-list"}) })
	if code != exitOK {
		t.Errorf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, "subclass-cycle") || !strings.Contains(out, "missing-label") {
		t.Errorf("listing looks incomplete:\n%s", out)
	}
}

func TestDiff(t *testing.T) {
	a := write(t, "a.ofn", cleanDoc)
	b := write(t, "b.ofn", strings.Replace(cleanDoc, "SubClassOf(:Child :Root)", "SubClassOf(:Child owl:Thing)", 1))

	out, code := capture(t, func() int { return run([]string{"diff", a, b}) })
	if code != exitOK {
		t.Errorf("exit = %d, want 0 without -exit-code", code)
	}
	if !strings.Contains(out, "- SubClassOf(:Child :Root)") {
		t.Errorf("diff should show the removal:\n%s", out)
	}

	_, code = capture(t, func() int { return run([]string{"diff", "-exit-code", a, b}) })
	if code != exitFailed {
		t.Errorf("exit = %d, want 1 with -exit-code on a difference", code)
	}
	_, code = capture(t, func() int { return run([]string{"diff", "-exit-code", a, a}) })
	if code != exitOK {
		t.Errorf("exit = %d, want 0 comparing a file to itself", code)
	}
}

func TestProfile(t *testing.T) {
	path := write(t, "clean.ofn", cleanDoc)
	out, code := capture(t, func() int { return run([]string{"profile", path}) })
	if code != exitOK {
		t.Errorf("exit = %d, want 0", code)
	}
	for _, want := range []string{"EL", "QL", "RL", "DL"} {
		if !strings.Contains(out, want) {
			t.Errorf("output should mention %s:\n%s", want, out)
		}
	}
}

func TestFmtRoundTrips(t *testing.T) {
	path := write(t, "clean.ofn", cleanDoc)
	out, code := capture(t, func() int { return run([]string{"fmt", path}) })
	if code != exitOK {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "SubClassOf(:Child :Root)") {
		t.Errorf("formatted output lost an axiom:\n%s", out)
	}
}

func TestFmtWriteInPlace(t *testing.T) {
	path := write(t, "clean.ofn", cleanDoc)
	if _, code := capture(t, func() int { return run([]string{"fmt", "-w", path}) }); code != exitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "SubClassOf(:Child :Root)") {
		t.Errorf("file was rewritten badly:\n%s", data)
	}
	// Formatting must be idempotent.
	before := string(data)
	if _, code := capture(t, func() int { return run([]string{"fmt", "-w", path}) }); code != exitOK {
		t.Fatal("second fmt failed")
	}
	after, _ := os.ReadFile(path)
	if string(after) != before {
		t.Errorf("fmt is not idempotent:\n--- first ---\n%s\n--- second ---\n%s", before, after)
	}
}

func TestStats(t *testing.T) {
	path := write(t, "clean.ofn", cleanDoc)
	out, code := capture(t, func() int { return run([]string{"stats", path}) })
	if code != exitOK {
		t.Errorf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, "classes") || !strings.Contains(out, "SubClassOf") {
		t.Errorf("stats output looks wrong:\n%s", out)
	}
}

func TestParseErrorIsReportedWithFilename(t *testing.T) {
	path := write(t, "broken.ofn", "Ontology(Frobnicate(<http://x/A>))")
	out, code := capture(t, func() int { return run([]string{"lint", path}) })
	if code != exitProblem {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(out, "broken.ofn") || !strings.Contains(out, "line 1") {
		t.Errorf("error should name the file and line:\n%s", out)
	}
}

func TestUnknownCommand(t *testing.T) {
	_, code := capture(t, func() int { return run([]string{"frobnicate"}) })
	if code != exitProblem {
		t.Errorf("exit = %d, want 2", code)
	}
	_, code = capture(t, func() int { return run(nil) })
	if code != exitProblem {
		t.Errorf("no arguments: exit = %d, want 2", code)
	}
}
