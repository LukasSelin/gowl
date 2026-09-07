package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"gowl/el"
	"gowl/owl"
)

func cmdClassify(args []string) int {
	fs := flag.NewFlagSet("classify", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "write one JSON object to stdout instead of text")
	axioms := fs.Bool("axioms", false, "write the inferred taxonomy as an ontology document")
	explain := fs.String("explain", "", "explain one subsumption, given as \"sub super\"")
	unsatOnly := fs.Bool("unsatisfiable", false, "report only the unsatisfiable classes")
	failIncoherent := fs.Bool("fail-on-incoherent", false, "exit 1 when a class is unsatisfiable")
	files, err := parseArgs(fs, args)
	if err != nil {
		return exitProblem
	}
	if len(files) != 1 {
		fmt.Fprintln(os.Stderr, "gowl classify: need exactly one file")
		return exitProblem
	}

	o, err := load(files[0])
	if err != nil {
		return failWith(*jsonOut, err)
	}
	c := el.Classify(o)

	if *explain != "" {
		return runExplain(o, c, *explain, *jsonOut)
	}

	// The inferred taxonomy as a document is a format of its own, so -json and
	// -axioms are alternatives rather than a combination.
	if *axioms && !*jsonOut {
		inferred := owl.New(o.IRI)
		inferred.Prefixes = o.Prefixes
		inferred.Add(c.InferredAxioms()...)
		fmt.Print(inferred.Functional())
		return classifyExit(c, *failIncoherent)
	}

	if *jsonOut {
		writeJSON(classifyJSON(files[0], o, c, *axioms, *unsatOnly))
		return classifyExit(c, *failIncoherent)
	}

	unsat := c.UnsatisfiableClasses()
	if *unsatOnly {
		if len(unsat) == 0 {
			fmt.Println("no unsatisfiable classes")
		}
		for _, cl := range unsat {
			fmt.Println(o.Render(cl))
		}
		return classifyExit(c, *failIncoherent)
	}

	fmt.Printf("classes             %d\n", len(c.Classes()))
	fmt.Printf("inferred axioms     %d\n", len(c.InferredAxioms()))
	fmt.Printf("consistent          %t\n", c.IsConsistent())
	fmt.Printf("coherent            %t\n", c.IsCoherent())
	if len(unsat) > 0 {
		fmt.Printf("\nunsatisfiable classes (%d)\n", len(unsat))
		for _, cl := range unsat {
			fmt.Printf("  %s\n", o.Render(cl))
		}
	}
	if u := c.Unsupported(); len(u) > 0 {
		fmt.Printf("\nnot classified (%d %s outside OWL 2 EL)\n", len(u), plural(len(u), "axiom"))
		for _, ax := range u {
			fmt.Printf("  %s\n", o.Render(ax))
		}
		fmt.Println("\nnote: " + incompleteNote)
	}
	return classifyExit(c, *failIncoherent)
}

func classifyExit(c *el.Classification, failIncoherent bool) int {
	if failIncoherent && !c.IsCoherent() {
		return exitFailed
	}
	return exitOK
}

// runExplain handles -explain "sub super".
func runExplain(o *owl.Ontology, c *el.Classification, arg string, jsonOut bool) int {
	names := strings.Fields(arg)
	if len(names) != 2 {
		return failWith(jsonOut, fmt.Errorf("-explain needs two class names, as \"sub super\""))
	}
	sub, err := o.Prefixes.Expand(names[0])
	if err != nil {
		return failWith(jsonOut, err)
	}
	super, err := o.Prefixes.Expand(names[1])
	if err != nil {
		return failWith(jsonOut, err)
	}

	subClass, superClass := owl.Class(sub), owl.Class(super)
	holds := c.IsSubsumedBy(subClass, superClass)
	why := c.Explain(subClass, superClass)

	if jsonOut {
		writeJSON(explainJSON(o, subClass, superClass, holds, why))
		return exitOK
	}
	if !holds {
		fmt.Printf("%s is not subsumed by %s\n", o.Render(subClass), o.Render(superClass))
		return exitOK
	}
	fmt.Printf("%s ⊑ %s follows from:\n", o.Render(subClass), o.Render(superClass))
	for _, ax := range why {
		fmt.Printf("  %s\n", o.Render(ax))
	}
	if len(why) == 0 {
		fmt.Println("  (asserted directly, or derived without a recorded axiom)")
	}
	return exitOK
}

// incompleteNote is shared by the text and JSON output so the caveat cannot
// drift between them.
const incompleteNote = "axioms outside OWL 2 EL were skipped, so a subsumption that is " +
	"not reported may still be entailed"

// plural picks the singular or plural form of a word for a count.
func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
