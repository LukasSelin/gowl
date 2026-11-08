package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gowl/internal/gocode"
	"gowl/lint"
	"gowl/owl"
)

// designDir is where a module keeps its plans, next to its go.mod. A plan
// describes one change and goes through review with it. Once merged it is part
// of the base, so it stops being checked: it records a decision rather than
// promising that the code will always look that way.
const designDir = "design"

// cmdPlan checks a plan before any of it is built, by reading the module at the
// base as though the plan were already code. See gocode.ReviewPlan.
func cmdPlan(args []string) int {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	base := fs.String("base", "", "the commit to plan against (default: where this branch left the default branch)")
	failOn := fs.String("fail-on", "error", "lowest severity that fails the run: info, warning or error")
	jsonOut := fs.Bool("json", false, "write one JSON object to stdout instead of text")
	files, err := parseArgs(fs, args)
	if err != nil {
		return exitProblem
	}
	if len(files) < 1 || len(files) > 2 {
		fmt.Fprintln(os.Stderr, "gowl plan: need a plan file, and at most one module directory")
		return exitProblem
	}
	root := "."
	if len(files) == 2 {
		root = files[1]
	}

	threshold, err := lint.ParseSeverity(*failOn)
	if err != nil {
		return failWith(*jsonOut, err)
	}
	rev, label := resolveBase(root, *base)
	commit, before, err := buildAt(root, rev)
	if err != nil {
		return failWith(*jsonOut, err)
	}
	plan, err := loadPlan(files[0], before)
	if err != nil {
		return failWith(*jsonOut, err)
	}

	d := gocode.ReviewPlan(plan, before)
	if *jsonOut {
		writeJSON(planDoc{
			Design:    filepath.ToSlash(files[0]),
			sprawlDoc: sprawlJSON(filepath.ToSlash(root), label, commit, before, d, d.Findings, 0, threshold),
		})
	} else {
		printSprawl(label, commit, before, d, d.Findings, 0)
	}
	if worst, any := lint.MaxSeverity(d.Findings); any && worst >= threshold {
		return exitFailed
	}
	return exitOK
}

// cmdConform compares a module's code with the plans for a change. Unless -design
// names them, the plans are the files under design/ that differ from the base.
// See gocode.Conform.
func cmdConform(args []string) int {
	fs := flag.NewFlagSet("conform", flag.ContinueOnError)
	base := fs.String("base", "", "the commit the change starts from (default: where this branch left the default branch)")
	failOn := fs.String("fail-on", "error", "lowest severity that fails the run: info, warning or error")
	seen := fs.String("seen", "", "leave out findings already listed in this file, and add the reported ones to it")
	jsonOut := fs.Bool("json", false, "write one JSON object to stdout instead of text")
	var named []string
	fs.Func("design", "a plan file to check against; may be repeated (default: the plans under design/ that differ from the base)", func(s string) error {
		named = append(named, s)
		return nil
	})
	dirs, err := parseArgs(fs, args)
	if err != nil {
		return exitProblem
	}
	if len(dirs) > 1 {
		fmt.Fprintln(os.Stderr, "gowl conform: need at most one module directory")
		return exitProblem
	}
	root := "."
	if len(dirs) == 1 {
		root = dirs[0]
	}

	threshold, err := lint.ParseSeverity(*failOn)
	if err != nil {
		return failWith(*jsonOut, err)
	}
	// The working tree first: when it does not build, there is no point
	// copying out the base.
	after, err := gocode.Build(root)
	if err != nil {
		return failWith(*jsonOut, err)
	}
	rev, label := resolveBase(root, *base)
	commit, before, err := buildAt(root, rev)
	if err != nil {
		return failWith(*jsonOut, err)
	}

	paths := named
	if len(paths) == 0 {
		if paths, err = changedDesigns(root, rev); err != nil {
			return failWith(*jsonOut, err)
		}
	}
	plan := owl.New("")
	for _, p := range paths {
		one, err := loadPlan(p, after)
		if err != nil {
			return failWith(*jsonOut, err)
		}
		plan = gocode.Overlay(plan, one)
	}

	c := gocode.Conform(plan, before, after)
	findings, already := c.Findings, 0
	if *seen != "" {
		if findings, already, err = unseen(*seen, findings); err != nil {
			return failWith(*jsonOut, err)
		}
	}
	worst, any := lint.MaxSeverity(findings)
	failed := any && worst >= threshold

	if *jsonOut {
		writeJSON(conformJSON(filepath.ToSlash(root), label, commit, paths, after, c, findings, already, threshold))
	} else {
		printConform(label, commit, paths, c, findings, already)
	}
	if failed {
		return exitFailed
	}
	return exitOK
}

func printConform(base, commit string, paths []string, c gocode.Conformance, findings []lint.Finding, already int) {
	fmt.Printf("compared with %s (%s): %d plan(s), %d planned item(s) checked\n", base, shortCommit(commit), len(paths), c.Checked)
	if len(paths) > 0 {
		names := make([]string, len(paths))
		for i, p := range paths {
			names[i] = filepath.ToSlash(p)
		}
		fmt.Printf("  %-10s %s\n", "plans:", strings.Join(names, ", "))
	}
	if len(c.Unchecked) > 0 {
		iris := make([]string, len(c.Unchecked))
		for i, iri := range c.Unchecked {
			iris[i] = string(iri)
		}
		fmt.Printf("  %-10s %s (outside the module)\n", "unchecked:", strings.Join(iris, ", "))
	}
	fmt.Println()
	printFindings(findings, already)
}

// resolveBase turns the -base flag into a commit to compare with and a name to
// report it by. Without one it is where this branch left the default branch, so
// that a plan committed partway through a branch still counts. Outside any
// branch, or with no default branch to find, it is HEAD.
func resolveBase(dir, base string) (rev, label string) {
	if base != "" {
		return base, base
	}
	for _, ref := range []string{"origin/HEAD", "origin/main", "origin/master", "main", "master"} {
		if commit, err := git(dir, "merge-base", "HEAD", ref); err == nil && commit != "" {
			return commit, ref + " merge-base"
		}
	}
	return "HEAD", "HEAD"
}

// changedDesigns lists the plans under dir/design that differ from rev, tracked
// or not, leaving out any the change deletes.
func changedDesigns(dir, rev string) ([]string, error) {
	tracked, err := git(dir, "diff", "--name-only", "--relative", "--diff-filter=d", rev, "--", designDir)
	if err != nil {
		return nil, err
	}
	untracked, err := git(dir, "ls-files", "--others", "--exclude-standard", "--", designDir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, name := range strings.Fields(tracked + "\n" + untracked) {
		if strings.EqualFold(filepath.Ext(name), ".ofn") {
			out = append(out, filepath.Join(dir, filepath.FromSlash(name)))
		}
	}
	sort.Strings(out)
	return out, nil
}

// loadPlan parses a plan and makes sure it is about this module. Its names have
// to be in the module's namespace, and a plan written under the wrong prefix
// would otherwise match nothing and report only the things it failed to mention.
func loadPlan(path string, module *owl.Ontology) (*owl.Ontology, error) {
	plan, err := load(path)
	if err != nil {
		return nil, err
	}
	ns := gocode.Namespace(module)
	for _, e := range plan.Signature() {
		if strings.HasPrefix(string(e.IRI()), string(ns)) {
			return plan, nil
		}
	}
	return nil, fmt.Errorf("%s: names nothing in this module's namespace; start it with Prefix(:=<%s>)", filepath.ToSlash(path), ns)
}
