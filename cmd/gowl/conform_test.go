package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// holdingsPlan names exactly what storeAfter adds.
const holdingsPlan = `Prefix(:=<https://example.com/store#>)
Prefix(xsd:=<http://www.w3.org/2001/XMLSchema#>)

Ontology(<https://example.com/store/design/holdings>
    Declaration(Class(:store.Item))
    Declaration(Class(:store.Holding))
    Declaration(ObjectProperty(:store.Holding.What))
    ObjectPropertyDomain(:store.Holding.What :store.Holding)
    ObjectPropertyRange(:store.Holding.What :store.Item)
    Declaration(DataProperty(:store.Holding.Amount))
    DataPropertyDomain(:store.Holding.Amount :store.Holding)
    DataPropertyRange(:store.Holding.Amount xsd:integer)
    Declaration(Class(:store.Memo))
    Declaration(DataProperty(:store.Memo.Text))
    DataPropertyDomain(:store.Memo.Text :store.Memo)
    DataPropertyRange(:store.Memo.Text xsd:string)
)
`

// putPlan writes a plan under the module's design directory.
func putPlan(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, "design", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// gitIn runs a git command in dir with a known identity.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir,
		"-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", args[0], err, out)
	}
}

func TestConformWithNoPlanReportsTheChange(t *testing.T) {
	dir := storeRepo(t)
	out, code := capture(t, func() int { return run([]string{"conform", dir}) })
	if code != exitFailed {
		t.Errorf("exit = %d, want 1\n%s", code, out)
	}
	for _, want := range []string{
		"0 plan(s), 0 planned item(s) checked",
		"error: unplanned-type: type :store.Holding is new, and no plan names it",
		"error: unplanned-type: type :store.Memo is new, and no plan names it",
		"2 finding(s)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q:\n%s", want, out)
		}
	}
}

func TestConformFindsPlansUnderDesign(t *testing.T) {
	dir := storeRepo(t)
	putPlan(t, dir, "holdings.ofn", holdingsPlan)
	putPlan(t, dir, "notes.txt", "not a plan")

	out, code := capture(t, func() int { return run([]string{"conform", "-fail-on", "info", dir}) })
	if code != exitOK {
		t.Errorf("exit = %d, want 0\n%s", code, out)
	}
	for _, want := range []string{"1 plan(s), 6 planned item(s) checked", "design/holdings.ofn", "no findings"} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "notes.txt") {
		t.Errorf("only .ofn files are plans:\n%s", out)
	}
}

func TestConformReadsSeveralPlansAsOne(t *testing.T) {
	dir := storeRepo(t)
	head, tail, _ := strings.Cut(holdingsPlan, "    Declaration(Class(:store.Memo))")
	putPlan(t, dir, "holding.ofn", head+")\n")
	putPlan(t, dir, "memo.ofn", strings.Split(head, "Ontology(")[0]+"Ontology(<https://example.com/store/design/memo>\n    Declaration(Class(:store.Memo))"+tail)

	out, code := capture(t, func() int { return run([]string{"conform", "-fail-on", "info", dir}) })
	if code != exitOK || !strings.Contains(out, "2 plan(s)") {
		t.Errorf("exit = %d, want 0 with two plans\n%s", code, out)
	}
}

func TestConformDesignFlagNamesThePlan(t *testing.T) {
	dir := storeRepo(t)
	plan := filepath.Join(t.TempDir(), "elsewhere.ofn")
	if err := os.WriteFile(plan, []byte(holdingsPlan), 0o644); err != nil {
		t.Fatal(err)
	}
	putPlan(t, dir, "wrong.ofn", "this does not parse")

	out, code := capture(t, func() int { return run([]string{"conform", "-design", plan, dir}) })
	if code != exitOK || !strings.Contains(out, "1 plan(s), 6 planned item(s) checked") {
		t.Errorf("exit = %d, want 0: -design replaces the search\n%s", code, out)
	}
}

