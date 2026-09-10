// Command gowl inspects OWL 2 ontologies written in functional syntax.
//
// Usage:
//
//	gowl lint    [-disable rules] [-fail-on severity] [-json] file.ofn
//	gowl diff    [-summary] [-json] old.ofn new.ofn
//	gowl profile [-v] [-json] file.ofn
//	gowl classify [-axioms] [-explain "sub super"] [-json] file.ofn
//	gowl fmt     [-w] [-canonical] file.ofn
//	gowl stats   [-json] file.ofn
//
// Exit status is 0 on success, 1 when a check fails (lint findings at or above
// the fail threshold, or a non-empty diff under -exit-code), and 2 on a usage
// or I/O error.
//
// Every command except fmt takes -json, which writes one JSON object to stdout
// instead of text — including on failure, so a caller that parses stdout always
// gets a document. Exit statuses are the same in both modes. fmt has no -json
// because its output is an ontology document, not a report. The document shapes
// live in json.go.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gowl/lint"
	"gowl/owl"
	"gowl/vocab"
)

const (
	exitOK      = 0
	exitFailed  = 1
	exitProblem = 2
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return exitProblem
	}
	switch args[0] {
	case "lint":
		return cmdLint(args[1:])
	case "diff":
		return cmdDiff(args[1:])
	case "profile":
		return cmdProfile(args[1:])
	case "classify":
		return cmdClassify(args[1:])
	case "fmt":
		return cmdFmt(args[1:])
	case "stats":
		return cmdStats(args[1:])
	case "help", "-h", "--help":
		usage()
		return exitOK
	default:
		fmt.Fprintf(os.Stderr, "gowl: unknown command %q\n\n", args[0])
		usage()
		return exitProblem
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `gowl inspects OWL 2 ontologies in functional syntax.

  gowl lint    [-disable rules] [-fail-on severity] [-json] file.ofn
  gowl diff    [-summary] [-exit-code] [-json] old.ofn new.ofn
  gowl profile [-v] [-json] file.ofn
  gowl classify [-axioms] [-explain "sub super"] [-unsatisfiable] [-json] file.ofn
  gowl fmt     [-w] [-canonical] file.ofn
  gowl stats   [-json] file.ofn

classify runs the OWL 2 EL reasoner: it infers the class hierarchy, finds
classes that cannot have instances, and explains why a subsumption holds.

Every command except fmt takes -json for machine-readable output.
Run "gowl <command> -h" for the flags of one command.
`)
}

// parseArgs parses flags that may appear before, after or between positional
// arguments. Go's flag package stops at the first non-flag argument, which
// makes "gowl profile file.ofn -v" fail; each round of this loop consumes one
// positional and re-parses the rest, so "-flag value" pairs still bind.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// load parses an ontology file, reporting a usable error.
func load(path string) (*owl.Ontology, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	o, err := owl.ParseFunctional(f)
	if err != nil {
		// Parse errors carry "owl: line N"; prefix the filename so editors and
		// CI logs can locate them.
		return nil, fmt.Errorf("%s: %w", filepath.ToSlash(path), err)
	}
	return o, nil
}

func fail(err error) int {
	fmt.Fprintln(os.Stderr, "gowl:", err)
	return exitProblem
}

// --- lint -------------------------------------------------------------------

// standardVocabularies are the vocabularies gowl lint suggests reusing, in the
// order it would rather reuse them. The first to name a term wins, so the
// precise ones come first and schema.org, which names nearly everything, goes
// last.
var standardVocabularies = []string{
	"rdf", "rdfs", "skos", "skosxl", "dcterms", "dc",
	"foaf", "prov", "org", "time", "dcat", "schema",
}

// standardTermsRule builds the prefer-standard-term rule over everything in
// vocab/. Its index is built on first use, so listing the rules stays cheap.
func standardTermsRule() lint.Rule {
	refs := make([]*owl.Vocabulary, 0, len(standardVocabularies))
	for _, name := range standardVocabularies {
		if e, ok := vocab.ByPrefix(name); ok {
			refs = append(refs, e.Vocabulary())
		}
	}
	return lint.PreferStandardTerms(refs...)
}

