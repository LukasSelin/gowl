package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const storeBefore = `// Package store keeps stock.
package store

// Item is something in stock.
type Item struct{ Name string }

// Stock counts items.
type Stock struct {
	Item  *Item
	Count int
}
`

// storeAfter copies Stock under another name and adds a type nothing uses.
const storeAfter = storeBefore + `
// Holding repeats Stock.
type Holding struct {
	What   *Item
	Amount int
}

// Memo relates to nothing.
type Memo struct{ Text string }
`

// storeRepo commits the store module, in a subdirectory of a fresh git
// repository so the module's place in the tree is exercised, then rewrites it
// to storeAfter without committing. It returns the module directory.
func storeRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	t.Setenv("GOWORK", "off")
	// Keep the user's git configuration (signing, hooks, templates) out of it.
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	repo := t.TempDir()
	dir := filepath.Join(repo, "store")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	put := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitDo := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo,
			"-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", args[0], err, out)
		}
	}
	put("go.mod", "module example.com/store\n\ngo 1.21\n")
	put("store.go", storeBefore)
	gitDo("init", "-q")
	gitDo("add", ".")
	gitDo("commit", "-q", "-m", "store")
	put("store.go", storeAfter)
	return dir
}

func TestSprawlReportsTheChange(t *testing.T) {
	dir := storeRepo(t)
	out, code := capture(t, func() int { return run([]string{"sprawl", "-fail-on", "warning", dir}) })
	if code != exitFailed {
		t.Errorf("exit = %d, want 1 for a warning at -fail-on warning\n%s", code, out)
	}
	for _, want := range []string{
		"2 new type(s), 0 changed",
		"new:     :store.Holding, :store.Memo",
		"warning: same-shape-type: :store.Holding has the same fields as :store.Stock",
		"info: new-orphan: new type :store.Memo",
		"2 finding(s)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q:\n%s", want, out)
		}
	}
	// Stock existed before and did not change, so it is not reported.
	if strings.Contains(out, "same-shape-type: :store.Stock") {
		t.Errorf("Stock did not change and should not be reported:\n%s", out)
	}
}

func TestSprawlDefaultDoesNotFail(t *testing.T) {
	dir := storeRepo(t)
	if out, code := capture(t, func() int { return run([]string{"sprawl", dir}) }); code != exitOK {
		t.Errorf("exit = %d, want 0: nothing reaches the default -fail-on error\n%s", code, out)
	}
}

// -seen is how a hook reports each finding once: the second run is quiet.
func TestSprawlSeen(t *testing.T) {
	dir := storeRepo(t)
	seen := filepath.Join(t.TempDir(), "seen")
	args := []string{"sprawl", "-fail-on", "info", "-seen", seen, dir}

	out, code := capture(t, func() int { return run(args) })
	if code != exitFailed || !strings.Contains(out, "2 finding(s)") {
		t.Fatalf("first run: exit %d\n%s", code, out)
	}
	out, code = capture(t, func() int { return run(args) })
	if code != exitOK {
		t.Errorf("second run: exit = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "no new findings (2 reported before)") {
		t.Errorf("second run should report nothing new:\n%s", out)
	}
}

func TestSprawlJSON(t *testing.T) {
	dir := storeRepo(t)
	out, code := capture(t, func() int { return run([]string{"sprawl", "-json", dir}) })
	if code != exitOK {
		t.Fatalf("exit = %d\n%s", code, out)
	}
	var doc sprawlDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if doc.Base != "HEAD" || len(doc.BaseCommit) < 40 {
		t.Errorf("base = %q at %q, want HEAD at a full commit id", doc.Base, doc.BaseCommit)
	}
	if got := strings.Join(doc.Added, " "); got != "https://example.com/store#store.Holding https://example.com/store#store.Memo" {
		t.Errorf("added = %s", got)
	}
	if doc.Changed == nil || len(doc.Changed) != 0 {
		t.Errorf("changed should be an empty list, got %#v", doc.Changed)
	}
	if doc.Summary.Total != 2 || doc.Summary.Worst != "warning" || doc.Summary.Failed {
		t.Errorf("summary = %+v", doc.Summary)
	}
}

func TestSprawlBadBase(t *testing.T) {
	dir := storeRepo(t)
	out, code := capture(t, func() int { return run([]string{"sprawl", "-base", "no-such-branch", dir}) })
	if code != exitProblem || !strings.Contains(out, "not a commit") {
		t.Errorf("exit = %d, want 2 with a clear message\n%s", code, out)
	}
}