// A plan committed partway through a branch has to keep counting, and so does
// one left over from before the branch not to.
func TestConformDefaultBaseIsWhereTheBranchStarted(t *testing.T) {
	dir := storeRepo(t)
	trunk := func() string {
		out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}()
	if trunk != "main" && trunk != "master" {
		gitIn(t, dir, "branch", "-m", trunk, "main")
		trunk = "main"
	}
	gitIn(t, dir, "checkout", "-q", "-b", "feature")
	putPlan(t, dir, "holdings.ofn", holdingsPlan)
	gitIn(t, dir, "add", "-A", "..")
	gitIn(t, dir, "commit", "-q", "-m", "plan and code")

	out, code := capture(t, func() int { return run([]string{"conform", "-fail-on", "info", dir}) })
	if code != exitOK {
		t.Errorf("exit = %d, want 0\n%s", code, out)
	}
	for _, want := range []string{"compared with " + trunk + " merge-base", "1 plan(s), 6 planned item(s) checked"} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q:\n%s", want, out)
		}
	}

	// Back on the trunk with the branch merged, the plan is part of the base and
	// no longer checked.
	gitIn(t, dir, "checkout", "-q", trunk)
	gitIn(t, dir, "merge", "-q", "feature")
	out, code = capture(t, func() int { return run([]string{"conform", "-fail-on", "info", dir}) })
	if code != exitOK || !strings.Contains(out, "0 plan(s)") {
		t.Errorf("exit = %d, want 0 and no plans once merged\n%s", code, out)
	}
}

func TestConformLeavesOutADeletedPlan(t *testing.T) {
	dir := storeRepo(t)
	path := putPlan(t, dir, "old.ofn", holdingsPlan)
	gitIn(t, dir, "add", "-A", "..")
	gitIn(t, dir, "commit", "-q", "-m", "plan")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	out, code := capture(t, func() int { return run([]string{"conform", "-base", "HEAD", dir}) })
	if code != exitOK || !strings.Contains(out, "0 plan(s)") {
		t.Errorf("exit = %d, want 0 with no plans\n%s", code, out)
	}
}

func TestConformRefusesAPlanInTheWrongNamespace(t *testing.T) {
	dir := storeRepo(t)
	putPlan(t, dir, "holdings.ofn", strings.ReplaceAll(holdingsPlan, "https://example.com/store#", "https://example.com/other#"))

	out, code := capture(t, func() int { return run([]string{"conform", dir}) })
	if code != exitProblem {
		t.Errorf("exit = %d, want 2\n%s", code, out)
	}
	if !strings.Contains(out, "Prefix(:=<https://example.com/store#>)") {
		t.Errorf("the message should say which prefix to use:\n%s", out)
	}
}

// -seen is how the hook reports each finding once: the second run is quiet.
func TestConformSeen(t *testing.T) {
	dir := storeRepo(t)
	seen := filepath.Join(t.TempDir(), "seen")
	args := []string{"conform", "-seen", seen, dir}

	out, code := capture(t, func() int { return run(args) })
	if code != exitFailed || !strings.Contains(out, "2 finding(s)") {
		t.Fatalf("first run: exit %d\n%s", code, out)
	}
	out, code = capture(t, func() int { return run(args) })
	if code != exitOK || !strings.Contains(out, "no new findings (2 reported before)") {
		t.Errorf("second run: exit = %d, want 0 and nothing new\n%s", code, out)
	}
}