func cmdLint(args []string) int {
	fs := flag.NewFlagSet("lint", flag.ContinueOnError)
	disable := fs.String("disable", "", "comma-separated rule names to skip")
	failOn := fs.String("fail-on", "error", "lowest severity that fails the run: info, warning or error")
	list := fs.Bool("list", false, "list the available rules and exit")
	jsonOut := fs.Bool("json", false, "write one JSON object to stdout instead of text")
	files, err := parseArgs(fs, args)
	if err != nil {
		return exitProblem
	}

	rules := append(lint.Default(), standardTermsRule())
	if *list {
		if *jsonOut {
			writeJSON(rulesJSON(rules))
			return exitOK
		}
		for _, r := range rules {
			fmt.Printf("%-22s %-8s %s\n", r.Name, r.Severity, r.Description)
		}
		return exitOK
	}
	if len(files) != 1 {
		fmt.Fprintln(os.Stderr, "gowl lint: need exactly one file")
		return exitProblem
	}

	threshold, err := lint.ParseSeverity(*failOn)
	if err != nil {
		return failWith(*jsonOut, err)
	}

	kept, unknown := lint.Select(rules, strings.Split(*disable, ","))
	if len(unknown) > 0 {
		return failWith(*jsonOut, fmt.Errorf("unknown rule(s) in -disable: %s", strings.Join(unknown, ", ")))
	}

	o, err := load(files[0])
	if err != nil {
		return failWith(*jsonOut, err)
	}

	findings := lint.Run(o, kept)
	worst, any := lint.MaxSeverity(findings)
	failed := any && worst >= threshold

	if *jsonOut {
		writeJSON(lintJSON(files[0], o, findings, threshold))
		if failed {
			return exitFailed
		}
		return exitOK
	}

	for _, f := range findings {
		fmt.Printf("%s: %s: %s\n", f.Severity, f.Rule, f.Message)
	}
	if !any {
		fmt.Println("no findings")
		return exitOK
	}
	fmt.Printf("\n%d finding(s)\n", len(findings))
	if failed {
		return exitFailed
	}
	return exitOK
}

// --- diff -------------------------------------------------------------------

func cmdDiff(args []string) int {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	summary := fs.Bool("summary", false, "print only a one-line summary")
	exitCode := fs.Bool("exit-code", false, "exit 1 when the ontologies differ")
	jsonOut := fs.Bool("json", false, "write one JSON object to stdout instead of text")
	files, err := parseArgs(fs, args)
	if err != nil {
		return exitProblem
	}
	if len(files) != 2 {
		fmt.Fprintln(os.Stderr, "gowl diff: need two files")
		return exitProblem
	}

	from, err := load(files[0])
	if err != nil {
		return failWith(*jsonOut, err)
	}
	to, err := load(files[1])
	if err != nil {
		return failWith(*jsonOut, err)
	}

	d := owl.DiffOntologies(from, to)
	if *jsonOut {
		writeJSON(diffJSON(files[0], files[1], d, *summary))
		if *exitCode && !d.Empty() {
			return exitFailed
		}
		return exitOK
	}

	if *summary {
		fmt.Println(d.Summary())
	} else if d.Empty() {
		fmt.Println("no changes")
	} else {
		fmt.Print(d.String())
		fmt.Printf("\n%s\n", d.Summary())
	}

	if *exitCode && !d.Empty() {
		return exitFailed
	}
	return exitOK
}

// --- profile ----------------------------------------------------------------

func cmdProfile(args []string) int {
	fs := flag.NewFlagSet("profile", flag.ContinueOnError)
	verbose := fs.Bool("v", false, "list every violation, not just a count")
	jsonOut := fs.Bool("json", false, "write one JSON object to stdout instead of text")
	files, err := parseArgs(fs, args)
	if err != nil {
		return exitProblem
	}
	if len(files) != 1 {
		fmt.Fprintln(os.Stderr, "gowl profile: need exactly one file")
		return exitProblem
	}

	o, err := load(files[0])
	if err != nil {
		return failWith(*jsonOut, err)
	}

	if *jsonOut {
		writeJSON(profileJSON(files[0], o, *verbose))
		return exitOK
	}

	for _, p := range owl.AllProfiles {
		violations := owl.CheckProfile(o, p)
		if len(violations) == 0 {
			fmt.Printf("%-3s yes\n", p)
			continue
		}
		fmt.Printf("%-3s no (%d violation(s))\n", p, len(violations))
		if *verbose {
			for _, v := range violations {
				fmt.Printf("      %s: %s\n", v.Reason, o.Render(v.Axiom))
			}
		}
	}
	if !*verbose {
		fmt.Println("\nrun with -v to see the violations")
	}
	// DL coverage is partial, so say so rather than let "yes" overpromise.
	fmt.Println("\nnote: " + dlCheckNote)
	return exitOK
}

