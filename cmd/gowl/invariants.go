package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gowl/internal/gocode"
	"gowl/lint"
	"gowl/owl"
)

// invariantsFile is where, under design/, a module keeps the rules its code
// holds on every change. Unlike a plan it is never retired: it is checked
// whenever the code changes, and whenever it does.
const invariantsFile = "invariants.ofn"

// isInvariants reports whether path names an invariants file rather than a plan.
func isInvariants(path string) bool {
	return strings.EqualFold(filepath.Base(path), invariantsFile)
}

// loadInvariants reads the invariants for the module in root: the file named
// by -invariants or -file, or else design/invariants.ofn when there is one. It
// returns a nil ontology when there are none to check.
func loadInvariants(root, named string, module *owl.Ontology) (*owl.Ontology, string, error) {
	path := named
	if path == "" {
		path = filepath.Join(root, designDir, invariantsFile)
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			return nil, "", nil
		}
	}
	inv, err := loadPlan(path, module)
	if err != nil {
		return nil, "", err
	}
	return inv, path, nil
}

// knownPackages lists the names from outside the code that invariants may
// use: the packages of the standard library and of the module's dependencies,
// and every package and type the given plans name, which may not be written
// yet.
func knownPackages(root string, module *owl.Ontology, plans ...*owl.Ontology) ([]owl.NamedIndividual, error) {
	paths, err := gocode.KnownPackages(root)
	if err != nil {
		return nil, err
	}
	out := make([]owl.NamedIndividual, 0, len(paths))
	for _, p := range paths {
		out = append(out, gocode.PackageIndividual(module, p))
	}
	for _, plan := range plans {
		out = append(out, plan.Individuals()...)
		// A rule names a type as an individual, and a plan as a class.
		for _, c := range plan.Classes() {
			out = append(out, owl.NamedIndividual(c.IRI()))
		}
	}
	return out, nil
}

// cmdInvariants checks a module's code against its invariants, as it stands in
// the working tree. See gocode.CheckInvariants.
func cmdInvariants(args []string) int {
	fs := flag.NewFlagSet("invariants", flag.ContinueOnError)
	file := fs.String("file", "", "the invariants to check (default: design/invariants.ofn)")
	failOn := fs.String("fail-on", "error", "lowest severity that fails the run: info, warning or error")
	seen := fs.String("seen", "", "leave out findings already listed in this file, and add the reported ones to it")
	facts := fs.Bool("facts", false, "print the facts the rules are checked against, as an ontology, and check nothing")
	jsonOut := fs.Bool("json", false, "write one JSON object to stdout instead of text")
	dirs, err := parseArgs(fs, args)
	if err != nil {
		return exitProblem
	}
	if len(dirs) > 1 {
		fmt.Fprintln(os.Stderr, "gowl invariants: need at most one module directory")
		return exitProblem
	}
	if *facts && *jsonOut {
		fmt.Fprintln(os.Stderr, "gowl invariants: -facts prints an ontology, so it takes no -json")
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
	module, err := gocode.Build(root)
	if err != nil {
		return failWith(*jsonOut, err)
	}
	inv, path, err := loadInvariants(root, *file, module)
	if err != nil {
		return failWith(*jsonOut, err)
	}
	if inv == nil && *facts {
		// The code's facts are there to read with no invariants at all, which
		// is when a person is about to write the first rule.
		inv = owl.New("")
		inv.Prefixes = module.Prefixes
	}
	if inv == nil {
		if *jsonOut {
			writeJSON(invariantsJSON(filepath.ToSlash(root), "", module, gocode.Conformance{}, nil, 0, threshold))
		} else {
			fmt.Printf("no invariants: %s does not exist\n", filepath.ToSlash(filepath.Join(root, designDir, invariantsFile)))
		}
		return exitOK
	}

	// A package a plan for this change names may not be written yet, and the
	// invariants already place it; that is not a misspelling.
	var plans []*owl.Ontology
	if rev, _ := resolveBase(root, ""); rev != "" {
		if changed, err := changedDesigns(root, rev); err == nil {
			for _, p := range changed {
				if plan, err := load(p); err == nil {
					plans = append(plans, plan)
				}
			}
		}
	}
	known, err := knownPackages(root, module, plans...)
	if err != nil {
		return failWith(*jsonOut, err)
	}
	if *facts {
		fmt.Print(gocode.Facts(inv, module, known).Functional())
		return exitOK
	}

	c := gocode.CheckInvariants(inv, module, known)
	findings, already := c.Findings, 0
	if *seen != "" {
		if findings, already, err = unseen(*seen, findings); err != nil {
			return failWith(*jsonOut, err)
		}
	}
	if *jsonOut {
		writeJSON(invariantsJSON(filepath.ToSlash(root), filepath.ToSlash(path), inv, c, findings, already, threshold))
	} else {
		fmt.Printf("checked against %s: %d rule(s)\n\n", filepath.ToSlash(path), c.Checked)
		printFindings(findings, already)
	}
	if worst, any := lint.MaxSeverity(findings); any && worst >= threshold {
		return exitFailed
	}
	return exitOK
}
