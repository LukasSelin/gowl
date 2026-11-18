package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/LukasSelin/gowl/internal/gocode"
)

// cmdGoCode describes a Go module's code as an ontology: exported types as
// classes, struct fields as properties, packages as individuals, and methods
// and functions as signatures. Like fmt it prints an ontology document rather
// than a report, so it takes no -json.
func cmdGoCode(args []string) int {
	fs := flag.NewFlagSet("gocode", flag.ContinueOnError)
	out := fs.String("o", "", "write the ontology to this file instead of stdout")
	prefix := fs.String("prefix", "", "option to abbreviate the module's own packages under this prefix name")
	dirs, err := parseArgs(fs, args)
	if err != nil {
		return exitProblem
	}
	if len(dirs) > 1 {
		fmt.Fprintln(os.Stderr, "gowl gocode: need at most one module directory")
		return exitProblem
	}
	root := "."
	if len(dirs) == 1 {
		root = dirs[0]
	}

	// An empty name leaves BuildPrefixed on its default, so the flag needs no sentinel.
	o, err := gocode.BuildPrefixed(root, *prefix)
	if err != nil {
		return fail(err)
	}
	doc := o.Functional()
	if *out == "" {
		fmt.Print(doc)
		return exitOK
	}
	if err := os.WriteFile(*out, []byte(doc), 0o644); err != nil {
		return fail(err)
	}
	return exitOK
}