func TestConformJSON(t *testing.T) {
	dir := storeRepo(t)
	path := putPlan(t, dir, "holdings.ofn", strings.Replace(holdingsPlan, "    Declaration(Class(:store.Memo))",
		"    Declaration(Class(foaf:Person))\n    Declaration(Class(:store.Refund))\n    Declaration(Class(:store.Memo))", 1))
	if err := os.WriteFile(path, []byte("Prefix(foaf:=<http://xmlns.com/foaf/0.1/>)\n"+mustRead(t, path)), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := capture(t, func() int { return run([]string{"conform", "-json", "-base", "HEAD", dir}) })
	if code != exitFailed {
		t.Fatalf("exit = %d, want 1\n%s", code, out)
	}
	var doc conformDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if doc.Base != "HEAD" || len(doc.BaseCommit) < 40 {
		t.Errorf("base = %q at %q", doc.Base, doc.BaseCommit)
	}
	if len(doc.Designs) != 1 || !strings.HasSuffix(doc.Designs[0], "design/holdings.ofn") {
		t.Errorf("designs = %v", doc.Designs)
	}
	if strings.Join(doc.Unchecked, " ") != "http://xmlns.com/foaf/0.1/Person" {
		t.Errorf("unchecked = %v", doc.Unchecked)
	}
	if doc.Summary.Total != 1 || doc.Summary.Worst != "error" || !doc.Summary.Failed {
		t.Errorf("summary = %+v", doc.Summary)
	}
	if doc.Findings[0].Rule != "missing-type" {
		t.Errorf("findings = %+v", doc.Findings)
	}
}

// A function the change adds is the package's to offer and the plan's to
// name. Unplanned, it is a warning: below what fails a run by default, and what
// the hook asks about.
func TestConformReportsAnUnplannedFunction(t *testing.T) {
	dir := storeRepo(t)
	putPlan(t, dir, "holdings.ofn", holdingsPlan)
	if err := os.WriteFile(filepath.Join(dir, "count.go"), []byte("package store\n\n// Count counts a stock.\nfunc Count(s *Stock) int { return s.Count }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := capture(t, func() int { return run([]string{"conform", dir}) })
	want := "warning: unplanned-func: :package.example.com.store now offers the function Count(*store.Stock) int, and no plan names it"
	if code != exitOK || !strings.Contains(out, want) {
		t.Errorf("exit = %d, want 0 and %q\n%s", code, want, out)
	}
	if out, code := capture(t, func() int { return run([]string{"conform", "-fail-on", "warning", dir}) }); code != exitFailed {
		t.Errorf("exit = %d, want 1 at -fail-on warning\n%s", code, out)
	}

	putPlan(t, dir, "holdings.ofn", strings.Replace(holdingsPlan, "    Declaration(Class(:store.Memo))",
		"    AnnotationAssertion(:func :package.example.com.store \"Count(s *Stock) int\")\n    Declaration(Class(:store.Memo))", 1))
	out, code = capture(t, func() int { return run([]string{"conform", "-fail-on", "warning", dir}) })
	if code != exitOK || !strings.Contains(out, "7 planned item(s) checked") {
		t.Errorf("exit = %d, want 0 with the function among the 7 items checked\n%s", code, out)
	}
}

func TestConformJSONListsAreNeverNull(t *testing.T) {
	dir := storeRepo(t)
	putPlan(t, dir, "holdings.ofn", holdingsPlan)
	out, _ := capture(t, func() int { return run([]string{"conform", "-json", "-base", "HEAD", dir}) })
	for _, want := range []string{`"unchecked": []`, `"findings": []`} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %s:\n%s", want, out)
		}
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// A plan is checked against the base, so it can be checked before a line of it
// is written. storeRepo's working tree is not consulted here.
func TestPlanFindsARepeatBeforeTheCodeExists(t *testing.T) {
	dir := storeRepo(t)
	path := putPlan(t, dir, "holdings.ofn", holdingsPlan)

	out, code := capture(t, func() int { return run([]string{"plan", "-base", "HEAD", "-fail-on", "warning", path, dir}) })
	if code != exitFailed {
		t.Errorf("exit = %d, want 1 for a warning at -fail-on warning\n%s", code, out)
	}
	for _, want := range []string{
		"2 new type(s), 0 changed",
		"warning: same-shape-type: :store.Holding has the same fields as :store.Stock",
		"info: new-orphan: new type :store.Memo",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q:\n%s", want, out)
		}
	}
}

func TestPlanDefaultDoesNotFailOnWarnings(t *testing.T) {
	dir := storeRepo(t)
	path := putPlan(t, dir, "holdings.ofn", holdingsPlan)
	if out, code := capture(t, func() int { return run([]string{"plan", path, dir}) }); code != exitOK {
		t.Errorf("exit = %d, want 0\n%s", code, out)
	}
}

func TestPlanJSON(t *testing.T) {
	dir := storeRepo(t)
	path := putPlan(t, dir, "holdings.ofn", holdingsPlan)
	out, code := capture(t, func() int { return run([]string{"plan", "-json", path, dir}) })
	if code != exitOK {
		t.Fatalf("exit = %d\n%s", code, out)
	}
	var doc struct {
		Design string   `json:"design"`
		Added  []string `json:"added"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if !strings.HasSuffix(doc.Design, "design/holdings.ofn") || len(doc.Added) != 2 {
		t.Errorf("doc = %+v", doc)
	}
}

func TestPlanRefusals(t *testing.T) {
	dir := storeRepo(t)
	bad := putPlan(t, dir, "bad.ofn", "this does not parse")
	wrong := putPlan(t, dir, "wrong.ofn", strings.ReplaceAll(holdingsPlan, "https://example.com/store#", "https://example.com/other#"))

	for name, args := range map[string][]string{
		"no file":         {"plan"},
		"too many":        {"plan", bad, dir, "extra"},
		"does not parse":  {"plan", bad, dir},
		"wrong namespace": {"plan", wrong, dir},
		"missing file":    {"plan", filepath.Join(dir, "design", "none.ofn"), dir},
	} {
		t.Run(name, func(t *testing.T) {
			if out, code := capture(t, func() int { return run(args) }); code != exitProblem {
				t.Errorf("exit = %d, want 2\n%s", code, out)
			}
		})
	}
}