// dlCheckNote is shared by the text and JSON output so the caveat cannot drift
// between them.
const dlCheckNote = "the DL check covers only the simple-property restriction"

// --- fmt --------------------------------------------------------------------

func cmdFmt(args []string) int {
	fs := flag.NewFlagSet("fmt", flag.ContinueOnError)
	write := fs.Bool("w", false, "rewrite the file in place instead of printing")
	canonical := fs.Bool("canonical", false, "also canonicalize and sort axioms")
	files, err := parseArgs(fs, args)
	if err != nil {
		return exitProblem
	}
	if len(files) != 1 {
		fmt.Fprintln(os.Stderr, "gowl fmt: need exactly one file")
		return exitProblem
	}
	path := files[0]

	o, err := load(path)
	if err != nil {
		return fail(err)
	}

	if *canonical {
		o.Rewrite(owl.CanonicalAxiom).Sort()
	}

	out := o.Functional()
	if !*write {
		fmt.Print(out)
		return exitOK
	}
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		return fail(err)
	}
	return exitOK
}

// --- stats ------------------------------------------------------------------

func cmdStats(args []string) int {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "write one JSON object to stdout instead of text")
	files, err := parseArgs(fs, args)
	if err != nil {
		return exitProblem
	}
	if len(files) != 1 {
		fmt.Fprintln(os.Stderr, "gowl stats: need exactly one file")
		return exitProblem
	}

	o, err := load(files[0])
	if err != nil {
		return failWith(*jsonOut, err)
	}

	counts, kinds := axiomKindCounts(o)
	if *jsonOut {
		writeJSON(statsJSON(files[0], o, kinds, counts))
		return exitOK
	}

	if o.IRI != "" {
		fmt.Printf("ontology            %s\n", o.IRI)
	}
	if o.VersionIRI != "" {
		fmt.Printf("version             %s\n", o.VersionIRI)
	}
	fmt.Printf("imports             %d\n", len(o.Imports))
	fmt.Printf("axioms              %d\n", o.Len())
	fmt.Printf("classes             %d\n", len(o.Classes()))
	fmt.Printf("object properties   %d\n", len(o.ObjectProperties()))
	fmt.Printf("data properties     %d\n", len(o.DataProperties()))
	fmt.Printf("individuals         %d\n", len(o.Individuals()))
	fmt.Printf("datatypes           %d\n", len(o.Datatypes()))

	fmt.Println("\nby axiom type")
	for _, k := range kinds {
		fmt.Printf("  %-32s %d\n", k, counts[k])
	}
	return exitOK
}

// axiomKindCounts tallies axioms by functional-syntax keyword, returning the
// keywords ordered most frequent first and then alphabetically.
func axiomKindCounts(o *owl.Ontology) (map[string]int, []string) {
	counts := make(map[string]int)
	for ax := range o.All() {
		counts[axiomKind(ax)]++
	}
	kinds := make([]string, 0, len(counts))
	for k := range counts {
		kinds = append(kinds, k)
	}
	sort.Slice(kinds, func(i, j int) bool {
		if counts[kinds[i]] != counts[kinds[j]] {
			return counts[kinds[i]] > counts[kinds[j]]
		}
		return kinds[i] < kinds[j]
	})
	return counts, kinds
}

// axiomKind names an axiom by its functional-syntax keyword.
func axiomKind(ax owl.Axiom) string {
	s := owl.Functional(owl.Unwrap(ax))
	if i := strings.IndexByte(s, '('); i > 0 {
		return s[:i]
	}
	return s
}
